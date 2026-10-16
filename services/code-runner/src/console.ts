import {
  executionInput, executionResult, object, MAX_OUTPUT_BYTES, REVISION,
  type ExecutionInput, type ExecutionResult, type Language,
} from "./contract";

export const CONSOLE_REVISION = "edsger-console-v1";
export const MAX_INPUT_BYTES = 8192;
const MAX_FRAMES = 1024, MAX_WIRE_BYTES = 1_048_576, MAX_FRAME_BYTES = 524_288;
const encoder = new TextEncoder();
export interface ConsoleInput { sequence: number; text: string; eof: boolean }
export type ConsoleFrame =
  | {type: "ready"}
  | {type: "output"; stream: "stdout" | "stderr"; text: string}
  | {type: "result"; execution: ExecutionResult; elapsedMs?: number; revision?: string};

function exact(value: Record<string, unknown>, keys: string[]): void {
  if (Object.keys(value).length !== keys.length || Object.keys(value).some(key => !keys.includes(key))) throw Error("invalid_console_frame");
}

export function interactiveExecutionInput(raw: unknown): ExecutionInput {
  const input = executionInput(raw);
  if (input.mode !== "run" || input.language === "html" || input.language === "sql" || input.stdin.includes("\0")) throw Error("invalid_request");
  // A prefilled final answer is a complete input line; empty input leaves the
  // program's pipe open. Never truncate a character or an answer to fit the cap.
  if (input.stdin && !input.stdin.endsWith("\n")) input.stdin += "\n";
  if (encoder.encode(input.stdin).byteLength > MAX_INPUT_BYTES) throw Error("invalid_request");
  return input;
}

export function consoleInput(raw: unknown): ConsoleInput {
  const value = object(raw);
  exact(value, ["sequence", "text", "eof"]);
  if (!Number.isSafeInteger(value.sequence) || (value.sequence as number) < 1 || (value.sequence as number) > 128 ||
      typeof value.text !== "string" || value.text.includes("\0") || typeof value.eof !== "boolean" ||
      encoder.encode(value.text).byteLength > MAX_INPUT_BYTES || (value.eof && value.text !== "")) throw Error("invalid_request");
  return value as unknown as ConsoleInput;
}

/** Strict bounded protocol decoder. No guest-provided frames or fields pass through. */
export async function consumeConsole(
  body: ReadableStream<Uint8Array>, language: Language, origin: boolean,
  onFrame: (frame: ConsoleFrame) => void | Promise<void>, signal?: AbortSignal,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder("utf-8", {fatal: true, ignoreBOM: false});
  let pending = "", wireBytes = 0, frameBytes = 0, outputBytes = 0, frames = 0, ready = false, final = false;
  const transcript = {stdout: "", stderr: ""};
  const stop = () => { void reader.cancel().catch(() => {}); };
  signal?.addEventListener("abort", stop, {once: true});
  const line = async (text: string) => {
    if (!text || final || encoder.encode(text).byteLength > MAX_FRAME_BYTES) throw Error("invalid_console_frame");
    const value = object(JSON.parse(text));
    let frame: ConsoleFrame;
    if (value.type === "ready") {
      exact(value, ["type"]);
      if (ready) throw Error("invalid_console_frame");
      ready = true;
      frame = {type: "ready"};
    } else if (value.type === "output") {
      exact(value, ["type", "stream", "text"]);
      if (!["stdout", "stderr"].includes(value.stream as string) || typeof value.text !== "string") throw Error("invalid_console_frame");
      outputBytes += encoder.encode(value.text).byteLength;
      if (outputBytes > MAX_OUTPUT_BYTES) throw Error("invalid_console_frame");
      transcript[value.stream as "stdout" | "stderr"] += value.text;
      frame = {type: "output", stream: value.stream as "stdout" | "stderr", text: value.text};
    } else if (value.type === "result") {
      exact(value, origin ? ["type", "execution", "elapsedMs", "revision"] : ["type", "execution"]);
      const execution = executionResult(value.execution, language);
      if (execution.previewHTML !== undefined || execution.stdout !== transcript.stdout || execution.stderr !== transcript.stderr) throw Error("invalid_console_frame");
      if (origin && (value.revision !== REVISION || !Number.isSafeInteger(value.elapsedMs) || (value.elapsedMs as number) < 1 || (value.elapsedMs as number) > 45_000)) throw Error("invalid_console_frame");
      final = true;
      frame = origin ? {type: "result", execution, elapsedMs: value.elapsedMs as number, revision: REVISION} : {type: "result", execution};
    } else throw Error("invalid_console_frame");
    if (frame.type !== "result" && ++frames > MAX_FRAMES) throw Error("invalid_console_frame");
    await onFrame(frame);
  };
  try {
    if (signal?.aborted) throw Error("console_stopped");
    for (;;) {
      const {done, value} = await reader.read();
      if (signal?.aborted) throw Error("console_stopped");
      if (done) break;
      wireBytes += value.byteLength;
      if (wireBytes > MAX_WIRE_BYTES) throw Error("invalid_console_frame");
      // Count raw bytes between LF boundaries before decoding. This also bounds
      // a malformed/incomplete UTF-8 frame without repeatedly copying a buffer.
      for (const byte of value) {
        if (byte === 10) frameBytes = 0;
        else if (++frameBytes > MAX_FRAME_BYTES) throw Error("invalid_console_frame");
      }
      pending += decoder.decode(value, {stream: true});
      let newline;
      while ((newline = pending.indexOf("\n")) !== -1) {
        const next = pending.slice(0, newline);
        pending = pending.slice(newline + 1);
        await line(next);
      }
    }
    pending += decoder.decode();
    if (pending !== "" || !final) throw Error("invalid_console_frame");
  } finally {
    signal?.removeEventListener("abort", stop);
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}

export const consoleHeaders = {
  "Content-Type": "application/x-ndjson", "Cache-Control": "no-store",
  "X-Content-Type-Options": "nosniff", "X-Edsger-Console-Revision": CONSOLE_REVISION,
};
export function encodeConsoleFrame(frame: ConsoleFrame): Uint8Array { return encoder.encode(JSON.stringify(frame) + "\n"); }

/** Eager bounded pump: neither admission nor instance lifetime ends at headers. */
export function consoleResponse(options: {
  source: ReadableStream<Uint8Array>; language: Language; origin: boolean;
  onFrame?: (frame: ConsoleFrame) => ConsoleFrame;
  finish: () => Promise<void>; abort: () => void; signal?: AbortSignal;
  waitUntil?: (promise: Promise<void>) => void;
}): Response {
  let cancelled = false;
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const completion = (async () => {
        let failed = false;
        let result: ConsoleFrame | undefined;
        try {
          await consumeConsole(options.source, options.language, options.origin, frame => {
            const accepted = options.onFrame?.(frame) ?? frame;
            // A result is authoritative only after a clean EOF and lifecycle
            // cleanup. Never expose a successful-looking final before either.
            if (accepted.type === "result") result = accepted;
            else if (!cancelled) controller.enqueue(encodeConsoleFrame(accepted));
          }, options.signal);
        } catch { failed = true; options.abort(); }
        finally {
          try { await options.finish(); } catch { failed = true; }
          if (!cancelled) {
            if (failed) controller.error(Error("runner_unavailable"));
            else {
              if (result) controller.enqueue(encodeConsoleFrame(result));
              controller.close();
            }
          }
        }
      })();
      options.waitUntil?.(completion);
    },
    async cancel() { cancelled = true; options.abort(); await options.finish(); },
  });
  return new Response(body, {headers: consoleHeaders});
}

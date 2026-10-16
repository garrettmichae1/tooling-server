import { DurableObject } from "cloudflare:workers";
import { executionInput, executionResult, json, readJSON, LIFETIME_MS, REVISION, UUID, MAX_REQUEST_BYTES, LANGUAGES, type Language } from "./contract";
import { forwardJob } from "./runtime-errors";
import { CONSOLE_REVISION, consoleInput, consoleResponse, interactiveExecutionInput } from "./console";
export { CodeAdmission } from "./admission";
interface Env { CODE_RUNNER_ENABLED?: string; CODE_RUNNER_INTERACTIVE_ENABLED?: string; CODE_CORE_JOB: DurableObjectNamespace; CODE_SYSTEMS_JOB: DurableObjectNamespace; CODE_JVM_JOB: DurableObjectNamespace; CODE_DOTNET_JOB: DurableObjectNamespace; CODE_SWIFT_JOB: DurableObjectNamespace; CODE_ADMISSION: DurableObjectNamespace }
const PROFILE: Record<Language, keyof Pick<Env, "CODE_CORE_JOB" | "CODE_SYSTEMS_JOB" | "CODE_JVM_JOB" | "CODE_DOTNET_JOB" | "CODE_SWIFT_JOB">> = {
  c: "CODE_SYSTEMS_JOB", cpp: "CODE_SYSTEMS_JOB", go: "CODE_SYSTEMS_JOB", rust: "CODE_SYSTEMS_JOB",
  java: "CODE_JVM_JOB", kotlin: "CODE_JVM_JOB", csharp: "CODE_DOTNET_JOB", swift: "CODE_SWIFT_JOB",
  python: "CODE_CORE_JOB", javascript: "CODE_CORE_JOB", typescript: "CODE_CORE_JOB", lua: "CODE_CORE_JOB", ruby: "CODE_CORE_JOB", php: "CODE_CORE_JOB", r: "CODE_CORE_JOB", perl: "CODE_CORE_JOB", bash: "CODE_CORE_JOB", sql: "CODE_CORE_JOB", html: "CODE_CORE_JOB"
};
// This Worker has NO public domain/workers.dev/preview URL. The verified paid
// backend alone holds its service binding. Never bind model or Apple secrets here.
export async function serve(request: Request, env: Env, ctx?: ExecutionContext): Promise<Response> {
  const url = new URL(request.url), match = /^\/([^/]+)(?:\/(stream|input))?$/.exec(url.pathname);
  if (url.search || url.hash || !match || !UUID.test(match[1]) || !["POST", "DELETE"].includes(request.method) || (request.method === "DELETE" && match[2])) return json({error: "invalid_request"}, 400);
  const id = match[1], route = match[2];
  if (!env.CODE_ADMISSION || request.headers.get("X-Edsger-Execution-Revision") !== REVISION || (request.method === "POST" && env.CODE_RUNNER_ENABLED !== "true")) return json({error: "service_unavailable"}, 503);
  if (route && (env.CODE_RUNNER_INTERACTIVE_ENABLED !== "true" || request.headers.get("X-Edsger-Console-Revision") !== CONSOLE_REVISION)) return json({error: "service_unavailable"}, 503);
  if (request.method === "DELETE" || route === "input") {
    const language = request.headers.get("X-Edsger-Execution-Language") as Language;
    if (!LANGUAGES.includes(language) || !env[PROFILE[language]] || (route && ["html", "sql"].includes(language))) return json({error: "invalid_request"}, 400);
    const namespace = env[PROFILE[language]];
    const job = namespace.get(namespace.idFromName(id));
    if (request.method === "DELETE") return forwardJob(job, new Request("https://job/cancel", {method: "DELETE"}));
    let input;
    try { input = consoleInput(await readJSON(request, 65_536)); } catch { return json({error: "invalid_request"}, 400); }
    return forwardJob(job, new Request("https://job/input", {method: "POST", headers: {
      "Content-Type": "application/json", "X-Edsger-Execution-Revision": REVISION,
      "X-Edsger-Console-Revision": CONSOLE_REVISION, "X-Edsger-Execution-Language": language,
    }, body: JSON.stringify(input), signal: request.signal}));
  }
  let input;
  try { input = route === "stream" ? interactiveExecutionInput(await readJSON(request)) : executionInput(await readJSON(request)); }
  catch { return json({error: "invalid_request"}, 400); }
  const namespace = env[PROFILE[input.language]];
  if (!namespace) return json({error: "service_unavailable"}, 503);
  const job = namespace.get(namespace.idFromName(id));
  const admission = env.CODE_ADMISSION.get(env.CODE_ADMISSION.idFromName("global-v1"));
  const admitted = await admission.fetch(new Request(`https://admission/${id}`, {method: "POST"}));
  if (!admitted.ok) return admitted;
  let releasing: Promise<void> | undefined;
  const release = () => releasing ??= (async () => {
    const response = await admission.fetch(new Request(`https://admission/${id}`, {method: "DELETE"}));
    await response.body?.cancel();
  })();
  if (!route) {
    try { return await forwardJob(job, new Request("https://job/run", {method: "POST", body: JSON.stringify(input), signal: request.signal})); }
    finally { await release(); }
  }
  const stopped = new AbortController();
  let stopping: Promise<boolean> | undefined;
  const stop = () => stopped.abort();
  const timer = setTimeout(stop, LIFETIME_MS);
  request.signal.addEventListener("abort", stop, {once: true});
  const abort = () => {
    stop();
    // Stop is idempotent; the durable used/cancelled markers prevent replay even
    // if the stream is lost while its final frame is in transit.
    stopping ??= (async () => {
      let deadline: ReturnType<typeof setTimeout> | undefined;
      const confirmation = (async () => {
        try {
          const response = await forwardJob(job, new Request("https://job/cancel", {method: "DELETE", signal: AbortSignal.timeout(5000)}));
          const value = await readJSON(response, 2048) as Record<string, unknown>;
          return response.ok && value?.cancelled === true && Object.keys(value).length === 1;
        } catch { return false; }
      })();
      try { return await Promise.race([confirmation, new Promise<boolean>(resolve => { deadline = setTimeout(() => resolve(false), 5000); })]); }
      finally { if (deadline !== undefined) clearTimeout(deadline); }
    })();
  };
  const finish = async () => {
    clearTimeout(timer); request.signal.removeEventListener("abort", stop);
    if (stopped.signal.aborted) {
      abort();
      // A disconnected stream cannot prove the instance stopped. Keep its slot
      // until expiry unless DELETE confirms hard destruction completed.
      if (!await stopping) return;
    }
    await release();
  };
  try {
    if (request.signal.aborted) { abort(); await finish(); return json({error: "runner_unavailable"}, 502); }
    const response = await forwardJob(job, new Request("https://job/run-stream", {method: "POST", headers: {
      "Content-Type": "application/json", "X-Edsger-Execution-Revision": REVISION,
      "X-Edsger-Console-Revision": CONSOLE_REVISION,
    }, body: JSON.stringify(input), signal: stopped.signal}));
    if (!response.ok) {
      // Platform/transport failures can happen after launch. Prove destruction
      // before reusing its global slot; explicit pre-launch rejections are safe.
      if (![400, 429, 503].includes(response.status)) abort();
      await finish(); return response;
    }
    if (response.headers.get("Content-Type")?.split(";", 1)[0] !== "application/x-ndjson" || !response.body) {
      abort(); void response.body?.cancel().catch(() => {});
      await finish(); return json({error: "runner_unavailable"}, 502);
    }
    return consoleResponse({source: response.body, language: input.language, origin: true, finish, abort, signal: stopped.signal,
      waitUntil: promise => ctx?.waitUntil(promise)});
  } catch { abort(); await finish(); return json({error: "runner_unavailable"}, 502); }
}
export default {fetch: serve};

class CodeJob extends DurableObject<Env> {
  protected languages: readonly string[] = [];
  private live?: {
    language: Language; key: string; ready: boolean; stopped: AbortController;
    timer: ReturnType<typeof setTimeout>; start: number; pendingInputs: number;
    inputWaiters: Set<() => void>;
    finish: () => Promise<void>;
  };
  async fetch(request: Request): Promise<Response> {
    const path = new URL(request.url).pathname;
    if (request.method === "POST" && (path === "/run-stream" || path === "/input")) {
      if (this.env.CODE_RUNNER_INTERACTIVE_ENABLED !== "true" || request.headers.get("X-Edsger-Execution-Revision") !== REVISION || request.headers.get("X-Edsger-Console-Revision") !== CONSOLE_REVISION) return json({error: "service_unavailable"}, 503);
      return path === "/input" ? this.sendInput(request) : this.runStream(request);
    }
    if (request.method === "DELETE" && new URL(request.url).pathname === "/cancel") {
      await this.ctx.storage.put("cancelled", true);
      if (this.live) { this.live.stopped.abort(); await this.live.finish(); }
      else await this.ctx.container?.destroy();
      return json({cancelled: true});
    }
    if (request.method !== "POST" || new URL(request.url).pathname !== "/run") return json({error: "not_found"}, 404);
    let input;
    try { input = executionInput(await readJSON(request)); } catch { return json({error: "invalid_request"}, 400); }
    if (!this.languages.includes(input.language)) return json({error: "invalid_request"}, 400);
    const admitted = await this.ctx.storage.transaction(async tx => {
      if (await tx.get("used") || await tx.get("cancelled")) return false;
      await tx.put("used", true); await tx.setAlarm(Date.now() + LIFETIME_MS);
      return true;
    });
    if (!admitted || !this.ctx.container) return json({error: "duplicate_request"}, 409);
    const start = performance.now();
    let result: ReturnType<typeof executionResult> | undefined;
    try {
      this.ctx.container.start({enableInternet: false});
      await this.ctx.container.setInactivityTimeout(LIFETIME_MS);
      const port = this.ctx.container.getTcpPort(8080), deadline = performance.now() + 12_000;
      for (;;) {
        if (await this.ctx.storage.get("cancelled")) throw Error("cancelled");
        try { const health = await port.fetch(new Request("http://container/health", {signal: AbortSignal.timeout(1000)})); if (health.ok) break; await health.body?.cancel(); } catch {}
        if (performance.now() > deadline) throw Error("startup_timeout");
        await new Promise(resolve => setTimeout(resolve, 100));
      }
      const response = await port.fetch(new Request("http://container/execute", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(input), signal: AbortSignal.timeout(31_000)}));
      if (!response.ok) throw Error("runner_unavailable");
      result = executionResult(await readJSON(response, MAX_REQUEST_BYTES), input.language);
    } catch {
      result = {language: input.language, status: await this.ctx.storage.get("cancelled") ? "cancelled" : "timeout", stdout: "", stderr: "Execution stopped before a complete result was available.", exitCode: 124, truncated: false};
    } finally {
      // destroy is a hard termination. SIGTERM alone would let hostile code
      // delay shutdown. No instance or disk is reused for another execution.
      await this.ctx.container.destroy(); await this.ctx.storage.deleteAlarm();
    }
    return json({execution: result, elapsedMs: Math.min(LIFETIME_MS, Math.max(1, Math.ceil(performance.now() - start))), revision: REVISION});
  }
  private async runStream(request: Request): Promise<Response> {
    let input;
    try { input = interactiveExecutionInput(await readJSON(request)); } catch { return json({error: "invalid_request"}, 400); }
    if (!this.languages.includes(input.language)) return json({error: "invalid_request"}, 400);
    const admitted = await this.ctx.storage.transaction(async tx => {
      if (await tx.get("used") || await tx.get("cancelled")) return false;
      await tx.put("used", true); await tx.setAlarm(Date.now() + LIFETIME_MS);
      return true;
    });
    if (!admitted || !this.ctx.container) return json({error: "duplicate_request"}, 409);
    const container = this.ctx.container, stopped = new AbortController(), start = performance.now();
    let finishing: Promise<void> | undefined;
    const stop = () => stopped.abort();
    const timer = setTimeout(stop, LIFETIME_MS);
    const live = this.live = {
      language: input.language, key: crypto.randomUUID(), ready: false as boolean, stopped, timer, start, pendingInputs: 0,
      inputWaiters: new Set<() => void>(),
      finish: () => finishing ??= (async () => {
        live.ready = false;
        // A short-lived program can exit between accepting a line and returning
        // its ACK. Let already-pending ACKs finish before normal destruction.
        // Stop/alarm abort this grace immediately; it never extends the deadline.
        if (live.pendingInputs && !stopped.signal.aborted) await new Promise<void>(resolve => {
          const settled = () => {
            clearTimeout(grace); live.inputWaiters.delete(settled);
            stopped.signal.removeEventListener("abort", settled); resolve();
          };
          const grace = setTimeout(settled, Math.max(0, Math.min(250, LIFETIME_MS - (performance.now() - start))));
          live.inputWaiters.add(settled); stopped.signal.addEventListener("abort", settled, {once: true});
          if (stopped.signal.aborted) settled();
        });
        clearTimeout(timer); request.signal.removeEventListener("abort", stop);
        if (this.live === live) this.live = undefined;
        stopped.abort();
        // This is the only place the live session's instance is destroyed.
        await container.destroy();
        // If hard destruction fails, preserve the alarm so the platform can
        // retry termination. A failed cleanup must never erase its safety net.
        await this.ctx.storage.deleteAlarm();
      })(),
    };
    request.signal.addEventListener("abort", stop, {once: true});
    try {
      if (request.signal.aborted || await this.ctx.storage.get("cancelled")) throw Error("cancelled");
      container.start({enableInternet: false, env: {CODE_RUNNER_INTERACTIVE_ENABLED: "true"}});
      await container.setInactivityTimeout(LIFETIME_MS);
      const port = container.getTcpPort(8080), deadline = performance.now() + 12_000;
      for (;;) {
        if (stopped.signal.aborted || await this.ctx.storage.get("cancelled")) throw Error("cancelled");
        try {
          const health = await port.fetch(new Request("http://container/health", {signal: AbortSignal.timeout(1000)}));
          const healthy = health.ok; await health.body?.cancel(); if (healthy) break;
        } catch {}
        if (performance.now() > deadline) throw Error("startup_timeout");
        await new Promise(resolve => setTimeout(resolve, 100));
      }
      const response = await port.fetch(new Request("http://container/execute-stream", {
        method: "POST", headers: {"Content-Type": "application/json", "X-Edsger-Session-Key": live.key},
        body: JSON.stringify(input), signal: stopped.signal,
      }));
      if (!response.ok || response.headers.get("Content-Type")?.split(";", 1)[0] !== "application/x-ndjson" || !response.body) {
        await response.body?.cancel(); throw Error("runner_unavailable");
      }
      return consoleResponse({
        source: response.body, language: input.language, origin: false, signal: stopped.signal,
        abort: stop, finish: live.finish, waitUntil: promise => this.ctx.waitUntil(promise),
        onFrame: frame => {
          if (frame.type === "ready") live.ready = true;
          if (frame.type === "result") {
            live.ready = false;
            return {...frame, elapsedMs: Math.min(LIFETIME_MS, Math.max(1, Math.ceil(performance.now() - start))), revision: REVISION};
          }
          return frame;
        },
      });
    } catch { await live.finish(); return json({error: "runner_unavailable"}, 502); }
  }

  private async sendInput(request: Request): Promise<Response> {
    let input;
    try { input = consoleInput(await readJSON(request, 65_536)); } catch { return json({error: "invalid_request"}, 400); }
    const live = this.live;
    if (!live || !live.ready || live.stopped.signal.aborted) return json({error: "input_not_ready"}, 409);
    if (request.headers.get("X-Edsger-Execution-Language") !== live.language) return json({error: "invalid_request"}, 400);
    // Bound concurrent delivery work too. The runtime serializes writes and is
    // authoritative for ordering, byte totals, EOF and idempotent ACKs.
    if (live.pendingInputs >= 8) return json({error: "capacity_limited"}, 429);
    live.pendingInputs++;
    try {
      const response = await this.ctx.container!.getTcpPort(8080).fetch(new Request("http://container/input", {
        method: "POST", headers: {"Content-Type": "application/json", "X-Edsger-Session-Key": live.key},
        body: JSON.stringify(input), signal: live.stopped.signal,
      }));
      const result = await readJSON(response, 2048);
      const value = result as Record<string, unknown>;
      if (response.ok) {
        if (!value || Object.keys(value).length !== 2 || value.accepted !== true || value.sequence !== input.sequence) throw Error("invalid_response");
        return json({accepted: true, sequence: input.sequence});
      }
      const errors: Record<string, {status: number; code: string}> = {
        invalid_request: {status: 400, code: "invalid_request"}, not_ready: {status: 409, code: "input_not_ready"},
        out_of_order: {status: 409, code: "input_out_of_order"}, changed_duplicate: {status: 409, code: "input_out_of_order"},
        input_closed: {status: 409, code: "input_closed"}, limit: {status: 413, code: "input_limit"},
      };
      if (value && Object.keys(value).length === 1 && typeof value.error === "string" && errors[value.error]?.status === response.status) return json({error: errors[value.error].code}, response.status);
      throw Error("invalid_response");
    } catch { return json({error: "runner_unavailable"}, 502); }
    finally {
      if (--live.pendingInputs === 0) for (const settled of [...live.inputWaiters]) settled();
    }
  }

  async alarm(): Promise<void> {
    await this.ctx.storage.put("cancelled", true);
    if (this.live) { this.live.stopped.abort(); await this.live.finish(); }
    else await this.ctx.container?.destroy();
  }
}
export class CodeCoreJob extends CodeJob { protected languages = LANGUAGES.filter(l => PROFILE[l] === "CODE_CORE_JOB"); }
export class CodeSystemsJob extends CodeJob { protected languages = LANGUAGES.filter(l => PROFILE[l] === "CODE_SYSTEMS_JOB"); }
export class CodeJVMJob extends CodeJob { protected languages = ["java", "kotlin"]; }
export class CodeDotnetJob extends CodeJob { protected languages = ["csharp"]; }
export class CodeSwiftJob extends CodeJob { protected languages = ["swift"]; }

// The first-tool contract matches the app gateway's independently tested
// study-tool.ts. This host imports no payment, receipt, or model code.
export const STUDY_REQUEST_BYTES = 32_768;
export const STUDY_RESPONSE_BYTES = 1_100_000;
const encoder = new TextEncoder();
interface Question { prompt: string; choices: string[]; correct_index: number; explanation: string }
export interface StudyCall { tool: "make_study_app"; arguments: { title: string; questions: Question[] } }

function object(raw: unknown, keys: string[]): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw Error("invalid_request");
  const value = raw as Record<string, unknown>;
  if (Object.keys(value).length !== keys.length || keys.some(key => !Object.hasOwn(value, key))) throw Error("invalid_request");
  return value;
}
function string(raw: unknown, limit: number, nonblank = true): string {
  if (typeof raw !== "string" || raw.includes("\0") || encoder.encode(raw).length > limit || (nonblank && !raw.trim())) throw Error("invalid_request");
  return raw;
}

// Study-only JSON reader: preserve the live completions parser, but reject
// ambiguous duplicate keys and strings that cannot round-trip through Swift/Go.
export async function strictStudyJSON(request: Request | Response, limit: number): Promise<unknown> {
  const reader = request.body?.getReader();
  if (!reader) throw Error("invalid_request");
  const chunks: Uint8Array[] = [];
  let count = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      count += value.byteLength;
      if (count > limit) throw Error("payload_too_large");
      chunks.push(value);
    }
  } catch (error) { await reader.cancel(); throw error; }
  const bytes = new Uint8Array(count);
  let offset = 0;
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
  const source = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
  let position = 0;
  const invalid = () => { throw Error("invalid_request"); };
  const whitespace = () => { while (position < source.length && /[ \t\n\r]/.test(source[position])) position++; };
  const quoted = (): string => {
    if (source[position] !== '"') return invalid();
    const start = position++;
    for (;;) {
      if (position >= source.length) return invalid();
      const char = source[position++];
      if (char === "\\") { position++; continue; }
      if (char !== '"') continue;
      const value = JSON.parse(source.slice(start, position)) as string;
      for (let i = 0; i < value.length; i++) {
        const code = value.charCodeAt(i);
        if (code === 0 || (code >= 0xdc00 && code <= 0xdfff)) return invalid();
        if (code >= 0xd800 && code <= 0xdbff) {
          const next = value.charCodeAt(++i);
          if (!(next >= 0xdc00 && next <= 0xdfff)) return invalid();
        }
      }
      return value;
    }
  };
  const value = (depth: number): void => {
    if (depth > 32) return invalid();
    whitespace();
    const char = source[position];
    if (char === '"') { quoted(); return; }
    if (char === "{" || char === "[") {
      position++; whitespace();
      const closing = char === "{" ? "}" : "]", keys = new Set<string>();
      if (source[position] === closing) { position++; return; }
      for (;;) {
        if (char === "{") {
          whitespace(); const key = quoted();
          if (keys.has(key)) return invalid(); keys.add(key);
          whitespace(); if (source[position++] !== ":") return invalid();
        }
        value(depth + 1); whitespace();
        if (source[position] === closing) { position++; return; }
        if (source[position++] !== ",") return invalid();
      }
    }
    const start = position;
    while (position < source.length && !/[ \t\n\r,\]}]/.test(source[position])) position++;
    if (start === position) return invalid();
  };
  value(0); whitespace();
  if (position !== source.length) return invalid();
  return JSON.parse(source);
}
// One bounded contract. Property order is canonical for the idempotency digest.
export function studyCall(raw: unknown): StudyCall {
  const root = object(raw, ["tool", "arguments"]);
  if (root.tool !== "make_study_app") throw Error("invalid_request");
  const args = object(root.arguments, ["title", "questions"]);
  const title = string(args.title, 120);
  if (!Array.isArray(args.questions) || args.questions.length < 1 || args.questions.length > 5) throw Error("invalid_request");
  const questions = args.questions.map(raw => {
    const q = object(raw, ["prompt", "choices", "correct_index", "explanation"]);
    const prompt = string(q.prompt, 1000), explanation = string(q.explanation, 2000, false);
    if (!Array.isArray(q.choices) || q.choices.length < 2 || q.choices.length > 6) throw Error("invalid_request");
    const choices = q.choices.map(choice => string(choice, 500));
    const distinct = choices.map(choice => choice.trim().replace(/\s+/gu, " ").toLowerCase());
    if (new Set(distinct).size !== choices.length || typeof q.correct_index !== "number" || !Number.isInteger(q.correct_index) || q.correct_index < 0 || q.correct_index >= choices.length) throw Error("invalid_request");
    return { prompt, choices, correct_index: q.correct_index, explanation };
  });
  const call: StudyCall = { tool: "make_study_app", arguments: { title, questions } };
  if (encoder.encode(JSON.stringify(call)).length > STUDY_REQUEST_BYTES) throw Error("payload_too_large");
  return call;
}


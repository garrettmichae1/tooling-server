import { strictStudyJSON } from "./contract.js";
export { strictStudyJSON };
export const WORKSHEET_REQUEST_BYTES = 262_144;
export const WORKSHEET_RESPONSE_BYTES = 1_100_000;
const encoder = new TextEncoder();

function object(raw: unknown, keys: string[]): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw) || Object.keys(raw).sort().join(",") !== keys.sort().join(",")) throw Error("invalid_request");
  return raw as Record<string, unknown>;
}
function text(raw: unknown, limit: number): string {
  if (typeof raw !== "string" || !raw.trim() || raw.includes("\0") || encoder.encode(raw).length > limit) throw Error("invalid_request");
  return raw;
}
export function worksheetCall(raw: unknown) {
  const root = object(raw, ["tool", "arguments"]);
  if (root.tool !== "make_worksheet") throw Error("invalid_request");
  const args = object(root.arguments, ["title", "instructions", "exercises"]);
  const title = text(args.title, 120), instructions = text(args.instructions, 500);
  if (!Array.isArray(args.exercises) || args.exercises.length < 1 || args.exercises.length > 30) throw Error("invalid_request");
  const exercises = args.exercises.map(value => {
    const e = object(value, ["prompt", "answer", "explanation"]);
    return { prompt: text(e.prompt, 1000), answer: text(e.answer, 1000), explanation: text(e.explanation, 2000) };
  });
  const call = { tool: "make_worksheet", arguments: { title, instructions, exercises } };
  if (encoder.encode(JSON.stringify(call)).length > WORKSHEET_REQUEST_BYTES) throw Error("payload_too_large");
  return call;
}
export function worksheetResult(raw: unknown): unknown {
  const root = object(raw, ["ok", "tool", "result"]);
  if (root.ok !== true || root.tool !== "make_worksheet") throw Error("invalid_response");
  const r = object(root.result, ["filename", "media_type", "html", "answer_key_html", "offline", "network_requests", "preview_requires_scripts", "content_verified"]);
  if (r.filename !== "worksheet.html" || r.media_type !== "text/html; charset=utf-8" || r.offline !== true || r.network_requests !== false || r.preview_requires_scripts !== false || r.content_verified !== false) throw Error("invalid_response");
  for (const value of [r.html, r.answer_key_html]) {
    if (typeof value !== "string" || !value.startsWith("<!doctype html>") || encoder.encode(value).length > 524_288) throw Error("invalid_response");
  }
  return raw;
}

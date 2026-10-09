// Protocol v1 is mirrored in friendly-potato. Never accept commands, URLs,
// environment variables, dependency installers, or runtime limits from clients.
export const REVISION = "edsger-execution-v1";
export const LANGUAGES = ["c", "cpp", "python", "javascript", "typescript", "lua", "java", "csharp", "go", "rust", "ruby", "php", "kotlin", "swift", "r", "perl", "bash", "sql", "html"] as const;
export type Language = typeof LANGUAGES[number];
export const MAX_REQUEST_BYTES = 524_288, MAX_SOURCE_BYTES = 262_144, MAX_OUTPUT_BYTES = 65_536, MAX_PREVIEW_BYTES = 262_144;
export const UUID = /^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/i;
export const LIFETIME_MS = 45_000;
export interface ExecutionInput { language: Language; entrypoint: string; files: { path: string; content: string }[]; stdin: string; mode: "run" | "preview" }
export interface ExecutionResult { language: Language; status: "completed" | "failed" | "timeout" | "cancelled"; stdout: string; stderr: string; exitCode: number; truncated: boolean; previewHTML?: string }
export function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw Error("invalid_request");
  return value as Record<string, unknown>;
}
function exact(value: Record<string, unknown>, keys: string[]) { if (Object.keys(value).length !== keys.length || Object.keys(value).some(k => !keys.includes(k))) throw Error("invalid_request"); }
export function safePath(path: unknown): path is string {
  return typeof path === "string" && path.length <= 192 && path.split("/").every(p => /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$/.test(p) && !p.includes("..") && p !== "node_modules") && !path.includes("\\");
}
const extensions: Record<Language, string[]> = {
  c: ["c"], cpp: ["cpp", "cc", "cxx"], python: ["py"], javascript: ["js", "mjs", "cjs", "jsx"], typescript: ["ts", "tsx"], lua: ["lua"], java: ["java"], csharp: ["cs"], go: ["go"], rust: ["rs"], ruby: ["rb"], php: ["php"], kotlin: ["kt"], swift: ["swift"], r: ["r", "R"], perl: ["pl"], bash: ["sh"], sql: ["sql"], html: ["html", "htm"]
};
export function executionInput(raw: unknown): ExecutionInput {
  const v = object(raw); exact(v, ["language", "entrypoint", "files", "stdin", "mode"]);
  if (!LANGUAGES.includes(v.language as Language) || !safePath(v.entrypoint) || !Array.isArray(v.files) || v.files.length < 1 || v.files.length > 64 || typeof v.stdin !== "string" || new TextEncoder().encode(v.stdin).length > 8192 || !["run", "preview"].includes(v.mode as string)) throw Error("invalid_request");
  const language = v.language as Language;
  if (!extensions[language].includes(v.entrypoint.split(".").pop()!) || (v.mode === "preview" && !["javascript", "typescript", "html"].includes(language)) || (language === "html" && v.mode !== "preview")) throw Error("invalid_request");
  let size = 0; const paths = new Set<string>();
  const files = v.files.map(raw => { const file = object(raw); exact(file, ["path", "content"]);
    if (!safePath(file.path) || typeof file.content !== "string" || file.content.includes("\0") || paths.has(file.path.toLowerCase())) throw Error("invalid_request");
    paths.add(file.path.toLowerCase()); size += new TextEncoder().encode(file.content).length;
    return { path: file.path, content: file.content };
  });
  if (size > MAX_SOURCE_BYTES || !files.some(f => f.path === v.entrypoint) || [...paths].some(path => [...paths].some(other => other !== path && other.startsWith(path + "/")))) throw Error("invalid_request");
  return { language, entrypoint: v.entrypoint, files, stdin: v.stdin, mode: v.mode as ExecutionInput["mode"] };
}
export function executionResult(raw: unknown, language: Language): ExecutionResult {
  const v = object(raw), allowed = ["language", "status", "stdout", "stderr", "exitCode", "truncated", "previewHTML"];
  if (Object.keys(v).some(k => !allowed.includes(k)) || v.language !== language || !["completed", "failed", "timeout", "cancelled"].includes(v.status as string) || typeof v.stdout !== "string" || typeof v.stderr !== "string" || new TextEncoder().encode(v.stdout + v.stderr).length > MAX_OUTPUT_BYTES || !Number.isSafeInteger(v.exitCode) || Math.abs(v.exitCode as number) > 255 || typeof v.truncated !== "boolean" || (v.previewHTML !== undefined && (typeof v.previewHTML !== "string" || new TextEncoder().encode(v.previewHTML).length > MAX_PREVIEW_BYTES))) throw Error("invalid_response");
  return v as unknown as ExecutionResult;
}
export function json(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" } }); }
export async function readJSON(request: Request | Response, limit = MAX_REQUEST_BYTES): Promise<unknown> {
  const reader = request.body?.getReader(); if (!reader) throw Error("invalid_request");
  const chunks: Uint8Array[] = []; let size = 0;
  try { for (;;) { const {value, done} = await reader.read(); if (done) break; size += value.length; if (size > limit) throw Error("payload_too_large"); chunks.push(value); } }
  catch (error) { await reader.cancel(); throw error; }
  const data = new Uint8Array(size); let at = 0; for (const c of chunks) { data.set(c, at); at += c.length; }
  return JSON.parse(new TextDecoder("utf-8", { fatal: true, ignoreBOM: false }).decode(data));
}

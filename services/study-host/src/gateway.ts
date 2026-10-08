import { strictStudyJSON, studyCall, STUDY_REQUEST_BYTES, STUDY_RESPONSE_BYTES } from "./contract.js";
import { serveWorksheet, type WorksheetBindings } from "./worksheet-gateway.js";

// Structural binding type keeps admission testable without a container SDK or
// payment backend. Only one operator-selected object name is ever reachable.
export interface HostBindings extends WorksheetBindings {
  FIGURE_TOKEN?: string;
  STUDY_HOST_ENABLED?: string;
  STUDY_CONTAINER: { getByName(name: string): { fetch(request: Request): Promise<Response> } };
}
export const CONTAINER_NAME = "study-sandbox-v1";

function credential(value: unknown): value is string {
  return typeof value === "string" && /^[\x21-\x7e]{32,256}$/.test(value);
}
function json(value: unknown, status = 200, retryAfter?: string): Response {
  const headers = new Headers({
    "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store",
    "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer",
    "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
  });
  if (retryAfter) headers.set("Retry-After", retryAfter);
  return new Response(JSON.stringify(value), { status, headers });
}
function failure(code: string, status: number, retryAfter?: string): Response {
  return json({ ok: false, error: code, code }, status, retryAfter);
}

async function authorized(request: Request, expected: string): Promise<boolean> {
  const value = request.headers.get("Authorization") ?? "";
  if (!value.startsWith("Bearer ") || !credential(value.slice(7))) return false;
  const encoder = new TextEncoder();
  const [a, b] = await Promise.all([
    crypto.subtle.digest("SHA-256", encoder.encode(expected)),
    crypto.subtle.digest("SHA-256", encoder.encode(value.slice(7))),
  ]);
  const left = new Uint8Array(a), right = new Uint8Array(b);
  let different = 0;
  for (let i = 0; i < left.length; i++) different |= left[i] ^ right[i];
  return different === 0;
}

function result(raw: unknown): unknown {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw Error("invalid_response");
  const root = raw as Record<string, unknown>;
  if (Object.keys(root).sort().join(",") !== "ok,result,tool" || root.ok !== true || root.tool !== "make_study_app") throw Error("invalid_response");
  if (!root.result || typeof root.result !== "object" || Array.isArray(root.result)) throw Error("invalid_response");
  const r = root.result as Record<string, unknown>;
  if (Object.keys(r).sort().join(",") !== "content_verified,filename,html,media_type,network_requests,offline,preview_requires_scripts" ||
      r.filename !== "study-app.html" || r.media_type !== "text/html; charset=utf-8" ||
      r.offline !== true || r.network_requests !== false || r.preview_requires_scripts !== true || r.content_verified !== false ||
      typeof r.html !== "string" || !r.html.startsWith("<!doctype html>") || new TextEncoder().encode(r.html).length > 1_048_576) throw Error("invalid_response");
  return raw;
}

export async function serve(request: Request, env: HostBindings): Promise<Response> {
  const url = new URL(request.url);
  const tls = request.cf?.tlsVersion;
  if (url.protocol !== "https:" || (tls && !["TLSv1.2", "TLSv1.3"].includes(String(tls)))) return failure("https_required", 400);
  if (url.search || url.hash || url.username || url.password || url.port || request.headers.has("Upgrade")) return failure("invalid_request", 400);
  // Public liveness checks cover the edge only and never wake a billed container.
  // An authenticated fixed quiz verifies the Go process end to end.
  if (url.pathname === "/health" && request.method === "GET") {
    return json({ ok: true, service: "edsger-study-tool-edge", tool: "make_study_app", worksheet_enabled: env.WORKSHEET_HOST_ENABLED === "true" && credential(env.FIGURE_TOKEN) && Boolean(env.WORKSHEET_IP_LIMIT && env.WORKSHEET_ADMISSION && env.WORKSHEET_CONTAINER), enabled: env.STUDY_HOST_ENABLED === "true" && credential(env.FIGURE_TOKEN) });
  }
  if (url.pathname === "/v1/worksheets") return serveWorksheet(request, env);
  if (url.pathname !== "/v1/tools/call") return failure("not_found", 404);
  if (request.method !== "POST") return failure("method_not_allowed", 405);
  if (env.STUDY_HOST_ENABLED !== "true" || !credential(env.FIGURE_TOKEN)) return failure("service_unavailable", 503);
  // Check the server credential before body work or any object lookup/cold start.
  if (!await authorized(request, env.FIGURE_TOKEN)) return failure("unauthorized", 401);
  const contentType = request.headers.get("Content-Type") ?? "";
  if (!/^application\/json(?:\s*;\s*charset\s*=\s*(?:utf-8|"utf-8"))?$/i.test(contentType) || request.headers.has("Content-Encoding")) return failure("unsupported_media_type", 415);
  const declared = request.headers.get("Content-Length");
  if (declared !== null && (!/^\d+$/.test(declared) || Number(declared) > STUDY_REQUEST_BYTES)) return failure("payload_too_large", 413);
  let payload: string;
  try { payload = JSON.stringify(studyCall(await strictStudyJSON(request, STUDY_REQUEST_BYTES))); }
  catch (error) {
    const tooLarge = error instanceof Error && error.message === "payload_too_large";
    return failure(tooLarge ? "payload_too_large" : "invalid_request", tooLarge ? 413 : 400);
  }
  if (request.signal.aborted) return failure("request_cancelled", 409);
  const controller = new AbortController();
  let rejectCancelled!: (error: Error) => void;
  const cancelled = new Promise<never>((_, reject) => { rejectCancelled = reject; });
  const cancel = () => { controller.abort(); rejectCancelled(Error("request_cancelled")); };
  request.signal.addEventListener("abort", cancel, { once: true });
  let timer: ReturnType<typeof setTimeout>;
  // Bound cold-start and response work inside the app gateway's 15-second budget.
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(() => { controller.abort(); reject(Error("origin_timeout")); }, 14_000);
  });
  try {
    const execute = async (): Promise<Response> => {
      const upstream = await env.STUDY_CONTAINER.getByName(CONTAINER_NAME).fetch(new Request("http://container/v1/tools/call", {
        method: "POST", headers: { Authorization: `Bearer ${env.FIGURE_TOKEN}`, "Content-Type": "application/json" },
        body: payload, signal: controller.signal, redirect: "manual",
      }));
      if (upstream.status !== 200 || upstream.headers.get("Content-Type")?.split(";")[0].trim().toLowerCase() !== "application/json") {
        await upstream.body?.cancel();
        return failure(upstream.status === 429 ? "rate_limited" : "service_unavailable", upstream.status === 429 ? 429 : 503, upstream.status === 429 ? "60" : "5");
      }
      const checked = result(await strictStudyJSON(upstream, STUDY_RESPONSE_BYTES));
      if (controller.signal.aborted) throw Error("request_cancelled");
      return json(checked);
    };
    return await Promise.race([execute(), deadline, cancelled]);
  } catch {
    return failure(request.signal.aborted ? "request_cancelled" : "service_unavailable", request.signal.aborted ? 409 : 503, "5");
  } finally {
    clearTimeout(timer!);
    request.signal.removeEventListener("abort", cancel);
  }
}

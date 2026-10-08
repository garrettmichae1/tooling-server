import { strictStudyJSON, worksheetCall, worksheetResult, WORKSHEET_REQUEST_BYTES, WORKSHEET_RESPONSE_BYTES } from "./worksheet-contract.js";

export const WORKSHEET_CONTAINER_NAME = "worksheet-sandbox-v1";
export const WORKSHEET_ADMISSION_NAME = "worksheet-admission-v1";
interface Service { getByName(name: string): { fetch(request: Request): Promise<Response> } }
export interface WorksheetBindings {
  FIGURE_TOKEN?: string;
  WORKSHEET_HOST_ENABLED?: string;
  WORKSHEET_IP_LIMIT?: { limit(options: { key: string }): Promise<{ success: boolean }> };
  WORKSHEET_ADMISSION?: Service;
  WORKSHEET_CONTAINER?: Service;
}
function json(value: unknown, status = 200, retry?: string): Response {
  const headers = new Headers({ "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'" });
  if (retry) headers.set("Retry-After", retry);
  return new Response(JSON.stringify(value), { status, headers });
}
function failure(code: string, status: number, retry?: string): Response { return json({ ok: false, error: code, code }, status, retry); }

// This free route renders bounded data only. It cannot call models or reach
// payment workers. Edge/IP and persisted global budgets precede container work.
export async function serveWorksheet(request: Request, env: WorksheetBindings): Promise<Response> {
  if (request.method !== "POST") return failure("method_not_allowed", 405);
  if (env.WORKSHEET_HOST_ENABLED !== "true" || !/^[\x21-\x7e]{32,256}$/.test(env.FIGURE_TOKEN ?? "") || !env.WORKSHEET_IP_LIMIT || !env.WORKSHEET_ADMISSION || !env.WORKSHEET_CONTAINER) return failure("service_unavailable", 503);
  const ip = request.headers.get("CF-Connecting-IP");
  if (!ip || ip.length > 64) return failure("service_unavailable", 503);
  try {
    if (!(await env.WORKSHEET_IP_LIMIT.limit({ key: ip })).success) return failure("rate_limited", 429, "60");
  } catch { return failure("service_unavailable", 503); }
  if (request.headers.get("X-Edsger-Worksheet-Consent") !== "v1") return failure("consent_required", 403);
  if (!/^application\/json(?:\s*;\s*charset\s*=\s*(?:utf-8|"utf-8"))?$/i.test(request.headers.get("Content-Type") ?? "") || request.headers.has("Content-Encoding")) return failure("unsupported_media_type", 415);
  const length = request.headers.get("Content-Length");
  if (length !== null && (!/^\d+$/.test(length) || Number(length) > WORKSHEET_REQUEST_BYTES)) return failure("payload_too_large", 413);
  let payload: string;
  try { payload = JSON.stringify(worksheetCall(await strictStudyJSON(request, WORKSHEET_REQUEST_BYTES))); }
  catch (error) { const large = error instanceof Error && error.message === "payload_too_large"; return failure(large ? "payload_too_large" : "invalid_request", large ? 413 : 400); }
  if (request.signal.aborted) return failure("request_cancelled", 409);
  const controller = new AbortController();
  let rejectCancelled!: (error: Error) => void;
  const cancelled = new Promise<never>((_, reject) => { rejectCancelled = reject; });
  const cancel = () => { controller.abort(); rejectCancelled(Error("request_cancelled")); };
  request.signal.addEventListener("abort", cancel, { once: true });
  let timer: ReturnType<typeof setTimeout>;
  const deadline = new Promise<never>((_, reject) => { timer = setTimeout(() => { controller.abort(); reject(Error("origin_timeout")); }, 14_000); });
  try {
    const execute = async () => {
      const admitted = await env.WORKSHEET_ADMISSION!.getByName(WORKSHEET_ADMISSION_NAME).fetch(new Request("http://admission/admit", { method: "POST", signal: controller.signal }));
      const status = admitted.status; await admitted.body?.cancel();
      if (controller.signal.aborted) throw Error("request_cancelled");
      if (status === 429) return failure("rate_limited", 429, "60");
      if (status !== 204) return failure("service_unavailable", 503);
      const response = await env.WORKSHEET_CONTAINER!.getByName(WORKSHEET_CONTAINER_NAME).fetch(new Request("http://container/v1/tools/call", {
        method: "POST", headers: { Authorization: `Bearer ${env.FIGURE_TOKEN}`, "Content-Type": "application/json" }, body: payload, redirect: "manual", signal: controller.signal,
      }));
      if (response.status !== 200 || response.headers.get("Content-Type")?.split(";")[0].trim().toLowerCase() !== "application/json") {
        const limited = response.status === 429; await response.body?.cancel();
        return failure(limited ? "rate_limited" : "service_unavailable", limited ? 429 : 503, limited ? "60" : "5");
      }
      const result = worksheetResult(await strictStudyJSON(response, WORKSHEET_RESPONSE_BYTES));
      if (controller.signal.aborted) throw Error("request_cancelled");
      return json(result);
    };
    return await Promise.race([execute(), deadline, cancelled]);
  } catch { return failure(request.signal.aborted ? "request_cancelled" : "service_unavailable", request.signal.aborted ? 409 : 503, "5"); }
  finally { clearTimeout(timer!); request.signal.removeEventListener("abort", cancel); }
}

import { strictStudyJSON, wordSearchCall, wordSearchResult, WORD_SEARCH_REQUEST_BYTES, WORD_SEARCH_RESPONSE_BYTES } from "./word-search-contract.js";

export const WORD_SEARCH_CONTAINER_NAME = "word-search-v1";
export const WORD_SEARCH_ADMISSION_NAME = "word-search-admission-v1";
interface Service { getByName(name: string): { fetch(request: Request): Promise<Response> } }
export interface WordSearchBindings {
  FIGURE_TOKEN?: string;
  WORD_SEARCH_HOST_ENABLED?: string;
  WORD_SEARCH_IP_LIMIT?: { limit(options: { key: string }): Promise<{ success: boolean }> };
  WORD_SEARCH_ADMISSION?: Service;
  WORD_SEARCH_CONTAINER?: Service;
}
function json(value: unknown, status = 200, retry?: string): Response {
  const headers = new Headers({ "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'" });
  if (retry) headers.set("Retry-After", retry);
  return new Response(JSON.stringify(value), { status, headers });
}
function failure(code: string, status: number, retry?: string): Response { return json({ ok: false, error: code, code }, status, retry); }

// This free route renders bounded data only. It cannot call models or reach
// payment workers. Edge/IP and persisted global budgets precede container work.
export async function serveWordSearch(request: Request, env: WordSearchBindings): Promise<Response> {
  if (request.method !== "POST") return failure("method_not_allowed", 405);
  if (env.WORD_SEARCH_HOST_ENABLED !== "true" || !/^[\x21-\x7e]{32,256}$/.test(env.FIGURE_TOKEN ?? "") || !env.WORD_SEARCH_IP_LIMIT || !env.WORD_SEARCH_ADMISSION || !env.WORD_SEARCH_CONTAINER) return failure("service_unavailable", 503);
  const ip = request.headers.get("CF-Connecting-IP");
  if (!ip || ip.length > 64) return failure("service_unavailable", 503);
  try {
    if (!(await env.WORD_SEARCH_IP_LIMIT.limit({ key: ip })).success) return failure("rate_limited", 429, "60");
  } catch { return failure("service_unavailable", 503); }
  if (request.headers.get("X-Edsger-Word-Search-Consent") !== "v1") return failure("consent_required", 403);
  if (!/^application\/json(?:\s*;\s*charset\s*=\s*(?:utf-8|"utf-8"))?$/i.test(request.headers.get("Content-Type") ?? "") || request.headers.has("Content-Encoding")) return failure("unsupported_media_type", 415);
  const length = request.headers.get("Content-Length");
  if (length !== null && (!/^\d+$/.test(length) || Number(length) > WORD_SEARCH_REQUEST_BYTES)) return failure("payload_too_large", 413);
  let payload: string;
  let call: ReturnType<typeof wordSearchCall>;
  try { call = wordSearchCall(await strictStudyJSON(request, WORD_SEARCH_REQUEST_BYTES)); payload = JSON.stringify(call); }
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
      const admitted = await env.WORD_SEARCH_ADMISSION!.getByName(WORD_SEARCH_ADMISSION_NAME).fetch(new Request("http://admission/admit", { method: "POST", signal: controller.signal }));
      const status = admitted.status; await admitted.body?.cancel();
      if (controller.signal.aborted) throw Error("request_cancelled");
      if (status === 429) return failure("rate_limited", 429, "60");
      if (status !== 204) return failure("service_unavailable", 503);
      const response = await env.WORD_SEARCH_CONTAINER!.getByName(WORD_SEARCH_CONTAINER_NAME).fetch(new Request("http://container/v1/tools/call", {
        method: "POST", headers: { Authorization: `Bearer ${env.FIGURE_TOKEN}`, "Content-Type": "application/json" }, body: payload, redirect: "manual", signal: controller.signal,
      }));
      if (response.status === 422) {
        try {
          const failureBody = await strictStudyJSON(response, 256) as Record<string, unknown>;
          if (failureBody && Object.keys(failureBody).sort().join(",") === "code,error,ok" && failureBody.ok === false && failureBody.code === "generation_failed" && failureBody.error === "generation_failed") return failure("generation_failed", 422);
        } catch { /* Only a fixed trusted-origin code may cross this boundary. */ }
        return failure("service_unavailable", 503, "5");
      }
      if (response.status !== 200 || response.headers.get("Content-Type")?.split(";")[0].trim().toLowerCase() !== "application/json") {
        const limited = response.status === 429; await response.body?.cancel();
        return failure(limited ? "rate_limited" : "service_unavailable", limited ? 429 : 503, limited ? "60" : "5");
      }
      const result = await wordSearchResult(await strictStudyJSON(response, WORD_SEARCH_RESPONSE_BYTES), call);
      if (controller.signal.aborted) throw Error("request_cancelled");
      return json(result);
    };
    return await Promise.race([execute(), deadline, cancelled]);
  } catch { return failure(request.signal.aborted ? "request_cancelled" : "service_unavailable", request.signal.aborted ? 409 : 503, "5"); }
  finally { clearTimeout(timer!); request.signal.removeEventListener("abort", cancel); }
}

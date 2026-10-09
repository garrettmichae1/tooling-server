import { DurableObject } from "cloudflare:workers";
import { executionInput, executionResult, json, readJSON, LIFETIME_MS, REVISION, UUID, MAX_REQUEST_BYTES, LANGUAGES, type Language } from "./contract";
export { CodeAdmission } from "./admission";
interface Env { CODE_RUNNER_ENABLED?: string; CODE_CORE_JOB: DurableObjectNamespace; CODE_SYSTEMS_JOB: DurableObjectNamespace; CODE_JVM_JOB: DurableObjectNamespace; CODE_DOTNET_JOB: DurableObjectNamespace; CODE_SWIFT_JOB: DurableObjectNamespace; CODE_ADMISSION: DurableObjectNamespace }
const PROFILE: Record<Language, keyof Pick<Env, "CODE_CORE_JOB" | "CODE_SYSTEMS_JOB" | "CODE_JVM_JOB" | "CODE_DOTNET_JOB" | "CODE_SWIFT_JOB">> = {
  c: "CODE_SYSTEMS_JOB", cpp: "CODE_SYSTEMS_JOB", go: "CODE_SYSTEMS_JOB", rust: "CODE_SYSTEMS_JOB",
  java: "CODE_JVM_JOB", kotlin: "CODE_JVM_JOB", csharp: "CODE_DOTNET_JOB", swift: "CODE_SWIFT_JOB",
  python: "CODE_CORE_JOB", javascript: "CODE_CORE_JOB", typescript: "CODE_CORE_JOB", lua: "CODE_CORE_JOB", ruby: "CODE_CORE_JOB", php: "CODE_CORE_JOB", r: "CODE_CORE_JOB", perl: "CODE_CORE_JOB", bash: "CODE_CORE_JOB", sql: "CODE_CORE_JOB", html: "CODE_CORE_JOB"
};
// This Worker has NO public domain/workers.dev/preview URL. The verified paid
// backend alone holds its service binding. Never bind model or Apple secrets here.
export async function serve(request: Request, env: Env): Promise<Response> {
  const url = new URL(request.url), id = url.pathname.slice(1);
  if (url.search || url.hash || !UUID.test(id) || !["POST", "DELETE"].includes(request.method)) return json({error: "invalid_request"}, 400);
  if (!env.CODE_ADMISSION || request.headers.get("X-Edsger-Execution-Revision") !== REVISION || (request.method === "POST" && env.CODE_RUNNER_ENABLED !== "true")) return json({error: "service_unavailable"}, 503);
  if (request.method === "DELETE") {
    const language = request.headers.get("X-Edsger-Execution-Language") as Language;
    if (!LANGUAGES.includes(language) || !env[PROFILE[language]]) return json({error: "invalid_request"}, 400);
    const namespace = env[PROFILE[language]];
    return namespace.get(namespace.idFromName(id)).fetch(new Request("https://job/cancel", {method: "DELETE"}));
  }
  let input;
  try { input = executionInput(await readJSON(request)); }
  catch { return json({error: "invalid_request"}, 400); }
  const namespace = env[PROFILE[input.language]];
  if (!namespace) return json({error: "service_unavailable"}, 503);
  const job = namespace.get(namespace.idFromName(id));
  const admission = env.CODE_ADMISSION.get(env.CODE_ADMISSION.idFromName("global-v1"));
  const admitted = await admission.fetch(new Request(`https://admission/${id}`, {method: "POST"}));
  if (!admitted.ok) return admitted;
  try { return await job.fetch(new Request("https://job/run", {method: "POST", body: JSON.stringify(input), signal: request.signal})); }
  finally { await admission.fetch(new Request(`https://admission/${id}`, {method: "DELETE"})); }
}
export default {fetch: serve};

class CodeJob extends DurableObject<Env> {
  protected languages: readonly string[] = [];
  async fetch(request: Request): Promise<Response> {
    if (request.method === "DELETE" && new URL(request.url).pathname === "/cancel") {
      await this.ctx.storage.put("cancelled", true); await this.ctx.container?.destroy();
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
  async alarm(): Promise<void> { await this.ctx.storage.put("cancelled", true); await this.ctx.container?.destroy(); }
}
export class CodeCoreJob extends CodeJob { protected languages = LANGUAGES.filter(l => PROFILE[l] === "CODE_CORE_JOB"); }
export class CodeSystemsJob extends CodeJob { protected languages = LANGUAGES.filter(l => PROFILE[l] === "CODE_SYSTEMS_JOB"); }
export class CodeJVMJob extends CodeJob { protected languages = ["java", "kotlin"]; }
export class CodeDotnetJob extends CodeJob { protected languages = ["csharp"]; }
export class CodeSwiftJob extends CodeJob { protected languages = ["swift"]; }

import { DurableObject } from "cloudflare:workers";

// A separate object holds counts only, never identities, supplied content,
// receipts, model reservations, provider keys, or the app's paid ledger.
export class WordSearchAdmission extends DurableObject {
  async fetch(request: Request): Promise<Response> {
    if (request.method !== "POST" || new URL(request.url).pathname !== "/admit") return new Response(null, { status: 404 });
    let admitted: boolean;
    try {
      admitted = await this.ctx.storage.transaction(async transaction => {
        const now = Date.now(), day = Math.floor(now / 86_400_000), minute = Math.floor(now / 60_000);
        if (![day, minute].every(value => Number.isSafeInteger(value) && value >= 0)) throw Error("invalid_budget_state");
        const old = await transaction.get<{ day: number; used: number; minute: number; recent: number }>("budget");
        // Only absence creates a budget. Falsy or malformed persisted values must
        // never reset an anonymous public route's paid-container limits.
        if (old !== undefined && (
          !old || typeof old !== "object" || Array.isArray(old) ||
          Object.keys(old).sort().join(",") !== "day,minute,recent,used" ||
          ![old.day, old.minute, old.used, old.recent].every(value => Number.isSafeInteger(value) && value >= 0) ||
          old.used < 1 || old.used > 500 || old.recent < 1 || old.recent > 30 || old.recent > old.used ||
          Math.floor(old.minute / 1440) !== old.day || old.day > day || old.minute > minute
        )) throw Error("invalid_budget_state");
        const used = old?.day === day ? old.used : 0, recent = old?.minute === minute ? old.recent : 0;
        // Fixed deployment-reviewable cap; fail closed on storage errors.
        if (used >= 500 || recent >= 30) return false;
        await transaction.put("budget", { day, used: used + 1, minute, recent: recent + 1 });
        return true;
      });
    } catch { return new Response(null, { status: 500 }); }
    return new Response(null, { status: admitted ? 204 : 429 });
  }
}

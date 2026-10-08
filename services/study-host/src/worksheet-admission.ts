import { DurableObject } from "cloudflare:workers";

// A separate object holds counts only, never identities, supplied content,
// receipts, model reservations, provider keys, or the app's paid ledger.
export class WorksheetAdmission extends DurableObject {
  async fetch(request: Request): Promise<Response> {
    if (request.method !== "POST" || new URL(request.url).pathname !== "/admit") return new Response(null, { status: 404 });
    const admitted = await this.ctx.storage.transaction(async transaction => {
      const now = Date.now(), day = Math.floor(now / 86_400_000), minute = Math.floor(now / 60_000);
      const old = await transaction.get<{ day: number; used: number; minute: number; recent: number }>("budget");
      if (old && (![old.day,old.minute,old.used,old.recent].every(value => Number.isSafeInteger(value) && value >= 0) || old.used > 500 || old.recent > 30)) throw Error("invalid_budget_state");
      const used = old?.day === day ? old.used : 0, recent = old?.minute === minute ? old.recent : 0;
      // Fixed deployment-reviewable cap; fail closed on storage errors.
      if (used >= 500 || recent >= 30) return false;
      await transaction.put("budget", { day, used: used + 1, minute, recent: recent + 1 });
      return true;
    });
    return new Response(null, { status: admitted ? 204 : 429 });
  }
}

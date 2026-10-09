import { DurableObject } from "cloudflare:workers";
import { json, LIFETIME_MS, UUID } from "./contract";
// Persistent, account-wide admission BEFORE a job can start a microVM. Limits
// deliberately include abandoned jobs and survive restarts/deployments.
export class CodeAdmission extends DurableObject {
  async fetch(request: Request): Promise<Response> {
    const id = new URL(request.url).pathname.slice(1); if (!UUID.test(id)) return json({error: "invalid_request"}, 400);
    if (request.method === "DELETE") { await this.ctx.storage.delete("active:" + id); return json({released: true}); }
    if (request.method !== "POST") return json({error: "not_found"}, 404);
    return this.ctx.storage.transaction(async tx => {
      const now = Date.now(), minute = Math.floor(now / 60_000), day = Math.floor(now / 86_400_000), month = new Date(now).toISOString().slice(0, 7);
      const active = await tx.list<number>({prefix: "active:"});
      for (const [key, expiry] of active) if (expiry <= now) { await tx.delete(key); active.delete(key); }
      const current = await tx.get<{minute: number; minuteCount: number; day: number; dayCount: number; month: string; monthCount: number}>("counters") ?? {minute, minuteCount: 0, day, dayCount: 0, month, monthCount: 0};
      if (current.minute !== minute) { current.minute = minute; current.minuteCount = 0; }
      if (current.day !== day) { current.day = day; current.dayCount = 0; }
      if (current.month !== month) { current.month = month; current.monthCount = 0; }
      if (active.has("active:" + id)) return json({error: "duplicate_request"}, 409);
      if (active.size >= 8 || current.minuteCount >= 30 || current.dayCount >= 500 || current.monthCount >= 10_000) return json({error: "capacity_limited"}, 429);
      current.minuteCount++; current.dayCount++; current.monthCount++;
      await tx.put("counters", current); await tx.put("active:" + id, now + LIFETIME_MS + 15_000);
      return json({admitted: true});
    });
  }
}

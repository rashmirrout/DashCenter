// observability.ts — thin SPA wrappers around the public
// /v1/observability/counters REST endpoints (proxied through dashw's
// /api/v1/* mount so the browser never talks to dashd directly).
//
// PE-3c add-on: covers the DELETE endpoints used by the Clear
// affordances on both the topology-v2 CounterWidget and the
// /eni-stats page. GET surfaces (snapshot, stream, details) are
// still consumed via `useCounterStream` / `useQuery` callers; these
// helpers are deliberately one-shot mutations only.

const BASE = '/api/v1/observability/counters';

/**
 * Outcome of a single-DPU clear. `cleared=true` means dashd had a
 * cached entry and removed it; `cleared=false` is the 404 path
 * (already absent — idempotent, not an error).
 */
export interface ClearCounterTargetResult {
  cleared: boolean;
  dpuId: string;
}

/**
 * DELETE /api/v1/observability/counters/{dpu_id} — wipe one DPU's
 * cached counter entry on dashd. Idempotent: a second call returns
 * `cleared=false` cleanly. Throws on transport errors and on
 * non-{200, 404} status codes.
 */
export async function clearCounterTarget(dpuId: string): Promise<ClearCounterTargetResult> {
  if (!dpuId) throw new Error('clearCounterTarget: dpuId required');
  const url = `${BASE}/${encodeURIComponent(dpuId)}?reset_sim=true`;
  const res = await fetch(url, { method: 'DELETE' });
  if (res.status === 404) {
    return { cleared: false, dpuId };
  }
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`DELETE ${url} returned ${res.status}: ${body.slice(0, 200)}`);
  }
  const json = (await res.json()) as { cleared?: boolean; dpu_id?: string };
  return { cleared: !!json.cleared, dpuId: json.dpu_id ?? dpuId };
}

/**
 * Outcome of a fleet-wide clear.
 */
export interface ClearAllCountersResult {
  cleared: number;
}

/**
 * DELETE /api/v1/observability/counters — wipe every cached counter
 * entry on dashd. Returns the number of entries removed.
 */
export async function clearAllCounters(): Promise<ClearAllCountersResult> {
  const res = await fetch(`${BASE}?reset_sim=true`, { method: 'DELETE' });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`DELETE ${BASE} returned ${res.status}: ${body.slice(0, 200)}`);
  }
  const json = (await res.json()) as { cleared?: number };
  return { cleared: typeof json.cleared === 'number' ? json.cleared : 0 };
}

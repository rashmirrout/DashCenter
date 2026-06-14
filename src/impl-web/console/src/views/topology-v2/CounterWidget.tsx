// PE-3c / PD-G5: Counter sparkline widget for the /topology-v2 DPU
// inspector drawer.
//
// Subscribes to useCountersStore for the selected DPU's series and
// renders top-line numbers + 4 inline SVG sparklines (no external chart
// library). Each sparkline auto-scales to its own min/max with a 5-
// sample smoothing window so bursty drop counters don't visually jitter.
//
// Exported helpers `smooth` and `sparklinePath` are pure functions so
// they are unit-testable in isolation.

import {
  useCountersStore,
  selectSeries,
  type CounterSample,
} from '@/stores/counters-store';

/**
 * Apply a rolling-mean smoothing window. Returns [] for empty input;
 * window <= 1 acts as identity. Empty edge cases never produce NaN.
 */
export function smooth(values: number[], window: number): number[] {
  if (values.length === 0) return [];
  if (values.length === 1) return [...values];
  if (window <= 1) return [...values];

  const result: number[] = [];
  for (let i = 0; i < values.length; i++) {
    const start = Math.max(0, i - Math.floor(window / 2));
    const end = Math.min(values.length, i + Math.ceil(window / 2));
    const sum = values.slice(start, end).reduce((a, b) => a + b, 0);
    result.push(sum / (end - start));
  }
  return result;
}

/**
 * Generate an SVG path string for a sparkline from numeric values.
 * Returns "" for empty input. Auto-scales to fit (height * 0.8) of
 * the available height with a 10 % padding above/below. All-equal
 * values render a horizontal line at y=height/2 (no division-by-zero).
 */
export function sparklinePath(
  values: number[],
  width: number,
  height: number,
): string {
  if (values.length === 0) return '';

  let min = values[0]!;
  let max = values[0]!;
  for (let i = 1; i < values.length; i++) {
    const v = values[i]!;
    if (v < min) min = v;
    if (v > max) max = v;
  }
  const range = max - min;
  const baseline = height / 2;
  const xStep = width / (values.length - 1 || 1);

  let path = '';
  for (let i = 0; i < values.length; i++) {
    const x = i * xStep;
    const v = values[i]!;
    const y = range === 0
      ? baseline
      : baseline + (height * 0.4) - ((v - min) / range) * (height * 0.8);
    path += (i === 0 ? `M ${x},${y}` : ` L ${x},${y}`);
  }
  return path;
}

export function CounterWidget({ dpuId }: { dpuId: string }) {
  const series = useCountersStore(selectSeries(dpuId));

  if (!series?.samples || series.samples.length === 0) {
    return (
      <div className="text-sm text-[color:var(--text-muted)] p-3 rounded-md bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)]">
        No counter data yet for {dpuId} — waiting for first poll round.
      </div>
    );
  }

  const latest = series.samples[series.samples.length - 1];
  if (!latest) {
    return (
      <div className="text-sm text-[color:var(--text-muted)] p-3 rounded-md bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)]">
        No counter data yet for {dpuId} — waiting for first poll round.
      </div>
    );
  }
  const sparklineSamples = series.samples.slice(-60);

  const counters: Array<{
    label: string;
    key: keyof Omit<CounterSample, 'at'>;
    value: number;
    values: number[];
  }> = [
    { label: 'VXLAN Decap', key: 'vxlan_decap', value: latest.vxlan_decap,
      values: sparklineSamples.map((s) => s.vxlan_decap) },
    { label: 'VXLAN Encap', key: 'vxlan_encap', value: latest.vxlan_encap,
      values: sparklineSamples.map((s) => s.vxlan_encap) },
    { label: 'Drop ACL In', key: 'drop_acl_in', value: latest.drop_acl_in,
      values: sparklineSamples.map((s) => s.drop_acl_in) },
    { label: 'Flow Table Size', key: 'flow_table_size', value: latest.flow_table_size,
      values: sparklineSamples.map((s) => s.flow_table_size) },
  ];

  return (
    <div className="rounded-md bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] p-3">
      <h3 className="text-xs font-semibold text-[color:var(--text-primary)] mb-3 uppercase tracking-wide">
        Counter Summary
      </h3>
      <div className="grid grid-cols-2 gap-4">
        {counters.map((counter) => {
          const smoothed = smooth(counter.values, 5);
          const pathStr = sparklinePath(smoothed, 120, 32);
          return (
            <div key={counter.key} className="flex flex-col gap-1">
              <div className="text-xs text-[color:var(--text-muted)]">{counter.label}</div>
              <div className="text-sm font-mono font-semibold text-[color:var(--text-primary)]">
                {counter.value.toLocaleString()}
              </div>
              {pathStr && (
                <svg
                  width={120}
                  height={32}
                  viewBox="0 0 120 32"
                  className="mt-1 w-full"
                  data-testid={`sparkline-${counter.key}`}
                >
                  <path
                    d={pathStr}
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1"
                    className="text-[color:var(--text-muted)]"
                  />
                </svg>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

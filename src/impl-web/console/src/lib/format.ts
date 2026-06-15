/** Format IPv4 address from numeric or string */
export function formatIp(ip: string | number | null | undefined): string {
  if (ip == null) return '—';
  if (typeof ip === 'number') {
    if (!Number.isFinite(ip)) return '—';
    return [
      (ip >>> 24) & 0xff,
      (ip >>> 16) & 0xff,
      (ip >>> 8) & 0xff,
      ip & 0xff,
    ].join('.');
  }
  return ip || '—';
}

/** Format MAC address with colon separators */
export function formatMac(mac: string | null | undefined): string {
  if (!mac) return '—';
  const clean = mac.replace(/[^a-fA-F0-9]/g, '');
  if (clean.length !== 12) return mac;
  return clean.match(/.{2}/g)!.join(':').toLowerCase();
}

/** Format bytes to human-readable (KB, MB, GB, TB) */
export function formatBytes(bytes: number | null | undefined, decimals = 1): string {
  if (bytes == null || !Number.isFinite(bytes)) return '—';
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k));
  const idx = Math.min(Math.max(i, 0), sizes.length - 1);
  return `${(bytes / Math.pow(k, idx)).toFixed(decimals)} ${sizes[idx]}`;
}

/** Format duration in seconds to human-readable */
export function formatDuration(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60);
    const s = Math.round(seconds % 60);
    return s > 0 ? `${m}m ${s}s` : `${m}m`;
  }
  if (seconds < 86400) {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    return m > 0 ? `${h}h ${m}m` : `${h}h`;
  }
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  return h > 0 ? `${d}d ${h}h` : `${d}d`;
}

/** Format percentage (0-100) with specified decimals */
export function formatPercent(value: number | null | undefined, decimals = 1): string {
  if (value == null || !Number.isFinite(value)) return '—';
  return `${value.toFixed(decimals)}%`;
}

/** Format large numbers with abbreviation (1.2K, 3.4M) */
export function formatNumber(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '—';
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}K`;
  if (n < 1_000_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  return `${(n / 1_000_000_000).toFixed(1)}B`;
}

/** Format packets per second */
export function formatPps(pps: number | null | undefined): string {
  if (pps == null || !Number.isFinite(pps)) return '—';
  if (pps < 1000) return `${pps} pps`;
  if (pps < 1_000_000) return `${(pps / 1000).toFixed(1)}K pps`;
  return `${(pps / 1_000_000).toFixed(1)}M pps`;
}

/** Format bits per second */
export function formatBps(bps: number | null | undefined): string {
  if (bps == null || !Number.isFinite(bps)) return '—';
  if (bps < 1000) return `${bps} bps`;
  if (bps < 1_000_000) return `${(bps / 1000).toFixed(1)} Kbps`;
  if (bps < 1_000_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`;
  return `${(bps / 1_000_000_000).toFixed(1)} Gbps`;
}

/** Relative time ago string */
export function timeAgo(date: Date | string | number | null | undefined): string {
  if (date == null) return '—';
  const then = new Date(date).getTime();
  if (!Number.isFinite(then)) return '—';
  const now = Date.now();
  const diffSec = Math.floor((now - then) / 1000);

  if (diffSec < 0) return 'in future';
  if (diffSec < 5) return 'just now';
  if (diffSec < 60) return `${diffSec}s ago`;
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
  return `${Math.floor(diffSec / 86400)}d ago`;
}

/** Truncate string with ellipsis */
export function truncate(str: string | null | undefined, maxLen: number): string {
  if (str == null) return '';
  return str.length <= maxLen ? str : `${str.slice(0, maxLen - 1)}…`;
}

/** Strip protobuf state prefix (e.g., "DPU_STATE_UP" → "UP") */
export function stripStatePrefix(state: string | null | undefined): string {
  if (!state) return 'UNKNOWN';
  return state.replace(/^DPU_STATE_/, '').replace(/^ENI_STATE_/, '').replace(/^STATE_/, '');
}

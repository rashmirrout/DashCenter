/* ── Polling intervals (ms) ────────────────────────────────── */
export const POLL_INTERVALS = {
  /** Fleet summary, topology — relatively stable */
  FLEET: 10_000,
  /** DPU detail — moderately dynamic */
  DPU: 5_000,
  /** Vnet detail, capacity — moderately dynamic */
  VNET: 10_000,
  /** dashd health — lightweight */
  HEALTH: 5_000,
  /** Audit log polling (Phase A, replaced by WS in Phase B) */
  AUDIT: 5_000,
  /** Capacity stats */
  CAPACITY: 15_000,
  /** Inventory */
  INVENTORY: 30_000,
} as const;

/* ── WebSocket endpoints (Phase B — stubbed for Phase A) ──── */
export const WS_ENDPOINTS = {
  DPU_STATUS: '/ws/dpu-status',
  EVENTS: '/ws/events',
  FLOWS: (dpuId: string) => `/ws/flows/${dpuId}`,
  COUNTERS: (dpuId: string) => `/ws/counters/${dpuId}`,
  AUDIT: '/ws/audit',
  DRAIN: (dpuId: string) => `/ws/drain/${dpuId}`,
  MIGRATION: (sessionId: string) => `/ws/migration/${sessionId}`,
  HA_EVENTS: '/ws/ha-events',
} as const;

/* ── API base paths ───────────────────────────────────────── */
export const API_BASE = {
  REST: '/api/v1',
  ADMIN: '/api/admin',
  CONSOLE: '/api/console',
  SIM: '/api/sim',
} as const;

/* ── Resource kinds ───────────────────────────────────────── */
/* These literals are used BOTH as URL path segments hitting dashd
 * (`/api/v1/{ns}/{kind}/{name}`) AND as internal dispatch keys.
 * They must therefore match the slugs dashd's router exposes.
 * Notably `ha-sets` (not `ha`) — verified empirically; the older
 * `ha` slug returns HTTP 405 on PUT. */
export const RESOURCE_KINDS = [
  'vnets',
  'enis',
  'acl-policies',
  'route-policies',
  'ha-sets',
  'service-tunnels',
  'vnet-mappings',
] as const;

export type ResourceKind = (typeof RESOURCE_KINDS)[number];

/* ── DPU states ───────────────────────────────────────────── */
export const DPU_STATES = [
  'UNKNOWN',
  'REGISTERED',
  'CONNECTING',
  'CONNECTED',
  'SYNCING',
  'READY',
  'DRAINING',
  'CORDONED',
  'DISCONNECTED',
] as const;

export type DpuState = (typeof DPU_STATES)[number];

/* ── Status color map ─────────────────────────────────────── */
export const STATUS_COLORS: Record<string, string> = {
  /* DPU lifecycle */
  READY: 'var(--accent-green)',
  CONNECTED: 'var(--accent-green)',
  SYNCING: 'var(--accent-cyan)',
  CONNECTING: 'var(--accent-amber)',
  DRAINING: 'var(--accent-amber)',
  CORDONED: 'var(--accent-amber)',
  DISCONNECTED: 'var(--accent-red)',
  UNKNOWN: 'var(--text-muted)',
  REGISTERED: 'var(--accent-cyan)',
  /* ENI / link admin state */
  UP: 'var(--accent-green)',
  DOWN: 'var(--accent-red)',
  ACTIVE: 'var(--accent-green)',
  INACTIVE: 'var(--text-muted)',
  /* Policy verdicts (lowercase forms are normalized via stripStatePrefix) */
  ALLOW: 'var(--accent-green)',
  PERMIT: 'var(--accent-green)',
  DENY: 'var(--accent-red)',
  DROP: 'var(--accent-red)',
  ALLOW_AND_CONTINUE: 'var(--accent-cyan)',
  /* Cluster / HA roles */
  LEADER: 'var(--accent-green)',
  FOLLOWER: 'var(--accent-cyan)',
  HEALTHY: 'var(--accent-green)',
  DEGRADED: 'var(--accent-amber)',
  OFFLINE: 'var(--accent-red)',
  ERROR: 'var(--accent-red)',
  FAILED: 'var(--accent-red)',
  WARNING: 'var(--accent-amber)',
};

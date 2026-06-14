import { cn } from "@/lib/cn";
import { STATUS_COLORS } from "@/lib/constants";
import { stripStatePrefix } from "@/lib/format";

interface StatusBadgeProps {
  status: string | null | undefined;
  size?: "sm" | "md";
  /** Strip noisy proto prefix (DPU_STATE_, ENI_STATE_) before display. */
  prettify?: boolean;
  className?: string;
}

/**
 * Compute the pulse animation class for a given status.
 * Exported for unit tests.
 *
 * Per LLD §10.1:
 *   HEALTHY  → slow pulse (3s)
 *   DEGRADED → medium pulse (2s)
 *   OFFLINE  → fast pulse (1s)
 *   UNKNOWN  → no pulse
 */
export function pulseClassForStatus(status: string | null | undefined): string {
  if (!status) return "";
  const s = stripStatePrefix(status).toUpperCase();
  if (
    s === "UP" ||
    s === "READY" ||
    s === "CONNECTED" ||
    s === "ACTIVE" ||
    s === "HEALTHY" ||
    s === "LEADER" ||
    s === "ALLOW" ||
    s === "PERMIT"
  ) {
    return "animate-pulse-slow";
  }
  if (
    s === "DEGRADED" ||
    s === "DRAINING" ||
    s === "CORDONED" ||
    s === "CONNECTING" ||
    s === "SYNCING" ||
    s === "REGISTERED" ||
    s === "FOLLOWER" ||
    s === "WARNING"
  ) {
    return "animate-pulse-medium";
  }
  if (
    s === "DOWN" ||
    s === "DISCONNECTED" ||
    s === "OFFLINE" ||
    s === "ERROR" ||
    s === "DENY" ||
    s === "DROP" ||
    s === "FAILED"
  ) {
    return "animate-pulse-fast";
  }
  return "";
}

export function StatusBadge({
  status,
  size = "sm",
  prettify = true,
  className,
}: StatusBadgeProps) {
  const safeStatus = status ?? "UNKNOWN";
  // dashd emits admin states as lowercase ("up", "down"), but the
  // STATUS_COLORS map is keyed by uppercase canonical names. Normalize
  // before the lookup so the green/red blink lands correctly for both
  // wire formats. We try (a) the exact key, (b) the uppercased key, and
  // (c) the prefix-stripped uppercased key, in that order.
  const stripped = stripStatePrefix(safeStatus);
  const color =
    STATUS_COLORS[safeStatus] ??
    STATUS_COLORS[safeStatus.toUpperCase()] ??
    STATUS_COLORS[stripped] ??
    STATUS_COLORS[stripped.toUpperCase()] ??
    "var(--text-muted)";
  const label = prettify ? stripped : safeStatus;
  const pulse = pulseClassForStatus(safeStatus);

  return (
    <span
      data-testid="status-badge"
      data-status={safeStatus}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full font-medium uppercase tracking-wider",
        size === "sm" ? "px-2 py-0.5 text-[10px]" : "px-2.5 py-1 text-xs",
        className
      )}
      style={{
        color,
        borderColor: color,
        border: "1px solid",
        backgroundColor: `color-mix(in srgb, ${color} 8%, transparent)`,
      }}
    >
      <span
        aria-hidden
        className={cn("inline-block rounded-full shrink-0", pulse)}
        style={{
          width: size === "sm" ? 7 : 9,
          height: size === "sm" ? 7 : 9,
          backgroundColor: color,
          // Layered glow: tight inner ring + wider outer halo. Both fade in/out
          // with the pulse animation so the dot reads as a live "heartbeat".
          boxShadow: pulse
            ? `0 0 4px ${color}, 0 0 10px ${color}, 0 0 16px color-mix(in srgb, ${color} 60%, transparent)`
            : undefined,
        }}
      />
      {label}
    </span>
  );
}
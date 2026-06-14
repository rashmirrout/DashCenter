/* ═══════════════════════════════════════════════════════════════
 * ConfirmDialog — destructive-action confirmation modal.
 *
 * Minimal implementation shipped as part of A-IF (Interactive
 * Forms) so the new /resources page has a safe Delete affordance.
 * Will be generalised / re-themed in A-PH2-G4 when the wider
 * confirm pattern lands.
 *
 * Added in A-IF4-G3.
 * ═══════════════════════════════════════════════════════════════ */

import { useEffect, useState, type ReactNode } from "react";
import { AlertTriangle, Loader2, X } from "lucide-react";
import { cn } from "@/lib/cn";

export interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  message: ReactNode;
  /** Confirm button label. Defaults to "Delete". */
  confirmLabel?: string;
  cancelLabel?: string;
  /** Tone of the confirm button. */
  tone?: "danger" | "warning" | "neutral";
  /** Optional reason input shown above the buttons. */
  showReasonInput?: boolean;
  reasonPlaceholder?: string;
  /** Called on confirm. Receives the reason (or "" when not shown). */
  onConfirm: (reason: string) => Promise<unknown> | void;
}

export function ConfirmDialog({
  open,
  onClose,
  title,
  message,
  confirmLabel = "Delete",
  cancelLabel = "Cancel",
  tone = "danger",
  showReasonInput = false,
  reasonPlaceholder = "Reason (for audit log)…",
  onConfirm,
}: ConfirmDialogProps) {
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset transient state on open
  useEffect(() => {
    if (open) {
      setReason("");
      setBusy(false);
      setError(null);
    }
  }, [open]);

  // Esc to close
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape" && !busy) onClose();
    }
    window.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose, busy]);

  if (!open) return null;

  async function handleConfirm() {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await onConfirm(reason);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed.");
    } finally {
      setBusy(false);
    }
  }

  const toneClasses =
    tone === "danger"
      ? "bg-[color:var(--accent-red)]/15 text-[color:var(--accent-red)] border-[color:var(--accent-red)]/40 hover:bg-[color:var(--accent-red)]/25"
      : tone === "warning"
        ? "bg-[color:var(--accent-amber)]/15 text-[color:var(--accent-amber)] border-[color:var(--accent-amber)]/40 hover:bg-[color:var(--accent-amber)]/25"
        : "bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)] border-[color:var(--accent-cyan)]/40 hover:bg-[color:var(--accent-cyan)]/25";

  const toneIconColor =
    tone === "danger"
      ? "text-[color:var(--accent-red)]"
      : tone === "warning"
        ? "text-[color:var(--accent-amber)]"
        : "text-[color:var(--accent-cyan)]";

  return (
    <div
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="confirm-dialog-title"
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
    >
      <button
        type="button"
        aria-label="Cancel"
        onClick={() => !busy && onClose()}
        className="absolute inset-0 bg-black/60 backdrop-blur-[2px] animate-fade-in"
      />
      <div className="relative w-[440px] max-w-full bg-[color:var(--bg-secondary)] border border-[color:var(--border-default)] rounded-xl shadow-2xl animate-slide-up overflow-hidden">
        <header className="flex items-start gap-3 p-4 border-b border-[color:var(--border-subtle)]">
          <AlertTriangle
            size={20}
            className={cn("shrink-0 mt-0.5", toneIconColor)}
            aria-hidden
          />
          <div className="flex-1 min-w-0">
            <h2
              id="confirm-dialog-title"
              className="text-base font-semibold text-[color:var(--text-primary)]"
            >
              {title}
            </h2>
            <div className="text-sm text-[color:var(--text-secondary)] mt-1">
              {message}
            </div>
          </div>
          <button
            type="button"
            onClick={() => !busy && onClose()}
            disabled={busy}
            aria-label="Close"
            className="text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)] hover:bg-white/5 p-1 rounded disabled:opacity-40"
          >
            <X size={14} />
          </button>
        </header>

        <div className="p-4 space-y-3">
          {showReasonInput && (
            <input
              type="text"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={reasonPlaceholder}
              disabled={busy}
              className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50"
            />
          )}
          {error && (
            <p
              role="alert"
              className="text-xs text-[color:var(--accent-red)] p-2 rounded bg-[color:var(--accent-red)]/10 border border-[color:var(--accent-red)]/30"
            >
              {error}
            </p>
          )}
        </div>

        <footer className="flex items-center justify-end gap-2 p-3 border-t border-[color:var(--border-subtle)] bg-[color:var(--bg-tertiary)]/50">
          <button
            type="button"
            onClick={() => !busy && onClose()}
            disabled={busy}
            className="px-3 py-1.5 text-sm rounded-md border border-[color:var(--border-default)] text-[color:var(--text-secondary)] hover:bg-white/5 disabled:opacity-40"
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            onClick={handleConfirm}
            disabled={busy}
            className={cn(
              "px-4 py-1.5 text-sm rounded-md border flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-wait",
              toneClasses,
            )}
          >
            {busy && <Loader2 size={14} className="animate-spin" aria-hidden />}
            {busy ? "Working…" : confirmLabel}
          </button>
        </footer>
      </div>
    </div>
  );
}
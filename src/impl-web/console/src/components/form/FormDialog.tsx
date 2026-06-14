/* ═══════════════════════════════════════════════════════════════
 * FormDialog — modal wrapper for the interactive-forms page.
 *
 * A self-contained dialog that:
 *   • Validates a zod schema on submit
 *   • Surfaces inline errors via a `errorAt(path)` helper
 *   • Manages its own value state (uncontrolled by default)
 *   • Shows a top-of-modal error banner for validation + submit failures
 *   • Provides a loading state during async submit
 *   • Locks body scroll, closes on Escape, animates entry/exit
 *
 * Render-prop API: the dialog body is `children(controller)`
 * where `controller = { values, errorAt, setField, setValues }`.
 * Field-level components read `values` / call `setField`.
 *
 * Added in A-IF2-G1.
 * ═══════════════════════════════════════════════════════════════ */

import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { X, AlertCircle, Loader2 } from "lucide-react";
import type { ZodType } from "zod";
import { cn } from "@/lib/cn";

/* ── Public types ──────────────────────────────────────────── */

export interface FormController<T> {
  /** Current form values (always live; updates on every setField). */
  values: T;
  /**
   * Returns the first validation error message for the given
   * dotted-path field, or undefined when valid.
   *
   * Examples:
   *   errorAt("metadata.name")
   *   errorAt("rules.0.priority")
   *   errorAt("params.tunnel")
   */
  errorAt: (path: string) => string | undefined;
  /** Set a single field by dotted path. Mutates immutably. */
  setField: (path: string, value: unknown) => void;
  /** Wholesale replace the form values. */
  setValues: (next: T) => void;
}

export interface FormDialogProps<T> {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  /** zod schema used to validate `values` on submit. */
  schema: ZodType<T>;
  /** Initial form values. Re-applied whenever the dialog re-opens. */
  defaultValues: T;
  /**
   * Called with validated values on submit. Return a Promise — the
   * dialog stays in its loading state until the promise settles,
   * then closes on resolve or shows the rejection's message.
   */
  onSubmit: (values: T) => Promise<unknown>;
  submitLabel?: string;
  cancelLabel?: string;
  width?: "sm" | "md" | "lg" | "xl";
  children: (controller: FormController<T>) => ReactNode;
}

/* ── Internal helpers (tiny lodash.get/set replacements) ──── */

function getByPath(obj: unknown, path: string): unknown {
  if (obj == null) return undefined;
  const parts = path.split(".");
  let cur: unknown = obj;
  for (const p of parts) {
    if (cur == null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[p];
  }
  return cur;
}

function setByPath<T>(obj: T, path: string, value: unknown): T {
  const parts = path.split(".");
  // Defensive deep clone via JSON. Form values are JSON-shaped by contract.
  const clone = JSON.parse(JSON.stringify(obj ?? {})) as Record<string, unknown>;
  let cur: Record<string, unknown> = clone;
  for (let i = 0; i < parts.length - 1; i++) {
    const key = parts[i]!;
    const next = cur[key];
    if (next == null || typeof next !== "object") {
      // Auto-create. Numeric segments create arrays; others create objects.
      const nextSegment = parts[i + 1]!;
      cur[key] = /^\d+$/.test(nextSegment) ? [] : {};
    }
    cur = cur[key] as Record<string, unknown>;
  }
  cur[parts[parts.length - 1]!] = value;
  return clone as T;
}

/* ── Width map ─────────────────────────────────────────────── */

const WIDTH_MAP: Record<NonNullable<FormDialogProps<unknown>["width"]>, string> = {
  sm: "w-[420px]",
  md: "w-[560px]",
  lg: "w-[720px]",
  xl: "w-[920px]",
};

/* ── Component ─────────────────────────────────────────────── */

export function FormDialog<T>({
  open,
  onClose,
  title,
  subtitle,
  schema,
  defaultValues,
  onSubmit,
  submitLabel = "Save",
  cancelLabel = "Cancel",
  width = "md",
  children,
}: FormDialogProps<T>) {
  /* ── State ─────────────────────────────────────────────── */
  const [values, setValues] = useState<T>(defaultValues);
  const [errorsByPath, setErrorsByPath] = useState<Record<string, string>>({});
  const [topError, setTopError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Re-apply defaultValues whenever the dialog re-opens with potentially
  // new defaults (the parent toggles `open` true → false → true with
  // different values for Create vs Edit vs Clone).
  useEffect(() => {
    if (open) {
      setValues(defaultValues);
      setErrorsByPath({});
      setTopError(null);
      setSubmitting(false);
    }
    // We deliberately do NOT depend on `defaultValues` identity to avoid
    // recursive resets when the parent re-renders. The intent here is
    // "reset on every open transition". Use a key prop on FormDialog
    // if a true value-driven reset is needed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  /* ── Body scroll lock + Escape ──────────────────────────── */
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape" && !submitting) onClose();
    }
    window.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose, submitting]);

  /* ── Controller ────────────────────────────────────────── */
  const setField = useCallback((path: string, value: unknown) => {
    setValues((prev) => setByPath(prev, path, value));
    // Clear the per-field error optimistically once the user edits.
    setErrorsByPath((prev) => {
      if (!(path in prev)) return prev;
      const { [path]: _removed, ...rest } = prev;
      return rest;
    });
    setTopError(null);
  }, []);

  const errorAt = useCallback(
    (path: string) => errorsByPath[path],
    [errorsByPath],
  );

  const controller = useMemo<FormController<T>>(
    () => ({
      values,
      errorAt,
      setField,
      setValues: (next: T) => {
        setValues(next);
        setErrorsByPath({});
        setTopError(null);
      },
    }),
    [values, errorAt, setField],
  );

  /* ── Submit handler ────────────────────────────────────── */
  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (submitting) return;

      const parsed = schema.safeParse(values);
      if (!parsed.success) {
        // Flatten the zod issue list into a path→message map.
        const next: Record<string, string> = {};
        for (const issue of parsed.error.issues) {
          const key = issue.path.map(String).join(".");
          // First error wins per path so the UI doesn't yo-yo.
          if (!(key in next)) next[key] = issue.message;
        }
        setErrorsByPath(next);
        setTopError(
          parsed.error.issues.length === 1
            ? "Please fix the error below and try again."
            : `Please fix the ${parsed.error.issues.length} errors below and try again.`,
        );
        return;
      }

      setErrorsByPath({});
      setTopError(null);
      setSubmitting(true);
      try {
        await onSubmit(parsed.data);
        onClose();
      } catch (err) {
        setTopError(
          err instanceof Error
            ? err.message
            : "Submit failed. Please try again.",
        );
      } finally {
        setSubmitting(false);
      }
    },
    [submitting, schema, values, onSubmit, onClose],
  );

  /* ── Read-area key shortcut for testing/dev convenience ─ */
  // Pressing Ctrl/Cmd+Enter inside the body submits the form.
  // (The Submit button does the same; this is a power-user nicety.)
  const onBodyKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        void handleSubmit(e as unknown as React.FormEvent);
      }
    },
    [handleSubmit],
  );

  /* ── Render ─────────────────────────────────────────────── */
  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="form-dialog-title"
      data-testid="form-dialog"
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
    >
      {/* Backdrop */}
      <button
        type="button"
        aria-label="Cancel and close"
        onClick={() => !submitting && onClose()}
        className="absolute inset-0 bg-black/60 backdrop-blur-[2px] animate-fade-in"
      />

      {/* Panel */}
      <div
        className={cn(
          "relative flex flex-col max-h-[90vh] bg-[color:var(--bg-secondary)] border border-[color:var(--border-default)] rounded-xl shadow-2xl animate-slide-up overflow-hidden",
          WIDTH_MAP[width],
          "max-w-full",
        )}
      >
        {/* Header */}
        <header className="flex items-start justify-between gap-3 p-4 border-b border-[color:var(--border-subtle)]">
          <div className="min-w-0">
            <h2
              id="form-dialog-title"
              className="text-base font-semibold text-[color:var(--text-primary)] truncate"
            >
              {title}
            </h2>
            {subtitle && (
              <p className="text-xs text-[color:var(--text-secondary)] mt-0.5 truncate">
                {subtitle}
              </p>
            )}
          </div>
          <button
            type="button"
            onClick={() => !submitting && onClose()}
            disabled={submitting}
            aria-label="Close dialog"
            className="p-1.5 rounded-md text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)] hover:bg-white/5 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <X size={16} />
          </button>
        </header>

        {/* Body — single <form> wraps everything so submit Enter works. */}
        <form
          onSubmit={handleSubmit}
          onKeyDown={onBodyKeyDown}
          className="flex flex-col flex-1 min-h-0"
        >
          <div className="flex-1 overflow-y-auto p-4 space-y-4">
            {topError && (
              <div
                role="alert"
                className="flex items-start gap-2 p-2.5 rounded-md bg-[color:var(--accent-red)]/10 border border-[color:var(--accent-red)]/30 text-[color:var(--accent-red)] text-xs"
              >
                <AlertCircle size={14} className="mt-0.5 shrink-0" aria-hidden />
                <span>{topError}</span>
              </div>
            )}
            {children(controller)}
          </div>

          {/* Sticky footer */}
          <footer className="flex items-center justify-end gap-2 p-3 border-t border-[color:var(--border-subtle)] bg-[color:var(--bg-tertiary)]/50">
            <button
              type="button"
              onClick={() => !submitting && onClose()}
              disabled={submitting}
              className="px-3 py-1.5 text-sm rounded-md border border-[color:var(--border-default)] text-[color:var(--text-secondary)] hover:bg-white/5 hover:text-[color:var(--text-primary)] disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              {cancelLabel}
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="px-4 py-1.5 text-sm rounded-md bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)] border border-[color:var(--accent-cyan)]/40 hover:bg-[color:var(--accent-cyan)]/25 disabled:opacity-50 disabled:cursor-wait transition-colors flex items-center gap-1.5"
            >
              {submitting && <Loader2 size={14} className="animate-spin" aria-hidden />}
              {submitting ? "Saving…" : submitLabel}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}

/* ── Internal helpers re-exported for tests ───────────────── */
export const __formInternals = { getByPath, setByPath };
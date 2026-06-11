import { Component, type ErrorInfo, type ReactNode } from 'react';

/* ── Props ─────────────────────────────────────────────────── */
interface ErrorBoundaryProps {
  /** Component tree to wrap */
  children: ReactNode;
  /** Optional fallback UI; receives error + reset function */
  fallback?: (props: { error: Error; reset: () => void }) => ReactNode;
  /** Called when an error is caught (e.g., for logging) */
  onError?: (error: Error, info: ErrorInfo) => void;
  /** Label for the view (used in default fallback) */
  viewName?: string;
}

interface ErrorBoundaryState {
  error: Error | null;
}

/**
 * Per-view React error boundary.
 *
 * Catches render-time exceptions in the subtree and shows a recovery UI
 * instead of crashing the entire application. Each view route wraps its
 * content in an ErrorBoundary so a failure in one view doesn't break
 * navigation to other views.
 *
 * Usage:
 * ```tsx
 * <ErrorBoundary viewName="Fleet">
 *   <FleetView />
 * </ErrorBoundary>
 * ```
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    this.props.onError?.(error, info);

    // Log to console in development
    if (import.meta.env.DEV) {
      console.error(`[ErrorBoundary${this.props.viewName ? `:${this.props.viewName}` : ''}]`, error, info);
    }
  }

  private reset = () => {
    this.setState({ error: null });
  };

  render() {
    const { error } = this.state;

    if (error) {
      // Custom fallback
      if (this.props.fallback) {
        return this.props.fallback({ error, reset: this.reset });
      }

      // Default fallback
      return (
        <div className="flex flex-col items-center justify-center py-16 text-center" role="alert">
          <div className="w-16 h-16 rounded-full bg-accent-red/10 flex items-center justify-center mb-4">
            <svg
              className="w-8 h-8 text-accent-red"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={2}
              aria-hidden="true"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z"
              />
            </svg>
          </div>

          <h2 className="text-lg font-semibold text-text-primary mb-1">
            {this.props.viewName ? `${this.props.viewName} failed to render` : 'Something went wrong'}
          </h2>

          <p className="text-sm text-text-secondary max-w-md mb-2">
            An unexpected error occurred. You can try again or navigate to another view.
          </p>

          {import.meta.env.DEV && (
            <details className="text-left w-full max-w-lg mb-4">
              <summary className="text-xs text-text-muted cursor-pointer hover:text-text-secondary">
                Error details (dev only)
              </summary>
              <pre className="mt-2 p-3 bg-bg-elevated rounded-lg text-xs text-accent-red font-mono overflow-auto max-h-48 border border-border">
                {error.message}
                {'\n\n'}
                {error.stack}
              </pre>
            </details>
          )}

          <div className="flex gap-3">
            <button
              onClick={this.reset}
              className="px-4 py-2 text-sm rounded-lg bg-accent-cyan/10 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/20 transition-colors focus:outline-none focus:ring-2 focus:ring-accent-cyan/50"
            >
              Try again
            </button>
            <button
              onClick={() => {
                this.reset();
                window.location.href = '/dashboard';
              }}
              className="px-4 py-2 text-sm rounded-lg border border-border text-text-secondary hover:bg-bg-elevated transition-colors focus:outline-none focus:ring-2 focus:ring-border"
            >
              Go to Dashboard
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}

/**
 * Query error handler for TanStack Query `onError`.
 * Returns a user-friendly message from an API error.
 */
export function getQueryErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    // ApiError from our client
    if ('status' in error) {
      const status = (error as { status: number }).status;
      if (status === 403) return 'Access denied. You may not have permission for this operation.';
      if (status === 404) return 'The requested resource was not found.';
      if (status === 502 || status === 503) return 'Backend service is unavailable. dashd may be down.';
      if (status >= 500) return `Server error (${status}). Please try again later.`;
    }
    return error.message;
  }
  return 'An unexpected error occurred.';
}
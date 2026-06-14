import { forwardRef, type InputHTMLAttributes } from 'react';
import { cn } from '@/lib/cn';

/* ── Shared base input style ──────────────────────────────── */
const baseInputClass =
  'w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50 disabled:cursor-not-allowed';

/* ── Label wrapper ─────────────────────────────────────────── */
interface FieldWrapperProps {
  label: string;
  htmlFor: string;
  error?: string;
  hint?: string;
  required?: boolean;
  children: React.ReactNode;
  className?: string;
}

export function FieldWrapper({ label, htmlFor, error, hint, required, children, className }: FieldWrapperProps) {
  return (
    <div className={cn('flex flex-col gap-1', className)}>
      <label htmlFor={htmlFor} className="text-xs font-medium text-text-secondary">
        {label}
        {required && <span className="text-accent-red ml-0.5" aria-hidden="true">*</span>}
      </label>
      {children}
      {error && (
        <span className="text-xs text-accent-red" role="alert">{error}</span>
      )}
      {hint && !error && (
        <span className="text-xs text-text-muted">{hint}</span>
      )}
    </div>
  );
}

/* ── IPv4 / IPv6 Input ─────────────────────────────────────── */
const IPV4_PATTERN = /^(\d{1,3}\.){0,3}\d{0,3}$/;
const IPV6_PATTERN = /^[0-9a-fA-F:]*$/;

interface IpInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  /** IPv4 or IPv6 (default: both) */
  version?: 4 | 6;
  label?: string;
  error?: string;
}

export const IpInput = forwardRef<HTMLInputElement, IpInputProps>(
  ({ version, label, error, className, id, ...props }, ref) => {
    const inputId = id ?? `ip-input-${version ?? 'any'}`;
    const placeholder =
      version === 4 ? '10.0.0.1' : version === 6 ? '2001:db8::1' : '10.0.0.1 or 2001:db8::1';
    const pattern = version === 4 ? IPV4_PATTERN : version === 6 ? IPV6_PATTERN : undefined;

    const input = (
      <input
        ref={ref}
        id={inputId}
        type="text"
        inputMode={version === 4 ? 'numeric' : 'text'}
        placeholder={placeholder}
        className={cn(baseInputClass, error && 'border-accent-red focus:ring-accent-red/50', className)}
        aria-invalid={!!error}
        aria-describedby={error ? `${inputId}-error` : undefined}
        onKeyDown={(e) => {
          // Allow control keys
          if (e.ctrlKey || e.metaKey || e.altKey) return;
          if (['Backspace', 'Delete', 'Tab', 'ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;

          if (version === 4 && pattern && !/[\d.]/.test(e.key)) {
            e.preventDefault();
          }
          if (version === 6 && !/[0-9a-fA-F:]/.test(e.key)) {
            e.preventDefault();
          }
        }}
        {...props}
      />
    );

    if (label) {
      return (
        <FieldWrapper label={label} htmlFor={inputId} error={error} required={props.required}>
          {input}
        </FieldWrapper>
      );
    }
    return input;
  },
);
IpInput.displayName = 'IpInput';

/* ── MAC Address Input ─────────────────────────────────────── */
interface MacInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label?: string;
  error?: string;
}

export const MacInput = forwardRef<HTMLInputElement, MacInputProps>(
  ({ label, error, className, id, ...props }, ref) => {
    const inputId = id ?? 'mac-input';

    const input = (
      <input
        ref={ref}
        id={inputId}
        type="text"
        placeholder="00:11:22:33:44:55"
        maxLength={17}
        className={cn(baseInputClass, error && 'border-accent-red focus:ring-accent-red/50', className)}
        aria-invalid={!!error}
        onKeyDown={(e) => {
          if (e.ctrlKey || e.metaKey || e.altKey) return;
          if (['Backspace', 'Delete', 'Tab', 'ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
          if (!/[0-9a-fA-F:]/.test(e.key)) {
            e.preventDefault();
          }
        }}
        {...props}
      />
    );

    if (label) {
      return (
        <FieldWrapper label={label} htmlFor={inputId} error={error} required={props.required}>
          {input}
        </FieldWrapper>
      );
    }
    return input;
  },
);
MacInput.displayName = 'MacInput';

/* ── CIDR Input ────────────────────────────────────────────── */
interface CidrInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label?: string;
  error?: string;
}

export const CidrInput = forwardRef<HTMLInputElement, CidrInputProps>(
  ({ label, error, className, id, ...props }, ref) => {
    const inputId = id ?? 'cidr-input';

    const input = (
      <input
        ref={ref}
        id={inputId}
        type="text"
        placeholder="10.0.0.0/24"
        className={cn(baseInputClass, error && 'border-accent-red focus:ring-accent-red/50', className)}
        aria-invalid={!!error}
        onKeyDown={(e) => {
          if (e.ctrlKey || e.metaKey || e.altKey) return;
          if (['Backspace', 'Delete', 'Tab', 'ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
          if (!/[0-9a-fA-F.:/]/.test(e.key)) {
            e.preventDefault();
          }
        }}
        {...props}
      />
    );

    if (label) {
      return (
        <FieldWrapper label={label} htmlFor={inputId} error={error} required={props.required}>
          {input}
        </FieldWrapper>
      );
    }
    return input;
  },
);
CidrInput.displayName = 'CidrInput';

/* ── Port Range Input ──────────────────────────────────────── */
interface PortRangeInputProps {
  label?: string;
  error?: string;
  startValue?: number;
  endValue?: number;
  onStartChange?: (value: number) => void;
  onEndChange?: (value: number) => void;
  disabled?: boolean;
  required?: boolean;
  className?: string;
  id?: string;
}

export function PortRangeInput({
  label,
  error,
  startValue,
  endValue,
  onStartChange,
  onEndChange,
  disabled,
  required,
  className,
  id,
}: PortRangeInputProps) {
  const inputId = id ?? 'port-range-input';

  const content = (
    <div className={cn('flex items-center gap-2', className)}>
      <input
        id={`${inputId}-start`}
        type="number"
        min={0}
        max={65535}
        value={startValue ?? ''}
        onChange={(e) => onStartChange?.(Number(e.target.value))}
        placeholder="0"
        disabled={disabled}
        className={cn(baseInputClass, 'w-24', error && 'border-accent-red focus:ring-accent-red/50')}
        aria-label="Start port"
      />
      <span className="text-text-muted text-sm">–</span>
      <input
        id={`${inputId}-end`}
        type="number"
        min={0}
        max={65535}
        value={endValue ?? ''}
        onChange={(e) => onEndChange?.(Number(e.target.value))}
        placeholder="65535"
        disabled={disabled}
        className={cn(baseInputClass, 'w-24', error && 'border-accent-red focus:ring-accent-red/50')}
        aria-label="End port"
      />
    </div>
  );

  if (label) {
    return (
      <FieldWrapper label={label} htmlFor={`${inputId}-start`} error={error} required={required}>
        {content}
      </FieldWrapper>
    );
  }
  return content;
}

/* ── Namespace Selector ────────────────────────────────────── */
interface NamespaceSelectorProps {
  value: string;
  onChange: (value: string) => void;
  namespaces?: string[];
  label?: string;
  disabled?: boolean;
  className?: string;
  id?: string;
}

export function NamespaceSelector({
  value,
  onChange,
  namespaces = ['default'],
  label = 'Namespace',
  disabled,
  className,
  id,
}: NamespaceSelectorProps) {
  const inputId = id ?? 'namespace-selector';

  return (
    <FieldWrapper label={label} htmlFor={inputId}>
      <select
        id={inputId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        className={cn(baseInputClass, 'cursor-pointer', className)}
      >
        {namespaces.map((ns) => (
          <option key={ns} value={ns}>
            {ns}
          </option>
        ))}
      </select>
    </FieldWrapper>
  );
}

/* ── VNI Input ─────────────────────────────────────────────── */
interface VniInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label?: string;
  error?: string;
}

export const VniInput = forwardRef<HTMLInputElement, VniInputProps>(
  ({ label, error, className, id, ...props }, ref) => {
    const inputId = id ?? 'vni-input';

    const input = (
      <input
        ref={ref}
        id={inputId}
        type="number"
        min={1}
        max={16777215}
        placeholder="100"
        className={cn(baseInputClass, error && 'border-accent-red focus:ring-accent-red/50', className)}
        aria-invalid={!!error}
        {...props}
      />
    );

    if (label) {
      return (
        <FieldWrapper label={label} htmlFor={inputId} error={error} hint="1–16777215" required={props.required}>
          {input}
        </FieldWrapper>
      );
    }
    return input;
  },
);
VniInput.displayName = 'VniInput';
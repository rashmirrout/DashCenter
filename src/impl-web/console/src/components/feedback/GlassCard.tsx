import type { CSSProperties, KeyboardEvent, ReactNode } from "react";
import { cn } from "@/lib/cn";

export type GlowVariant = "none" | "cyan" | "purple" | "green" | "amber" | "red";

interface GlassCardProps {
  children: ReactNode;
  className?: string;
  /** Apply a colored glow halo. */
  glow?: boolean | GlowVariant;
  /** Hoverable variant — adds hover state and pointer cursor. */
  hoverable?: boolean;
  /** Render-as for semantics. */
  as?: "div" | "section" | "article";
  /** Inline styles (e.g. minHeight). */
  style?: CSSProperties;
  /** Click handler. Sets role=button, tabIndex=0, and Enter handling. */
  onClick?: () => void;
  /** Aria-label when onClick is set. */
  ariaLabel?: string;
}

function glowClass(variant: GlowVariant): string {
  switch (variant) {
    case "cyan":
      return "glow-cyan";
    case "purple":
      return "glow-purple";
    case "green":
      return "glow-green";
    case "amber":
      return "glow-amber";
    case "red":
      return "glow-red";
    default:
      return "";
  }
}

function resolveGlow(glow: GlassCardProps["glow"]): GlowVariant {
  if (glow === true) return "cyan";
  if (!glow) return "none";
  return glow;
}

export function GlassCard({
  children,
  className,
  glow,
  hoverable,
  as: Tag = "div",
  style,
  onClick,
  ariaLabel,
}: GlassCardProps) {
  const glowVariant = resolveGlow(glow);
  const clickable = !!onClick;

  const handleKeyDown = clickable
    ? (e: KeyboardEvent<HTMLElement>) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }
    : undefined;

  return (
    <Tag
      className={cn(
        "glass-surface p-4",
        glowClass(glowVariant),
        (hoverable || clickable) && "glass-surface-hover transition-colors",
        clickable && "cursor-pointer",
        className
      )}
      style={style}
      role={clickable ? "button" : undefined}
      tabIndex={clickable ? 0 : undefined}
      aria-label={ariaLabel}
      onClick={onClick}
      onKeyDown={handleKeyDown}
    >
      {children}
    </Tag>
  );
}
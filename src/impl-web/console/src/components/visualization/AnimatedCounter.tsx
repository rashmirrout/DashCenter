/**
 * AnimatedCounter — smoothly counts a number from 0 (or the previous
 * value) up to the current value over a short duration.
 *
 * Pure framer-motion + requestAnimationFrame; no third-party library.
 * Honors `prefers-reduced-motion`: when reduced motion is requested,
 * the value is set immediately with no tween (the framer-motion
 * MotionValue still works but completes instantly via the global
 * `animation-duration: 0.001s` CSS rule).
 *
 * Usage:
 *   <AnimatedCounter value={fleet.dpu_count} duration={1.2} />
 *   <AnimatedCounter value={enis} formatter={formatNumber} />
 */
import { useEffect, useRef, useState } from 'react';
import { useMotionValue, useTransform, animate } from 'framer-motion';

export interface AnimatedCounterProps {
  /** Target numeric value. */
  value: number;
  /** Tween duration in seconds (default 1.0). */
  duration?: number;
  /** Custom formatter (default: rounded integer with locale separators). */
  formatter?: (n: number) => string;
  /** Extra Tailwind classes (e.g. for color/size). */
  className?: string;
}

const defaultFormatter = (n: number) => Math.round(n).toLocaleString();

export function AnimatedCounter({
  value,
  duration = 1.0,
  formatter = defaultFormatter,
  className,
}: AnimatedCounterProps) {
  const motionValue = useMotionValue(0);
  const rounded = useTransform(motionValue, (latest) => formatter(latest));
  const [display, setDisplay] = useState<string>(formatter(0));
  const prevValueRef = useRef<number>(0);

  // Mirror the MotionValue → React state so the DOM updates on each frame.
  useEffect(() => {
    const unsub = rounded.on('change', (v) => setDisplay(v));
    return unsub;
  }, [rounded]);

  // Animate from previous value → new value whenever `value` changes.
  useEffect(() => {
    const controls = animate(motionValue, value, {
      duration,
      ease: 'easeOut',
    });
    prevValueRef.current = value;
    return controls.stop;
  }, [value, duration, motionValue]);

  return (
    <span className={className} aria-label={String(value)}>
      {display}
    </span>
  );
}

export default AnimatedCounter;
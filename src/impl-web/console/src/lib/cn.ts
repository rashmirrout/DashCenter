import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

/** Merge Tailwind classes with clsx — handles conditional + dedup */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
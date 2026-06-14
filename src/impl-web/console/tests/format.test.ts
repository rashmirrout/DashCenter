import { describe, it, expect } from 'vitest';
import {
  formatIp, formatMac, formatBytes, formatDuration,
  formatPercent, formatNumber, formatPps, formatBps,
  timeAgo, truncate, stripStatePrefix,
} from '../src/lib/format';

describe('formatIp', () => {
  it('passes through string IPs', () => {
    expect(formatIp('10.0.0.1')).toBe('10.0.0.1');
  });
  it('converts numeric IP', () => {
    expect(formatIp(0x0A000001)).toBe('10.0.0.1');
  });
  it('handles 0.0.0.0', () => {
    expect(formatIp(0)).toBe('0.0.0.0');
  });
  it('handles 255.255.255.255', () => {
    expect(formatIp(0xFFFFFFFF)).toBe('255.255.255.255');
  });
  it('returns dash for null/undefined', () => {
    expect(formatIp(null)).toBe('—');
    expect(formatIp(undefined)).toBe('—');
  });
  it('returns dash for empty string', () => {
    expect(formatIp('')).toBe('—');
  });
  it('returns dash for non-finite numbers', () => {
    expect(formatIp(Number.NaN)).toBe('—');
    expect(formatIp(Number.POSITIVE_INFINITY)).toBe('—');
  });
});

describe('formatMac', () => {
  it('formats clean hex', () => {
    expect(formatMac('aabbccddeeff')).toBe('aa:bb:cc:dd:ee:ff');
  });
  it('passes through already formatted', () => {
    expect(formatMac('AA:BB:CC:DD:EE:FF')).toBe('aa:bb:cc:dd:ee:ff');
  });
  it('returns original if invalid length', () => {
    expect(formatMac('short')).toBe('short');
  });
  it('returns dash for null/undefined/empty', () => {
    expect(formatMac(null)).toBe('—');
    expect(formatMac(undefined)).toBe('—');
    expect(formatMac('')).toBe('—');
  });
});

describe('formatBytes', () => {
  it('formats 0', () => {
    expect(formatBytes(0)).toBe('0 B');
  });
  it('formats KB', () => {
    expect(formatBytes(1024)).toBe('1.0 KB');
  });
  it('formats MB', () => {
    expect(formatBytes(1048576)).toBe('1.0 MB');
  });
  it('formats GB', () => {
    expect(formatBytes(1073741824)).toBe('1.0 GB');
  });
  it('returns dash for null/undefined/NaN', () => {
    expect(formatBytes(null)).toBe('—');
    expect(formatBytes(undefined)).toBe('—');
    expect(formatBytes(Number.NaN)).toBe('—');
  });
  it('clamps very small values to bytes unit', () => {
    expect(formatBytes(1)).toBe('1.0 B');
  });
});

describe('formatDuration', () => {
  it('formats seconds', () => {
    expect(formatDuration(30)).toBe('30s');
  });
  it('formats minutes', () => {
    expect(formatDuration(90)).toBe('1m 30s');
  });
  it('formats whole minutes (no leftover seconds)', () => {
    expect(formatDuration(120)).toBe('2m');
  });
  it('formats hours', () => {
    expect(formatDuration(3661)).toBe('1h 1m');
  });
  it('formats whole hours (no leftover minutes)', () => {
    expect(formatDuration(7200)).toBe('2h');
  });
  it('formats days', () => {
    expect(formatDuration(90000)).toBe('1d 1h');
  });
  it('formats whole days (no leftover hours)', () => {
    expect(formatDuration(172800)).toBe('2d');
  });
  it('handles negative', () => {
    expect(formatDuration(-1)).toBe('—');
  });
  it('returns dash for null/undefined/NaN', () => {
    expect(formatDuration(null)).toBe('—');
    expect(formatDuration(undefined)).toBe('—');
    expect(formatDuration(Number.NaN)).toBe('—');
  });
});

describe('formatPercent', () => {
  it('formats with default decimals', () => {
    expect(formatPercent(75.5)).toBe('75.5%');
  });
  it('formats with 0 decimals', () => {
    expect(formatPercent(75.567, 0)).toBe('76%');
  });
  it('returns dash for null/undefined/NaN', () => {
    expect(formatPercent(null)).toBe('—');
    expect(formatPercent(undefined)).toBe('—');
    expect(formatPercent(Number.NaN)).toBe('—');
  });
});

describe('formatNumber', () => {
  it('formats small numbers', () => {
    expect(formatNumber(42)).toBe('42');
  });
  it('formats thousands', () => {
    expect(formatNumber(1500)).toBe('1.5K');
  });
  it('formats millions', () => {
    expect(formatNumber(2500000)).toBe('2.5M');
  });
  it('formats billions', () => {
    expect(formatNumber(3_000_000_000)).toBe('3.0B');
  });
  it('returns dash for null/undefined/NaN', () => {
    expect(formatNumber(null)).toBe('—');
    expect(formatNumber(undefined)).toBe('—');
    expect(formatNumber(Number.NaN)).toBe('—');
  });
});

describe('formatPps', () => {
  it('formats small pps', () => {
    expect(formatPps(500)).toBe('500 pps');
  });
  it('formats K pps', () => {
    expect(formatPps(15000)).toBe('15.0K pps');
  });
  it('formats M pps', () => {
    expect(formatPps(2000000)).toBe('2.0M pps');
  });
  it('returns dash for null/undefined/NaN', () => {
    expect(formatPps(null)).toBe('—');
    expect(formatPps(undefined)).toBe('—');
    expect(formatPps(Number.NaN)).toBe('—');
  });
});

describe('formatBps', () => {
  it('formats small bps', () => {
    expect(formatBps(100)).toBe('100 bps');
  });
  it('formats Kbps', () => {
    expect(formatBps(5000)).toBe('5.0 Kbps');
  });
  it('formats Mbps', () => {
    expect(formatBps(5_000_000)).toBe('5.0 Mbps');
  });
  it('formats Gbps', () => {
    expect(formatBps(10000000000)).toBe('10.0 Gbps');
  });
  it('returns dash for null/undefined/NaN', () => {
    expect(formatBps(null)).toBe('—');
    expect(formatBps(undefined)).toBe('—');
    expect(formatBps(Number.NaN)).toBe('—');
  });
});

describe('timeAgo', () => {
  it('shows just now for recent', () => {
    expect(timeAgo(new Date())).toBe('just now');
  });
  it('shows seconds ago', () => {
    expect(timeAgo(Date.now() - 30000)).toBe('30s ago');
  });
  it('shows minutes ago', () => {
    expect(timeAgo(Date.now() - 120000)).toBe('2m ago');
  });
  it('shows hours ago', () => {
    expect(timeAgo(Date.now() - 2 * 3600 * 1000)).toBe('2h ago');
  });
  it('shows days ago', () => {
    expect(timeAgo(Date.now() - 3 * 86400 * 1000)).toBe('3d ago');
  });
  it('handles future dates', () => {
    expect(timeAgo(Date.now() + 60000)).toBe('in future');
  });
  it('returns dash for null/undefined/invalid', () => {
    expect(timeAgo(null)).toBe('—');
    expect(timeAgo(undefined)).toBe('—');
    expect(timeAgo('not-a-date')).toBe('—');
  });
});

describe('truncate', () => {
  it('returns short strings unchanged', () => {
    expect(truncate('hello', 10)).toBe('hello');
  });
  it('truncates long strings', () => {
    expect(truncate('hello world this is long', 10)).toBe('hello wor…');
  });
  it('returns empty for null/undefined', () => {
    expect(truncate(null, 10)).toBe('');
    expect(truncate(undefined, 10)).toBe('');
  });
});

describe('stripStatePrefix', () => {
  it('strips DPU_STATE_ prefix', () => {
    expect(stripStatePrefix('DPU_STATE_UP')).toBe('UP');
    expect(stripStatePrefix('DPU_STATE_DEGRADED')).toBe('DEGRADED');
  });
  it('strips ENI_STATE_ prefix', () => {
    expect(stripStatePrefix('ENI_STATE_ENABLED')).toBe('ENABLED');
  });
  it('strips generic STATE_ prefix', () => {
    expect(stripStatePrefix('STATE_OK')).toBe('OK');
  });
  it('passes through values with no recognized prefix', () => {
    expect(stripStatePrefix('READY')).toBe('READY');
  });
  it('returns UNKNOWN for null/undefined/empty', () => {
    expect(stripStatePrefix(null)).toBe('UNKNOWN');
    expect(stripStatePrefix(undefined)).toBe('UNKNOWN');
    expect(stripStatePrefix('')).toBe('UNKNOWN');
  });
});

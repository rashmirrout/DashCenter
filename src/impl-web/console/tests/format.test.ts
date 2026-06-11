import { describe, it, expect } from 'vitest';
import {
  formatIp, formatMac, formatBytes, formatDuration,
  formatPercent, formatNumber, formatPps, formatBps,
  timeAgo, truncate,
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
});

describe('formatDuration', () => {
  it('formats seconds', () => {
    expect(formatDuration(30)).toBe('30s');
  });
  it('formats minutes', () => {
    expect(formatDuration(90)).toBe('1m 30s');
  });
  it('formats hours', () => {
    expect(formatDuration(3661)).toBe('1h 1m');
  });
  it('formats days', () => {
    expect(formatDuration(90000)).toBe('1d 1h');
  });
  it('handles negative', () => {
    expect(formatDuration(-1)).toBe('—');
  });
});

describe('formatPercent', () => {
  it('formats with default decimals', () => {
    expect(formatPercent(75.5)).toBe('75.5%');
  });
  it('formats with 0 decimals', () => {
    expect(formatPercent(75.567, 0)).toBe('76%');
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
});

describe('formatBps', () => {
  it('formats small bps', () => {
    expect(formatBps(100)).toBe('100 bps');
  });
  it('formats Kbps', () => {
    expect(formatBps(5000)).toBe('5.0 Kbps');
  });
  it('formats Gbps', () => {
    expect(formatBps(10000000000)).toBe('10.0 Gbps');
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
});

describe('truncate', () => {
  it('returns short strings unchanged', () => {
    expect(truncate('hello', 10)).toBe('hello');
  });
  it('truncates long strings', () => {
    expect(truncate('hello world this is long', 10)).toBe('hello wor…');
  });
});
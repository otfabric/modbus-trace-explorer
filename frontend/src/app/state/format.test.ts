import { describe, expect, it } from 'vitest';
import { formatBytes, formatCount, formatDuration, formatFunction, formatLatency, formatRate, formatTime } from './format';

describe('formats', () => {
  it('states a rate per second and per minute', () => {
    expect(formatRate(1541, 800e9)).toBe('1.93/s · 116/min');
    expect(formatRate(3, 600e9)).toBe('0.00333/s · 0.2/min');
    expect(formatRate(1, 600e9)).toBe('–');
    expect(formatRate(5, 0)).toBe('–');
  });

  const t = Date.UTC(2026, 9, 14, 14, 2, 11, 384) * 1e6 + 210_123;
  it('times', () => {
    expect(formatTime(t, 'utc')).toBe('14:02:11.384210');
    expect(formatTime(t, 'relative', t - 3_723_000_000_000)).toBe('+01:02:03.000000');
    expect(formatTime(t, 'local')).toMatch(/^\d\d:02:11\.384210$/);
  });
  it('numbers and units', () => {
    expect(formatCount(912340)).toBe('912,340');
    expect(formatBytes(1_932_735_283)).toBe('1.8 GiB');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatLatency(4_200_000)).toBe('4.2 ms');
    expect(formatLatency(640_000)).toBe('640 µs');
    expect(formatLatency(undefined)).toBe('');
    expect(formatDuration(9_420e9)).toBe('2 h 37 min');
    expect(formatDuration(14e9)).toBe('14 s');
    expect(formatDuration(125e9)).toBe('2 min 5 s');
  });
  it('functions', () => {
    expect(formatFunction(3)).toBe('03 Read Holding');
    expect(formatFunction(0x41)).toBe('0x41 user-defined');
    expect(formatFunction(undefined)).toBe('');
  });
});

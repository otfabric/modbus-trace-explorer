import { describe, expect, it } from 'vitest';
import { compressedAxis, formatValue } from './AddressesView';

const iv = (start: number, end: number) => ({ start, end, count: 1, first_ns: 0, last_ns: 0 });

describe('compressedAxis', () => {
  it('collapses wide empty stretches and keeps near ranges together', () => {
    const { x, spans } = compressedAxis([iv(4096, 4157), iv(100, 100), iv(4160, 4221)], 1000, 10, 64);
    expect(spans).toEqual([
      { start: 100, end: 100 },
      { start: 4096, end: 4221 },
    ]);
    // 127 covered addresses share 990 units; the gap between the spans is 10.
    const unit = 990 / 127;
    expect(x(100)).toBe(0);
    expect(x(101)).toBeCloseTo(unit);
    expect(x(4096)).toBeCloseTo(unit + 10);
    expect(x(4222)).toBeCloseTo(1000);
    expect(x(4000)).toBeCloseTo(unit, 5);
  });

  it('handles nothing and one address', () => {
    expect(compressedAxis([], 100).spans).toEqual([]);
    const one = compressedAxis([iv(7, 7)], 100);
    expect(one.x(7)).toBe(0);
    expect(one.x(8)).toBe(100);
  });
});

describe('formatValue', () => {
  it('formats words as decimal, hex and binary, and bits as 0 or 1', () => {
    expect(formatValue(4660, 'dec', false)).toBe('4660');
    expect(formatValue(4660, 'hex', false)).toBe('0x1234');
    expect(formatValue(4660, 'bin', false)).toBe('0001 0010 0011 0100');
    expect(formatValue(1, 'hex', true)).toBe('1');
  });
});

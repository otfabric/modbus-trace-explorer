import { describe, expect, it } from 'vitest';
import { MAX_SPACER, maxTop, rankAt, reveal, scaled, scrollFor, spacerHeight, visibleRows } from './scroll';

const g = (total: number, viewport = 600, rowHeight = 24) => ({ total, rowHeight, viewport });

describe('scaled scroll', () => {
  it('is exact while the rows fit', () => {
    const s = g(10_000);
    expect(scaled(s)).toBe(false);
    expect(spacerHeight(s)).toBe(240_000);
    expect(rankAt(s, 24 * 123 + 5)).toBe(123);
    expect(scrollFor(s, 123)).toBe(24 * 123);
    expect(rankAt(s, 1e9)).toBe(maxTop(s));
  });

  it('scales ten million rows into the spacer, both ends reachable', () => {
    const s = g(10_000_000);
    expect(scaled(s)).toBe(true);
    expect(spacerHeight(s)).toBe(MAX_SPACER);
    expect(rankAt(s, 0)).toBe(0);
    const end = spacerHeight(s) - s.viewport;
    expect(rankAt(s, end)).toBe(maxTop(s));
    // Round trip at any rank lands within a row of it.
    for (const r of [1, 777, 5_000_000, 9_999_000, maxTop(s)]) {
      expect(Math.abs(rankAt(s, scrollFor(s, r)) - r)).toBeLessThanOrEqual(1);
    }
  });

  it('handles empty and tiny sets', () => {
    expect(rankAt(g(0), 100)).toBe(0);
    expect(scrollFor(g(3), 2)).toBe(0);
    expect(visibleRows(g(10, 5))).toBe(1);
  });

  it('reveals a row with the least movement', () => {
    const s = g(1000); // 25 rows visible
    expect(reveal(s, 100, 110)).toBe(100);
    expect(reveal(s, 100, 99)).toBe(99);
    expect(reveal(s, 100, 125)).toBe(101);
    expect(reveal(s, 100, 5000)).toBe(maxTop(s));
  });
});

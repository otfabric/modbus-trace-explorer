import { describe, expect, it } from 'vitest';
import { generate, MockService } from './mock';

describe('MockService', () => {
  const svc = new MockService(10_000);

  it('pages the whole timeline by rank, in time order', async () => {
    const a = await svc.timeline({ filter: {}, order: 'time', desc: false, offset: 0, limit: 50 });
    const b = await svc.timeline({ filter: {}, order: 'time', desc: false, offset: 50, limit: 50 });
    expect(a.total).toBe(10_000);
    expect(a.rows).toHaveLength(50);
    expect(b.rows[0]?.seq).toBe(51);
    const times = [...a.rows, ...b.rows].map((r) => r.t_ns);
    expect([...times].sort((x, y) => x - y)).toEqual(times);
  });

  it('filters and counts', async () => {
    const page = await svc.timeline({ filter: { classes: ['write'] }, order: 'time', desc: false, offset: 0, limit: 1000 });
    expect(page.total).toBeGreaterThan(0);
    expect(page.total).toBeLessThan(10_000);
    expect(page.rows.every((r) => r.class === 'write')).toBe(true);
  });

  it('orders by latency, both ways', async () => {
    const q = { filter: { statuses: ['ok' as const] }, order: 'latency' as const, offset: 0, limit: 20 };
    const up = await svc.timeline({ ...q, desc: false });
    const down = await svc.timeline({ ...q, desc: true });
    const lat = up.rows.map((r) => r.latency_ns ?? 0);
    expect([...lat].sort((x, y) => x - y)).toEqual(lat);
    expect(down.rows[0]?.latency_ns ?? 0).toBeGreaterThanOrEqual(up.rows[19]?.latency_ns ?? 0);
  });

  it('clamps the window and honors abort', async () => {
    const page = await svc.timeline({ filter: {}, order: 'time', desc: false, offset: 99_999, limit: 10 });
    expect(page.offset).toBe(9_999);
    expect(page.rows).toHaveLength(1);
    const ctl = new AbortController();
    ctl.abort();
    await expect(svc.timeline({ filter: {}, order: 'time', desc: false, offset: 0, limit: 1 }, ctl.signal)).rejects.toThrow();
  });

  it('is deterministic', () => {
    expect(generate(12345)).toEqual(generate(12345));
  });
});

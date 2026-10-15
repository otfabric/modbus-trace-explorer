import { describe, expect, it } from 'vitest';
import { generate, MockService } from './mock';
import type { ExplorerEvent } from './types';

describe('MockService', () => {
  const svc = new MockService({ rows: 10_000, analyzed: true, step: 0 });

  it('pages the whole timeline by rank, in time order', async () => {
    const a = await svc.timeline('mock', { filter: {}, order: 'time', desc: false, offset: 0, limit: 50 });
    const b = await svc.timeline('mock', { filter: {}, order: 'time', desc: false, offset: 50, limit: 50 });
    expect(a.total).toBe(10_000);
    expect(a.rows).toHaveLength(50);
    expect(b.rows[0]?.seq).toBe(51);
    const times = [...a.rows, ...b.rows].map((r) => r.t_ns);
    expect([...times].sort((x, y) => x - y)).toEqual(times);
  });

  it('filters and counts', async () => {
    const page = await svc.timeline('mock', { filter: { classes: ['write'] }, order: 'time', desc: false, offset: 0, limit: 1000 });
    expect(page.total).toBeGreaterThan(0);
    expect(page.total).toBeLessThan(10_000);
    expect(page.rows.every((r) => r.class === 'write')).toBe(true);
  });

  it('orders by latency, both ways', async () => {
    const q = { filter: { statuses: ['ok' as const] }, order: 'latency' as const, offset: 0, limit: 20 };
    const up = await svc.timeline('mock', { ...q, desc: false });
    const down = await svc.timeline('mock', { ...q, desc: true });
    const lat = up.rows.map((r) => r.latency_ns ?? 0);
    expect([...lat].sort((x, y) => x - y)).toEqual(lat);
    expect(down.rows[0]?.latency_ns ?? 0).toBeGreaterThanOrEqual(up.rows[19]?.latency_ns ?? 0);
  });

  it('clamps the window and honors abort', async () => {
    const page = await svc.timeline('mock', { filter: {}, order: 'time', desc: false, offset: 99_999, limit: 10 });
    expect(page.offset).toBe(9_999);
    expect(page.rows).toHaveLength(1);
    const ctl = new AbortController();
    ctl.abort();
    await expect(svc.timeline('mock', { filter: {}, order: 'time', desc: false, offset: 0, limit: 1 }, ctl.signal)).rejects.toThrow();
  });

  it('runs jobs with progress, and cancels them', async () => {
    const fresh = new MockService({ rows: 100, step: 0 });
    const events: ExplorerEvent[] = [];
    fresh.subscribe((e) => events.push(e));
    await expect(fresh.overview()).rejects.toThrow();
    const ref = await fresh.analyze('mock', { flows: [1], server_ports: [502], servers: [] });
    await new Promise((r) => setTimeout(r, 50));
    expect(events.at(-1)?.job).toMatchObject({ id: ref.id, state: 'done' });
    expect(events.some((e) => e.job?.state === 'running')).toBe(true);
    await expect(fresh.overview()).resolves.toMatchObject({ transactions: 100 });
    const slow = new MockService({ rows: 100, step: 1000 });
    const seen: ExplorerEvent[] = [];
    slow.subscribe((e) => seen.push(e));
    const job = await slow.scan();
    await slow.cancelJob(job.id);
    expect(seen.at(-1)?.job?.state).toBe('canceled');
  });

  it('parses a subset of the filter grammar and reports bad terms', async () => {
    const res = await svc.parseFilter('mock', 'unit:1,2 fc:3 -flag:retransmitted color:red -unit:1');
    expect(res.filter).toMatchObject({ units: [1, 2], functions: [3], not_flags: ['retransmitted'] });
    expect(res.errors.map((e) => e.term)).toEqual(['color:red', '-unit:1']);
  });

  it('describes a row and is deterministic', async () => {
    const d = await svc.detail('mock', 2);
    expect(d.request?.fields.length).toBeGreaterThan(4);
    expect(generate(12345)).toEqual(generate(12345));
  });
});

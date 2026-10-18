import { describe, expect, it } from 'vitest';
import { loadSchema, validate } from '../../test/schema';
import { MockService } from './mock';
import type { Service } from './service';
import type { ExplorerEvent, JobRef } from './types';

/** v must validate against docs/schema/<name>-1.schema.json, the engine's own contract (A17). */
function check(name: string, v: unknown) {
  expect(validate(loadSchema(name), JSON.parse(JSON.stringify(v))).join('\n'), name).toBe('');
}

/** Waits for the end of a mock job and returns its events. */
function finish(m: Service, ref: JobRef): Promise<ExplorerEvent[]> {
  return new Promise((resolve) => {
    const seen: ExplorerEvent[] = [];
    const off = m.subscribe((ev) => {
      seen.push(ev);
      const done = (ev.job?.id === ref.id && ev.job.state !== 'running') || (ev.history?.job === ref.id && ev.history.done);
      if (done) {
        off();
        resolve(seen);
      }
    });
  });
}

// The mock stands in for the engine in every UI test; its answers must have
// the engine's shapes, or the UI could be built against data the engine never sends.
describe('the mock Service keeps the engine contract (A17)', () => {
  it('answers every call with data that validates against the DTO schemas', async () => {
    const m: Service = new MockService({ rows: 3000, step: 1 });
    check('about', await m.about());
    const info = await m.open(['/traces/site.pcapng']);
    check('file-info', info);
    check('recent-files', await m.recent());
    const scan = await m.scan(info.id);
    for (const ev of await finish(m, scan)) check('event', ev);
    check('scan-result', await m.scanResult(info.id));
    const cfg = { flows: [1, 2], server_ports: [502, 1502], servers: [] };
    check('analyze-config', cfg);
    const analyze = await m.analyze(info.id, cfg);
    for (const ev of await finish(m, analyze)) check('event', ev);
    check('overview', await m.overview(info.id));
    const q = { filter: { units: [1] }, order: 'time' as const, desc: false, offset: 0, limit: 50 };
    check('timeline-query', q);
    const page = await m.timeline(info.id, q);
    check('timeline-page', page);
    for (const r of page.rows.slice(0, 10)) check('detail', await m.detail(info.id, r.seq));
    check('filter-parse', await m.parseFilter(info.id, 'unit:1 fc:3 from:+1m color:red'));
    check('devices', await m.devices(info.id, {}));
    check('connections', await m.connections(info.id, {}));
    check('patterns', await m.patterns(info.id, {}));
    const cq = { server: '10.0.3.21:502', unit: 1, space: 'holding', filter: {} };
    check('coverage-query', cq);
    check('coverage-map', await m.coverage(info.id, cq));
    const hq = { server: '10.0.3.21:502', unit: 1, space: 'holding', address: 4096, after: 0, limit: 0, every: false };
    check('history-query', hq);
    const hist = await m.addressHistory(info.id, hq);
    for (const ev of await finish(m, hist)) check('event', ev);
    check('quality', await m.quality(info.id));
    const lq = { filter: {}, order: 'time' as const, desc: false, t_ns: page.rows[3]?.t_ns ?? 0 };
    check('locate-query', lq);
    check('location', await m.locate(info.id, lq));
    const mx = { filter: {}, exceptions: true, change_only: false, tick_ns: 0, format: 'mbcap' as const, labels: { '10.0.3.21:502/1': 'Meter' } };
    check('mbcap-export', mx);
    check('mbcap-preview', await m.previewMBCAP(info.id, mx));
    const ex = await m.exportMBCAP(info.id, mx, '/out/x.mbcap');
    for (const ev of await finish(m, ex)) check('event', ev);
    check('export-result', await m.exportResult(ex.id));
    const tx = { filter: {}, format: 'csv' as const };
    check('timeline-export', tx);
    const te = await m.exportTimeline(info.id, tx, '/out/x.csv');
    await finish(m, te);
    check('export-result', await m.exportResult(te.id));
    check('subtrace-export', { filter: { conns: [1] } });
    const se = await m.exportSubtrace(info.id, { filter: { conns: [1] } }, '/out/x.pcapng');
    await finish(m, se);
    check('export-result', await m.exportResult(se.id));
    check('settings', await m.settings());
    check('cache-info', await m.cacheInfo());
  });

  it('answers for an MBCAP capture with data that validates (R24)', async () => {
    const m: Service = new MockService({ rows: 500, step: 1 });
    const info = await m.open(['/captures/site.mbcap']);
    check('file-info', info);
    expect(info.kind).toBe('capture');
    const ref = await m.importCapture(info.id);
    for (const ev of await finish(m, ref)) check('event', ev);
    const ov = await m.overview(info.id);
    check('overview', ov);
    expect(ov.capture?.samples).toBe(500);
  });

  it('rejects what the schemas forbid', () => {
    const s = loadSchema('location');
    expect(validate(s, { found: true, rank: 1, seq: 2, exact: true })).toEqual([]);
    expect(validate(s, { found: 'yes', rank: -1, seq: 2, exact: true, extra: 1 })).toEqual(['$.found: string, want boolean', '$.rank: -1 < 0', '$: unexpected extra']);
    expect(validate(s, { rank: 1, seq: 2, exact: true })).toEqual(['$: missing found']);
  });
});

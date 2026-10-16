import { ServiceError, type Service } from './service';
import type {
  About,
  ConnStats,
  CoverageMap,
  CoverageQuery,
  DeviceStats,
  Distribution,
  HistoryPoint,
  HistoryQuery,
  Pattern,
  Quality,
  AnalyzeConfig,
  Detail,
  ExplorerEvent,
  FileInfo,
  Filter,
  FilterParse,
  Flow,
  FunctionClass,
  JobRef,
  JobState,
  Overview,
  RecentFile,
  Row,
  ScanResult,
  Status,
  TimelinePage,
  TimelineQuery,
} from './types';

// An in-memory Service over a generated trace, for building and testing the
// user interface without the engine (ARCHITECTURE.md section 11). Every row
// is computed from its index, so a dataset of ten million rows costs no
// memory until a filter or an ordering needs an index list. Jobs advance on
// timers so the import screens can be seen and tested.

const START_NS = Date.UTC(2026, 9, 14, 12, 2, 11, 384) * 1e6;
const STEP_NS = 10_000_000; // 100 rows a second

interface Device {
  server: string;
  unit: number;
}

const DEVICES: Device[] = [
  { server: '10.0.3.21:502', unit: 1 },
  { server: '10.0.3.21:502', unit: 2 },
  { server: '10.0.3.22:1502', unit: 1 },
  { server: '10.0.3.30:502', unit: 255 },
];
const CLIENTS = ['10.0.3.5', '10.0.3.6'];

const EXCEPTIONS: Record<number, string> = { 1: 'Illegal function', 2: 'Illegal data address', 3: 'Illegal data value', 4: 'Server device failure' };

/** A small integer hash, so the same index always gives the same row. */
function mix(i: number, salt: number): number {
  let h = Math.imul(i ^ salt, 0x9e3779b1);
  h ^= h >>> 15;
  h = Math.imul(h, 0x85ebca6b);
  h ^= h >>> 13;
  return h >>> 0;
}

function hex(n: number, width = 4): string {
  return '0x' + n.toString(16).toUpperCase().padStart(width, '0');
}

interface Generated extends Row {
  dev: number;
  space?: string;
  start?: number;
  count?: number;
}

/** The row at index i of the generated trace, in time order. */
export function generate(i: number): Generated {
  const t_ns = START_NS + i * STEP_NS + (mix(i, 1) % 4_000_000);
  const dev = mix(i, 2) % DEVICES.length;
  const device = DEVICES[dev] as Device;
  const client = CLIENTS[dev % 2] as string;
  const conn = dev + 1;
  const base = { seq: i + 1, t_ns, conn, client, server: device.server, dev, flags: [] as string[], result: '' };
  if (i % 50_000 === 0) {
    return { ...base, kind: 'conn_open', summary: 'Connection opened (SYN)' };
  }
  if (i % 77_777 === 7) {
    return { ...base, kind: 'gap', summary: 'Gap: 1460 bytes missing (server to client, loss)' };
  }
  const r = mix(i, 3) % 1000;
  const addr = 4096 + (mix(i, 4) % 8) * 64;
  let fc = 3;
  let cls: FunctionClass = 'read';
  let summary = `Read 62 holding registers @ ${addr} (${hex(addr)})`;
  let result = '62 words';
  let space = 'holding';
  let start = addr;
  let count = 62;
  if (r < 150) {
    fc = 4;
    summary = `Read 2 input registers @ ${addr - 4096}`;
    result = '2 words';
    space = 'input';
    start = addr - 4096;
    count = 2;
  } else if (r < 170) {
    fc = 1;
    summary = 'Read 16 coils @ 0';
    result = '16 bits';
    space = 'coil';
    start = 0;
    count = 16;
  } else if (r < 178) {
    fc = 6;
    cls = 'write';
    const v = mix(i, 5) % 100;
    summary = `Write register @ 100 = ${hex(v)} (${v})`;
    result = 'OK';
    start = 100;
    count = 1;
  } else if (r < 181) {
    fc = 16;
    cls = 'write';
    summary = 'Write 2 registers @ 100';
    result = 'OK';
    start = 100;
    count = 2;
  } else if (r < 182) {
    fc = 43;
    summary = 'Read device identification (basic, from object 0)';
    result = 'OK';
    space = '';
    count = 0;
  }
  let status: Status = 'ok';
  let exception: number | undefined;
  const s = mix(i, 6) % 1000;
  if (s < 6) {
    status = 'exception';
    exception = 2;
    result = `Exception 02 ${EXCEPTIONS[2]}`;
  } else if (s < 8) {
    status = 'unanswered';
    result = 'No response observed';
  } else if (s < 9) {
    status = 'uncertain';
    result = 'Uncertain (gap)';
  }
  const flags: string[] = [];
  if (mix(i, 7) % 200 === 0) {
    flags.push('retransmitted');
  }
  const latency_ns = status === 'unanswered' ? undefined : 600_000 + (mix(i, 8) % 9_000_000);
  return {
    ...base,
    kind: 'txn',
    unit: device.unit,
    tid: i & 0xffff,
    fc,
    class: cls,
    status,
    exception,
    latency_ns,
    flags,
    summary,
    result,
    space,
    start,
    count,
  };
}

const TXN_TERMS: (keyof Filter)[] = ['units', 'functions', 'statuses', 'classes', 'exceptions', 'flags', 'not_flags', 'summary', 'space', 'start', 'end'];

function matches(row: Generated, f: Filter): boolean {
  if (f.servers?.length && !f.servers.some((s) => row.server === s || row.server.startsWith(s + ':'))) return false;
  if (f.clients?.length && !f.clients.includes(row.client)) return false;
  if (f.conns?.length && !f.conns.includes(row.conn)) return false;
  if (f.from_ns !== undefined && row.t_ns < f.from_ns) return false;
  if (f.to_ns !== undefined && row.t_ns >= f.to_ns) return false;
  const txnTerms = TXN_TERMS.some((k) => {
    const v = f[k];
    return v !== undefined && (!Array.isArray(v) || v.length > 0);
  });
  if (!txnTerms) return true;
  if (row.kind !== 'txn') return false;
  if (f.units?.length && (row.unit === undefined || !f.units.includes(row.unit))) return false;
  if (f.functions?.length && (row.fc === undefined || !f.functions.includes(row.fc))) return false;
  if (f.statuses?.length && (row.status === undefined || !f.statuses.includes(row.status))) return false;
  if (f.classes?.length && (row.class === undefined || !f.classes.includes(row.class))) return false;
  if (f.exceptions?.length && (row.exception === undefined || !f.exceptions.includes(row.exception))) return false;
  if (f.flags?.length && !f.flags.every((x) => row.flags.includes(x))) return false;
  if (f.not_flags?.length && f.not_flags.some((x) => row.flags.includes(x))) return false;
  if (f.summary !== undefined && row.summary !== f.summary) return false;
  if (f.space && row.space !== f.space) return false;
  if (f.start !== undefined || f.end !== undefined) {
    if (!row.count) return false;
    const lo = row.start ?? 0;
    const hi = lo + (row.count ?? 1) - 1;
    if (hi < (f.start ?? 0) || lo > (f.end ?? 65535)) return false;
  }
  return true;
}

const isEmpty = (f: Filter) => Object.values(f).every((v) => v === undefined || (Array.isArray(v) && v.length === 0));

function strip({ dev: _dev, space: _space, start: _start, count: _count, ...row }: Generated): Row {
  return row;
}

export interface MockOptions {
  /** Rows of the generated trace. */
  rows?: number;
  /** The trace opens analysed (the cache has it), skipping the import. */
  analyzed?: boolean;
  /** Milliseconds per progress step of a job (0 in tests). */
  step?: number;
}

export class MockService implements Service {
  private readonly rows: number;
  private readonly step: number;
  private analyzed: boolean;
  private cacheKey = '';
  private cache: Int32Array | null = null;
  private listeners = new Set<(e: ExplorerEvent) => void>();
  private jobs = new Map<string, () => void>();
  private nextJob = 0;
  private paths: string[] = ['/traces/site-2026-10-14.pcapng'];

  constructor(opts: MockOptions = {}) {
    this.rows = opts.rows ?? 1_000_000;
    this.analyzed = opts.analyzed ?? false;
    this.step = opts.step ?? 120;
  }

  async about(): Promise<About> {
    return { version: 'mock', go_modbus_trace: 'mock', wails: 'none', go: 'none', os: 'browser', arch: 'none' };
  }

  async open(paths: string[]): Promise<FileInfo> {
    if (paths.length === 0) throw new ServiceError('invalid_argument', 'no trace file');
    this.paths = paths;
    const name = (paths[0] ?? '').split('/').pop() ?? '';
    return {
      id: 'mock',
      paths,
      name: paths.length > 1 ? `${name} + ${paths.length - 1} more` : name,
      size: this.rows * 190,
      gzip: name.endsWith('.gz'),
      analyzed: this.analyzed,
      choices: this.analyzed ? { flows: [1, 2], server_ports: [502, 1502], servers: [] } : undefined,
    };
  }

  async close(): Promise<void> {}

  async recent(): Promise<RecentFile[]> {
    return [
      { paths: ['/traces/site-2026-10-14.pcapng'], name: 'site-2026-10-14.pcapng', exists: true },
      { paths: ['/traces/old.pcap'], name: 'old.pcap', exists: false },
    ];
  }

  subscribe(onEvent: (e: ExplorerEvent) => void): () => void {
    this.listeners.add(onEvent);
    return () => this.listeners.delete(onEvent);
  }

  private emit(job: JobState) {
    for (const l of this.listeners) l({ kind: 'job', job });
  }

  private run(kind: 'scan' | 'analyze', phases: string[], then: () => void): JobRef {
    const id = `${kind}-${++this.nextJob}`;
    const total = this.rows * 190;
    let k = 0;
    let stopped = false;
    const steps = 10;
    const tick = () => {
      if (stopped) return;
      k++;
      const read = Math.min(total, Math.round((total * k) / steps));
      const phase = phases[Math.min(phases.length - 1, Math.floor((k * phases.length) / (steps + 1)))] ?? kind;
      if (k <= steps) {
        this.emit({ id, kind, file: 'mock', state: 'running', phase, read, total, rows: kind === 'analyze' ? Math.round((this.rows * k) / steps) : 0, rate: 180e6 });
        setTimeout(tick, this.step);
        return;
      }
      this.jobs.delete(id);
      then();
      this.emit({ id, kind, file: 'mock', state: 'done', phase, read: total, total, rows: kind === 'analyze' ? this.rows : 0, rate: 180e6 });
    };
    this.jobs.set(id, () => {
      stopped = true;
      this.jobs.delete(id);
      this.emit({ id, kind, file: 'mock', state: 'canceled', phase: kind, read: 0, total, rows: 0, rate: 0 });
    });
    setTimeout(tick, this.step);
    return { id, kind };
  }

  async scan(): Promise<JobRef> {
    return this.run('scan', ['scan'], () => {});
  }

  async scanResult(): Promise<ScanResult> {
    const flow = (id: number, a: string, b: string, cls: Flow['class'], score: number, port?: number): Flow => ({
      id, a, b, role_source: 'syn', role_guessed: false, role_confidence: 1, open: 'syn', close: 'fin',
      packets: [this.rows, this.rows], bytes: [this.rows * 12, this.rows * 80], class: cls, score,
      adus: cls === 'modbus_tcp' ? [this.rows / 2, this.rows / 2] : [0, 0], server_port: port, gaps: 0,
    });
    return {
      summary: {
        file: this.paths[0]?.split('/').pop() ?? '', format: 'pcapng', gzip: false, input_bytes: this.rows * 190, bytes: this.rows * 170,
        packets: this.rows * 2 + 40, interfaces: [{ id: 0, part: 0, link: 'ethernet', snaplen: 262144, name: 'eth0', packets: this.rows * 2 }],
        connections: { total: 4, midstream: 0, evicted: 0 }, quality: {}, unsupported: { not_tcp: 12 }, sha256: '',
      },
      flows: [
        flow(1, '10.0.3.5:49822', '10.0.3.21:502', 'modbus_tcp', 0.99, 502),
        flow(2, '10.0.3.6:49830', '10.0.3.22:1502', 'modbus_tcp', 0.98, 1502),
        flow(3, '10.0.9.2:51133', '10.0.9.8:802', 'tls', 0),
        flow(4, '10.0.3.5:55012', '10.0.3.1:22', 'other_tcp', 0),
      ],
      server_ports: [502, 1502],
      selected: [1, 2],
      one_click: false,
    };
  }

  async analyze(_fileId: string, _cfg: AnalyzeConfig): Promise<JobRef> {
    return this.run('analyze', ['analyze', 'index'], () => {
      this.analyzed = true;
    });
  }

  async cancelJob(jobId: string): Promise<void> {
    this.jobs.get(jobId)?.();
  }

  async overview(): Promise<Overview> {
    if (!this.analyzed) throw new ServiceError('not_found', 'the trace is not analysed yet');
    return {
      file: this.paths[0]?.split('/').pop() ?? '',
      size: this.rows * 190,
      first_ns: START_NS,
      last_ns: START_NS + this.rows * STEP_NS,
      packets: this.rows * 2 + 40,
      connections: DEVICES.length,
      devices: DEVICES.length,
      transactions: this.rows,
      rows: this.rows,
      quality: 'warnings',
      quality_count: 3,
      trace_missing: false,
    };
  }

  /** The indexes of the rows that pass f in the order asked, or null for "all, in time order". */
  private selection(q: TimelineQuery): Int32Array | null {
    if (isEmpty(q.filter) && q.order === 'time') {
      return null;
    }
    const key = JSON.stringify([q.filter, q.order]);
    if (key !== this.cacheKey || !this.cache) {
      const out = new Int32Array(this.rows);
      let n = 0;
      for (let i = 0; i < this.rows; i++) {
        const r = generate(i);
        if (matches(r, q.filter) && (q.order !== 'latency' || r.latency_ns !== undefined)) {
          out[n++] = i;
        }
      }
      let sel = out.slice(0, n);
      if (q.order === 'latency') {
        const lat = (i: number) => generate(i).latency_ns ?? 0;
        sel = Int32Array.from(Array.from(sel).sort((a, b) => lat(a) - lat(b) || a - b));
      }
      this.cacheKey = key;
      this.cache = sel;
    }
    return this.cache;
  }

  async timeline(_fileId: string, q: TimelineQuery, signal?: AbortSignal): Promise<TimelinePage> {
    signal?.throwIfAborted();
    const sel = this.selection(q);
    const total = sel ? sel.length : this.rows;
    const offset = Math.max(0, Math.min(q.offset, Math.max(0, total - 1)));
    const end = Math.min(total, offset + Math.max(0, q.limit));
    const rows: Row[] = [];
    for (let k = offset; k < end; k++) {
      const rank = q.desc ? total - 1 - k : k;
      rows.push(strip(generate(sel ? (sel[rank] as number) : rank)));
    }
    return { offset, rows, total };
  }

  async detail(_fileId: string, seq: number): Promise<Detail> {
    const g = generate(seq - 1);
    const row = strip(g);
    if (row.kind !== 'txn') {
      return { row, text: row.summary, notes: [] };
    }
    const tid = row.tid ?? 0;
    const unit = row.unit ?? 1;
    const start = g.start ?? 0;
    const count = g.count ?? 1;
    const req = [tid >> 8, tid & 255, 0, 0, 0, 6, unit, row.fc ?? 3, start >> 8, start & 255, count >> 8, count & 255];
    const toHex = (b: number[]) => b.map((x) => x.toString(16).padStart(2, '0')).join('');
    const packet = (frame: number) => ({ frame, part: 0, t_ns: row.t_ns, interface: 0, offset: frame * 120, payload_offset: 54, payload_length: 12, display_filter: `frame.number == ${frame}` });
    return {
      row,
      request: {
        hex: toHex(req),
        name: 'Read Holding Registers',
        fields: [
          { name: 'transaction_id', offset: 0, length: 2, value: tid },
          { name: 'protocol_id', offset: 2, length: 2, value: 0 },
          { name: 'length', offset: 4, length: 2, value: 6 },
          { name: 'unit_id', offset: 6, length: 1, value: unit },
          { name: 'function', offset: 7, length: 1, value: row.fc },
          { name: 'starting_address', offset: 8, length: 2, value: start },
          { name: 'quantity', offset: 10, length: 2, value: count },
        ],
        warnings: [],
        packets: [packet(2 * seq + 1)],
      },
      response: { hex: '', name: 'Read Holding Registers', fields: [], warnings: [], packets: [packet(2 * seq + 2)] },
      text: `${row.client} asked ${row.server} unit ${unit}: ${row.summary.toLowerCase()}. ${row.result}.`,
      notes: row.flags.map((f) => ({ kind: 'flag' as const, code: f, meaning: 'Some of its bytes were seen more than once; the first copy was used.' })),
      conn: { id: row.conn, client: `${row.client}:49822`, server: row.server, open: 'syn', close: 'fin', role_source: 'syn', role_confidence: 1, transactions: 1000 },
      mbcap: row.class === 'write' ? 'Not exported: writes are not part of replay data.' : 'Will be exported to MBCAP.',
    };
  }

  /** The rows the views aggregate: the first 100,000 that pass the filter, so the mock stays quick. */
  private sample(f: Filter): Generated[] {
    const out: Generated[] = [];
    for (let i = 0; i < this.rows && out.length < 100_000; i++) {
      const r = generate(i);
      if (matches(r, f)) out.push(r);
    }
    return out;
  }

  async devices(_fileId: string, f: Filter): Promise<DeviceStats[]> {
    const by = new Map<string, { s: DeviceStats; clients: Set<string>; lat: number[] }>();
    for (const r of this.sample(f)) {
      if (r.kind !== 'txn') continue;
      const key = `${r.server}/${r.unit}`;
      let a = by.get(key);
      if (!a) {
        a = { s: { server: r.server, unit: r.unit ?? 0, transactions: 0, by_function: {}, by_status: {}, reads: 0, writes: 0, exceptions: 0, unanswered: 0, clients: [], first_ns: r.t_ns, last_ns: r.t_ns, rate: 0, bytes: 0 }, clients: new Set(), lat: [] };
        by.set(key, a);
      }
      const d = a.s;
      d.transactions++;
      d.by_function[String(r.fc)] = (d.by_function[String(r.fc)] ?? 0) + 1;
      d.by_status[r.status ?? 'ok'] = (d.by_status[r.status ?? 'ok'] ?? 0) + 1;
      if (r.class === 'write') d.writes++;
      else d.reads++;
      if (r.status === 'exception') d.exceptions++;
      if (r.status === 'unanswered') d.unanswered++;
      a.clients.add(r.client);
      d.first_ns = Math.min(d.first_ns, r.t_ns);
      d.last_ns = Math.max(d.last_ns, r.t_ns);
      d.bytes += 140;
      if (r.latency_ns !== undefined) a.lat.push(r.latency_ns);
    }
    return [...by.values()]
      .map((a) => ({ ...a.s, clients: [...a.clients].sort(), rate: a.s.transactions / Math.max(1, (a.s.last_ns - a.s.first_ns) / 1e9), latency: dist(a.lat) }))
      .sort((x, y) => x.server.localeCompare(y.server) || x.unit - y.unit);
  }

  async connections(_fileId: string, f: Filter): Promise<ConnStats[]> {
    const by = new Map<number, ConnStats>();
    for (const r of this.sample(f)) {
      let c = by.get(r.conn);
      if (!c) {
        c = { id: r.conn, client: `${r.client}:${49820 + r.conn}`, server: r.server, open: 'syn', close: 'end_of_trace', role_source: 'syn', role_confidence: 1, role_guessed: false, first_ns: r.t_ns, last_ns: r.t_ns, transactions: 0, bytes: 0, gaps: 0, resyncs: 0, flags: [] };
        by.set(r.conn, c);
      }
      c.last_ns = Math.max(c.last_ns, r.t_ns);
      if (r.kind === 'txn') {
        c.transactions++;
        c.bytes += 140;
        for (const fl of r.flags) if (!c.flags.includes(fl)) c.flags.push(fl);
      }
      if (r.kind === 'gap') c.gaps++;
    }
    return [...by.values()].sort((a, b) => a.id - b.id);
  }

  async patterns(_fileId: string, f: Filter): Promise<Pattern[]> {
    const by = new Map<string, { p: Pattern; times: number[]; last?: string }>();
    for (const r of this.sample(f)) {
      if (r.kind !== 'txn') continue;
      const key = `${r.client}|${r.server}|${r.unit}|${r.summary}`;
      let a = by.get(key);
      if (!a) {
        a = { p: { client: r.client, server: r.server, unit: r.unit ?? 0, function: r.fc ?? 0, summary: r.summary, space: r.space, start: r.start ?? 0, count: r.count ?? 0, requests: 0, first_ns: r.t_ns, last_ns: r.t_ns, jitter_ns: 0, responses: 0, changed: 0, volatility: 0, volatility_class: 'static' }, times: [] };
        by.set(key, a);
      }
      a.p.requests++;
      a.p.last_ns = r.t_ns;
      a.times.push(r.t_ns);
      if (r.status === 'ok' || r.status === 'exception') {
        const answer = `${mix(r.seq, 9) % 5}`;
        if (a.last !== undefined && a.last !== answer) a.p.changed++;
        a.p.responses++;
        a.last = answer;
      }
    }
    return [...by.values()]
      .map(({ p, times }) => {
        const gaps = times.slice(1).map((t, k) => t - (times[k] as number));
        const interval = dist(gaps);
        const volatility = p.responses > 1 ? p.changed / (p.responses - 1) : 0;
        const volatility_class: Pattern['volatility_class'] = p.changed === 0 ? 'static' : volatility < 0.2 ? 'slow' : 'noisy';
        const jitter_ns = interval ? Math.round(interval.p50_ns / 20) : 0;
        return { ...p, interval, volatility, volatility_class, jitter_ns };
      })
      .sort((a, b) => b.requests - a.requests);
  }

  async coverage(_fileId: string, q: CoverageQuery): Promise<CoverageMap> {
    const spaces = new Set<string>();
    const read = new Map<number, { start: number; end: number; count: number; first_ns: number; last_ns: number }>();
    const written = new Map<number, { start: number; end: number; count: number; first_ns: number; last_ns: number }>();
    for (const r of this.sample({ ...q.filter, servers: [q.server], units: [q.unit] })) {
      if (r.kind !== 'txn' || !r.space || !r.count) continue;
      spaces.add(r.space);
      if (r.space !== q.space) continue;
      const list = r.class === 'write' ? written : read;
      const start = r.start ?? 0;
      const iv = list.get(start) ?? { start, end: start + r.count - 1, count: 0, first_ns: r.t_ns, last_ns: r.t_ns };
      iv.count++;
      iv.last_ns = r.t_ns;
      list.set(start, iv);
    }
    const sorted = (m: Map<number, { start: number; end: number; count: number; first_ns: number; last_ns: number }>) => [...m.values()].sort((a, b) => a.start - b.start);
    return { server: q.server, unit: q.unit, space: q.space, spaces: [...spaces].sort(), read: sorted(read), written: sorted(written) };
  }

  async addressHistory(_fileId: string, q: HistoryQuery): Promise<JobRef> {
    const id = `history-${++this.nextJob}`;
    const total = 3000;
    const limit = q.limit > 0 ? q.limit : 200_000;
    let k = q.after;
    let value = 1000;
    let stopped = false;
    const tick = () => {
      if (stopped) return;
      const points: HistoryPoint[] = [];
      while (k < total && points.length < 500 && k - q.after < limit) {
        const write = k % 400 === 399;
        const next = write ? 5000 : value + (mix(k, 11) % 10 === 0 ? (mix(k, 12) % 7) - 3 : 0);
        const changed = !write && next !== value;
        if (q.every || changed || write || k === q.after) {
          points.push({ t_ns: START_NS + k * 1e9, seq: k * 10 + 1, value: next, write, client: '10.0.3.5', changed });
        }
        if (!write) value = next;
        k++;
      }
      const done = k >= total || k - q.after >= limit;
      for (const l of this.listeners) {
        l({ kind: 'history', history: { job: id, points, done, next: done && k < total ? k : undefined, candidates: total, scanned: k } });
      }
      if (!done) setTimeout(tick, this.step);
      else this.jobs.delete(id);
    };
    this.jobs.set(id, () => {
      stopped = true;
      this.jobs.delete(id);
    });
    setTimeout(tick, this.step);
    return { id, kind: 'history' };
  }

  async quality(): Promise<Quality> {
    const c = (code: string, label: string, count: number, severity: 'info' | 'warning' | 'problem', meaning: string, filter?: Filter) => ({ code, label, count, severity, meaning, filter });
    return {
      conditions: [
        c('capture_drops', 'Dropped by the capture host', 0, 'problem', 'The capture tool reported packets it could not keep: reads may be missing.'),
        c('gaps', 'Gaps', Math.floor(this.rows / 77_777), 'problem', 'Bytes of a connection the capture missed; transactions next to them are uncertain.', { flags: ['gap_adjacent'] }),
        c('retransmitted', 'Retransmissions', Math.floor(this.rows / 200), 'info', 'Segments seen more than once; the first copy was used.', { flags: ['retransmitted'] }),
        c('unanswered', 'Unanswered requests', Math.floor(this.rows / 500), 'warning', 'Requests with no response observed before the connection or the trace ended.', { statuses: ['unanswered'] }),
        c('role_guessed', 'Guessed roles', 0, 'info', 'Which side is the server was inferred from the traffic, not from the handshake.', { flags: ['role_guessed'] }),
      ],
      facts: [
        { label: 'Format', value: 'pcapng' },
        { label: 'Link types', value: '[ethernet]' },
        { label: 'Packets', value: String(this.rows * 2 + 40) },
      ],
    };
  }

  /** A small version of the Go parser (engine.parseFilter): enough terms to develop against. */
  async parseFilter(_fileId: string, text: string): Promise<FilterParse> {
    const filter: Filter = {};
    const errors: FilterParse['errors'] = [];
    for (const term of text.split(/\s+/).filter(Boolean)) {
      const neg = term.startsWith('-');
      const [key, value = ''] = (neg ? term.slice(1) : term).split(':', 2) as [string, string?];
      const vals = value.split(',').filter(Boolean);
      const nums = vals.map(Number);
      if (!value || (neg && key !== 'flag')) {
        errors.push({ term, message: neg ? 'only flag: can be negated' : 'not a filter term; use key:value' });
        continue;
      }
      switch (key) {
        case 'unit':
          filter.units = nums;
          break;
        case 'fc':
          filter.functions = nums;
          break;
        case 'conn':
          filter.conns = nums;
          break;
        case 'server':
          filter.servers = vals;
          break;
        case 'client':
          filter.clients = vals;
          break;
        case 'class':
          filter.classes = vals as FunctionClass[];
          break;
        case 'status':
          filter.statuses = vals as Status[];
          break;
        case 'flag':
          if (neg) filter.not_flags = vals;
          else filter.flags = vals;
          break;
        default:
          errors.push({ term, message: `unknown key "${key}"` });
      }
      if (nums.some(Number.isNaN) && ['unit', 'fc', 'conn'].includes(key)) {
        errors.push({ term, message: 'not a number' });
      }
    }
    return { filter, errors };
  }
}

/** Durations summarised as service.Distribution does (exact quantiles, power-of-two buckets). */
function dist(ns: number[]): Distribution | undefined {
  if (ns.length === 0) return undefined;
  const v = [...ns].sort((a, b) => a - b);
  const q = (p: number) => v[Math.min(v.length - 1, Math.ceil(p * v.length) - 1)] as number;
  const lo = Math.floor(Math.log2(Math.max(1, v[0] as number)));
  const hi = Math.floor(Math.log2(Math.max(1, v[v.length - 1] as number))) + 1;
  const histogram = [];
  for (let b = lo; b < hi; b++) {
    const lo_ns = 2 ** b;
    const hi_ns = 2 ** (b + 1);
    histogram.push({ lo_ns, hi_ns, count: v.filter((x) => x >= lo_ns && x < hi_ns).length });
  }
  return { count: v.length, min_ns: v[0] as number, p50_ns: q(0.5), p95_ns: q(0.95), p99_ns: q(0.99), max_ns: v[v.length - 1] as number, histogram };
}

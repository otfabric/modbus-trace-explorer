import engineSite from '../../test/fixtures/engine-site.json';
import { ServiceError, type Service } from './service';
import type {
  About,
  Location,
  LocateQuery,
  CacheInfo,
  Exclusion,
  ExportResult,
  JobKind,
  MBCAPExport,
  MBCAPPreview,
  PreviewTarget,
  Settings,
  SubtraceExport,
  TimelineExport,
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
  FileSummary,
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
  protected listeners = new Set<(e: ExplorerEvent) => void>();
  private jobs = new Map<string, () => void>();
  private nextJob = 0;
  private paths: string[] = ['/traces/site-2026-10-14.pcapng'];
  private results = new Map<string, ExportResult>();
  private prefs: Settings = { time_mode: 'local', server_ports: [], one_based: false, cache_cap: 2 * 1024 ** 3 };
  private cacheCleared = false;
  /** During a simulated analysis: the rows analysed so far (null: all of them). */
  private shown: number | null = null;

  /** The rows the timeline shows: all, or those analysed so far. */
  private get visible(): number {
    return this.shown ?? this.rows;
  }

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
      kind: this.isCapture() ? 'capture' : 'trace',
      analyzed: this.analyzed,
      prepare: this.analyzed && name.endsWith('.gz') ? true : undefined,
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

  private run(kind: JobKind, phases: string[], then: () => void): JobRef {
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
        // An analysis shows its rows from the third step on (R21).
        const ready = kind === 'analyze' && k >= 3;
        if (ready) {
          this.analyzed = true;
          this.shown = Math.round((this.rows * k) / steps);
        }
        this.emit({ id, kind, file: 'mock', state: 'running', phase, read, total, rows: kind === 'analyze' ? Math.round((this.rows * k) / steps) : 0, rate: 180e6, ready: ready || undefined });
        setTimeout(tick, this.step);
        return;
      }
      this.jobs.delete(id);
      if (kind === 'analyze') this.shown = null;
      then();
      this.emit({ id, kind, file: 'mock', state: 'done', phase, read: total, total, rows: kind === 'analyze' ? this.rows : 0, rate: 180e6 });
    };
    this.jobs.set(id, () => {
      stopped = true;
      this.jobs.delete(id);
      if (kind === 'analyze') {
        this.shown = null;
        this.analyzed = false;
      }
      this.emit({ id, kind, file: 'mock', state: 'canceled', phase: kind, read: 0, total, rows: 0, rate: 0 });
    });
    setTimeout(tick, this.step);
    return { id, kind };
  }

  async scan(): Promise<JobRef> {
    return this.run('scan', ['scan'], () => {});
  }

  async scanResult(): Promise<ScanResult> {
    // The library's shapes come from the engine's own answer for a small
    // trace (src/test/fixtures, written by make golden), scaled to this
    // trace's size, so the mock keeps every field the engine sends (A17).
    const base = engineSite.scan_result;
    const f0 = base.flows[0] as unknown as Flow;
    const flow = (id: number, a: string, b: string, cls: Flow['class'], score: number, port?: number): Flow => ({
      ...f0, id, a, b, role_source: 'syn', role_guessed: false, role_confidence: 1, open: 'syn', close: 'fin',
      packets: [this.rows, this.rows], bytes: [this.rows * 12, this.rows * 80], class: cls, score,
      adus: cls === 'modbus_tcp' ? [this.rows / 2, this.rows / 2] : [0, 0], server_port: port, gaps: 0,
    });
    const sum = base.summary as unknown as FileSummary;
    return {
      summary: {
        ...sum,
        file: this.paths[0]?.split('/').pop() ?? '', format: 'pcapng', gzip: false, input_bytes: this.rows * 190, bytes: this.rows * 170,
        packets: this.rows * 2 + 40,
        interfaces: [{ ...(sum.interfaces[0] as FileSummary['interfaces'][number]), id: 0, part: 0, link: 'ethernet', snaplen: 262144, name: 'eth0', packets: this.rows * 2 }],
        connections: { ...sum.connections, total: 4, midstream: 0, evicted: 0 }, unsupported: { not_tcp: 12 },
        sha256: '9f2c4e0d7a1b3c5e8f6a2d4b1c3e5f7a9b0c2d4e6f8a1b3c5d7e9f0a2b4c6d8e',
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

  /** The mock opens a name ending in .mbcap (or .mbcap.jsonl, .mbcap.csv) as a capture. */
  private isCapture(): boolean {
    return this.paths.some((p) => /\.mbcap(\.jsonl|\.csv)?$/.test(p));
  }

  async importCapture(): Promise<JobRef> {
    if (!this.isCapture()) throw new ServiceError('invalid_argument', 'this is a trace: scan and analyse it');
    return this.run('capture', ['import', 'index'], () => {
      this.analyzed = true;
    });
  }

  async prepare(): Promise<JobRef> {
    return this.run('prepare', ['decompress'], () => {});
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
      last_ns: START_NS + this.visible * STEP_NS,
      packets: this.rows * 2 + 40,
      connections: DEVICES.length,
      devices: DEVICES.length,
      transactions: this.visible,
      rows: this.visible,
      quality: 'warnings',
      quality_count: 3,
      trace_missing: false,
      summary: this.isCapture() ? undefined : (await this.scanResult()).summary,
      capture: this.isCapture()
        ? { encoding: 'mbcap', producer: 'modbus-trace-explorer 0.1.0', tick_ns: 1000, wall_start_ns: START_NS, change_only: false, exceptions: true,
            targets: DEVICES.length, streams: CLIENTS.length, samples: this.rows, end_known: true, truncated: false,
            provenance: ['modbus-trace-explorer 0.1.0 export mbcap'] }
        : undefined,
    };
  }

  /** The indexes of the rows that pass f in the order asked, or null for "all, in time order". */
  private selection(q: TimelineQuery): Int32Array | null {
    if (isEmpty(q.filter) && q.order === 'time') {
      return null;
    }
    const key = JSON.stringify([q.filter, q.order, this.visible]);
    if (key !== this.cacheKey || !this.cache) {
      const out = new Int32Array(this.visible);
      let n = 0;
      for (let i = 0; i < this.visible; i++) {
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
    const total = sel ? sel.length : this.visible;
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
        c('retransmitted', 'Retransmissions', Math.floor(this.rows / 200), 'info', 'Segments seen more than once; the first copy was used. A copy that arrives after its transaction is complete marks no row.', { flags: ['retransmitted'] }),
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

  async locate(_fileId: string, q: LocateQuery): Promise<Location> {
    const sel = this.selection({ filter: q.filter, order: q.seq !== undefined ? q.order : 'time', desc: false, offset: 0, limit: 0 });
    const total = sel ? sel.length : this.visible;
    if (total === 0) return { found: false, rank: 0, seq: 0, exact: false, note: 'No rows match the filter.' };
    const rowAt = (k: number) => (sel ? (sel[k] as number) : k);
    const at = (k: number, exact: boolean, note?: string): Location => ({ found: true, rank: q.desc ? total - 1 - k : k, seq: rowAt(k) + 1, exact, note });
    const search = (pred: (k: number) => boolean) => {
      let lo = 0;
      let hi = total;
      while (lo < hi) {
        const mid = (lo + hi) >> 1;
        if (pred(mid)) hi = mid;
        else lo = mid + 1;
      }
      return lo;
    };
    if (q.t_ns !== undefined) {
      const t = q.t_ns;
      const k = search((m) => generate(rowAt(m)).t_ns >= t);
      if (k >= total) return at(total - 1, false, 'The trace ends before that time: the last row.');
      return at(k, generate(rowAt(k)).t_ns === t);
    }
    if (q.frame !== undefined) {
      // The mock's request of row seq is frame 2·seq+1 and its response 2·seq+2 (as detail says).
      const f = q.frame;
      const k = search((m) => 2 * (rowAt(m) + 1) + 2 >= f);
      if (k >= total) return { found: false, rank: 0, seq: 0, exact: false, note: `No row of the filter carries frame ${f} or a later one.` };
      const seq = rowAt(k) + 1;
      const exact = f === 2 * seq + 1 || f === 2 * seq + 2;
      return at(k, exact, exact ? undefined : `No row carries frame ${f}; the next one is shown.`);
    }
    const seq = q.seq ?? 0;
    let rank = -1;
    for (let k = 0; k < total; k++) if (rowAt(k) === seq - 1) rank = q.desc ? total - 1 - k : k;
    if (rank < 0) return { found: false, rank: 0, seq: 0, exact: false, note: 'The row is not in the filter.' };
    const conn = generate(seq - 1).conn;
    const step = (q.step ?? 1) < 0 ? -1 : 1;
    for (let r = rank + step; r >= 0 && r < total; r += step) {
      const k = q.desc ? total - 1 - r : r;
      if (generate(rowAt(k)).conn === conn) return { found: true, rank: r, seq: rowAt(k) + 1, exact: true };
    }
    return { found: false, rank: 0, seq: 0, exact: false, note: 'No further row of this connection.' };
  }

  async diagnostics(fileId: string): Promise<string> {
    const lines = ['Modbus Trace Explorer mock', 'go-modbus-trace mock', 'Wails none', 'browser'];
    if (fileId) lines.push(`trace: 1 files, ${Math.round((this.rows * 190) / 2 ** 20)} MiB, analysed ${this.analyzed}`, `index: ${this.rows} rows`);
    return lines.join('\n') + '\n';
  }

  async previewMBCAP(_fileId: string, x: MBCAPExport): Promise<MBCAPPreview> {
    const noTime = { ...x.filter, from_ns: undefined, to_ns: undefined };
    const counts = new Map<string, number>();
    const targets = new Map<string, PreviewTarget>();
    let transactions = 0;
    let samples = 0;
    for (let i = 0; i < Math.min(this.rows, 100_000); i++) {
      const r = generate(i);
      if (r.kind !== 'txn') continue;
      transactions++;
      let reason = '';
      if (r.class === 'write') reason = 'write_function';
      else if (r.status === 'unanswered') reason = 'unanswered';
      else if (r.status === 'exception' && !x.exceptions) reason = 'exceptions_disabled';
      else if (!matches(r, noTime)) reason = 'not_selected';
      else if (!matches(r, x.filter)) reason = 'outside_window';
      if (reason) {
        counts.set(reason, (counts.get(reason) ?? 0) + 1);
        continue;
      }
      samples++;
      const key = `${r.server}/${r.unit}`;
      let t = targets.get(key);
      if (!t) {
        t = { key, server: r.server, unit: r.unit ?? 0, label: x.labels?.[key] || `${r.server} unit ${r.unit}`, samples: 0, clients: [] };
        targets.set(key, t);
      }
      t.samples++;
      if (!t.clients.includes(r.client)) t.clients.push(r.client);
    }
    const meaning: Record<string, string> = {
      write_function: 'Writes change a device; a capture holds reads only.',
      unanswered: 'No response was observed.',
      exceptions_disabled: 'Exception responses are switched off for this export.',
      not_selected: 'Outside the selection.',
      outside_window: 'Outside the time window.',
    };
    const excluded: Exclusion[] = Object.keys(meaning)
      .filter((k) => counts.get(k))
      .map((k) => ({ reason: k, count: counts.get(k) ?? 0, meaning: meaning[k] ?? '' }));
    const notes = ['A response without a usable timestamp is found only by the export (invalid_time).'];
    if (x.change_only) notes.unshift('Change-only drops a response equal to the last one of its range: the export can write fewer samples than shown.');
    return { selection: '', transactions, samples, up_to: x.change_only, targets: [...targets.values()], excluded, notes };
  }

  private exportJob(kind: 'mbcap' | 'timeline' | 'subtrace', path: string, result: () => Omit<ExportResult, 'job' | 'kind' | 'file'>): JobRef {
    if (!path.startsWith('/')) throw new ServiceError('invalid_argument', 'the export needs a full path');
    const ref = this.run(kind, [kind], () => {
      this.results.set(ref.id, { job: ref.id, kind, file: path.split('/').pop() ?? path, ...result() });
    });
    return ref;
  }

  async exportMBCAP(fileId: string, x: MBCAPExport, path: string): Promise<JobRef> {
    const pv = await this.previewMBCAP(fileId, x);
    const samples = x.change_only ? Math.round(pv.samples * 0.4) : pv.samples;
    const excluded = Object.fromEntries(pv.excluded.map((e) => [e.reason, e.count]));
    return this.exportJob('mbcap', path, () => ({
      format: x.format,
      report: {
        ...engineSite.mbcap_export.report,
        schema_version: 1, library: 'go-modbus-trace mock', capture_library: 'go-modbus-capture mock', format: 'MBCAP draft 0.1',
        selection: pv.selection, tick_ns: x.tick_ns || 1000, transactions_seen: pv.transactions, samples_written: samples,
        change_only_dropped: pv.samples - samples, excluded,
        targets: pv.targets.map((t, k) => ({ ...engineSite.mbcap_export.report.targets[0], id: k + 1, server: t.server, unit: t.unit, label: t.label, samples: t.samples,
          streams: t.clients.map((c, n) => ({ id: n + 1, client: c, samples: t.samples })) })),
        warnings: [{ code: 'stale_after_write', target: 1, text: 'A write came after the last read of holding 4096-4157.' }],
        replay: { samples, changed_values: samples * 3, estimated_in_memory_bytes: 12 * 1024 ** 2 + samples * 750 },
      },
      replay: [`mbcapctl verify ${path}`, ...pv.targets.map((_t, k) =>
        `modbusctl server replay --input ${path} --listen 127.0.0.1${pv.targets.length > 1 ? ` --target ${k + 1}` : ''} --port ${1502 + k} --before-first absent --missing exception`)],
      caveats: ['Writes are not replayed: the device answers with what was read.', 'Only what was read exists: other addresses answer with an exception.'],
      doc: 'https://github.com/otfabric/go-modbus-trace/blob/main/docs/REPLAY-WORKFLOW.md',
    }));
  }

  async exportTimeline(_fileId: string, x: TimelineExport, path: string): Promise<JobRef> {
    const rows = this.sample(x.filter).length;
    return this.exportJob('timeline', path, () => ({ format: x.format, rows, late: 0 }));
  }

  async exportSubtrace(_fileId: string, x: SubtraceExport, path: string): Promise<JobRef> {
    const f = x.filter;
    if (f.units?.length || f.functions?.length || f.statuses?.length || f.classes?.length || f.flags?.length || f.space || f.summary) {
      throw new ServiceError('invalid_argument', 'a sub-trace selects connections (server, client, conn) and a time window, not transactions: remove the other terms');
    }
    return this.exportJob('subtrace', path, () => ({ format: 'pcapng', packets: this.rows * 2, bytes: this.rows * 180, connections: f.conns?.length || 4 }));
  }

  async exportResult(jobId: string): Promise<ExportResult> {
    const r = this.results.get(jobId);
    if (!r) throw new ServiceError('not_found', `no finished export ${jobId}`);
    return r;
  }

  async saveReport(jobId: string): Promise<void> {
    await this.exportResult(jobId);
  }

  async settings(): Promise<Settings> {
    return { ...this.prefs, server_ports: [...this.prefs.server_ports] };
  }

  async saveSettings(s: Settings): Promise<Settings> {
    if (s.server_ports.some((p) => p < 1 || p > 65535)) throw new ServiceError('invalid_argument', 'server ports: 1 to 65535');
    if (s.cache_cap < 256 * 1024 ** 2) throw new ServiceError('invalid_argument', 'cache size: 256 MiB to 1 TiB');
    this.prefs = { ...s, server_ports: [...new Set(s.server_ports)].sort((a, b) => a - b) };
    return this.settings();
  }

  async cacheInfo(): Promise<CacheInfo> {
    const bytes = this.analyzed && !this.cacheCleared ? this.rows * 90 : 0;
    return { dir: '/Users/me/Library/Caches/modbus-trace-explorer', bytes, cap: this.prefs.cache_cap, entries: bytes ? 1 : 0 };
  }

  async clearCache(): Promise<CacheInfo> {
    this.cacheCleared = true;
    return this.cacheInfo();
  }

  async clearRecent(): Promise<void> {}

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
        case 'from':
        case 'to': {
          const ns = mockTime(value);
          if (ns === undefined) errors.push({ term, message: 'not a time: use 14:30, 14:30:05.250 or +15m' });
          else if (key === 'from') filter.from_ns = ns;
          else filter.to_ns = ns;
          break;
        }
        case 'addr': {
          const m = /^(?:(coil|discrete|holding|input)\/?)?(?:(\d+)(?:-(\d+))?)?$/.exec(value);
          if (!m || (!m[1] && !m[2])) {
            errors.push({ term, message: 'use addr:holding/4096-4160 or addr:4100' });
            break;
          }
          if (m[1]) filter.space = m[1] as Filter['space'];
          if (m[2]) {
            filter.start = Number(m[2]);
            filter.end = Number(m[3] ?? m[2]);
          }
          break;
        }
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

/** A time of the filter grammar against the mock trace: +90s, +15m, +1h, or a clock time of its day (local). */
export function mockTime(v: string): number | undefined {
  const off = /^\+(\d+(?:\.\d+)?)(s|m|h)$/.exec(v);
  if (off) return START_NS + Math.round(Number(off[1]) * { s: 1, m: 60, h: 3600 }[off[2] as 's' | 'm' | 'h'] * 1e9);
  const clock = /^(\d{1,2}):(\d{2})(?::(\d{2})(?:\.(\d{1,9}))?)?$/.exec(v);
  if (!clock) return undefined;
  const d = new Date(START_NS / 1e6);
  d.setHours(Number(clock[1]), Number(clock[2]), Number(clock[3] ?? 0), 0);
  const frac = clock[4] ? Number(clock[4].padEnd(9, '0')) : 0;
  return d.getTime() * 1e6 + frac;
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

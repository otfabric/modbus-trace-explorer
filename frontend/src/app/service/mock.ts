import { ServiceError, type Service } from './service';
import type {
  About,
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

const TXN_TERMS: (keyof Filter)[] = ['units', 'functions', 'statuses', 'classes', 'exceptions', 'flags', 'not_flags', 'space', 'start', 'end'];

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

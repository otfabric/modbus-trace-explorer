import type { Service } from './service';
import type { About, Filter, FunctionClass, Overview, Row, Status, TimelinePage, TimelineQuery } from './types';

// An in-memory Service over a generated trace, for building and testing the
// user interface without the engine (ARCHITECTURE.md section 11). Every row
// is computed from its index, so a dataset of ten million rows costs no
// memory until a filter or an ordering needs an index list.

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
  /** Index of the device in DEVICES, for filtering without strings. */
  dev: number;
}

/** The row at index i of the generated trace, in time order. */
export function generate(i: number): Generated {
  const t_ns = START_NS + i * STEP_NS + (mix(i, 1) % 4_000_000);
  const dev = mix(i, 2) % DEVICES.length;
  const device = DEVICES[dev] as Device;
  const client = CLIENTS[dev % 2] as string;
  const conn = dev + 1;
  const base = { seq: i + 1, t_ns, conn, client, server: device.server, dev, flags: [] as string[] };
  if (i % 50_000 === 0) {
    return { ...base, kind: 'conn_open', summary: 'Connection opened (SYN)', result: '' };
  }
  if (i % 77_777 === 7) {
    return { ...base, kind: 'gap', summary: 'Gap: about 1,460 bytes missing (server to client)', result: '' };
  }
  const r = mix(i, 3) % 1000;
  const addr = 4096 + (mix(i, 4) % 8) * 64;
  let fc = 3;
  let cls: FunctionClass = 'read';
  let summary = `Read 62 holding registers @ ${addr} (${hex(addr)})`;
  let result = '62 words';
  if (r < 150) {
    fc = 4;
    summary = `Read 2 input registers @ ${addr - 4096}`;
    result = '2 words';
  } else if (r < 170) {
    fc = 1;
    summary = 'Read 16 coils @ 0';
    result = '16 bits';
  } else if (r < 178) {
    fc = 6;
    cls = 'write';
    const v = mix(i, 5) % 100;
    summary = `Write register @ 100 = ${hex(v)} (${v})`;
    result = 'OK';
  } else if (r < 181) {
    fc = 16;
    cls = 'write';
    summary = 'Write 2 registers @ 100';
    result = 'OK';
  } else if (r < 182) {
    fc = 43;
    summary = 'Read device identification (basic, from object 0)';
    result = '3 objects';
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
  };
}

function matches(row: Generated, f: Filter): boolean {
  if (f.servers?.length && !f.servers.includes(row.server)) return false;
  if (f.units?.length && (row.unit === undefined || !f.units.includes(row.unit))) return false;
  if (f.clients?.length && !f.clients.includes(row.client)) return false;
  if (f.conns?.length && !f.conns.includes(row.conn)) return false;
  if (f.functions?.length && (row.fc === undefined || !f.functions.includes(row.fc))) return false;
  if (f.statuses?.length && (row.status === undefined || !f.statuses.includes(row.status))) return false;
  if (f.classes?.length && (row.class === undefined || !f.classes.includes(row.class))) return false;
  if (f.flags?.length && !f.flags.every((x) => row.flags.includes(x))) return false;
  if (f.not_flags?.length && f.not_flags.some((x) => row.flags.includes(x))) return false;
  if (f.from_ns !== undefined && row.t_ns < f.from_ns) return false;
  if (f.to_ns !== undefined && row.t_ns >= f.to_ns) return false;
  return true;
}

const isEmpty = (f: Filter) => Object.values(f).every((v) => v === undefined || (Array.isArray(v) && v.length === 0));

function strip({ dev: _dev, ...row }: Generated): Row {
  return row;
}

export class MockService implements Service {
  private readonly rows: number;
  private readonly empty: boolean;
  private cacheKey = '';
  private cache: Int32Array | null = null;

  /** empty: no trace is open (the empty state). */
  constructor(rows = 1_000_000, empty = false) {
    this.rows = rows;
    this.empty = empty;
  }

  async about(): Promise<About> {
    return { version: 'mock', go_modbus_trace: 'mock', wails: 'none', go: 'none', os: 'browser', arch: 'none' };
  }

  async overview(): Promise<Overview | null> {
    if (this.empty) return null;
    return {
      file: 'site-2026-10-14.pcapng',
      size: this.rows * 190,
      first_ns: START_NS,
      last_ns: START_NS + this.rows * STEP_NS,
      packets: this.rows * 2 + 40,
      connections: DEVICES.length,
      devices: DEVICES.length,
      transactions: this.rows,
      quality: 'warnings',
      quality_count: 3,
    };
  }

  /** The indexes of the rows that pass f in the order asked, or null for "all, in time order". */
  private selection(q: TimelineQuery): Int32Array | null {
    const plain = isEmpty(q.filter) && q.order === 'time';
    if (plain) {
      return null;
    }
    const key = JSON.stringify([q.filter, q.order]);
    if (key !== this.cacheKey || !this.cache) {
      const out = new Int32Array(this.rows);
      let n = 0;
      for (let i = 0; i < this.rows; i++) {
        if (matches(generate(i), q.filter)) {
          out[n++] = i;
        }
      }
      let sel = out.slice(0, n);
      if (q.order === 'latency') {
        const lat = (i: number) => generate(i).latency_ns ?? Number.MAX_SAFE_INTEGER;
        sel = Int32Array.from(Array.from(sel).sort((a, b) => lat(a) - lat(b) || a - b));
      }
      this.cacheKey = key;
      this.cache = sel;
    }
    return this.cache;
  }

  async timeline(q: TimelineQuery, signal?: AbortSignal): Promise<TimelinePage> {
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
}

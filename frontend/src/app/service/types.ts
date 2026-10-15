// The data the Service exchanges with the user interface. These mirror the
// Go DTOs of internal/service (snake_case JSON); M0 drafts the timeline
// types the mock and the table spike need, M1 makes them the engine's and
// publishes their JSON Schemas (docs/schema/).

/** Versions in this build (Go: service.About). */
export interface About {
  version: string;
  go_modbus_trace: string;
  wails: string;
  go: string;
  os: string;
  arch: string;
}

export type RowKind = 'txn' | 'conn_open' | 'conn_close' | 'gap' | 'resync' | 'note';
export type Status = 'ok' | 'exception' | 'unanswered' | 'orphan_response' | 'ambiguous' | 'malformed' | 'uncertain';
export type FunctionClass = 'read' | 'write' | 'other';
export type Space = 'coil' | 'discrete' | 'holding' | 'input';

/** The facts of the open trace for the file strip (R4). */
export interface Overview {
  file: string;
  size: number;
  first_ns: number;
  last_ns: number;
  packets: number;
  connections: number;
  devices: number;
  transactions: number;
  /** ok: nothing to report; warnings: worth a look; problems: results are incomplete. */
  quality: 'ok' | 'warnings' | 'problems';
  quality_count: number;
}

/**
 * The active filter (R7). Every field narrows; an empty or absent field
 * does not. It maps one to one to the library's selection, plus the
 * index-only terms (flags).
 */
export interface Filter {
  servers?: string[];
  units?: number[];
  clients?: string[];
  conns?: number[];
  functions?: number[];
  statuses?: Status[];
  classes?: FunctionClass[];
  flags?: string[];
  not_flags?: string[];
  from_ns?: number;
  to_ns?: number;
  space?: Space;
  start?: number;
  end?: number;
}

/** One timeline row (R5). Transactions are request and response together. */
export interface Row {
  seq: number;
  kind: RowKind;
  t_ns: number;
  conn: number;
  client: string;
  server: string;
  unit?: number;
  tid?: number;
  fc?: number;
  class?: FunctionClass;
  status?: Status;
  exception?: number;
  latency_ns?: number;
  flags: string[];
  /** The request in words, per UX.md section 3 (built for displayed rows only). */
  summary: string;
  /** The Result column: "62 words", "Exception 02 Illegal data address", ... */
  result: string;
}

export type Order = 'time' | 'latency';

/**
 * A window of the filtered, ordered timeline. `offset` is a rank in the
 * filtered set (0 = first row): the table maps its scrollbar to ranks, so a
 * jump anywhere costs one query (ARCHITECTURE.md section 11).
 */
export interface TimelineQuery {
  filter: Filter;
  order: Order;
  desc: boolean;
  offset: number;
  limit: number;
}

export interface TimelinePage {
  /** Rank of rows[0] in the filtered set. */
  offset: number;
  rows: Row[];
  /** Rows in the filtered set. */
  total: number;
}

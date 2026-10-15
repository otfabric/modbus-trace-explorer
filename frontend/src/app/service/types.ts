// The data the Service exchanges with the user interface. These mirror the
// Go types of internal/service (snake_case JSON) one to one; the JSON
// Schemas in docs/schema/ are generated from the Go side and a test checks
// the mock's data against them.

/** Versions in this build (Go: service.About). */
export interface About {
  version: string;
  go_modbus_trace: string;
  wails: string;
  go: string;
  os: string;
  arch: string;
}

/** An open trace (service.FileInfo). */
export interface FileInfo {
  id: string;
  paths: string[];
  name: string;
  size: number;
  gzip: boolean;
  /** The cache holds a complete analysis: no import needed. */
  analyzed: boolean;
  choices?: AnalyzeConfig;
}

export interface RecentFile {
  paths: string[];
  name: string;
  exists: boolean;
}

export interface JobRef {
  id: string;
  kind: 'scan' | 'analyze';
}

export type JobStatus = 'running' | 'done' | 'failed' | 'canceled';

export interface JobState {
  id: string;
  kind: 'scan' | 'analyze';
  file: string;
  state: JobStatus;
  phase: string;
  read: number;
  total: number;
  rows: number;
  rate: number;
  error?: { code: string; message: string };
  warning?: string;
}

export interface ExplorerEvent {
  kind: 'job';
  job?: JobState;
}

/** A TCP flow from the library's Scan (trace.Flow), the fields the Flows step shows. */
export interface Flow {
  id: number;
  a: string;
  b: string;
  role_source: string;
  role_guessed: boolean;
  role_confidence: number;
  open: string;
  close: string;
  first?: string;
  last?: string;
  packets: [number, number];
  bytes: [number, number];
  class: 'modbus_tcp' | 'tls' | 'other_tcp';
  score: number;
  adus: [number, number];
  server_port?: number;
  gaps: number;
}

/** The library's file summary (trace.FileSummary), the fields the application reads. */
export interface FileSummary {
  file: string;
  format: string;
  input_bytes: number;
  packets: number;
  first?: string;
  last?: string;
  gzip: boolean;
  bytes: number;
  interfaces: { id: number; part: number; link: string; snaplen: number; name?: string; packets: number; drops?: number }[];
  connections: { total: number; midstream: number; evicted: number };
  quality: Record<string, number>;
  unsupported: Record<string, number>;
  transactions?: Record<string, number>;
  capture_drops?: number;
  sha256: string;
  error?: string;
}

/** The Flows step (service.ScanResult). */
export interface ScanResult {
  summary: FileSummary;
  flows: Flow[];
  server_ports: number[];
  selected: number[];
  one_click: boolean;
}

/** What the user confirmed in the Flows step (service.AnalyzeConfig). */
export interface AnalyzeConfig {
  flows: number[];
  server_ports: number[];
  servers: string[];
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
  rows: number;
  /** ok: nothing to report; warnings: worth a look; problems: results are incomplete. */
  quality: 'ok' | 'warnings' | 'problems';
  quality_count: number;
  /** The trace is not at its path any more: detail is not available. */
  trace_missing: boolean;
  summary?: FileSummary;
}

/** The active filter (R7). Every field narrows; an absent or empty field does not. */
export interface Filter {
  servers?: string[];
  units?: number[];
  clients?: string[];
  conns?: number[];
  functions?: number[];
  statuses?: Status[];
  classes?: FunctionClass[];
  exceptions?: number[];
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
  summary: string;
  result: string;
}

export type Order = 'time' | 'latency';

/** A window of the filtered, ordered timeline, by rank (ARCHITECTURE.md section 6). */
export interface TimelineQuery {
  filter: Filter;
  order: Order;
  desc: boolean;
  offset: number;
  limit: number;
}

export interface TimelinePage {
  offset: number;
  rows: Row[];
  total: number;
}

export interface Field {
  name: string;
  /** Byte offset in the ADU (7 is the function code). */
  offset: number;
  length: number;
  value?: number;
  hex?: string;
}

export interface PacketRef {
  frame: number;
  part: number;
  t_ns?: number;
  interface: number;
  offset: number;
  payload_offset: number;
  payload_length: number;
  display_filter: string;
}

export interface ADUDetail {
  /** The whole ADU (MBAP header, then PDU) as hex; empty when the bytes cannot be read. */
  hex: string;
  name: string;
  fields: Field[];
  warnings: string[];
  packets: PacketRef[];
}

export interface Note {
  kind: 'flag' | 'warning' | 'fact';
  code: string;
  meaning: string;
}

export interface ConnDetail {
  id: number;
  client: string;
  server: string;
  open: string;
  close: string;
  role_source: string;
  role_confidence: number;
  transactions: number;
}

/** The detail pane of one row (R6). */
export interface Detail {
  row: Row;
  request?: ADUDetail;
  response?: ADUDetail;
  text: string;
  notes: Note[];
  conn?: ConnDetail;
  mbcap?: string;
  extra?: unknown;
}

export interface FilterParse {
  filter: Filter;
  errors: { term: string; message: string }[];
}

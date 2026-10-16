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
  kind: 'scan' | 'analyze' | 'history';
}

export type JobStatus = 'running' | 'done' | 'failed' | 'canceled';

export interface JobState {
  id: string;
  kind: 'scan' | 'analyze' | 'history';
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
  kind: 'job' | 'history';
  job?: JobState;
  history?: HistoryChunk;
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
  /** Exactly this request summary (a pattern's "show in timeline"). */
  summary?: string;
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

/** Durations summarised: exact quantiles and buckets a power of two apart (service.Distribution). */
export interface Distribution {
  count: number;
  min_ns: number;
  p50_ns: number;
  p95_ns: number;
  p99_ns: number;
  max_ns: number;
  histogram: { lo_ns: number; hi_ns: number; count: number }[];
}

/** One server and unit (R8; service.DeviceStats). */
export interface DeviceStats {
  server: string;
  unit: number;
  transactions: number;
  by_function: Record<string, number>;
  by_status: Record<string, number>;
  reads: number;
  writes: number;
  exceptions: number;
  unanswered: number;
  clients: string[];
  first_ns: number;
  last_ns: number;
  rate: number;
  bytes: number;
  latency?: Distribution;
}

/** One connection (R9; service.ConnStats). */
export interface ConnStats {
  id: number;
  client: string;
  server: string;
  open: string;
  close: string;
  role_source: string;
  role_confidence: number;
  role_guessed: boolean;
  first_ns: number;
  last_ns: number;
  transactions: number;
  bytes: number;
  gaps: number;
  resyncs: number;
  flags: string[];
}

/** A recurring request (R10; service.Pattern). */
export interface Pattern {
  client: string;
  server: string;
  unit: number;
  function: number;
  summary: string;
  space?: string;
  start: number;
  count: number;
  requests: number;
  first_ns: number;
  last_ns: number;
  interval?: Distribution;
  jitter_ns: number;
  responses: number;
  changed: number;
  volatility: number;
  volatility_class: 'static' | 'slow' | 'noisy';
}

export interface CoverageQuery {
  server: string;
  unit: number;
  space: string;
  filter: Filter;
}

export interface AddressInterval {
  start: number;
  /** Inclusive. */
  end: number;
  count: number;
  first_ns: number;
  last_ns: number;
}

/** What was read and written of a data space (R11; service.CoverageMap). */
export interface CoverageMap {
  server: string;
  unit: number;
  space: string;
  spaces: string[];
  read: AddressInterval[];
  written: AddressInterval[];
}

export interface HistoryQuery {
  server: string;
  unit: number;
  space: string;
  address: number;
  after: number;
  limit: number;
  every: boolean;
}

export interface HistoryPoint {
  t_ns: number;
  seq: number;
  value: number;
  write: boolean;
  client: string;
  changed: boolean;
}

/** A batch of an address history job (service.HistoryChunk). */
export interface HistoryChunk {
  job: string;
  points: HistoryPoint[];
  done: boolean;
  next?: number;
  candidates: number;
  scanned: number;
}

export interface Condition {
  code: string;
  label: string;
  count: number;
  severity: 'info' | 'warning' | 'problem';
  meaning: string;
  filter?: Filter;
}

/** R12 (service.Quality). */
export interface Quality {
  conditions: Condition[];
  facts: { label: string; value: string }[];
}

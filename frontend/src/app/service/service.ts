import type {
  About,
  CacheInfo,
  ExportResult,
  MBCAPExport,
  MBCAPPreview,
  Settings,
  SubtraceExport,
  TimelineExport,
  ConnStats,
  CoverageMap,
  CoverageQuery,
  DeviceStats,
  Filter,
  HistoryQuery,
  Pattern,
  Quality,
  AnalyzeConfig,
  Detail,
  ExplorerEvent,
  FileInfo,
  FilterParse,
  JobRef,
  Overview,
  RecentFile,
  ScanResult,
  TimelinePage,
  TimelineQuery,
} from './types';

export type ErrorCode = 'invalid_argument' | 'not_found' | 'canceled' | 'unavailable' | 'internal';

/** The error of a Service call, as the Go side reports it (service.Error). */
export class ServiceError extends Error {
  readonly code: ErrorCode;
  constructor(code: ErrorCode, message: string) {
    super(message);
    this.name = 'ServiceError';
    this.code = code;
  }
}

export function toServiceError(err: unknown): ServiceError {
  if (err instanceof ServiceError) {
    return err;
  }
  return new ServiceError('internal', err instanceof Error ? err.message : String(err));
}

/**
 * What the user interface can ask of the application (ARCHITECTURE.md
 * section 2; Go: internal/service.Service). The Wails adapter calls the Go
 * engine; the mock answers from a generated dataset.
 */
export interface Service {
  about(): Promise<About>;

  /** Prepares the trace made of paths; says whether an analysis is cached. */
  open(paths: string[]): Promise<FileInfo>;
  close(fileId: string): Promise<void>;
  recent(): Promise<RecentFile[]>;

  /** Starts the first pass; progress and completion arrive through subscribe. */
  scan(fileId: string): Promise<JobRef>;
  scanResult(fileId: string): Promise<ScanResult>;
  /** Decompresses an analysed gzip trace again (FileInfo.prepare); progress arrives through subscribe. */
  prepare(fileId: string): Promise<JobRef>;
  analyze(fileId: string, cfg: AnalyzeConfig): Promise<JobRef>;
  cancelJob(jobId: string): Promise<void>;
  /** Delivers job events; returns the unsubscribe. */
  subscribe(onEvent: (event: ExplorerEvent) => void): () => void;

  overview(fileId: string): Promise<Overview>;
  /** A window of the filtered timeline; abort stale requests with signal. */
  timeline(fileId: string, query: TimelineQuery, signal?: AbortSignal): Promise<TimelinePage>;
  detail(fileId: string, seq: number): Promise<Detail>;

  devices(fileId: string, filter: Filter): Promise<DeviceStats[]>;
  connections(fileId: string, filter: Filter): Promise<ConnStats[]>;
  patterns(fileId: string, filter: Filter): Promise<Pattern[]>;
  coverage(fileId: string, query: CoverageQuery): Promise<CoverageMap>;
  /** Starts reading one address's values; chunks arrive as "history" events. */
  addressHistory(fileId: string, query: HistoryQuery): Promise<JobRef>;
  quality(fileId: string): Promise<Quality>;
  /** Counts from the index what an MBCAP export would write and leave out (R13). */
  previewMBCAP(fileId: string, x: MBCAPExport): Promise<MBCAPPreview>;
  /** Starts an export job into path; ExportResult has what it wrote when it is done. */
  exportMBCAP(fileId: string, x: MBCAPExport, path: string): Promise<JobRef>;
  exportTimeline(fileId: string, x: TimelineExport, path: string): Promise<JobRef>;
  exportSubtrace(fileId: string, x: SubtraceExport, path: string): Promise<JobRef>;
  exportResult(jobId: string): Promise<ExportResult>;
  saveReport(jobId: string, path: string): Promise<void>;
  settings(): Promise<Settings>;
  saveSettings(s: Settings): Promise<Settings>;
  cacheInfo(): Promise<CacheInfo>;
  clearCache(): Promise<CacheInfo>;
  clearRecent(): Promise<void>;
  parseFilter(fileId: string, text: string): Promise<FilterParse>;
}

// The only file that touches the Wails runtime. It calls the Go service App
// in internal/gui; method names here must match its exported methods.

import { Call, Clipboard, Events } from '@wailsio/runtime';
import type { MenuAction, Platform, SaveKind } from '../../app/platform/platform';
import { loadTheme, persistTheme } from '../../app/platform/themeStorage';
import { ServiceError, toServiceError, type ErrorCode, type Service } from '../../app/service/service';
import type {
  About,
  Location,
  LocateQuery,
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
} from '../../app/service/types';

const BINDING = 'github.com/otfabric/modbus-trace-explorer/internal/gui.App';
const OPEN_EVENT = 'explorer:open';
const JOB_EVENT = 'explorer:event';
const MENU_EVENT = 'explorer:menu';

interface ErrorBody {
  code: ErrorCode;
  message: string;
}

const isErrorBody = (value: unknown): value is ErrorBody =>
  typeof value === 'object' && value !== null && 'code' in value && 'message' in value;

/** A Go service.Error arrives as the rejection's cause or as JSON in its message. */
export function fromGo(err: unknown): ServiceError {
  const candidates: unknown[] = [];
  if (err instanceof Error) {
    candidates.push(err.cause);
    try {
      candidates.push(JSON.parse(err.message));
    } catch {
      /* a plain message */
    }
  }
  const body = candidates.find(isErrorBody);
  return body ? new ServiceError(body.code, body.message) : toServiceError(err);
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  try {
    return (await Call.ByName(`${BINDING}.${method}`, ...args)) as T;
  } catch (err) {
    throw fromGo(err);
  }
}

export class WailsService implements Service {
  about = () => call<About>('About');
  open = (paths: string[]) => call<FileInfo>('Open', paths);
  close = (id: string) => call<void>('Close', id);
  recent = () => call<RecentFile[]>('Recent');
  scan = (id: string) => call<JobRef>('Scan', id);
  scanResult = (id: string) => call<ScanResult>('ScanResult', id);
  prepare = (id: string) => call<JobRef>('Prepare', id);
  importCapture = (id: string) => call<JobRef>('ImportCapture', id);
  analyze = (id: string, cfg: AnalyzeConfig) => call<JobRef>('Analyze', id, cfg);
  cancelJob = (jobId: string) => call<void>('CancelJob', jobId);
  overview = (id: string) => call<Overview>('Overview', id);
  detail = (id: string, seq: number) => call<Detail>('Detail', id, seq);
  parseFilter = (id: string, text: string) => call<FilterParse>('ParseFilter', id, text);
  devices = (id: string, f: Filter) => call<DeviceStats[]>('Devices', id, f);
  connections = (id: string, f: Filter) => call<ConnStats[]>('Connections', id, f);
  patterns = (id: string, f: Filter) => call<Pattern[]>('Patterns', id, f);
  coverage = (id: string, q: CoverageQuery) => call<CoverageMap>('Coverage', id, q);
  addressHistory = (id: string, q: HistoryQuery) => call<JobRef>('AddressHistory', id, q);
  quality = (id: string) => call<Quality>('Quality', id);
  locate = (id: string, q: LocateQuery) => call<Location>('Locate', id, q);
  diagnostics = (id: string) => call<string>('Diagnostics', id);
  previewMBCAP = (id: string, x: MBCAPExport) => call<MBCAPPreview>('PreviewMBCAP', id, x);
  exportMBCAP = (id: string, x: MBCAPExport, path: string) => call<JobRef>('ExportMBCAP', id, x, path);
  exportTimeline = (id: string, x: TimelineExport, path: string) => call<JobRef>('ExportTimeline', id, x, path);
  exportSubtrace = (id: string, x: SubtraceExport, path: string) => call<JobRef>('ExportSubtrace', id, x, path);
  exportResult = (job: string) => call<ExportResult>('ExportResult', job);
  saveReport = (job: string, path: string) => call<void>('SaveReport', job, path);
  settings = () => call<Settings>('Settings');
  saveSettings = (s: Settings) => call<Settings>('SaveSettings', s);
  cacheInfo = () => call<CacheInfo>('CacheInfo');
  clearCache = () => call<CacheInfo>('ClearCache');
  clearRecent = () => call<void>('ClearRecent');

  // A stale request is not cancelled on the Go side (Wails calls are not
  // abortable); its answer is dropped here.
  async timeline(id: string, q: TimelineQuery, signal?: AbortSignal): Promise<TimelinePage> {
    signal?.throwIfAborted();
    const page = await call<TimelinePage>('Timeline', id, q);
    signal?.throwIfAborted();
    return page;
  }

  subscribe(onEvent: (event: ExplorerEvent) => void): () => void {
    return Events.On(JOB_EVENT, (event) => onEvent(event.data as ExplorerEvent));
  }
}

export class WailsPlatform implements Platform {
  persistTheme = persistTheme;
  loadTheme = loadTheme;

  // The Go side shows the native dialog; the paths come from it, not from the page.
  chooseTraces = () => call<string[]>('ChooseTraces');
  async chooseSave(name: string, kind: SaveKind): Promise<string | null> {
    const path = await call<string>('ChooseSave', name, kind);
    return path === '' ? null : path;
  }

  onOpen(handler: (paths: string[]) => void): () => void {
    // Paths handed over before the window was ready (command line, "open with").
    void call<string[]>('PendingFiles').then((paths) => {
      if (paths.length > 0) handler(paths);
    });
    return Events.On(OPEN_EVENT, (event) => handler(event.data as string[]));
  }

  openExternal(url: string): void {
    void call<void>('OpenExternal', url);
  }

  onMenu(handler: (action: MenuAction) => void): () => void {
    return Events.On(MENU_EVENT, (event) => handler(event.data as MenuAction));
  }

  smokeMode = () => call<boolean>('SmokeMode');

  smokeDone(rows: number, problem: string): void {
    void call<void>('SmokeDone', rows, problem);
  }

  copyText(text: string): Promise<void> {
    return Clipboard.SetText(text);
  }
}

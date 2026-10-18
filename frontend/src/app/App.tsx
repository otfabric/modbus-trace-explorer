import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { AboutDialog } from './components/AboutDialog';
import { Chip } from './components/ui';
import { DetailPane } from './components/DetailPane';
import { EmptyState } from './components/EmptyState';
import { ExportDialog } from './components/ExportDialog';
import { FilterBar } from './components/FilterBar';
import { FlowsStep } from './components/FlowsStep';
import { GoToDialog, type GoTo } from './components/GoToDialog';
import { Popover } from './components/Popover';
import { Resizer, useDetailWidth } from './components/Resizer';
import { Header } from './components/Header';
import { ImportProgress } from './components/ImportProgress';
import { SettingsDialog } from './components/SettingsDialog';
import { StatusJobs, zoneLabel } from './components/StatusJobs';
import {
  AddressIcon,
  DeviceIcon,
  ErrorIcon,
  ExportIcon,
  PatternIcon,
  PlugIcon,
  QualityIcon,
  TimelineIcon,
  WarningIcon,
} from './icons';
import type { MenuAction, Platform } from './platform/platform';
import { toServiceError, type Service } from './service/service';
import type { AnalyzeConfig, Detail, FileInfo, Filter, JobState, LocateQuery, Order, Overview, RecentFile, ScanResult, Settings, TimelineQuery } from './service/types';
import { formatBytes, formatCount, formatDuration, formatTime, type TimeMode } from './state/format';
import { useTheme } from './state/useTheme';
import { TimelineTable } from './table/TimelineTable';
import { AddressesView } from './views/AddressesView';
import { ConversationsView } from './views/ConversationsView';
import { DevicesView } from './views/DevicesView';
import { PatternsView } from './views/PatternsView';
import { QualityView } from './views/QualityView';

interface AppProps {
  service: Service;
  platform: Platform;
}

const VIEWS: { key: string; label: string; icon: ReactNode }[] = [
  { key: 'timeline', label: 'Timeline', icon: <TimelineIcon size={16} /> },
  { key: 'devices', label: 'Devices', icon: <DeviceIcon size={16} /> },
  { key: 'conversations', label: 'Conversations', icon: <PlugIcon size={16} /> },
  { key: 'patterns', label: 'Patterns', icon: <PatternIcon size={16} /> },
  { key: 'addresses', label: 'Addresses', icon: <AddressIcon size={16} /> },
  { key: 'quality', label: 'Quality', icon: <QualityIcon size={16} /> },
];

const TIME_MODES: TimeMode[] = ['local', 'utc', 'relative'];

/** Where the user is: no trace, importing it, or looking at it. */
type Phase =
  | { kind: 'empty' }
  | { kind: 'scanning'; file: FileInfo; job: JobState | null; jobId: string }
  | { kind: 'flows'; file: FileInfo; result: ScanResult; warning?: string }
  | { kind: 'analyzing'; file: FileInfo; job: JobState | null; jobId: string; result?: ScanResult }
  | { kind: 'preparing'; file: FileInfo; job: JobState | null; jobId: string }
  | { kind: 'importing'; file: FileInfo; job: JobState | null; jobId: string }
  | { kind: 'ready'; file: FileInfo; warning?: string };

/** R4: the capture facts behind the file strip. */
function CaptureFacts({ ov }: { ov: Overview }) {
  const [open, setOpen] = useState(false);
  const s = ov.summary;
  if (!s) return null;
  const unsupported = Object.entries(s.unsupported ?? {}).filter(([, n]) => n > 0);
  const truncated = s.quality?.snaplen_truncated ?? 0;
  return (
    <span className="relative">
      <button type="button" aria-expanded={open} className="text-muted hover:text-accent" onClick={() => setOpen((v) => !v)}>
        Capture facts
      </button>
      {open && (
        <Popover label="Capture facts" align="right" onClose={() => setOpen(false)}>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 font-mono text-[12px]">
            <dt className="text-muted">format</dt>
            <dd>
              {s.format}
              {s.gzip ? ' (gzip)' : ''}
            </dd>
            <dt className="text-muted">packets</dt>
            <dd>{formatCount(s.packets)}</dd>
            <dt className="text-muted">link types</dt>
            <dd>{[...new Set(s.interfaces.map((i) => i.link))].join(', ') || '–'}</dd>
            <dt className="text-muted">interfaces</dt>
            <dd>
              {s.interfaces.map((i) => (
                <div key={`${i.part}-${i.id}`}>
                  {i.name ?? `#${i.id}`}: {i.link}, snap length {i.snaplen}, {formatCount(i.packets)} packets
                  {i.drops ? `, ${formatCount(i.drops)} dropped` : ''}
                </div>
              ))}
            </dd>
            <dt className="text-muted">cut by snap length</dt>
            <dd>{truncated ? formatCount(truncated) : 'none'}</dd>
            <dt className="text-muted">capture drops</dt>
            <dd>{s.capture_drops ? formatCount(s.capture_drops) : 'none reported'}</dd>
            <dt className="text-muted">unsupported</dt>
            <dd>{unsupported.length ? unsupported.map(([k, n]) => `${k} ${formatCount(n)}`).join(', ') : 'none'}</dd>
            <dt className="text-muted">SHA-256</dt>
            <dd className="break-all">{s.sha256 || '–'}</dd>
          </dl>
        </Popover>
      )}
    </span>
  );
}

function FileStrip({ ov, onQuality, onExport }: { ov: Overview; onQuality: () => void; onExport: () => void }) {
  const badge = { ok: 'Quality: OK', warnings: `Quality: ${ov.quality_count} warnings`, problems: `Quality: ${ov.quality_count} problems` }[ov.quality];
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-1 border-b border-line px-4 py-2 font-mono text-[13px]">
      <span className="text-fg-strong">{ov.file}</span>
      <span>{formatBytes(ov.size)}</span>
      <span>
        {formatTime(ov.first_ns, 'local')} – {formatTime(ov.last_ns, 'local')} ({formatDuration(ov.last_ns - ov.first_ns)})
      </span>
      {ov.capture ? (
        <>
          <Chip tone="accent" title={`MBCAP capture (${ov.capture.encoding}) by ${ov.capture.producer}: samples, not packets`}>
            MBCAP capture
          </Chip>
          <span>{formatCount(ov.devices)} targets</span>
          <span>{formatCount(ov.transactions)} samples</span>
          <span>{formatCount(ov.connections)} streams</span>
        </>
      ) : (
        <>
          <span>{formatCount(ov.devices)} devices</span>
          <span>{formatCount(ov.transactions)} transactions</span>
          <span>{formatCount(ov.connections)} connections</span>
        </>
      )}
      <span className="ml-auto flex items-center gap-3">
        <CaptureFacts ov={ov} />
        {ov.trace_missing && (
          <span className="inline-flex items-center gap-1 text-wire">
            <WarningIcon size={14} /> The trace is not at its path: detail is not available
          </span>
        )}
        <button
          type="button"
          onClick={onQuality}
          className={`inline-flex items-center gap-1 hover:underline ${ov.quality === 'problems' ? 'text-danger' : ov.quality === 'warnings' ? 'text-wire' : 'text-muted'}`}
        >
          {ov.quality === 'ok' ? <QualityIcon size={14} /> : <WarningIcon size={14} />}
          {badge}
        </button>
        <button
          type="button"
          onClick={onExport}
          disabled={ov.trace_missing || ov.capture !== undefined}
          title={
            ov.capture ? 'Exports work on traces; this is already a capture' : ov.trace_missing ? 'Exports read the trace, which is not at its path' : 'Export (Ctrl/Cmd+E)'
          }
          className="inline-flex items-center gap-1 hover:text-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          <ExportIcon size={14} /> Export
        </button>
      </span>
    </div>
  );
}

export function App({ service, platform }: AppProps) {
  const [theme, toggleTheme] = useTheme(platform);
  const [phase, setPhase] = useState<Phase>({ kind: 'empty' });
  const [error, setError] = useState<string | null>(null);
  const [recent, setRecent] = useState<RecentFile[]>([]);
  const [overview, setOverview] = useState<Overview | null>(null);
  const [view, setView] = useState('timeline');
  const [filter, setFilter] = useState<Filter>({});
  const [order, setOrder] = useState<Order>('time');
  const [desc, setDesc] = useState(false);
  const [timeMode, setTimeMode] = useState<TimeMode>('local');
  const [selected, setSelected] = useState<number | null>(null);
  const [detail, setDetail] = useState<Detail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [showDetail, setShowDetail] = useState(true);
  const [detailWidth, setDetailWidth] = useDetailWidth();
  const [total, setTotal] = useState(0);
  const [dialog, setDialog] = useState<'export' | 'settings' | 'about' | 'goto-time' | 'goto-frame' | null>(null);
  const [goTo, setGoTo] = useState<{ rank: number; nonce: number } | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const detailBox = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setNotice(null), 6000);
    return () => clearTimeout(t);
  }, [notice]);
  const [oneBased, setOneBased] = useState(false);
  const filterBox = useRef<HTMLInputElement>(null);
  const phaseRef = useRef(phase);
  phaseRef.current = phase;

  const fail = useCallback((err: unknown) => {
    setError(toServiceError(err).message);
    setPhase({ kind: 'empty' });
  }, []);

  useEffect(() => {
    service.recent().then(setRecent, () => setRecent([]));
  }, [service, phase.kind]);

  // The smoke test (A15): import what the command line gave without asking,
  // and report once the timeline shows rows, or the first error.
  const [smoke, setSmoke] = useState(false);
  const smokeReported = useRef(false);
  useEffect(() => {
    platform.smokeMode().then(setSmoke, () => setSmoke(false));
  }, [platform]);
  const reportSmoke = useCallback(
    (rows: number, problem: string) => {
      if (!smoke || smokeReported.current) return;
      smokeReported.current = true;
      platform.smokeDone(rows, problem);
    },
    [smoke, platform],
  );
  useEffect(() => {
    if (error) reportSmoke(0, error);
  }, [error, reportSmoke]);
  useEffect(() => {
    if (phase.kind === 'ready' && total > 0) reportSmoke(total, '');
  }, [phase.kind, total, reportSmoke]);

  // The saved settings (R20) set the time display and the address labels.
  const applySettings = useCallback((s: Settings) => {
    setTimeMode(s.time_mode);
    setOneBased(s.one_based);
  }, []);
  useEffect(() => {
    service.settings().then(applySettings, () => {});
  }, [service, applySettings]);

  // A trace is ready: its overview, and a fresh view state.
  const ready = useCallback(
    (file: FileInfo, warning?: string) => {
      setFilter({});
      setSelected(null);
      setDetail(null);
      setPhase({ kind: 'ready', file, warning });
      service.overview(file.id).then(setOverview, fail);
    },
    [service, fail],
  );

  const open = useCallback(
    async (paths: string[]) => {
      if (paths.length === 0) return;
      setError(null);
      try {
        const cur = phaseRef.current;
        if (cur.kind !== 'empty') await service.close(cur.file.id).catch(() => {});
        const file = await service.open(paths);
        if (file.analyzed && file.prepare) {
          const ref = await service.prepare(file.id);
          setPhase({ kind: 'preparing', file, job: null, jobId: ref.id });
          return;
        }
        if (file.analyzed) {
          ready(file);
          return;
        }
        setOverview(null);
        if (file.kind === 'capture') {
          // An MBCAP capture (R24): no scan and no Flows step.
          const ref = await service.importCapture(file.id);
          setPhase({ kind: 'importing', file, job: null, jobId: ref.id });
          return;
        }
        const ref = await service.scan(file.id);
        setPhase({ kind: 'scanning', file, job: null, jobId: ref.id });
      } catch (err) {
        fail(err);
      }
    },
    [service, ready, fail],
  );

  // Job events drive the import. A job can end before the window knows its
  // ID (a tiny or broken file fails in milliseconds): its final event is
  // kept and applied as soon as the phase names the job.
  const earlyEnds = useRef(new Map<string, JobState>());
  const onJob = useRef<(job: JobState) => void>(() => {});
  onJob.current = (job: JobState) => {
    const cur = phaseRef.current;
    if ((cur.kind !== 'scanning' && cur.kind !== 'analyzing' && cur.kind !== 'preparing' && cur.kind !== 'importing') || job.id !== cur.jobId) {
      if (job.state !== 'running') {
        earlyEnds.current.set(job.id, job);
        if (earlyEnds.current.size > 16) earlyEnds.current.delete(earlyEnds.current.keys().next().value as string);
      }
      return;
    }
    if (job.state === 'running') {
      setPhase({ ...cur, job });
      return;
    }
    if (job.state === 'failed') {
      fail(new Error(job.error?.message ?? 'the job failed'));
      return;
    }
    if (cur.kind === 'preparing') {
      if (job.state === 'canceled') setPhase({ kind: 'empty' });
      else ready({ ...cur.file, prepare: false });
      return;
    }
    if (cur.kind === 'importing') {
      if (job.state === 'canceled') setPhase({ kind: 'empty' });
      else ready({ ...cur.file, analyzed: true }, job.warning);
      return;
    }
    if (cur.kind === 'scanning') {
      if (job.state === 'canceled') {
        setPhase({ kind: 'empty' });
        return;
      }
      service.scanResult(cur.file.id).then((result) => setPhase({ kind: 'flows', file: cur.file, result, warning: job.warning }), fail);
      return;
    }
    if (job.state === 'canceled') {
      if (cur.result) setPhase({ kind: 'flows', file: cur.file, result: cur.result });
      else setPhase({ kind: 'empty' });
      return;
    }
    ready({ ...cur.file, analyzed: true }, job.warning);
  };
  useEffect(() => service.subscribe((ev) => ev.job && onJob.current(ev.job)), [service]);
  const waitingFor = phase.kind === 'scanning' || phase.kind === 'analyzing' || phase.kind === 'preparing' || phase.kind === 'importing' ? phase.jobId : null;
  useEffect(() => {
    if (!waitingFor) return;
    const ended = earlyEnds.current.get(waitingFor);
    if (ended) {
      earlyEnds.current.delete(waitingFor);
      onJob.current(ended);
    }
  }, [waitingFor]);

  useEffect(() => platform.onOpen((paths) => void open(paths)), [platform, open]);

  const closeTrace = useCallback(() => {
    const cur = phaseRef.current;
    if (cur.kind === 'empty') return;
    void service.close(cur.file.id).catch(() => {});
    setOverview(null);
    setDialog(null);
    setPhase({ kind: 'empty' });
  }, [service]);

  // The native menu (ARCHITECTURE.md section 12): the window owns the state, the menu names the action.
  const menuRef = useRef<(a: MenuAction) => void>(() => {});
  menuRef.current = (a: MenuAction) => {
    const ready = phaseRef.current.kind === 'ready';
    switch (a.action) {
      case 'open':
        void platform.chooseTraces().then(open);
        break;
      case 'recent':
        if (a.paths) void open(a.paths);
        break;
      case 'clear-recent':
        void service.clearRecent().then(() => setRecent([]));
        break;
      case 'close':
        closeTrace();
        break;
      case 'export':
        if (ready && phaseRef.current.kind === 'ready' && phaseRef.current.file.kind !== 'capture') setDialog('export');
        break;
      case 'settings':
        setDialog('settings');
        break;
      case 'about':
        setDialog('about');
        break;
      case 'theme':
        toggleTheme();
        break;
      case 'time':
        setTimeMode((m) => TIME_MODES[(TIME_MODES.indexOf(m) + 1) % TIME_MODES.length] ?? 'local');
        break;
      case 'detail':
        setShowDetail((v) => !v);
        break;
    }
  };
  useEffect(() => platform.onMenu((a) => menuRef.current(a)), [platform]);

  useEffect(() => {
    if (smoke && phase.kind === 'flows' && phase.result.selected.length === 0) {
      reportSmoke(0, 'the scan found no Modbus/TCP flow');
    } else if (smoke && phase.kind === 'flows') {
      void analyze(phase.file, phase.result, { flows: phase.result.selected, server_ports: phase.result.server_ports, servers: [] });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [smoke, phase.kind]);

  const analyze = async (file: FileInfo, result: ScanResult, cfg: AnalyzeConfig) => {
    try {
      const ref = await service.analyze(file.id, cfg);
      setPhase({ kind: 'analyzing', file, job: null, jobId: ref.id, result });
    } catch (err) {
      setError(toServiceError(err).message);
    }
  };

  const fileId = phase.kind === 'ready' ? phase.file.id : '';
  const firstNs = overview?.first_ns ?? 0;
  // "Show in timeline": the view's selection narrows the filter.
  const show = useCallback((f: Filter) => {
    setFilter((cur) => ({ ...cur, ...f }));
    setSelected(null);
    setView('timeline');
  }, []);
  // Go to time or frame, and N/P: the engine finds the row, the table shows it.
  const locate = useCallback(
    async (q: Partial<LocateQuery>) => {
      if (!fileId) return;
      const byTime = q.t_ns !== undefined || q.frame !== undefined;
      const o: Order = byTime ? 'time' : order;
      try {
        const loc = await service.locate(fileId, { filter, order: o, desc, ...q });
        setNotice(loc.note ?? null);
        if (!loc.found) return;
        if (byTime && order !== 'time') setOrder('time');
        setView('timeline');
        setSelected(loc.seq);
        setGoTo({ rank: loc.rank, nonce: Date.now() + Math.random() });
      } catch (err) {
        setNotice(toServiceError(err).message);
      }
    },
    [service, fileId, filter, order, desc],
  );
  const fetchPage = useCallback((q: TimelineQuery, signal: AbortSignal) => service.timeline(fileId, q, signal), [service, fileId]);

  // The detail pane follows the selection.
  useEffect(() => {
    if (selected === null || !fileId) {
      setDetail(null);
      return;
    }
    let live = true;
    setDetailError(null);
    service.detail(fileId, selected).then(
      (d) => live && setDetail(d),
      (err: unknown) => live && setDetailError(toServiceError(err).message),
    );
    return () => {
      live = false;
    };
  }, [service, fileId, selected]);

  // What the key handler reads without being bound again on every change.
  const keyState = useRef({ selected, detail, locate });
  keyState.current = { selected, detail, locate };

  // UX.md section 9.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target;
      const typing = t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement || t instanceof HTMLSelectElement;
      const inDialog = t instanceof Element && t.closest('[role=dialog]') !== null;
      const mod = e.metaKey || e.ctrlKey;
      const cur = keyState.current;
      const ready = phaseRef.current.kind === 'ready';
      if (mod && e.key.toLowerCase() === 'g') {
        e.preventDefault();
        if (ready) setDialog(e.shiftKey ? 'goto-frame' : 'goto-time');
      } else if (mod && e.key.toLowerCase() === 'c' && !typing && ready && cur.detail && !window.getSelection()?.toString()) {
        // Copy the selected row as text, or with Shift its raw bytes (UX.md section 9).
        e.preventDefault();
        const d = cur.detail;
        const text = e.shiftKey ? [d.request?.hex, d.response?.hex].filter(Boolean).join('\n') : d.text;
        if (text) void platform.copyText(text).then(() => setNotice(e.shiftKey ? 'Raw bytes copied.' : 'Row copied.'));
      } else if (mod && e.key.toLowerCase() === 'o') {
        e.preventDefault();
        void platform.chooseTraces().then(open);
      } else if (mod && e.key.toLowerCase() === 'e') {
        e.preventDefault();
        if (phaseRef.current.kind === 'ready' && phaseRef.current.file.kind !== 'capture') setDialog('export');
      } else if (mod && e.key === ',') {
        e.preventDefault();
        setDialog('settings');
      } else if ((mod && e.key.toLowerCase() === 'f') || (!typing && e.key === '/')) {
        e.preventDefault();
        filterBox.current?.focus();
      } else if (typing || mod || inDialog) {
        return;
      } else if (e.key === 'Enter' && t instanceof Element && t.getAttribute('role') === 'grid') {
        e.preventDefault();
        detailBox.current?.focus();
      } else if ((e.key === 'n' || e.key === 'p') && ready && cur.selected !== null) {
        void cur.locate({ seq: cur.selected, step: e.key === 'n' ? 1 : -1 });
      } else if (/^[1-6]$/.test(e.key)) {
        setView(VIEWS[Number(e.key) - 1]?.key ?? 'timeline');
      } else if (e.key.toLowerCase() === 't') {
        setTimeMode((m) => TIME_MODES[(TIME_MODES.indexOf(m) + 1) % TIME_MODES.length] ?? 'local');
      } else if (e.key.toLowerCase() === 'd') {
        setShowDetail((v) => !v);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [platform, open]);

  const cancelJob = () => {
    if (phase.kind === 'scanning' || phase.kind === 'analyzing' || phase.kind === 'preparing' || phase.kind === 'importing') void service.cancelJob(phase.jobId);
  };

  let body: ReactNode;
  switch (phase.kind) {
    case 'empty':
      body = <EmptyState onOpen={() => void platform.chooseTraces().then(open)} onOpenRecent={(p) => void open(p)} recent={recent} error={error} />;
      break;
    case 'scanning':
      body = <ImportProgress title="Scanning the trace" name={phase.file.name} job={phase.job} onCancel={cancelJob} />;
      break;
    case 'preparing':
      body = <ImportProgress title="Decompressing the trace" name={phase.file.name} job={phase.job} onCancel={cancelJob} />;
      break;
    case 'importing':
      body = <ImportProgress title="Importing the capture" name={phase.file.name} job={phase.job} onCancel={cancelJob} />;
      break;
    case 'analyzing':
      body = <ImportProgress title="Analysing the selected flows" name={phase.file.name} job={phase.job} onCancel={cancelJob} />;
      break;
    case 'flows':
      body = (
        <FlowsStep
          name={phase.file.name}
          result={phase.result}
          warning={phase.warning ?? error ?? undefined}
          onAnalyze={(cfg) => void analyze(phase.file, phase.result, cfg)}
          onCancel={() => {
            void service.close(phase.file.id);
            setPhase({ kind: 'empty' });
          }}
        />
      );
      break;
    case 'ready':
      body = (
        <>
          {overview && <FileStrip ov={overview} onQuality={() => setView('quality')} onExport={() => setDialog('export')} />}
          {phase.warning && (
            <p role="alert" className="flex shrink-0 items-center gap-2 border-b border-line px-4 py-1 text-wire">
              <WarningIcon size={14} /> {phase.warning}
            </p>
          )}
          <FilterBar
            ref={filterBox}
            filter={filter}
            total={total}
            parse={(text) => service.parseFilter(phase.file.id, text)}
            onChange={(f) => {
              setFilter(f);
              setSelected(null);
            }}
          />
          <div className="flex min-h-0 flex-1">
            <nav aria-label="Views" className="flex w-44 shrink-0 flex-col gap-0.5 border-r border-line p-2">
              {VIEWS.map((v, i) => (
                <button
                  key={v.key}
                  type="button"
                  aria-current={view === v.key ? 'page' : undefined}
                  onClick={() => setView(v.key)}
                  className={`flex items-center gap-2 rounded px-2 py-1.5 text-left text-[13px] ${
                    view === v.key ? 'bg-accent-soft text-accent-strong' : 'text-fg hover:text-accent'
                  }`}
                >
                  {v.icon}
                  {v.label}
                  <span className="ml-auto font-mono text-[11px] text-muted">{i + 1}</span>
                </button>
              ))}
            </nav>
            <main className="flex min-w-0 flex-1 flex-col">
              {view === 'timeline' ? (
                <TimelineTable
                  fetchPage={fetchPage}
                  filter={filter}
                  order={order}
                  desc={desc}
                  timeMode={timeMode}
                  firstNs={firstNs}
                  selectedSeq={selected}
                  onSelect={(_rank, row) => setSelected(row.seq)}
                  onTotal={setTotal}
                  goTo={goTo}
                />
              ) : view === 'devices' ? (
                <DevicesView service={service} fileId={fileId} filter={filter} timeMode={timeMode} firstNs={firstNs} onShow={show} />
              ) : view === 'conversations' ? (
                <ConversationsView
                  service={service}
                  fileId={fileId}
                  filter={filter}
                  timeMode={timeMode}
                  firstNs={firstNs}
                  fetchPage={fetchPage}
                  selectedSeq={selected}
                  onSelect={(row) => setSelected(row.seq)}
                />
              ) : view === 'patterns' ? (
                <PatternsView service={service} fileId={fileId} filter={filter} timeMode={timeMode} firstNs={firstNs} onShow={show} />
              ) : view === 'addresses' ? (
                <AddressesView service={service} fileId={fileId} filter={filter} timeMode={timeMode} firstNs={firstNs} oneBased={oneBased} />
              ) : (
                <QualityView service={service} fileId={fileId} onShow={show} />
              )}
            </main>
            {showDetail && (view === 'timeline' || view === 'conversations') && (
              <Resizer width={detailWidth} onWidth={setDetailWidth} />
            )}
            {showDetail && (view === 'timeline' || view === 'conversations') && (
              <aside
                ref={detailBox}
                tabIndex={-1}
                aria-label="Detail"
                style={{ width: detailWidth }}
                className="flex shrink-0 flex-col border-l border-line outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                <DetailPane
                  detail={detail}
                  error={detailError}
                  loading={selected !== null}
                  timeMode={timeMode}
                  firstNs={overview?.first_ns ?? 0}
                  copy={(t) => void platform.copyText(t)}
                />
              </aside>
            )}
          </div>
        </>
      );
      break;
  }

  return (
    <div className="flex h-full flex-col">
      <Header theme={theme} onToggleTheme={toggleTheme} onSettings={() => setDialog('settings')} />
      {body}
      {dialog === 'export' && phase.kind === 'ready' && (
        <ExportDialog
          service={service}
          platform={platform}
          fileId={phase.file.id}
          name={phase.file.name}
          filter={filter}
          firstNs={firstNs}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog === 'about' && <AboutDialog service={service} platform={platform} fileId={fileId} onClose={() => setDialog(null)} />}
      {(dialog === 'goto-time' || dialog === 'goto-frame') && phase.kind === 'ready' && (
        <GoToDialog
          mode={dialog === 'goto-time' ? 'time' : 'frame'}
          files={phase.file.paths}
          parse={(text) => service.parseFilter(phase.file.id, text)}
          onClose={() => setDialog(null)}
          onGo={(target: GoTo) => {
            setDialog(null);
            void locate(target);
          }}
        />
      )}
      {dialog === 'settings' && (
        <SettingsDialog
          onAbout={() => setDialog('about')}
          service={service}
          theme={theme}
          onToggleTheme={toggleTheme}
          onSaved={applySettings}
          onRecentCleared={() => setRecent([])}
          onClose={() => setDialog(null)}
        />
      )}
      {phase.kind === 'ready' && (
        <footer className="flex h-7 shrink-0 items-center gap-4 border-t border-line px-4 font-mono text-[12px] text-muted">
          <span>{formatCount(total)} rows</span>
          <button type="button" className="hover:text-accent" onClick={() => setTimeMode((m) => TIME_MODES[(TIME_MODES.indexOf(m) + 1) % 3] ?? 'local')}>
            time: {timeMode}
            {timeMode === 'local' && ` (${zoneLabel(overview?.first_ns ?? Date.now() * 1e6)})`}
          </button>
          <button
            type="button"
            className="hover:text-accent"
            onClick={() => {
              if (order === 'time' && !desc) setOrder('latency');
              else if (order === 'latency' && !desc) setDesc(true);
              else {
                setOrder('time');
                setDesc(false);
              }
            }}
          >
            order: {order}
            {desc ? ' (descending)' : ''}
          </button>
          {notice && (
            <span role="status" className="text-fg">
              {notice}
            </span>
          )}
          {error && (
            <span className="inline-flex items-center gap-1 text-danger">
              <ErrorIcon size={12} /> {error}
            </span>
          )}
          <StatusJobs service={service} onEnded={setNotice} />
        </footer>
      )}
    </div>
  );
}

import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { DetailPane } from './components/DetailPane';
import { EmptyState } from './components/EmptyState';
import { ExportDialog } from './components/ExportDialog';
import { FilterBar } from './components/FilterBar';
import { FlowsStep } from './components/FlowsStep';
import { Header } from './components/Header';
import { ImportProgress } from './components/ImportProgress';
import { SettingsDialog } from './components/SettingsDialog';
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
import type { Platform } from './platform/platform';
import { toServiceError, type Service } from './service/service';
import type { AnalyzeConfig, Detail, FileInfo, Filter, JobState, Order, Overview, RecentFile, ScanResult, Settings, TimelineQuery } from './service/types';
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
  | { kind: 'ready'; file: FileInfo; warning?: string };

function FileStrip({ ov, onQuality, onExport }: { ov: Overview; onQuality: () => void; onExport: () => void }) {
  const badge = { ok: 'Quality: OK', warnings: `Quality: ${ov.quality_count} warnings`, problems: `Quality: ${ov.quality_count} problems` }[ov.quality];
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-1 border-b border-line px-4 py-2 font-mono text-[13px]">
      <span className="text-fg-strong">{ov.file}</span>
      <span>{formatBytes(ov.size)}</span>
      <span>
        {formatTime(ov.first_ns, 'local')} – {formatTime(ov.last_ns, 'local')} ({formatDuration(ov.last_ns - ov.first_ns)})
      </span>
      <span>{formatCount(ov.devices)} devices</span>
      <span>{formatCount(ov.transactions)} transactions</span>
      <span>{formatCount(ov.connections)} connections</span>
      <span className="ml-auto flex items-center gap-3">
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
          disabled={ov.trace_missing}
          title={ov.trace_missing ? 'Exports read the trace, which is not at its path' : 'Export (Ctrl/Cmd+E)'}
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
  const [total, setTotal] = useState(0);
  const [dialog, setDialog] = useState<'export' | 'settings' | null>(null);
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
        const ref = await service.scan(file.id);
        setPhase({ kind: 'scanning', file, job: null, jobId: ref.id });
      } catch (err) {
        fail(err);
      }
    },
    [service, ready, fail],
  );

  // Job events drive the import.
  useEffect(
    () =>
      service.subscribe((ev) => {
        const job = ev.job;
        const cur = phaseRef.current;
        if (!job || (cur.kind !== 'scanning' && cur.kind !== 'analyzing' && cur.kind !== 'preparing') || job.id !== cur.jobId) return;
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
      }),
    [service, ready, fail],
  );

  useEffect(() => platform.onOpen((paths) => void open(paths)), [platform, open]);

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

  // UX.md section 9.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = e.target instanceof HTMLInputElement;
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key.toLowerCase() === 'o') {
        e.preventDefault();
        void platform.chooseTraces().then(open);
      } else if (mod && e.key.toLowerCase() === 'e') {
        e.preventDefault();
        if (phaseRef.current.kind === 'ready') setDialog('export');
      } else if (mod && e.key === ',') {
        e.preventDefault();
        setDialog('settings');
      } else if ((mod && e.key.toLowerCase() === 'f') || (!typing && e.key === '/')) {
        e.preventDefault();
        filterBox.current?.focus();
      } else if (typing || mod) {
        return;
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
    if (phase.kind === 'scanning' || phase.kind === 'analyzing' || phase.kind === 'preparing') void service.cancelJob(phase.jobId);
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
              <aside aria-label="Detail" className="flex w-96 shrink-0 flex-col border-l border-line">
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
      {dialog === 'settings' && (
        <SettingsDialog
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
          {error && (
            <span className="inline-flex items-center gap-1 text-danger">
              <ErrorIcon size={12} /> {error}
            </span>
          )}
        </footer>
      )}
    </div>
  );
}

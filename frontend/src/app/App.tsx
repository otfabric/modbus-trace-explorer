import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { EmptyState } from './components/EmptyState';
import { Header } from './components/Header';
import { Chip } from './components/ui';
import {
  AddressIcon,
  DeviceIcon,
  ExportIcon,
  PatternIcon,
  PlugIcon,
  QualityIcon,
  TimelineIcon,
  WarningIcon,
} from './icons';
import type { Platform } from './platform/platform';
import type { Service } from './service/service';
import type { Filter, FunctionClass, Order, Overview, Row, Status } from './service/types';
import { formatBytes, formatCount, formatDuration, formatTime, type TimeMode } from './state/format';
import { useTheme } from './state/useTheme';
import { TimelineTable } from './table/TimelineTable';

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

function FileStrip({ ov }: { ov: Overview }) {
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
      <span className="ml-auto flex items-center gap-2">
        {ov.quality !== 'ok' && (
          <span className="inline-flex items-center gap-1 text-wire">
            <WarningIcon size={14} />
            Quality: {ov.quality_count} {ov.quality}
          </span>
        )}
        <span className="inline-flex items-center gap-1 text-muted" title="Exports arrive with M3">
          <ExportIcon size={14} /> Export
        </span>
      </span>
    </div>
  );
}

/** Quick filters for the M0 shell; the filter bar with chips and text grammar (R7) comes with M1. */
function QuickFilters({ filter, onChange, total }: { filter: Filter; onChange: (f: Filter) => void; total: number }) {
  const classes: FunctionClass[] = ['read', 'write'];
  const statuses: Status[] = ['exception', 'unanswered'];
  const toggle = <T,>(list: T[] | undefined, v: T) => (list?.includes(v) ? list.filter((x) => x !== v) : [...(list ?? []), v]);
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-line px-4 py-2 text-[13px]">
      {classes.map((c) => (
        <button key={c} type="button" aria-pressed={filter.classes?.includes(c) ?? false} onClick={() => onChange({ ...filter, classes: toggle(filter.classes, c) })}>
          <Chip tone={filter.classes?.includes(c) ? 'accent' : 'neutral'}>class:{c}</Chip>
        </button>
      ))}
      {statuses.map((s) => (
        <button key={s} type="button" aria-pressed={filter.statuses?.includes(s) ?? false} onClick={() => onChange({ ...filter, statuses: toggle(filter.statuses, s) })}>
          <Chip tone={filter.statuses?.includes(s) ? 'accent' : 'neutral'}>status:{s}</Chip>
        </button>
      ))}
      <span className="ml-auto font-mono text-muted">{formatCount(total)} rows</span>
    </div>
  );
}

export function App({ service, platform }: AppProps) {
  const [theme, toggleTheme] = useTheme(platform);
  const [overview, setOverview] = useState<Overview | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [view, setView] = useState('timeline');
  const [filter, setFilter] = useState<Filter>({});
  const [order, setOrder] = useState<Order>('time');
  const [desc, setDesc] = useState(false);
  const [timeMode, setTimeMode] = useState<TimeMode>('local');
  const [selected, setSelected] = useState<{ rank: number; row: Row } | null>(null);
  const [total, setTotal] = useState(0);

  const refresh = useCallback(() => {
    service.overview().then(setOverview, () => setOverview(null));
  }, [service]);
  useEffect(refresh, [refresh]);

  const opened = useCallback(
    (paths: string[]) => {
      if (paths.length === 0) return;
      // Opening and analysing a trace arrive with M1; the shell shows what it was given.
      setNotice(`Opening traces arrives with M1. Received: ${paths.join(', ')}`);
      refresh();
    },
    [refresh],
  );
  useEffect(() => platform.onOpen(opened), [platform, opened]);

  const onSelect = useCallback((rank: number, row: Row) => setSelected({ rank, row }), []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return;
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'o') {
        e.preventDefault();
        void platform.chooseTraces().then(opened);
      } else if (!e.metaKey && !e.ctrlKey && /^[1-6]$/.test(e.key)) {
        setView(VIEWS[Number(e.key) - 1]?.key ?? 'timeline');
      } else if (!e.metaKey && !e.ctrlKey && e.key.toLowerCase() === 't') {
        setTimeMode((m) => TIME_MODES[(TIME_MODES.indexOf(m) + 1) % TIME_MODES.length] ?? 'local');
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [platform, opened]);

  return (
    <div className="flex h-full flex-col">
      <Header theme={theme} onToggleTheme={toggleTheme} />
      {overview === null ? (
        <EmptyState onOpen={() => void platform.chooseTraces().then(opened)} notice={notice} />
      ) : (
        <>
          <FileStrip ov={overview} />
          <QuickFilters
            filter={filter}
            onChange={(f) => {
              setFilter(f);
              setSelected(null);
            }}
            total={total}
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
                  service={service}
                  filter={filter}
                  order={order}
                  desc={desc}
                  timeMode={timeMode}
                  firstNs={overview.first_ns}
                  selected={selected?.rank ?? null}
                  onSelect={onSelect}
                  onTotal={setTotal}
                />
              ) : (
                <p className="p-6 text-muted">This view arrives with M2.</p>
              )}
            </main>
            <aside aria-label="Detail" className="w-80 shrink-0 overflow-y-auto border-l border-line p-4 text-[13px]">
              {selected ? (
                <div className="flex flex-col gap-2">
                  <span className="ot-label">Row {formatCount(selected.rank + 1)}</span>
                  <p className="text-fg-strong">{selected.row.summary}</p>
                  <p>{selected.row.result}</p>
                  <p className="text-muted">The detail pane (fields, raw bytes, packets) arrives with M1.</p>
                </div>
              ) : (
                <p className="text-muted">Select a row.</p>
              )}
            </aside>
          </div>
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
          </footer>
        </>
      )}
    </div>
  );
}

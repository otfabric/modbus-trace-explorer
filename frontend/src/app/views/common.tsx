import { useEffect, useState, type ReactNode } from 'react';
import { ErrorIcon } from '../icons';
import { toServiceError } from '../service/service';
import type { Distribution } from '../service/types';
import { formatCount, formatLatency } from '../state/format';

/** The result of a service call that reruns when its inputs change. */
export function useLoad<T>(load: (signal: AbortSignal) => Promise<T>, deps: unknown[]): { data: T | null; error: string | null; loading: boolean } {
  const [state, setState] = useState<{ data: T | null; error: string | null; loading: boolean }>({ data: null, error: null, loading: true });
  useEffect(() => {
    const ctl = new AbortController();
    setState((s) => ({ ...s, loading: true, error: null }));
    load(ctl.signal).then(
      (data) => !ctl.signal.aborted && setState({ data, error: null, loading: false }),
      (err: unknown) => !ctl.signal.aborted && setState({ data: null, error: toServiceError(err).message, loading: false }),
    );
    return () => ctl.abort();
  }, deps);
  return state;
}

/** What a view shows before its data: loading, an error, or nothing to show. */
export function Status({ loading, error, empty }: { loading: boolean; error: string | null; empty?: string }) {
  if (error)
    return (
      <p role="alert" className="flex items-center gap-2 p-6 text-danger">
        <ErrorIcon size={14} /> {error}
      </p>
    );
  if (loading) return <p className="p-6 text-muted">Loading…</p>;
  if (empty) return <p className="p-6 text-muted">{empty}</p>;
  return null;
}

export const TABLE = 'w-full border-collapse font-mono text-[12px] [&_td]:border-b [&_td]:border-line [&_td]:px-2 [&_td]:py-1 [&_th]:px-2 [&_th]:py-1.5 [&_th]:text-left';

export function Th({ children, right }: { children?: ReactNode; right?: boolean }) {
  return <th className={`ot-label sticky top-0 bg-bg ${right ? 'text-right!' : ''}`}>{children}</th>;
}

/** A clickable table row that the keyboard can select too. */
export function SelectRow({ selected, onSelect, children }: { selected: boolean; onSelect: () => void; children: ReactNode }) {
  return (
    <tr
      tabIndex={0}
      aria-selected={selected}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect();
        }
      }}
      className={`cursor-pointer ${selected ? 'bg-accent-soft text-accent-strong' : 'hover:bg-surface'}`}
    >
      {children}
    </tr>
  );
}

export function Heading({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-3 pb-2">
      <h2 className="font-display text-lg font-semibold text-fg-strong">{children}</h2>
      <span className="ml-auto flex items-center gap-2">{actions}</span>
    </div>
  );
}

/** Label and value pairs. */
export function Facts({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-[13px]">
      {items.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-muted">{k}</dt>
          <dd className="font-mono">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

/** A chart with its data as a table, one switch away (charts always have a table alternative). */
export function ChartOrTable({ label, chart, table }: { label: string; chart: ReactNode; table: ReactNode }) {
  const [asTable, setAsTable] = useState(false);
  return (
    <figure aria-label={label} className="flex flex-col gap-1">
      <figcaption className="flex items-center gap-2">
        <span className="ot-label">{label}</span>
        <button type="button" className="ml-auto text-[12px] text-muted hover:text-accent" aria-pressed={asTable} onClick={() => setAsTable((v) => !v)}>
          {asTable ? 'Show chart' : 'Show table'}
        </button>
      </figcaption>
      {asTable ? table : chart}
    </figure>
  );
}

/** A distribution of durations: quantiles, and its histogram as bars or a table. */
export function DistributionView({ label, dist }: { label: string; dist?: Distribution }) {
  if (!dist || dist.count === 0) return <p className="text-[13px] text-muted">{label}: none observed.</p>;
  const max = Math.max(1, ...dist.histogram.map((b) => b.count));
  const w = 24;
  const h = 80;
  return (
    <div className="flex flex-col gap-2">
      <Facts
        items={[
          ['count', formatCount(dist.count)],
          ['min', formatLatency(dist.min_ns)],
          ['median', formatLatency(dist.p50_ns)],
          ['p95', formatLatency(dist.p95_ns)],
          ['p99', formatLatency(dist.p99_ns)],
          ['max', formatLatency(dist.max_ns)],
        ]}
      />
      <ChartOrTable
        label={label}
        chart={
          <svg role="img" aria-label={`${label} histogram`} width={dist.histogram.length * w} height={h + 16} className="max-w-full overflow-visible">
            {dist.histogram.map((b, i) => {
              const bh = b.count === 0 ? 0 : Math.max(2, (b.count / max) * h);
              return (
                <g key={b.lo_ns}>
                  <title>{`${formatLatency(b.lo_ns)} – ${formatLatency(b.hi_ns)}: ${formatCount(b.count)}`}</title>
                  <rect x={i * w + 2} y={h - bh} width={w - 4} height={bh} className="fill-accent" />
                  {i % 3 === 0 && (
                    <text x={i * w + 2} y={h + 12} className="fill-muted font-mono text-[9px]">
                      {formatLatency(b.lo_ns)}
                    </text>
                  )}
                </g>
              );
            })}
          </svg>
        }
        table={
          <table className={TABLE}>
            <thead>
              <tr>
                <Th>from</Th>
                <Th>below</Th>
                <Th right>count</Th>
              </tr>
            </thead>
            <tbody>
              {dist.histogram.map((b) => (
                <tr key={b.lo_ns}>
                  <td>{formatLatency(b.lo_ns)}</td>
                  <td>{formatLatency(b.hi_ns)}</td>
                  <td className="text-right">{formatCount(b.count)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        }
      />
    </div>
  );
}

/** "12" or "12–19" for an inclusive address range. */
export function range(start: number, end: number): string {
  return start === end ? String(start) : `${start}–${end}`;
}

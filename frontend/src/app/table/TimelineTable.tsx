import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode, type WheelEvent } from 'react';
import { DotIcon, ErrorIcon, InfoIcon, PlugIcon, PlugOffIcon, ReadIcon, WarningIcon, WriteIcon } from '../icons';
import type { Filter, Order, Row, TimelinePage, TimelineQuery } from '../service/types';
import { formatCount, formatFunction, formatLatency, formatTime, type TimeMode } from '../state/format';
import { rankAt, reveal, scaled, scrollFor, spacerHeight, visibleRows, type Geometry } from './scroll';

export const ROW_HEIGHT = 24;
/** Rows fetched beyond the visible window on each side. */
const MARGIN = 40;

interface Props {
  /** Fetches a window of the timeline (Service.timeline for the open trace). */
  fetchPage: (q: TimelineQuery, signal: AbortSignal) => Promise<TimelinePage>;
  filter: Filter;
  order: Order;
  desc: boolean;
  timeMode: TimeMode;
  firstNs: number;
  /** The selected row (by seq), highlighted wherever it is. */
  selectedSeq: number | null;
  onSelect: (rank: number, row: Row) => void;
  onTotal?: (total: number) => void;
}

interface Window {
  /** The filter and order the rows belong to. */
  key: string;
  offset: number;
  rows: Row[];
  total: number;
}

const COLUMNS = ['#', 'Time', 'Client', 'Server', 'Unit', 'TID', 'Function', 'Summary', 'Result', 'Latency', 'Flags'];
const GRID = 'grid grid-cols-[5.5rem_8.5rem_7.5rem_9.5rem_3rem_4rem_9.5rem_minmax(14rem,1fr)_minmax(9rem,14rem)_5.5rem_4rem]';

function KindIcon({ row }: { row: Row }) {
  switch (row.kind) {
    case 'conn_open':
      return <PlugIcon size={14} />;
    case 'conn_close':
      return <PlugOffIcon size={14} />;
    case 'gap':
    case 'resync':
      return <WarningIcon size={14} className="text-wire" />;
    case 'note':
      return <InfoIcon size={14} className="text-blue" />;
    default:
      return row.class === 'write' ? <WriteIcon size={14} /> : row.class === 'read' ? <ReadIcon size={14} /> : <DotIcon size={14} />;
  }
}

function ResultCell({ row }: { row: Row }): ReactNode {
  if (row.status === 'exception') {
    return (
      <span className="inline-flex items-center gap-1 text-danger">
        <ErrorIcon size={14} />
        {row.result}
      </span>
    );
  }
  if (row.status && row.status !== 'ok') {
    return (
      <span className="inline-flex items-center gap-1 text-wire">
        <WarningIcon size={14} />
        {row.result}
      </span>
    );
  }
  return row.result;
}

/**
 * The timeline: a window of rows over the engine's index, with a scaled
 * scrollbar so that any number of rows can be reached (scroll.ts). Only the
 * visible rows and a margin are fetched; a change of filter or order aborts
 * the request in flight.
 */
export function TimelineTable({ fetchPage, filter, order, desc, timeMode, firstNs, selectedSeq, onSelect, onTotal }: Props) {
  const scroller = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState(600);
  const [first, setFirst] = useState(0);
  const [win, setWin] = useState<Window>({ key: '', offset: 0, rows: [], total: 0 });
  const [error, setError] = useState<string | null>(null);
  const programmatic = useRef(false);
  // The first visible row as of the last input, ahead of React's render.
  const firstRef = useRef(0);
  const wheelRest = useRef(0);

  const geometry: Geometry = { total: win.total, rowHeight: ROW_HEIGHT, viewport };
  const visible = visibleRows(geometry);

  // The viewport's height follows the window.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const measure = () => setViewport(Math.max(ROW_HEIGHT, el.clientHeight - ROW_HEIGHT));
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // A new filter or order starts at the top.
  const queryKey = JSON.stringify([filter, order, desc]);
  useEffect(() => {
    setFirst(0);
    firstRef.current = 0;
    current.current = null;
    programmatic.current = true;
    if (scroller.current) scroller.current.scrollTop = 0;
  }, [queryKey]);

  // Fetch when the visible range leaves what is loaded.
  useEffect(() => {
    // Nothing to fetch when the loaded window is of this query and covers
    // every visible row (an empty result covers everything).
    const covered = first >= win.offset && Math.min(win.total, first + visible) <= win.offset + win.rows.length;
    if (win.key === queryKey && covered) return;
    const ctl = new AbortController();
    const offset = Math.max(0, first - MARGIN);
    fetchPage({ filter, order, desc, offset, limit: visible + 2 * MARGIN }, ctl.signal)
      .then((page) => {
        setWin({ ...page, key: queryKey });
        setError(null);
        onTotal?.(page.total);
      })
      .catch((err: unknown) => {
        if (!ctl.signal.aborted) setError(err instanceof Error ? err.message : String(err));
      });
    return () => ctl.abort();
    // win is deliberately not a dependency: a new window must not refetch itself.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fetchPage, queryKey, first, visible]);

  const moveTo = useCallback(
    (rank: number) => {
      const g: Geometry = { total: win.total, rowHeight: ROW_HEIGHT, viewport };
      const next = Math.max(0, Math.min(rank, Math.max(0, win.total - visibleRows(g))));
      firstRef.current = next;
      setFirst(next);
      programmatic.current = true;
      if (scroller.current) scroller.current.scrollTop = scrollFor(g, next);
    },
    [win.total, viewport],
  );

  const onScroll = () => {
    if (programmatic.current) {
      programmatic.current = false;
      return;
    }
    const el = scroller.current;
    if (el) {
      firstRef.current = rankAt(geometry, el.scrollTop);
      setFirst(firstRef.current);
    }
  };

  // Above the spacer cap the wheel moves by rows, not by scaled pixels.
  const onWheel = (e: WheelEvent) => {
    if (!scaled(geometry)) return;
    e.preventDefault();
    wheelRest.current += e.deltaMode === 1 ? e.deltaY * ROW_HEIGHT : e.deltaY;
    const rows = Math.trunc(wheelRest.current / ROW_HEIGHT);
    if (rows !== 0) {
      wheelRest.current -= rows * ROW_HEIGHT;
      moveTo(firstRef.current + rows);
    }
  };

  // The selected rank as of the last key or click, so that key repeat does
  // not read a selection React has not rendered yet. A new filter or order
  // starts without one.
  const current = useRef<number | null>(null);
  useEffect(() => {
    if (selectedSeq === null) current.current = null;
  }, [selectedSeq]);
  const select = (rank: number) => {
    const clamped = Math.max(0, Math.min(rank, win.total - 1));
    current.current = clamped;
    const row = win.rows[clamped - win.offset];
    const top = reveal(geometry, firstRef.current, clamped);
    firstRef.current = top;
    moveTo(top);
    if (row) onSelect(clamped, row);
    else pendingSelect.current = clamped;
  };
  // A keyboard jump to a row not loaded yet selects it when it arrives.
  const pendingSelect = useRef<number | null>(null);
  useEffect(() => {
    const p = pendingSelect.current;
    if (p === null) return;
    const row = win.rows[p - win.offset];
    if (row) {
      pendingSelect.current = null;
      onSelect(p, row);
    }
  }, [win, onSelect]);

  const onKeyDown = (e: KeyboardEvent) => {
    const cur = current.current ?? firstRef.current;
    const keys: Record<string, number> = {
      ArrowDown: cur + 1,
      ArrowUp: cur - 1,
      PageDown: cur + visible,
      PageUp: cur - visible,
      Home: 0,
      End: win.total - 1,
    };
    const target = keys[e.key];
    if (target === undefined || win.total === 0) return;
    e.preventDefault();
    select(target);
  };

  const rows: { rank: number; row: Row | undefined }[] = [];
  for (let k = first; k < Math.min(win.total, first + visible + 1); k++) {
    rows.push({ rank: k, row: win.rows[k - win.offset] });
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={scroller}
        role="grid"
        aria-label="Timeline"
        aria-rowcount={win.total + 1}
        aria-colcount={COLUMNS.length}
        tabIndex={0}
        className="relative min-h-0 flex-1 overflow-y-auto overflow-x-auto font-mono text-[12.5px] outline-none"
        onScroll={onScroll}
        onWheel={onWheel}
        onKeyDown={onKeyDown}
      >
        <div style={{ height: spacerHeight(geometry) + ROW_HEIGHT }} className="relative min-w-[78rem]">
          <div className="sticky top-0 z-10">
            <div role="row" aria-rowindex={1} className={`${GRID} h-6 items-center gap-x-3 border-b border-line bg-surface px-3`}>
              {COLUMNS.map((c) => (
                <span key={c} role="columnheader" className="ot-label truncate">
                  {c}
                </span>
              ))}
            </div>
            {rows.map(({ rank, row }) => (
              <div
                key={rank}
                role="row"
                aria-rowindex={rank + 2}
                aria-selected={row !== undefined && row.seq === selectedSeq}
                onClick={() => {
                  if (!row) return;
                  current.current = rank;
                  onSelect(rank, row);
                }}
                className={`${GRID} h-6 cursor-default items-center gap-x-3 border-b border-line/60 px-3 ${
                  row !== undefined && row.seq === selectedSeq ? 'bg-accent-soft text-accent-strong' : 'hover:bg-surface'
                } ${row?.class === 'write' ? 'border-l-2 border-l-wire' : 'border-l-2 border-l-transparent'}`}
              >
                {row ? (
                  <>
                    <span role="gridcell" className="truncate text-muted">
                      {row.seq}
                    </span>
                    <span role="gridcell" className="truncate">
                      {formatTime(row.t_ns, timeMode, firstNs)}
                    </span>
                    <span role="gridcell" className="truncate">
                      {row.client}
                    </span>
                    <span role="gridcell" className="truncate">
                      {row.server}
                    </span>
                    <span role="gridcell">{row.unit ?? ''}</span>
                    <span role="gridcell">{row.tid ?? ''}</span>
                    <span role="gridcell" className="flex items-center gap-1 truncate">
                      <KindIcon row={row} />
                      {row.class === 'write' && <b className="text-wire">W</b>}
                      {formatFunction(row.fc)}
                    </span>
                    <span role="gridcell" className="truncate font-sans">
                      {row.summary}
                    </span>
                    <span role="gridcell" className="truncate font-sans">
                      <ResultCell row={row} />
                    </span>
                    <span role="gridcell" className="text-right">
                      {formatLatency(row.latency_ns)}
                    </span>
                    <span role="gridcell" className="truncate text-wire" title={row.flags.join(', ')}>
                      {row.flags.length > 0 ? <WarningIcon size={14} /> : null}
                    </span>
                  </>
                ) : (
                  <span role="gridcell" className="col-span-full text-muted">
                    …
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
      {error && (
        <p role="alert" className="border-t border-line px-3 py-1 text-danger">
          {error}
        </p>
      )}
      <p className="sr-only" aria-live="polite">
        {formatCount(win.total)} rows
      </p>
    </div>
  );
}

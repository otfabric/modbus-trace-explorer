import { useEffect, useRef, useState } from 'react';
import { Button, Field, TextInput, Toggle } from '../components/ui';
import { CloseIcon, InfoIcon, ReadIcon } from '../icons';
import { toServiceError, type Service } from '../service/service';
import type { AddressInterval, CoverageMap, Filter, HistoryChunk, HistoryPoint } from '../service/types';
import { formatCount, formatTime, type TimeMode } from '../state/format';
import { ChartOrTable, Status, TABLE, Th, range, useLoad } from './common';

interface Props {
  service: Service;
  fileId: string;
  filter: Filter;
  timeMode: TimeMode;
  firstNs: number;
  /** The Settings choice; the view's toggle starts from it. */
  oneBased?: boolean;
}

type Radix = 'dec' | 'hex' | 'bin';

export function formatValue(v: number, radix: Radix, bits: boolean): string {
  if (bits || radix === 'dec') return String(v);
  if (radix === 'hex') return `0x${v.toString(16).toUpperCase().padStart(4, '0')}`;
  return v.toString(2).padStart(16, '0').replace(/(\d{4})(?=\d)/g, '$1 ');
}

const isBits = (space: string) => space === 'coil' || space === 'discrete';

/** The rows of the history table drawn at most; the chart shows every point. */
const TABLE_ROWS = 2000;

/**
 * An axis for sparse address ranges: stretches of covered addresses (with
 * holes under near) share the width by length, and the wider empty
 * stretches between them collapse to a fixed gap.
 */
export function compressedAxis(intervals: AddressInterval[], width: number, gap = 10, near = 64): { x: (addr: number) => number; spans: { start: number; end: number }[] } {
  const spans: { start: number; end: number }[] = [];
  for (const iv of [...intervals].sort((a, b) => a.start - b.start)) {
    const last = spans[spans.length - 1];
    if (last && iv.start <= last.end + near) last.end = Math.max(last.end, iv.end);
    else spans.push({ start: iv.start, end: iv.end });
  }
  const covered = spans.reduce((n, s) => n + s.end - s.start + 1, 0);
  const scale = Math.max(0, width - gap * Math.max(0, spans.length - 1)) / Math.max(1, covered);
  const offsets: number[] = [];
  let at = 0;
  for (const s of spans) {
    offsets.push(at);
    at += (s.end - s.start + 1) * scale + gap;
  }
  const x = (addr: number) => {
    for (let i = spans.length - 1; i >= 0; i--) {
      const s = spans[i] as { start: number; end: number };
      if (addr >= s.start) return (offsets[i] ?? 0) + (Math.min(addr, s.end + 1) - s.start) * scale;
    }
    return 0;
  };
  return { x, spans };
}

/** The span starts to label: the widest spans first, none closer than 40 units to another. */
function labels(spans: { start: number; end: number }[], x: (a: number) => number) {
  const kept: { start: number; end: number }[] = [];
  for (const sp of [...spans].sort((a, b) => b.end - b.start - (a.end - a.start))) {
    if (kept.every((k) => Math.abs(x(k.start) - x(sp.start)) > 40)) kept.push(sp);
  }
  return kept;
}

/** The read and written ranges of one space as a strip: reads solid, writes hatched copper. */
function CoverageStrip({ map, base, onPick }: { map: CoverageMap; base: number; onPick: (addr: number) => void }) {
  const all = [...map.read, ...map.written];
  if (all.length === 0) return <p className="text-[13px] text-muted">Nothing of this space was read or written.</p>;
  const M = 56;
  const W = 860;
  const axis = compressedAxis(all, W - M);
  const x = (a: number) => M + axis.x(a);
  const bar = (iv: AddressInterval, y: number, cls: string, what: string) => (
    <rect
      key={`${what}${iv.start}`}
      x={x(iv.start)}
      y={y}
      width={Math.max(1.5, x(iv.end + 1) - x(iv.start))}
      height={16}
      className={`${cls} cursor-pointer`}
      onClick={() => onPick(iv.start)}
    >
      <title>{`${what} ${range(iv.start + base, iv.end + base)}: ${formatCount(iv.count)} times`}</title>
    </rect>
  );
  const table = (
    <table className={TABLE}>
      <thead>
        <tr>
          <Th>addresses</Th>
          <Th>kind</Th>
          <Th right>times</Th>
        </tr>
      </thead>
      <tbody>
        {[...map.read.map((iv) => ['read', iv] as const), ...map.written.map((iv) => ['written', iv] as const)]
          .sort((a, b) => a[1].start - b[1].start)
          .map(([what, iv]) => (
            <tr key={`${what}${iv.start}`} className="cursor-pointer hover:bg-surface" onClick={() => onPick(iv.start)}>
              <td>{range(iv.start + base, iv.end + base)}</td>
              <td>{what}</td>
              <td className="text-right">{formatCount(iv.count)}</td>
            </tr>
          ))}
      </tbody>
    </table>
  );
  return (
    <ChartOrTable
      label={`Coverage${base ? ' (1-based)' : ''}: ${axis.spans.map((sp) => range(sp.start + base, sp.end + base)).join(', ')}`}
      chart={
        <svg role="img" aria-label="Address coverage" viewBox={`0 0 ${W} 64`} className="w-full">
          <defs>
            <pattern id="written" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
              <rect width="6" height="6" className="fill-bg" />
              <line x1="0" y1="0" x2="0" y2="6" strokeWidth="3" className="stroke-wire" />
            </pattern>
          </defs>
          <text x={0} y={13} className="fill-muted font-mono text-[10px]">
            read
          </text>
          {map.read.map((iv) => bar(iv, 2, 'fill-accent', 'read'))}
          <text x={0} y={33} className="fill-muted font-mono text-[10px]">
            written
          </text>
          {map.written.map((iv) => bar(iv, 22, 'fill-[url(#written)] stroke-wire', 'written'))}
          {labels(axis.spans, x).map((sp) => (
              <text key={sp.start} x={x(sp.start)} y={52} className="fill-muted font-mono text-[9px]">
                {sp.start + base}
              </text>
            ))}
        </svg>
      }
      table={table}
    />
  );
}

/** The values over time as a step line; writes are copper dots. */
function StepChart({ points, radix, bits }: { points: HistoryPoint[]; radix: Radix; bits: boolean }) {
  if (points.length === 0) return null;
  const W = 800;
  const H = 140;
  const t0 = points[0]?.t_ns ?? 0;
  const t1 = points[points.length - 1]?.t_ns ?? t0;
  const vs = points.map((p) => p.value);
  const vmin = Math.min(...vs);
  const vmax = Math.max(...vs);
  const x = (t: number) => ((t - t0) / Math.max(1, t1 - t0)) * W;
  const y = (v: number) => H - 6 - ((v - vmin) / Math.max(1, vmax - vmin)) * (H - 12);
  // At most one vertex pair per pixel column keeps a long history drawable.
  const step = Math.max(1, Math.floor(points.length / (W * 2)));
  let d = '';
  let prevY = 0;
  points.forEach((p, i) => {
    if (i % step !== 0 && i !== points.length - 1) return;
    const px = x(p.t_ns).toFixed(1);
    const py = y(p.value).toFixed(1);
    d += i === 0 ? `M${px},${py}` : `L${px},${prevY}L${px},${py}`;
    prevY = Number(py);
  });
  const writes = points.filter((p) => p.write).slice(0, 2000);
  return (
    <svg role="img" aria-label="Values over time" viewBox={`0 0 ${W} ${H}`} className="w-full" preserveAspectRatio="none">
      <text x={2} y={10} className="fill-muted font-mono text-[9px]">
        {formatValue(vmax, radix, bits)}
      </text>
      <text x={2} y={H - 1} className="fill-muted font-mono text-[9px]">
        {formatValue(vmin, radix, bits)}
      </text>
      <path d={d} fill="none" strokeWidth={1.5} vectorEffect="non-scaling-stroke" className="stroke-accent" />
      {writes.map((p) => (
        <circle key={p.seq} cx={x(p.t_ns)} cy={y(p.value)} r={3} className="fill-wire" />
      ))}
    </svg>
  );
}

/** R11: what was read and written of a device, and the observed values of one address. */
export function AddressesView({ service, fileId, filter, timeMode, firstNs, oneBased: oneBasedSetting = false }: Props) {
  const devices = useLoad(() => service.devices(fileId, filter), [service, fileId, JSON.stringify(filter)]);
  const [device, setDevice] = useState<string>('');
  const [space, setSpace] = useState<string>('holding');
  const list = devices.data ?? [];
  const dev = list.find((d) => `${d.server}/${d.unit}` === device) ?? list[0];
  const coverage = useLoad(
    () => (dev ? service.coverage(fileId, { server: dev.server, unit: dev.unit, space, filter }) : Promise.resolve(null)),
    [service, fileId, dev?.server, dev?.unit, space, JSON.stringify(filter)],
  );

  const [address, setAddress] = useState('');
  const [every, setEvery] = useState(false);
  const [radix, setRadix] = useState<Radix>('dec');
  // Addresses are zero-based as sent; the 1-based reference is a label only.
  const [oneBased, setOneBased] = useState(oneBasedSetting);
  useEffect(() => setOneBased(oneBasedSetting), [oneBasedSetting]);
  const base = oneBased ? 1 : 0;
  const [points, setPoints] = useState<HistoryPoint[]>([]);
  const [job, setJob] = useState<{ id: string; scanned: number; candidates: number; done: boolean; next?: number } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const jobId = useRef<string | null>(null);
  // Chunks that arrived before the job's id was known.
  const early = useRef(new Map<string, HistoryChunk[]>());

  const take = (c: HistoryChunk) => {
    setPoints((ps) => (c.points.length ? ps.concat(c.points) : ps));
    setJob({ id: c.job, scanned: c.scanned, candidates: c.candidates, done: c.done, next: c.next });
  };

  useEffect(
    () =>
      service.subscribe((ev) => {
        if (ev.kind === 'history' && ev.history) {
          if (ev.history.job === jobId.current) take(ev.history);
          else if (jobId.current === null) early.current.set(ev.history.job, [...(early.current.get(ev.history.job) ?? []), ev.history]);
        } else if (ev.job?.kind === 'history' && ev.job.id === jobId.current && ev.job.state !== 'running') {
          if (ev.job.state === 'failed') setError(ev.job.error?.message ?? 'reading the history failed');
          setJob((j) => (j ? { ...j, done: true } : j));
        }
      }),
    [service],
  );

  // A new device or space ends the history shown.
  useEffect(() => {
    if (jobId.current) void service.cancelJob(jobId.current).catch(() => {});
    jobId.current = null;
    setPoints([]);
    setJob(null);
  }, [service, dev?.server, dev?.unit, space]);

  const load = async (after: number, fresh: boolean) => {
    const addr = Number(address);
    if (!dev || address === '' || !Number.isInteger(addr) || addr < 0 || addr > 65535) {
      setError('Enter an address from 0 to 65535.');
      return;
    }
    setError(null);
    if (jobId.current) await service.cancelJob(jobId.current).catch(() => {});
    if (fresh) setPoints([]);
    jobId.current = null;
    early.current.clear();
    try {
      const ref = await service.addressHistory(fileId, { server: dev.server, unit: dev.unit, space, address: addr, after, limit: 0, every });
      jobId.current = ref.id;
      setJob({ id: ref.id, scanned: after, candidates: 0, done: false });
      for (const c of early.current.get(ref.id) ?? []) take(c);
      early.current.clear();
    } catch (err) {
      setError(toServiceError(err).message);
    }
  };

  const cancel = () => {
    if (jobId.current) void service.cancelJob(jobId.current);
  };

  if (!devices.data) return <Status loading={devices.loading} error={devices.error} />;
  if (!dev) return <Status loading={false} error={null} empty="No devices match the filter." />;
  const bits = isBits(space);
  const spaces = coverage.data?.spaces.length ? coverage.data.spaces : ['coil', 'discrete', 'holding', 'input'];
  const running = job !== null && !job.done;
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-4">
      <p className="flex items-center gap-2 text-[13px] text-muted">
        <InfoIcon size={14} /> Observed, not a complete register map: only what clients read or wrote in this trace.
      </p>
      <div className="flex flex-wrap items-end gap-3">
        <Field label="Device">
          <select className="h-8 rounded border border-control bg-bg px-2 font-mono text-[13px]" value={`${dev.server}/${dev.unit}`} onChange={(e) => setDevice(e.target.value)}>
            {list.map((d) => (
              <option key={`${d.server}/${d.unit}`} value={`${d.server}/${d.unit}`}>
                {d.server} unit {d.unit}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Space">
          <select className="h-8 rounded border border-control bg-bg px-2 font-mono text-[13px]" value={space} onChange={(e) => setSpace(e.target.value)}>
            {['coil', 'discrete', 'holding', 'input'].map((s) => (
              <option key={s} value={s} disabled={!spaces.includes(s)}>
                {s}
              </option>
            ))}
          </select>
        </Field>
        <Toggle label="1-based labels" checked={oneBased} onChange={setOneBased} />
      </div>
      {coverage.data ? <CoverageStrip map={coverage.data} base={base} onPick={(a) => setAddress(String(a))} /> : <Status loading={coverage.loading} error={coverage.error} />}
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void load(0, true);
        }}
      >
        <Field label="Address (as sent, 0-based)">
          <TextInput aria-label="Address" value={address} onChange={(e) => setAddress(e.target.value)} className="w-28" inputMode="numeric" />
        </Field>
        {oneBased && /^\d+$/.test(address) && <span className="pb-1.5 font-mono text-[12px] text-muted">reference {Number(address) + 1}</span>}
        <Toggle label="Every read, not only changes" checked={every} onChange={setEvery} />
        <Button type="submit" variant="primary" icon={<ReadIcon size={14} />}>
          Show values
        </Button>
        {running && (
          <Button icon={<CloseIcon size={14} />} onClick={cancel}>
            Cancel
          </Button>
        )}
        {!bits && (
          <span className="ml-auto flex items-center gap-1 text-[12px]" role="group" aria-label="Number format">
            {(['dec', 'hex', 'bin'] as Radix[]).map((r) => (
              <button
                key={r}
                type="button"
                aria-pressed={radix === r}
                onClick={() => setRadix(r)}
                className={`rounded border px-2 py-0.5 font-mono ${radix === r ? 'border-accent text-accent' : 'border-line text-muted hover:text-accent'}`}
              >
                {r}
              </button>
            ))}
          </span>
        )}
      </form>
      {error && (
        <p role="alert" className="text-danger">
          {error}
        </p>
      )}
      {job && (
        <p className="font-mono text-[12px] text-muted" aria-live="polite">
          {formatCount(points.length)} values from {formatCount(job.scanned)} of {formatCount(job.candidates)} transactions
          {running ? '…' : ''}
          {job.done && job.next !== undefined && (
            <button type="button" className="ml-3 text-accent hover:underline" onClick={() => void load(job.next ?? 0, false)}>
              Load more
            </button>
          )}
        </p>
      )}
      {points.length > 0 && (
        <ChartOrTable
          label="Values"
          chart={<StepChart points={points} radix={radix} bits={bits} />}
          table={
            <div className="max-h-96 overflow-auto">
              <table className={TABLE} aria-label="Values">
                <thead>
                  <tr>
                    <Th>time</Th>
                    <Th right>value</Th>
                    <Th>kind</Th>
                    <Th>client</Th>
                    <Th right>row</Th>
                  </tr>
                </thead>
                <tbody>
                  {points.slice(0, TABLE_ROWS).map((p) => (
                    <tr key={`${p.seq}${p.write ? 'w' : ''}`}>
                      <td>{formatTime(p.t_ns, timeMode, firstNs)}</td>
                      <td className="text-right">{formatValue(p.value, radix, bits)}</td>
                      <td className={p.write ? 'text-wire' : ''}>{p.write ? 'write' : p.changed ? 'changed' : 'read'}</td>
                      <td>{p.client}</td>
                      <td className="text-right">{p.seq}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {points.length > TABLE_ROWS && <p className="p-2 text-[12px] text-muted">The first {formatCount(TABLE_ROWS)} of {formatCount(points.length)} values.</p>}
            </div>
          }
        />
      )}
    </div>
  );
}

import { useEffect, useRef, useState, type ReactNode } from 'react';
import { CopyIcon, ErrorIcon, ExportIcon, InfoIcon, WarningIcon } from '../icons';
import type { Platform, SaveKind } from '../platform/platform';
import { toServiceError, type Service } from '../service/service';
import type { DeviceStats, ExportResult, Filter, JobRef, JobState, MBCAPExport, MBCAPPreview } from '../service/types';
import { formatBytes, formatCount } from '../state/format';
import { Dialog } from './Dialog';
import { Button, Chip, Field, TextInput, Toggle } from './ui';

type Tab = 'mbcap' | 'timeline' | 'subtrace';
type Scope = 'all' | 'filter' | 'device';

interface Props {
  service: Service;
  platform: Platform;
  fileId: string;
  /** The trace's name, for the proposed file names. */
  name: string;
  filter: Filter;
  firstNs: number;
  onClose: () => void;
}

const TICKS = [
  { ns: 0, label: '1 µs (default)' },
  { ns: 1, label: '1 ns' },
  { ns: 1_000_000, label: '1 ms' },
];

const SELECT = 'h-8 rounded border border-control bg-bg px-2 font-mono text-[13px]';

/** "site.pcapng.gz" -> "site". */
export function baseName(name: string): string {
  return name.replace(/ \+ \d+ more$/, '').replace(/(\.gz)?$/, '').replace(/\.(pcapng|pcap|cap)$/, '');
}

/** Seconds from the first packet ("" for none) as an absolute time. */
function offset(firstNs: number, s: string): number | undefined {
  const v = Number(s);
  return s.trim() === '' || !Number.isFinite(v) || v < 0 ? undefined : firstNs + Math.round(v * 1e9);
}

function Radio<T extends string>({ name, value, options, onChange }: { name: string; value: T; options: [T, string][]; onChange: (v: T) => void }) {
  return (
    <div role="radiogroup" aria-label={name} className="flex flex-wrap gap-3 text-[13px]">
      {options.map(([v, label]) => (
        <label key={v} className="flex cursor-pointer items-center gap-1.5">
          <input type="radio" name={name} checked={value === v} onChange={() => onChange(v)} />
          {label}
        </label>
      ))}
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-2">
      <h3 className="ot-label">{title}</h3>
      {children}
    </section>
  );
}

/** R13-R16: the MBCAP, timeline and sub-trace exports, as jobs with progress, then what they wrote. */
export function ExportDialog({ service, platform, fileId, name, filter, firstNs, onClose }: Props) {
  const [tab, setTab] = useState<Tab>('mbcap');
  const [scope, setScope] = useState<Scope>(Object.keys(filter).length ? 'filter' : 'all');
  const [devices, setDevices] = useState<DeviceStats[]>([]);
  const [device, setDevice] = useState('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [exceptions, setExceptions] = useState(true);
  const [changeOnly, setChangeOnly] = useState(false);
  const [tick, setTick] = useState(0);
  const [format, setFormat] = useState<MBCAPExport['format']>('mbcap');
  const [tlFormat, setTlFormat] = useState<'csv' | 'jsonl'>('csv');
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<MBCAPPreview | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [running, setRunning] = useState<{ ref: JobRef; job: JobState | null } | null>(null);
  const [result, setResult] = useState<ExportResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  const runningRef = useRef(running);
  runningRef.current = running;

  useEffect(() => {
    service.devices(fileId, {}).then(setDevices, () => setDevices([]));
  }, [service, fileId]);

  const selection = (): Filter => {
    let f: Filter = {};
    if (scope === 'filter') f = { ...filter };
    if (scope === 'device') {
      const d = devices.find((x) => `${x.server}/${x.unit}` === device) ?? devices[0];
      if (d) f = { servers: [d.server], units: [d.unit] };
    }
    const fromNs = offset(firstNs, from);
    const toNs = offset(firstNs, to);
    if (fromNs !== undefined) f.from_ns = fromNs;
    if (toNs !== undefined) f.to_ns = toNs;
    return f;
  };
  const mbcapRequest = (): MBCAPExport => ({ filter: selection(), exceptions, change_only: changeOnly, tick_ns: tick, labels, format });
  const request = JSON.stringify(mbcapRequest());

  // The live preview follows every choice (debounced).
  useEffect(() => {
    if (tab !== 'mbcap') return;
    let live = true;
    const timer = setTimeout(() => {
      service.previewMBCAP(fileId, JSON.parse(request) as MBCAPExport).then(
        (p) => {
          if (!live) return;
          setPreview(p);
          setPreviewError(null);
        },
        (err: unknown) => live && setPreviewError(toServiceError(err).message),
      );
    }, 200);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [service, fileId, request, tab]);

  // The running export's progress and end.
  useEffect(
    () =>
      service.subscribe((ev) => {
        const cur = runningRef.current;
        const job = ev.job;
        if (!cur || !job || job.id !== cur.ref.id) return;
        if (job.state === 'running') {
          setRunning({ ...cur, job });
          return;
        }
        setRunning(null);
        if (job.state === 'done') service.exportResult(job.id).then(setResult, (err: unknown) => setError(toServiceError(err).message));
        else if (job.state === 'failed') setError(job.error?.message ?? 'the export failed');
      }),
    [service],
  );

  const start = async () => {
    setError(null);
    const base = baseName(name);
    const [file, kind]: [string, SaveKind] =
      tab === 'mbcap'
        ? [`${base}${{ mbcap: '.mbcap', jsonl: '.mbcap.jsonl', csv: '.mbcap.csv' }[format]}`, format]
        : tab === 'timeline'
          ? [`${base}-timeline.${tlFormat}`, tlFormat]
          : [`${base}-subtrace.pcapng`, 'pcapng'];
    const path = await platform.chooseSave(file, kind);
    if (!path) return;
    try {
      const ref =
        tab === 'mbcap'
          ? await service.exportMBCAP(fileId, mbcapRequest(), path)
          : tab === 'timeline'
            ? await service.exportTimeline(fileId, { filter: selection(), format: tlFormat }, path)
            : await service.exportSubtrace(fileId, { filter: selection() }, path);
      setRunning({ ref, job: null });
    } catch (err) {
      setError(toServiceError(err).message);
    }
  };

  const saveReport = async () => {
    if (!result) return;
    const path = await platform.chooseSave(`${baseName(name)}-report.json`, 'json');
    if (!path) return;
    try {
      await service.saveReport(result.job, path);
      setSaved(path.split(/[\\/]/).pop() ?? path);
    } catch (err) {
      setError(toServiceError(err).message);
    }
  };

  const title = result ? 'Export written' : 'Export';
  if (result) {
    return (
      <Dialog title={title} wide onClose={onClose}>
        <ExportResultView result={result} copy={(t) => void platform.copyText(t)} openDoc={(url) => platform.openExternal(url)} />
        {error && <Alert>{error}</Alert>}
        <div className="mt-4 flex items-center justify-end gap-2">
          {saved && <span className="mr-auto text-[13px] text-muted">Report saved as {saved}</span>}
          {result.report && <Button onClick={() => void saveReport()}>Save report…</Button>}
          <Button variant="primary" onClick={onClose} autoFocus>
            Done
          </Button>
        </div>
      </Dialog>
    );
  }

  const job = running?.job;
  return (
    <Dialog title={title} wide onClose={onClose}>
      <div role="tablist" aria-label="Export kind" className="mt-3 flex gap-1 border-b border-line">
        {(
          [
            ['mbcap', 'MBCAP capture'],
            ['timeline', 'Timeline'],
            ['subtrace', 'Sub-trace (pcapng)'],
          ] as [Tab, string][]
        ).map(([k, label]) => (
          <button
            key={k}
            type="button"
            role="tab"
            aria-selected={tab === k}
            disabled={running !== null}
            onClick={() => setTab(k)}
            className={`-mb-px border-b-2 px-3 py-1.5 text-[13px] ${tab === k ? 'border-accent text-accent-strong' : 'border-transparent text-muted hover:text-accent'}`}
          >
            {label}
          </button>
        ))}
      </div>
      <div className="mt-4 grid min-h-0 flex-1 grid-cols-1 gap-6 overflow-auto md:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
        <div className="flex flex-col gap-4">
          <Section title="What">
            <Radio
              name="Scope"
              value={scope}
              onChange={setScope}
              options={[
                ['all', tab === 'subtrace' ? 'Every connection' : 'Everything analysed'],
                ['filter', 'The current filter'],
                ...(tab === 'subtrace' ? [] : ([['device', 'One device']] as [Scope, string][])),
              ]}
            />
            {scope === 'device' && tab !== 'subtrace' && (
              <select aria-label="Device" className={SELECT} value={device} onChange={(e) => setDevice(e.target.value)}>
                {devices.map((d) => (
                  <option key={`${d.server}/${d.unit}`} value={`${d.server}/${d.unit}`}>
                    {d.server} unit {d.unit}
                  </option>
                ))}
              </select>
            )}
            <div className="flex gap-3">
              <Field label="From (s after the first packet)">
                <TextInput aria-label="From" inputMode="decimal" value={from} onChange={(e) => setFrom(e.target.value)} className="w-32" />
              </Field>
              <Field label="To (s)">
                <TextInput aria-label="To" inputMode="decimal" value={to} onChange={(e) => setTo(e.target.value)} className="w-32" />
              </Field>
            </div>
          </Section>
          {tab === 'mbcap' && (
            <Section title="Capture">
              <Toggle label="Exception responses" checked={exceptions} onChange={setExceptions} />
              <Toggle label="Change-only (drop repeated identical responses)" checked={changeOnly} onChange={setChangeOnly} />
              <div className="flex flex-wrap gap-3">
                <Field label="Tick">
                  <select aria-label="Tick" className={SELECT} value={tick} onChange={(e) => setTick(Number(e.target.value))}>
                    {TICKS.map((t) => (
                      <option key={t.ns} value={t.ns}>
                        {t.label}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label="Encoding">
                  <select aria-label="Encoding" className={SELECT} value={format} onChange={(e) => setFormat(e.target.value as MBCAPExport['format'])}>
                    <option value="mbcap">Binary (.mbcap)</option>
                    <option value="jsonl">JSON Lines (.mbcap.jsonl)</option>
                    <option value="csv">CSV (.mbcap.csv)</option>
                  </select>
                </Field>
              </div>
            </Section>
          )}
          {tab === 'timeline' && (
            <Section title="Format">
              <Radio
                name="Timeline format"
                value={tlFormat}
                onChange={setTlFormat}
                options={[
                  ['csv', 'CSV'],
                  ['jsonl', 'JSON Lines'],
                ]}
              />
              <p className="text-[13px] text-muted">The same rows as mbtracectl timeline writes for this selection, in time order.</p>
            </Section>
          )}
          {tab === 'subtrace' && (
            <p className="flex gap-2 text-[13px] text-muted">
              <InfoIcon size={14} className="mt-0.5 shrink-0" />
              The packets of the selected connections, as captured: writes and vendor functions are included verbatim. A sub-trace selects
              connections (server, client, conn) and a time window only.
            </p>
          )}
        </div>
        {tab === 'mbcap' && (
          <PreviewPanel preview={preview} error={previewError} labels={labels} onLabel={(k, v) => setLabels((l) => ({ ...l, [k]: v }))} />
        )}
      </div>
      {error && <Alert>{error}</Alert>}
      <div className="mt-4 flex items-center justify-end gap-2">
        {running ? (
          <>
            <span className="mr-auto font-mono text-[12px] text-muted" aria-live="polite">
              {job && job.total > 0 ? `${Math.round((100 * job.read) / job.total)}% of the trace read` : 'Starting…'}
            </span>
            <Button onClick={onClose} title="The export goes on; the status bar shows it">
              Continue in the background
            </Button>
            <Button onClick={() => void service.cancelJob(running.ref.id)}>Cancel</Button>
          </>
        ) : (
          <>
            <Button onClick={onClose}>Close</Button>
            <Button
              variant="primary"
              icon={<ExportIcon size={14} />}
              disabled={tab === 'mbcap' && (previewError !== null || preview?.samples === 0)}
              onClick={() => void start()}
            >
              Export…
            </Button>
          </>
        )}
      </div>
    </Dialog>
  );
}

function Alert({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="mt-3 flex items-start gap-2 text-[13px] text-danger">
      <ErrorIcon size={14} className="mt-0.5 shrink-0" /> {children}
    </p>
  );
}

/** The live preview from the index: samples, targets with editable labels, exclusions with reasons. */
function PreviewPanel({ preview, error, labels, onLabel }: { preview: MBCAPPreview | null; error: string | null; labels: Record<string, string>; onLabel: (key: string, label: string) => void }) {
  if (error) return <Alert>{error}</Alert>;
  if (!preview) return <p className="text-muted">Counting…</p>;
  const up = preview.up_to ? 'up to ' : '';
  return (
    <div className="flex flex-col gap-4" aria-label="Preview" role="region">
      <p className="text-[15px] text-fg-strong">
        {up}
        {formatCount(preview.samples)} samples of {formatCount(preview.transactions)} transactions, {preview.targets.length}{' '}
        {preview.targets.length === 1 ? 'target' : 'targets'}
      </p>
      <Section title="Targets">
        {preview.targets.length === 0 && <p className="text-[13px] text-muted">Nothing would be written.</p>}
        {preview.targets.map((t, i) => (
          <div key={t.key} className="flex flex-wrap items-center gap-2 text-[13px]">
            <span className="w-6 font-mono text-muted">{i + 1}</span>
            <TextInput aria-label={`Label of ${t.key}`} value={labels[t.key] ?? t.label} onChange={(e) => onLabel(t.key, e.target.value)} className="w-56" />
            <span className="font-mono text-muted">
              {up}
              {formatCount(t.samples)} samples, {t.clients.length} {t.clients.length === 1 ? 'stream' : 'streams'}
            </span>
          </div>
        ))}
      </Section>
      {preview.excluded.length > 0 && (
        <Section title="Left out">
          <table className="w-full text-[13px]">
            <tbody>
              {preview.excluded.map((x) => (
                <tr key={x.reason} className="align-top">
                  <td className="py-0.5 pr-3 text-right font-mono">{formatCount(x.count)}</td>
                  <td className="py-0.5 pr-3 font-mono whitespace-nowrap">{x.reason}</td>
                  <td className="py-0.5 text-muted">{x.meaning}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Section>
      )}
      {preview.notes.map((n) => (
        <p key={n} className="flex gap-2 text-[12px] text-muted">
          <InfoIcon size={12} className="mt-0.5 shrink-0" /> {n}
        </p>
      ))}
    </div>
  );
}

function Command({ text, copy }: { text: string; copy: (t: string) => void }) {
  return (
    <div className="flex items-start gap-2">
      <code className="min-w-0 flex-1 rounded border border-line bg-bg px-2 py-1 font-mono text-[12px] break-all">{text}</code>
      <button type="button" aria-label={`Copy ${text}`} title="Copy" onClick={() => copy(text)} className="mt-1 text-muted hover:text-accent">
        <CopyIcon size={14} />
      </button>
    </div>
  );
}

/** What an export wrote: the library's report and the replay hand-off for MBCAP (R13, R16). */
export function ExportResultView({ result, copy, openDoc }: { result: ExportResult; copy: (t: string) => void; openDoc: (url: string) => void }) {
  const r = result.report;
  return (
    <div className="mt-3 flex min-h-0 flex-col gap-4 overflow-auto">
      <p className="text-fg-strong">
        <span className="font-mono">{result.file}</span>
        {result.kind === 'timeline' && ` — ${formatCount(result.rows ?? 0)} rows`}
        {result.kind === 'subtrace' &&
          ` — ${formatCount(result.packets ?? 0)} packets of ${result.connections ?? 0} connections, ${formatBytes(result.bytes ?? 0)}`}
      </p>
      {result.kind === 'timeline' && (result.late ?? 0) > 0 && (
        <p className="text-[13px] text-wire">{formatCount(result.late ?? 0)} rows arrived after the reorder window and are out of time order.</p>
      )}
      {r && (
        <>
          <p className="text-[13px]">
            {formatCount(r.samples_written)} samples written of {formatCount(r.transactions_seen)} transactions
            {r.change_only_dropped > 0 && `, ${formatCount(r.change_only_dropped)} unchanged dropped`}; a replay needs about{' '}
            {formatBytes(r.replay.estimated_in_memory_bytes)} of memory.
          </p>
          <Section title="Targets">
            {r.targets.map((t) => (
              <p key={t.id} className="font-mono text-[12px]">
                {t.id}: {t.label} — {formatCount(t.samples)} samples, {t.streams.length} {t.streams.length === 1 ? 'client' : 'clients'}
              </p>
            ))}
          </Section>
          {r.warnings.filter((w) => w.code !== 'format_draft').length > 0 && (
            <Section title="Warnings">
              {r.warnings
                .filter((w) => w.code !== 'format_draft')
                .map((w, i) => (
                  <p key={i} className="flex gap-2 text-[13px]">
                    <WarningIcon size={14} className="mt-0.5 shrink-0 text-wire" />
                    <span>
                      <Chip tone="wire">{w.code}</Chip> {w.target ? `target ${w.target}: ` : ''}
                      {w.text}
                    </span>
                  </p>
                ))}
            </Section>
          )}
          <Section title="Replay it">
            {(result.replay ?? []).map((c) => (
              <Command key={c} text={c} copy={copy} />
            ))}
            <ul className="list-disc pl-5 text-[13px] text-muted">
              {(result.caveats ?? []).map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ul>
            {result.doc && (
              <button type="button" className="self-start text-[13px] text-accent hover:underline" onClick={() => openDoc(result.doc ?? '')}>
                The replay walkthrough (REPLAY-WORKFLOW.md)
              </button>
            )}
          </Section>
        </>
      )}
    </div>
  );
}

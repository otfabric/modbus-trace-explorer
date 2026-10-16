import { useState } from 'react';
import { CopyIcon, ErrorIcon, InfoIcon, WarningIcon } from '../icons';
import type { ADUDetail, Detail, Field } from '../service/types';
import { formatLatency, formatTime, type TimeMode } from '../state/format';

interface Props {
  detail: Detail | null;
  error: string | null;
  loading: boolean;
  timeMode: TimeMode;
  firstNs: number;
  copy: (text: string) => void;
}

const TABS = ['Summary', 'Request', 'Response', 'Raw', 'Packets', 'Notes'] as const;
type Tab = (typeof TABS)[number];

function value(f: Field): string {
  if (f.value !== undefined) {
    return `${f.value} (0x${f.value.toString(16).toUpperCase()})`;
  }
  return f.hex ?? '';
}

function bytesOf(hex: string): string[] {
  const out: string[] = [];
  for (let i = 0; i + 1 < hex.length; i += 2) out.push(hex.slice(i, i + 2).toUpperCase());
  return out;
}

/** The named fields of an ADU; selecting one highlights its bytes in Raw. */
function Fields({ adu, selected, onSelect }: { adu: ADUDetail | undefined; selected: Field | null; onSelect: (f: Field) => void }) {
  if (!adu) return <p className="text-muted">None observed.</p>;
  if (adu.fields.length === 0) return <p className="text-muted">The bytes are not available (the trace was moved or compressed, or IP reassembled them).</p>;
  return (
    <div className="flex flex-col gap-2">
      <p className="text-fg-strong">{adu.name}</p>
      <table className="w-full border-collapse font-mono text-[12px]">
        <thead>
          <tr className="text-left">
            <th className="ot-label py-1 font-normal">Field</th>
            <th className="ot-label py-1 font-normal">Value</th>
            <th className="ot-label py-1 font-normal">Bytes</th>
          </tr>
        </thead>
        <tbody>
          {adu.fields.map((f) => (
            <tr
              key={`${f.name}-${f.offset}`}
              aria-selected={selected === f}
              onClick={() => onSelect(f)}
              className={`cursor-pointer border-t border-line/60 ${selected === f ? 'bg-accent-soft text-accent-strong' : 'hover:bg-bg'}`}
            >
              <td className="py-0.5 pr-2">{f.name.replaceAll('_', ' ')}</td>
              <td className="py-0.5 pr-2 break-all">{value(f)}</td>
              <td className="py-0.5 text-muted">
                {f.offset}–{f.offset + f.length - 1}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {adu.warnings.map((w) => (
        <p key={w} className="flex items-center gap-1 text-wire">
          <WarningIcon size={14} /> {w}
        </p>
      ))}
    </div>
  );
}

/** Bytes (as two-digit hex) in the formats the Raw tab copies (UX.md section 4). */
export function byteString(bytes: string[], format: 'hex' | 'spaced' | 'c' | 'python'): string {
  switch (format) {
    case 'hex':
      return bytes.join('').toLowerCase();
    case 'spaced':
      return bytes.join(' ');
    case 'c':
      return `{ ${bytes.map((b) => `0x${b}`).join(', ')} }`;
    case 'python':
      return `b"${bytes.map((b) => `\\x${b.toLowerCase()}`).join('')}"`;
  }
}

const COPY_FORMATS: [Parameters<typeof byteString>[1], string][] = [
  ['spaced', 'hex'],
  ['hex', 'hex (no spaces)'],
  ['c', 'C'],
  ['python', 'Python'],
];

/**
 * A hex dump with offsets and ASCII; the MBAP header (bytes 0-6) apart from
 * the PDU. Click selects a byte, shift-click extends the selection; the copy
 * buttons copy the selection, or the whole ADU without one.
 */
function HexDump({ label, adu, highlight, copy }: { label: string; adu: ADUDetail | undefined; highlight: Field | null; copy: (t: string) => void }) {
  const [sel, setSel] = useState<{ anchor: number; from: number; to: number } | null>(null);
  if (!adu || !adu.hex) return null;
  const bytes = bytesOf(adu.hex);
  const rows: string[][] = [];
  for (let i = 0; i < bytes.length; i += 16) rows.push(bytes.slice(i, i + 16));
  const lit = (i: number) => highlight !== null && i >= highlight.offset && i < highlight.offset + highlight.length;
  const picked = (i: number) => sel !== null && i >= sel.from && i <= sel.to;
  const pick = (i: number, extend: boolean) => {
    if (extend && sel) setSel({ anchor: sel.anchor, from: Math.min(sel.anchor, i), to: Math.max(sel.anchor, i) });
    else if (sel && sel.from === i && sel.to === i) setSel(null);
    else setSel({ anchor: i, from: i, to: i });
  };
  const chosen = sel ? bytes.slice(sel.from, sel.to + 1) : bytes;
  const what = sel ? `bytes ${sel.from}–${sel.to}` : `the ${label.toLowerCase()}`;
  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className="ot-label">{label}</span>
        {sel && <span className="font-mono text-[11px] text-muted">{sel.to - sel.from + 1} selected</span>}
        <span className="ml-auto flex items-center gap-1 text-[11px]">
          <CopyIcon size={12} className="text-muted" />
          {COPY_FORMATS.map(([f, name]) => (
            <button key={f} type="button" aria-label={`Copy ${what} as ${name}`} className="rounded border border-line px-1 text-muted hover:text-accent" onClick={() => copy(byteString(chosen, f))}>
              {name}
            </button>
          ))}
        </span>
      </div>
      <pre className="font-mono text-[12px] leading-5 select-none">
        {rows.map((row, r) => (
          <div key={r}>
            <span className="text-muted">{(r * 16).toString(16).padStart(4, '0')} </span>
            {row.map((b, k) => {
              const i = r * 16 + k;
              return (
                <span key={k}>
                  <span
                    role="button"
                    tabIndex={-1}
                    aria-label={`${label} byte ${i}`}
                    aria-pressed={picked(i)}
                    onClick={(e) => pick(i, e.shiftKey)}
                    className={`cursor-pointer ${picked(i) ? 'bg-accent text-bg' : lit(i) ? 'bg-accent-soft text-accent-strong' : i < 7 ? 'text-muted' : ''}`}
                  >
                    {b}
                  </span>
                  {i === 6 ? ' | ' : ' '}
                </span>
              );
            })}
            <span className="text-muted">
              {' '}
              {row.map((b) => {
                const c = parseInt(b, 16);
                return c >= 32 && c < 127 ? String.fromCharCode(c) : '.';
              })}
            </span>
          </div>
        ))}
      </pre>
    </div>
  );
}

function Packets({ label, adu, timeMode, firstNs, copy }: { label: string; adu: ADUDetail | undefined; timeMode: TimeMode; firstNs: number; copy: (t: string) => void }) {
  if (!adu) return null;
  return (
    <div className="flex flex-col gap-1">
      <span className="ot-label">{label}</span>
      {adu.packets.map((p) => (
        <div key={`${p.part}-${p.frame}`} className="flex flex-wrap items-center gap-x-3 font-mono text-[12px]">
          <span className="text-fg-strong">frame {p.frame}</span>
          {p.part > 0 && <span>file {p.part + 1}</span>}
          {p.t_ns !== undefined && <span>{formatTime(p.t_ns, timeMode, firstNs)}</span>}
          <span className="text-muted">if {p.interface}</span>
          <span className="text-muted">
            bytes {p.payload_offset < 0 ? 'reassembled' : `${p.payload_offset}+${p.payload_length}`}
          </span>
          <button type="button" className="font-sans text-muted hover:text-accent" onClick={() => copy(String(p.frame))}>
            Copy frame number
          </button>
          <button type="button" className="font-sans text-muted hover:text-accent" onClick={() => copy(p.display_filter)}>
            Copy Wireshark filter
          </button>
        </div>
      ))}
    </div>
  );
}

/** R6: the selected row in words, fields, bytes, packets and notes. */
export function DetailPane({ detail, error, loading, timeMode, firstNs, copy }: Props) {
  const [tab, setTab] = useState<Tab>('Summary');
  const [field, setField] = useState<Field | null>(null);
  if (error) {
    return (
      <p role="alert" className="flex items-center gap-1 p-4 text-danger">
        <ErrorIcon size={14} /> {error}
      </p>
    );
  }
  if (!detail) {
    return <p className="p-4 text-muted">{loading ? 'Loading…' : 'Select a row.'}</p>;
  }
  const r = detail.row;
  const isTxn = r.kind === 'txn';
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div role="tablist" aria-label="Detail" className="flex flex-wrap gap-1 border-b border-line px-2 pt-2">
        {TABS.filter((t) => isTxn || t === 'Summary' || t === 'Notes').map((t) => (
          <button
            key={t}
            role="tab"
            type="button"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={`rounded-t px-2 py-1 text-[12px] ${tab === t ? 'bg-accent-soft text-accent-strong' : 'text-muted hover:text-accent'}`}
          >
            {t}
          </button>
        ))}
      </div>
      <div role="tabpanel" aria-label={tab} className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4 text-[13px]">
        {tab === 'Summary' && (
          <>
            <p className="text-fg-strong">{detail.text}</p>
            <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 font-mono text-[12px]">
              <dt className="text-muted">time</dt>
              <dd title={`${r.t_ns} ns`}>{formatTime(r.t_ns, timeMode, firstNs)}</dd>
              {r.latency_ns !== undefined && (
                <>
                  <dt className="text-muted">latency</dt>
                  <dd>{formatLatency(r.latency_ns)}</dd>
                </>
              )}
              <dt className="text-muted">connection</dt>
              <dd>{r.conn}</dd>
              {r.tid !== undefined && (
                <>
                  <dt className="text-muted">transaction ID</dt>
                  <dd>{r.tid}</dd>
                </>
              )}
            </dl>
            {detail.mbcap && (
              <p className="flex items-center gap-1 text-muted">
                <InfoIcon size={14} /> {detail.mbcap}
              </p>
            )}
            <button type="button" className="self-start text-muted hover:text-accent" onClick={() => copy(JSON.stringify(detail, null, 2))}>
              Copy as JSON
            </button>
          </>
        )}
        {tab === 'Request' && <Fields adu={detail.request} selected={field} onSelect={(f) => { setField(f); setTab('Raw'); }} />}
        {tab === 'Response' && <Fields adu={detail.response} selected={field} onSelect={(f) => { setField(f); setTab('Raw'); }} />}
        {tab === 'Raw' && (
          <>
            <HexDump label="Request" adu={detail.request} highlight={detail.request?.fields.includes(field as Field) ? field : null} copy={copy} />
            <HexDump label="Response" adu={detail.response} highlight={detail.response?.fields.includes(field as Field) ? field : null} copy={copy} />
            {!detail.request?.hex && !detail.response?.hex && <p className="text-muted">The bytes are not available.</p>}
          </>
        )}
        {tab === 'Packets' && (
          <>
            <Packets label="Request" adu={detail.request} timeMode={timeMode} firstNs={firstNs} copy={copy} />
            <Packets label="Response" adu={detail.response} timeMode={timeMode} firstNs={firstNs} copy={copy} />
          </>
        )}
        {tab === 'Notes' && (
          <ul className="flex flex-col gap-2">
            {detail.notes.length === 0 && <li className="text-muted">Nothing to note.</li>}
            {detail.notes.map((n) => (
              <li key={`${n.kind}-${n.code}`} className="flex gap-2">
                {n.kind === 'fact' ? <InfoIcon size={14} className="mt-0.5 shrink-0 text-blue" /> : <WarningIcon size={14} className="mt-0.5 shrink-0 text-wire" />}
                <span>
                  <b className="font-mono text-[12px]">{n.code}</b> {n.meaning}
                </span>
              </li>
            ))}
            {detail.mbcap && <li className="text-muted">{detail.mbcap}</li>}
          </ul>
        )}
      </div>
    </div>
  );
}

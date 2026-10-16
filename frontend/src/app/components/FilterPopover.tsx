import { useState } from 'react';
import type { Filter, FilterParse } from '../service/types';
import { Popover } from './Popover';
import { Button, Field, TextInput } from './ui';

const STATUSES = ['ok', 'exception', 'unanswered', 'orphan', 'ambiguous', 'malformed', 'uncertain'];
const CLASSES = ['read', 'write', 'other'];
const FUNCTIONS: [number, string][] = [
  [1, '01 Read Coils'],
  [2, '02 Read Discrete'],
  [3, '03 Read Holding'],
  [4, '04 Read Input'],
  [5, '05 Write Coil'],
  [6, '06 Write Register'],
  [15, '15 Write Coils'],
  [16, '16 Write Registers'],
  [23, '23 Read/Write'],
  [43, '43 Device ID'],
];
export const FLAGS = [
  'retransmitted', 'out_of_order', 'gap_adjacent', 'resynced', 'snaplen_truncated', 'midstream', 'role_guessed', 'role_unknown',
  'retried', 'clock_order', 'slow', 'invalid_pdu', 'serial_function_over_tcp', 'unknown_function', 'broadcast', 'foreign_protocol',
];
const SPACES = ['coil', 'discrete', 'holding', 'input'];
const SELECT = 'h-8 rounded border border-control bg-bg px-2 font-mono text-[13px]';

interface Controls {
  servers: string;
  units: string;
  clients: string;
  conns: string;
  functions: number[];
  statuses: string[];
  classes: string[];
  flag: string;
  notFlag: string;
  from: string;
  to: string;
  space: string;
  range: string;
}

const list = (s: string) =>
  s
    .split(/[\s,]+/)
    .filter(Boolean)
    .join(',');

/** The controls as the filter grammar writes them (UX.md section 6): the engine parses and checks the result. */
export function controlsText(c: Controls): string {
  const t: string[] = [];
  const add = (name: string, v: string) => v && t.push(`${name}:${v}`);
  add('server', list(c.servers));
  add('unit', list(c.units));
  add('client', list(c.clients));
  add('conn', list(c.conns));
  add('fc', c.functions.join(','));
  add('status', c.statuses.join(','));
  add('class', c.classes.join(','));
  if (c.flag) t.push(`flag:${c.flag}`);
  if (c.notFlag) t.push(`-flag:${c.notFlag}`);
  add('from', c.from.trim());
  add('to', c.to.trim());
  if (c.space || c.range.trim()) t.push(`addr:${[c.space, c.range.trim()].filter(Boolean).join('/')}`);
  return t.join(' ');
}

function initial(f: Filter): Controls {
  return {
    servers: (f.servers ?? []).join(', '),
    units: (f.units ?? []).join(', '),
    clients: (f.clients ?? []).join(', '),
    conns: (f.conns ?? []).join(', '),
    functions: f.functions ?? [],
    statuses: (f.statuses ?? []).map((s) => (s === 'orphan_response' ? 'orphan' : s)),
    classes: f.classes ?? [],
    flag: f.flags?.[0] ?? '',
    notFlag: f.not_flags?.[0] ?? '',
    from: '',
    to: '',
    space: f.space ?? '',
    range: f.start !== undefined ? (f.start === f.end ? `${f.start}` : `${f.start}-${f.end ?? 65535}`) : '',
  };
}

function Checks<T extends string | number>({ label, options, value, onChange }: { label: string; options: [T, string][]; value: T[]; onChange: (v: T[]) => void }) {
  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="ot-label mb-1">{label}</legend>
      <div className="flex flex-wrap gap-x-3 gap-y-1">
        {options.map(([v, name]) => (
          <label key={String(v)} className="flex items-center gap-1 whitespace-nowrap">
            <input type="checkbox" checked={value.includes(v)} onChange={(e) => onChange(e.target.checked ? [...value, v] : value.filter((x) => x !== v))} />
            {name}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/** R7: the filter controls. Apply replaces the terms they show; the time range keeps what the chips have unless set here. */
export function FilterPopover({ filter, parse, onApply, onClose }: { filter: Filter; parse: (text: string) => Promise<FilterParse>; onApply: (f: Filter) => void; onClose: () => void }) {
  const [c, setC] = useState<Controls>(() => initial(filter));
  const [errors, setErrors] = useState<FilterParse['errors']>([]);
  const set = (patch: Partial<Controls>) => setC((x) => ({ ...x, ...patch }));
  const apply = async () => {
    const res = await parse(controlsText(c));
    setErrors(res.errors);
    if (res.errors.length > 0) return;
    // The controls own every term but the time range, which stays as it was when left empty here.
    const kept: Filter = { from_ns: filter.from_ns, to_ns: filter.to_ns, summary: filter.summary, exceptions: filter.exceptions };
    onApply({ ...kept, ...res.filter });
    onClose();
  };
  return (
    <Popover label="Filter controls" onClose={onClose}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void apply();
        }}
      >
        <div className="grid grid-cols-2 gap-3">
          <Field label="Servers">
            <TextInput aria-label="Servers" value={c.servers} onChange={(e) => set({ servers: e.target.value })} placeholder="10.0.3.21:502" />
          </Field>
          <Field label="Units">
            <TextInput aria-label="Units" value={c.units} onChange={(e) => set({ units: e.target.value })} placeholder="1, 2" />
          </Field>
          <Field label="Clients">
            <TextInput aria-label="Clients" value={c.clients} onChange={(e) => set({ clients: e.target.value })} />
          </Field>
          <Field label="Connections">
            <TextInput aria-label="Connections" value={c.conns} onChange={(e) => set({ conns: e.target.value })} />
          </Field>
        </div>
        <Checks label="Functions" options={FUNCTIONS} value={c.functions} onChange={(functions) => set({ functions })} />
        <Checks label="Status" options={STATUSES.map((s) => [s, s] as [string, string])} value={c.statuses} onChange={(statuses) => set({ statuses })} />
        <Checks label="Class" options={CLASSES.map((s) => [s, s] as [string, string])} value={c.classes} onChange={(classes) => set({ classes })} />
        <div className="grid grid-cols-2 gap-3">
          <Field label="With flag">
            <select aria-label="With flag" className={SELECT} value={c.flag} onChange={(e) => set({ flag: e.target.value })}>
              <option value="">any</option>
              {FLAGS.map((f) => (
                <option key={f}>{f}</option>
              ))}
            </select>
          </Field>
          <Field label="Without flag">
            <select aria-label="Without flag" className={SELECT} value={c.notFlag} onChange={(e) => set({ notFlag: e.target.value })}>
              <option value="">none</option>
              {FLAGS.map((f) => (
                <option key={f}>{f}</option>
              ))}
            </select>
          </Field>
          <Field label="From (14:30, +15m)">
            <TextInput aria-label="From" value={c.from} onChange={(e) => set({ from: e.target.value })} />
          </Field>
          <Field label="To">
            <TextInput aria-label="To" value={c.to} onChange={(e) => set({ to: e.target.value })} />
          </Field>
          <Field label="Data space">
            <select aria-label="Data space" className={SELECT} value={c.space} onChange={(e) => set({ space: e.target.value })}>
              <option value="">any</option>
              {SPACES.map((s) => (
                <option key={s}>{s}</option>
              ))}
            </select>
          </Field>
          <Field label="Addresses (4096-4160)">
            <TextInput aria-label="Addresses" value={c.range} onChange={(e) => set({ range: e.target.value })} />
          </Field>
        </div>
        {errors.map((e) => (
          <p key={e.term} role="alert" className="text-danger">
            {e.term}: {e.message}
          </p>
        ))}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary">
            Apply
          </Button>
        </div>
      </form>
    </Popover>
  );
}

import { forwardRef, useState, type KeyboardEvent } from 'react';
import { CloseIcon, ErrorIcon, FilterIcon } from '../icons';
import type { Filter, FilterParse } from '../service/types';
import { formatCount } from '../state/format';
import { FilterPopover } from './FilterPopover';
import { TextInput } from './ui';

interface Props {
  filter: Filter;
  total: number;
  onChange: (f: Filter) => void;
  /** Parses the text box with the engine's grammar (UX.md section 6). */
  parse: (text: string) => Promise<FilterParse>;
}

type Key = keyof Filter;

/** The chips of a filter: one per field, in a fixed order, as the text grammar would write it. */
export function chips(f: Filter): { key: Key; label: string }[] {
  const out: { key: Key; label: string }[] = [];
  const list = (key: Key, name: string, v?: (string | number)[]) => {
    if (v && v.length > 0) out.push({ key, label: `${name}:${v.join(',')}` });
  };
  list('servers', 'server', f.servers);
  list('units', 'unit', f.units);
  list('clients', 'client', f.clients);
  list('conns', 'conn', f.conns);
  list('functions', 'fc', f.functions);
  list('classes', 'class', f.classes);
  list('statuses', 'status', f.statuses);
  list('exceptions', 'exc', f.exceptions);
  list('flags', 'flag', f.flags);
  if (f.summary !== undefined) out.push({ key: 'summary', label: `request:${f.summary}` });
  if (f.not_flags?.length) out.push({ key: 'not_flags', label: `-flag:${f.not_flags.join(',')}` });
  if (f.space || f.start !== undefined) {
    const range = f.start !== undefined ? (f.start === f.end ? `${f.start}` : `${f.start}-${f.end ?? 65535}`) : '';
    out.push({ key: 'space', label: `addr:${f.space ?? ''}${f.space && range ? '/' : ''}${range}` });
  }
  if (f.from_ns !== undefined) out.push({ key: 'from_ns', label: `from:${new Date(f.from_ns / 1e6).toISOString().slice(11, 19)}` });
  if (f.to_ns !== undefined) out.push({ key: 'to_ns', label: `to:${new Date(f.to_ns / 1e6).toISOString().slice(11, 19)}` });
  return out;
}

function without(f: Filter, key: Key): Filter {
  const next = { ...f };
  delete next[key];
  if (key === 'space') {
    delete next.start;
    delete next.end;
  }
  return next;
}

/** R7: the active filter as removable chips, a text box, the resulting count, and Reset. */
export const FilterBar = forwardRef<HTMLInputElement, Props>(function FilterBar({ filter, total, onChange, parse }, ref) {
  const [text, setText] = useState('');
  const [errors, setErrors] = useState<FilterParse['errors']>([]);
  const [controls, setControls] = useState(false);

  const apply = async () => {
    const res = await parse(text);
    setErrors(res.errors);
    if (res.errors.length === 0) {
      onChange({ ...filter, ...res.filter });
      setText('');
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      void apply();
    } else if (e.key === 'Escape') {
      setText('');
      setErrors([]);
    }
  };

  const active = chips(filter);
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-line px-4 py-2 text-[13px]">
      <span className="relative">
        <button
          type="button"
          aria-label="Filter controls"
          aria-expanded={controls}
          title="Filter controls"
          className="inline-flex items-center gap-1 rounded border border-line px-1.5 py-0.5 text-muted hover:border-accent hover:text-accent"
          onClick={() => setControls((v) => !v)}
        >
          <FilterIcon size={16} />
          Filters
        </button>
        {controls && <FilterPopover filter={filter} parse={parse} onApply={onChange} onClose={() => setControls(false)} />}
      </span>
      {active.map((c) => (
        <span key={c.key} className="inline-flex h-6 items-center gap-1 rounded border border-accent bg-accent-soft px-1.5 font-mono text-[12px] text-accent-strong">
          {c.label}
          <button type="button" aria-label={`Remove ${c.label}`} className="hover:text-fg-strong" onClick={() => onChange(without(filter, c.key))}>
            <CloseIcon size={12} />
          </button>
        </span>
      ))}
      {errors.map((e) => (
        <span key={e.term} role="alert" title={e.message} className="inline-flex h-6 items-center gap-1 rounded border border-danger px-1.5 font-mono text-[12px] text-danger">
          <ErrorIcon size={12} />
          {e.term}: {e.message}
        </span>
      ))}
      <TextInput
        ref={ref}
        aria-label="Filter"
        placeholder="unit:1 fc:3 status:exception -flag:retransmitted addr:holding/4096-4160 from:+15m"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKeyDown}
        className="min-w-[18rem] flex-1"
      />
      <span className="font-mono text-muted" aria-live="polite">
        {formatCount(total)} rows
      </span>
      <button
        type="button"
        className="text-muted hover:text-accent disabled:opacity-50"
        disabled={active.length === 0 && text === '' && errors.length === 0}
        onClick={() => {
          onChange({});
          setText('');
          setErrors([]);
        }}
      >
        Reset
      </button>
    </div>
  );
});

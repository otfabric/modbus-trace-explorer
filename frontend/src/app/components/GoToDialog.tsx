import { useState } from 'react';
import { toServiceError } from '../service/service';
import type { FilterParse } from '../service/types';
import { Dialog } from './Dialog';
import { Button, Field, TextInput } from './ui';

export type GoTo = { t_ns: number } | { frame: number; part: number };

interface Props {
  mode: 'time' | 'frame';
  /** The trace's files: a frame number counts within one of them. */
  files: string[];
  /** Parses with the engine's filter grammar ("from:" times). */
  parse: (text: string) => Promise<FilterParse>;
  onGo: (target: GoTo) => void;
  onClose: () => void;
}

/** Cmd/Ctrl+G: go to a time; Cmd/Ctrl+Shift+G: go to a frame (R5). */
export function GoToDialog({ mode, files, parse, onGo, onClose }: Props) {
  const [text, setText] = useState('');
  const [part, setPart] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const go = async () => {
    setError(null);
    if (mode === 'frame') {
      const n = Number(text.trim());
      if (!Number.isInteger(n) || n < 1) {
        setError('A frame number as Wireshark shows it: 1 or more.');
        return;
      }
      onGo({ frame: n, part });
      return;
    }
    try {
      const res = await parse(`from:${text.trim()}`);
      if (res.errors.length > 0 || res.filter.from_ns === undefined) {
        setError(res.errors[0]?.message ?? 'Not a time.');
        return;
      }
      onGo({ t_ns: res.filter.from_ns });
    } catch (err) {
      setError(toServiceError(err).message);
    }
  };
  return (
    <Dialog title={mode === 'time' ? 'Go to time' : 'Go to frame'} onClose={onClose}>
      <form
        className="mt-3 flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void go();
        }}
      >
        <Field label={mode === 'time' ? 'Time: 14:30:05, 14:30:05.250, an ISO time, or +15m from the start' : 'Frame number'}>
          <TextInput aria-label={mode === 'time' ? 'Time' : 'Frame'} value={text} onChange={(e) => setText(e.target.value)} autoFocus />
        </Field>
        {mode === 'frame' && files.length > 1 && (
          <Field label="In file">
            <select aria-label="File" className="h-8 rounded border border-control bg-bg px-2 font-mono text-[13px]" value={part} onChange={(e) => setPart(Number(e.target.value))}>
              {files.map((f, k) => (
                <option key={f} value={k}>
                  {f.split(/[\\/]/).pop()}
                </option>
              ))}
            </select>
          </Field>
        )}
        {error && (
          <p role="alert" className="text-[13px] text-danger">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary">
            Go
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

import { useEffect, useState } from 'react';
import { ErrorIcon } from '../icons';
import type { Theme } from '../platform/platform';
import { toServiceError, type Service } from '../service/service';
import type { CacheInfo, Settings } from '../service/types';
import { formatBytes } from '../state/format';
import { ConfirmDialog } from './ConfirmDialog';
import { Dialog } from './Dialog';
import { Button, Field, TextInput, Toggle } from './ui';

interface Props {
  service: Service;
  theme: Theme;
  onToggleTheme: () => void;
  /** Called with the settings as saved. */
  onSaved: (s: Settings) => void;
  onRecentCleared: () => void;
  onClose: () => void;
}

const GIB = 1024 ** 3;
const CAPS = [0.5, 1, 2, 5, 10, 20, 50].map((g) => g * GIB);
const SELECT = 'h-8 rounded border border-control bg-bg px-2 font-mono text-[13px]';

/** "502, 1502 503" -> [502, 1502, 503]; null when a term is not a port. */
export function parsePorts(text: string): number[] | null {
  const terms = text.split(/[\s,]+/).filter(Boolean);
  const ports = terms.map(Number);
  return ports.every((p) => Number.isInteger(p) && p >= 1 && p <= 65535) ? ports : null;
}

/** R20: theme, time display, default server ports, address display, the cache and the recent files. */
export function SettingsDialog({ service, theme, onToggleTheme, onSaved, onRecentCleared, onClose }: Props) {
  const [s, setS] = useState<Settings | null>(null);
  const [ports, setPorts] = useState('');
  const [cache, setCache] = useState<CacheInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [note, setNote] = useState<string | null>(null);

  useEffect(() => {
    service.settings().then((v) => {
      setS(v);
      setPorts(v.server_ports.join(', '));
    }, (err: unknown) => setError(toServiceError(err).message));
    service.cacheInfo().then(setCache, () => setCache(null));
  }, [service]);

  if (!s) {
    return (
      <Dialog title="Settings" onClose={onClose}>
        <p className="mt-3 text-muted">{error ?? 'Loading…'}</p>
      </Dialog>
    );
  }

  const save = async () => {
    const p = parsePorts(ports);
    if (!p) {
      setError('Server ports are numbers from 1 to 65535, separated by commas.');
      return;
    }
    try {
      const kept = await service.saveSettings({ ...s, server_ports: p });
      onSaved(kept);
      onClose();
    } catch (err) {
      setError(toServiceError(err).message);
    }
  };

  const caps = CAPS.includes(s.cache_cap) ? CAPS : [...CAPS, s.cache_cap].sort((a, b) => a - b);
  return (
    <Dialog title="Settings" onClose={onClose}>
      <div className="mt-4 flex flex-col gap-4 overflow-auto">
        <Toggle label="Light theme" checked={theme === 'light'} onChange={onToggleTheme} />
        <Field label="Time display">
          <select aria-label="Time display" className={SELECT} value={s.time_mode} onChange={(e) => setS({ ...s, time_mode: e.target.value as Settings['time_mode'] })}>
            <option value="local">Local time</option>
            <option value="utc">UTC</option>
            <option value="relative">Relative to the first packet</option>
          </select>
        </Field>
        <Field label="Server ports to suggest (besides 502)">
          <TextInput aria-label="Server ports" value={ports} onChange={(e) => setPorts(e.target.value)} placeholder="1502, 5020" />
        </Field>
        <Toggle label="1-based address labels (addresses stay as sent)" checked={s.one_based} onChange={(v) => setS({ ...s, one_based: v })} />
        <section aria-label="Analysis cache" className="flex flex-col gap-2">
          <span className="ot-label">Analysis cache</span>
          {cache && (
            <p className="font-mono text-[12px] break-all text-muted">
              {cache.dir}
              <br />
              {formatBytes(cache.bytes)} in {cache.entries} {cache.entries === 1 ? 'entry' : 'entries'}
            </p>
          )}
          <div className="flex items-end gap-2">
            <Field label="Size limit">
              <select aria-label="Cache size limit" className={SELECT} value={s.cache_cap} onChange={(e) => setS({ ...s, cache_cap: Number(e.target.value) })}>
                {caps.map((c) => (
                  <option key={c} value={c}>
                    {formatBytes(c)}
                  </option>
                ))}
              </select>
            </Field>
            <Button onClick={() => setConfirm(true)}>Clear cache…</Button>
          </div>
        </section>
        <div className="flex items-center gap-2">
          <Button
            onClick={() =>
              void service.clearRecent().then(() => {
                setNote('Recent files cleared.');
                onRecentCleared();
              })
            }
          >
            Clear recent files
          </Button>
          {note && (
            <span role="status" className="text-[13px] text-muted">
              {note}
            </span>
          )}
        </div>
        {error && (
          <p role="alert" className="flex items-center gap-2 text-[13px] text-danger">
            <ErrorIcon size={14} /> {error}
          </p>
        )}
      </div>
      <div className="mt-5 flex justify-end gap-2">
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="primary" onClick={() => void save()}>
          Save
        </Button>
      </div>
      {confirm && (
        <ConfirmDialog
          title="Clear the analysis cache?"
          confirmLabel="Clear cache"
          onCancel={() => setConfirm(false)}
          onConfirm={() => {
            setConfirm(false);
            service.clearCache().then(setCache, (err: unknown) => setError(toServiceError(err).message));
          }}
        >
          <p className="text-[13px]">Traces not open now are analysed again the next time they are opened. The open trace keeps its analysis.</p>
        </ConfirmDialog>
      )}
    </Dialog>
  );
}

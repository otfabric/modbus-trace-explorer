import { useMemo, useState } from 'react';
import { WarningIcon } from '../icons';
import type { AnalyzeConfig, Flow, ScanResult } from '../service/types';
import { formatBytes, formatCount, formatDuration } from '../state/format';
import { Button, TextInput } from './ui';

interface Props {
  name: string;
  result: ScanResult;
  warning?: string;
  onAnalyze: (cfg: AnalyzeConfig) => void;
  onCancel: () => void;
}

const CLASS_LABEL: Record<Flow['class'], string> = { modbus_tcp: 'Modbus/TCP', tls: 'TLS', other_tcp: 'Other TCP' };

/** The first and last time of a flow, as a duration. */
function duration(f: Flow): string {
  if (!f.first || !f.last) return '';
  return formatDuration((Date.parse(f.last) - Date.parse(f.first)) * 1e6);
}

/**
 * The Flows step (R2, UX.md section 2): the scan's flows with the
 * Modbus-like ones preselected, the server ports, and a per-flow "this
 * endpoint is the server" override.
 */
export function FlowsStep({ name, result, warning, onAnalyze, onCancel }: Props) {
  const [selected, setSelected] = useState<Set<number>>(() => new Set(result.selected));
  const [ports, setPorts] = useState(result.server_ports.join(', '));
  const [servers, setServers] = useState<Map<number, string>>(() => new Map());

  const parsedPorts = useMemo(() => {
    const out: number[] = [];
    for (const p of ports.split(/[\s,]+/).filter(Boolean)) {
      const n = Number(p);
      if (!Number.isInteger(n) || n < 1 || n > 65535) return null;
      out.push(n);
    }
    return out;
  }, [ports]);

  const modbus = result.flows.filter((f) => f.class === 'modbus_tcp');
  const tls = result.flows.filter((f) => f.class === 'tls');
  const otherPorts = [...new Set(result.flows.filter((f) => f.class === 'other_tcp').map((f) => Number(f.b.split(':').pop())))].slice(0, 6);
  const s = result.summary;

  const toggle = (id: number) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  };

  const swap = (f: Flow) => {
    const next = new Map(servers);
    if (next.has(f.id)) next.delete(f.id);
    else next.set(f.id, f.a);
    setServers(next);
    setSelected(new Set(selected).add(f.id));
  };

  const analyze = () =>
    onAnalyze({ flows: [...selected].sort((a, b) => a - b), server_ports: parsedPorts ?? [], servers: [...servers.values()] });

  return (
    <main className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-6">
      <header className="flex flex-wrap items-baseline gap-x-6 gap-y-1 font-mono text-[13px]">
        <h1 className="font-display text-xl font-semibold text-fg-strong">{name}</h1>
        <span>{formatBytes(s.input_bytes)}</span>
        <span>{s.format}</span>
        <span>{[...new Set(s.interfaces.map((i) => i.link))].join(', ')}</span>
        <span>{formatCount(s.packets)} packets</span>
        <span>{s.capture_drops ? `${formatCount(s.capture_drops)} dropped by the capture host` : '0 dropped by the capture host'}</span>
      </header>
      {warning && (
        <p role="alert" className="flex items-center gap-2 text-wire">
          <WarningIcon size={16} /> {warning}
        </p>
      )}
      {modbus.length === 0 && (
        <p role="alert" className="flex items-center gap-2 text-wire">
          <WarningIcon size={16} />
          No Modbus/TCP found. The trace has {formatCount(result.flows.length)} TCP flows
          {otherPorts.length > 0 && ` on ports ${otherPorts.join(', ')}`}. If your devices use another port, enter it below, or mark a flow's
          server.
        </p>
      )}
      {tls.length > 0 && (
        <p className="flex items-center gap-2 text-muted">
          This trace contains TLS on port {[...new Set(tls.map((f) => f.b.split(':').pop()))].join(', ')}; Modbus/TCP Security is encrypted and
          cannot be decoded.
        </p>
      )}
      <label className="flex flex-wrap items-center gap-2 text-[13px]">
        <span>Server ports to analyze</span>
        <TextInput aria-label="Server ports" value={ports} onChange={(e) => setPorts(e.target.value)} className="w-56" />
        <span className="text-muted">(found: {result.server_ports.join(', ')})</span>
        {parsedPorts === null && <span className="text-danger">ports are numbers from 1 to 65535</span>}
      </label>
      <table className="w-full border-collapse font-mono text-[12.5px]">
        <thead>
          <tr className="border-b border-line text-left">
            {['', 'Class', 'Client', 'Server', 'Packets', 'Bytes', 'Duration', 'Modbus score', ''].map((h, i) => (
              <th key={i} className="ot-label px-2 py-1.5 font-normal">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {result.flows.map((f) => {
            const swapped = servers.has(f.id);
            return (
              <tr key={f.id} className="border-b border-line/60">
                <td className="px-2 py-1">
                  <input type="checkbox" aria-label={`Analyze flow ${f.id}`} checked={selected.has(f.id)} onChange={() => toggle(f.id)} />
                </td>
                <td className="px-2 py-1">{CLASS_LABEL[f.class]}</td>
                <td className="px-2 py-1">{swapped ? f.b : f.a}</td>
                <td className="px-2 py-1">
                  {swapped ? f.a : f.b}
                  {f.role_guessed && <span className="text-wire"> (guessed)</span>}
                </td>
                <td className="px-2 py-1">{formatCount(f.packets[0] + f.packets[1])}</td>
                <td className="px-2 py-1">{formatBytes(f.bytes[0] + f.bytes[1])}</td>
                <td className="px-2 py-1">{duration(f)}</td>
                <td className="px-2 py-1">
                  {f.class === 'tls' ? 'not decodable' : f.class === 'modbus_tcp' || f.score > 0 ? f.score.toFixed(2) : '-'}
                </td>
                <td className="px-2 py-1 text-right">
                  {f.class !== 'tls' && (
                    <button
                      type="button"
                      aria-label={swapped ? `Make ${f.b} the server again` : `Make ${f.a} the server`}
                      title={swapped ? `Make ${f.b} the server again` : `This endpoint (${f.a}) is the server`}
                      className="font-sans text-[12px] text-muted hover:text-accent"
                      onClick={() => swap(f)}
                    >
                      {swapped ? 'Undo swap' : 'Swap roles'}
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <div className="flex justify-end gap-2">
        <Button onClick={onCancel}>Cancel</Button>
        <Button variant="primary" disabled={selected.size === 0 || parsedPorts === null} onClick={analyze} autoFocus>
          Analyze {selected.size} {selected.size === 1 ? 'flow' : 'flows'}
        </Button>
      </div>
    </main>
  );
}

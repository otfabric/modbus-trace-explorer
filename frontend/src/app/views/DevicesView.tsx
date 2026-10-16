import { useState } from 'react';
import { TimelineIcon } from '../icons';
import type { Service } from '../service/service';
import type { Filter } from '../service/types';
import { formatBytes, formatCount, formatFunction, formatLatency, formatTime, type TimeMode } from '../state/format';
import { Button } from '../components/ui';
import { DistributionView, Facts, Heading, SelectRow, Status, TABLE, Th, useLoad } from './common';

interface Props {
  service: Service;
  fileId: string;
  filter: Filter;
  timeMode: TimeMode;
  firstNs: number;
  onShow: (f: Filter) => void;
}

/** R8: every server and unit seen, and the detail of one. */
export function DevicesView({ service, fileId, filter, timeMode, firstNs, onShow }: Props) {
  const { data, error, loading } = useLoad(() => service.devices(fileId, filter), [service, fileId, JSON.stringify(filter)]);
  const [sel, setSel] = useState<string | null>(null);
  if (!data) return <Status loading={loading} error={error} />;
  if (data.length === 0) return <Status loading={false} error={null} empty="No devices match the filter." />;
  const d = data.find((x) => `${x.server}/${x.unit}` === sel) ?? data[0];
  return (
    <div className="flex min-h-0 flex-1">
      <div className="min-w-0 flex-1 overflow-auto">
        <table className={TABLE} aria-label="Devices">
          <thead>
            <tr>
              <Th>server</Th>
              <Th right>unit</Th>
              <Th right>transactions</Th>
              <Th right>reads</Th>
              <Th right>writes</Th>
              <Th right>exceptions</Th>
              <Th right>unanswered</Th>
              <Th right>rate/s</Th>
              <Th right>median latency</Th>
            </tr>
          </thead>
          <tbody>
            {data.map((x) => {
              const key = `${x.server}/${x.unit}`;
              return (
                <SelectRow key={key} selected={x === d} onSelect={() => setSel(key)}>
                  <td>{x.server}</td>
                  <td className="text-right">{x.unit}</td>
                  <td className="text-right">{formatCount(x.transactions)}</td>
                  <td className="text-right">{formatCount(x.reads)}</td>
                  <td className="text-right">{formatCount(x.writes)}</td>
                  <td className={`text-right ${x.exceptions ? 'text-danger' : ''}`}>{formatCount(x.exceptions)}</td>
                  <td className={`text-right ${x.unanswered ? 'text-wire' : ''}`}>{formatCount(x.unanswered)}</td>
                  <td className="text-right">{x.rate.toFixed(1)}</td>
                  <td className="text-right">{formatLatency(x.latency?.p50_ns)}</td>
                </SelectRow>
              );
            })}
          </tbody>
        </table>
      </div>
      {d && (
        <section aria-label="Device detail" className="flex w-80 shrink-0 flex-col gap-4 overflow-auto border-l border-line p-4">
          <Heading actions={<Button icon={<TimelineIcon size={14} />} onClick={() => onShow({ servers: [d.server], units: [d.unit] })}>Show in timeline</Button>}>
            {d.server} unit {d.unit}
          </Heading>
          <Facts
            items={[
              ['first', formatTime(d.first_ns, timeMode, firstNs)],
              ['last', formatTime(d.last_ns, timeMode, firstNs)],
              ['bytes', formatBytes(d.bytes)],
              ['clients', d.clients.join(', ')],
            ]}
          />
          <div>
            <span className="ot-label">By function</span>
            <Facts items={Object.entries(d.by_function).map(([fc, n]) => [formatFunction(Number(fc)), formatCount(n)])} />
          </div>
          <div>
            <span className="ot-label">By status</span>
            <Facts items={Object.entries(d.by_status).map(([s, n]) => [s, formatCount(n)])} />
          </div>
          <DistributionView label="Latency" dist={d.latency} />
        </section>
      )}
    </div>
  );
}

import { useState } from 'react';
import { Button, Chip } from '../components/ui';
import { TimelineIcon } from '../icons';
import type { Service } from '../service/service';
import type { Filter, Pattern } from '../service/types';
import { formatCount, formatDuration, formatFunction, formatLatency, formatRate, formatTime, type TimeMode } from '../state/format';
import { DistributionView, Facts, Heading, SelectRow, Status, TABLE, Th, range, useLoad } from './common';

interface Props {
  service: Service;
  fileId: string;
  filter: Filter;
  timeMode: TimeMode;
  firstNs: number;
  onShow: (f: Filter) => void;
}

const key = (p: Pattern) => `${p.client}|${p.server}|${p.unit}|${p.summary}`;

const VOLATILITY = { static: 'neutral', slow: 'accent', noisy: 'wire' } as const;

/** R10: the requests a client repeats, how regularly, and how often the answer changes. */
export function PatternsView({ service, fileId, filter, timeMode, firstNs, onShow }: Props) {
  const { data, error, loading } = useLoad(() => service.patterns(fileId, filter), [service, fileId, JSON.stringify(filter)]);
  const [sel, setSel] = useState<string | null>(null);
  if (!data) return <Status loading={loading} error={error} />;
  if (data.length === 0) return <Status loading={false} error={null} empty="No requests match the filter." />;
  const p = data.find((x) => key(x) === sel) ?? data[0];
  return (
    <div className="flex min-h-0 flex-1">
      <div className="min-w-0 flex-1 overflow-auto">
        <table className={TABLE} aria-label="Patterns">
          <thead>
            <tr>
              <Th>client</Th>
              <Th>device</Th>
              <Th>request</Th>
              <Th right>requests</Th>
              <Th right>interval</Th>
              <Th right>jitter</Th>
              <Th>values</Th>
            </tr>
          </thead>
          <tbody>
            {data.map((x) => (
              <SelectRow key={key(x)} selected={x === p} onSelect={() => setSel(key(x))}>
                <td>{x.client}</td>
                <td>
                  {x.server}/{x.unit}
                </td>
                <td>{x.summary}</td>
                <td className="text-right">{formatCount(x.requests)}</td>
                <td className="text-right">{formatLatency(x.interval?.p50_ns)}</td>
                <td className="text-right">{formatLatency(x.jitter_ns)}</td>
                <td>
                  <Chip tone={VOLATILITY[x.volatility_class]}>{x.volatility_class}</Chip>
                </td>
              </SelectRow>
            ))}
          </tbody>
        </table>
      </div>
      {p && (
        <section aria-label="Pattern detail" className="flex w-80 shrink-0 flex-col gap-4 overflow-auto border-l border-line p-4">
          <Heading
            actions={
              <Button
                icon={<TimelineIcon size={14} />}
                onClick={() => onShow({ summary: p.summary, clients: [p.client], servers: [p.server], units: [p.unit] })}
              >
                Show in timeline
              </Button>
            }
          >
            {formatFunction(p.function)}
          </Heading>
          <Facts
            items={[
              ['request', p.summary],
              ['addresses', p.count ? `${p.space ?? ''} ${range(p.start, p.start + p.count - 1)}` : '–'],
              ['client', p.client],
              ['device', `${p.server} unit ${p.unit}`],
              ['first', formatTime(p.first_ns, timeMode, firstNs)],
              ['last', formatTime(p.last_ns, timeMode, firstNs)],
              ['span', formatDuration(p.last_ns - p.first_ns)],
              ['rate', formatRate(p.requests, p.last_ns - p.first_ns)],
              ['responses', formatCount(p.responses)],
              ['answer changed', `${formatCount(p.changed)} times (${Math.round(p.volatility * 100)}%)`],
            ]}
          />
          <DistributionView label="Interval" dist={p.interval} />
        </section>
      )}
    </div>
  );
}

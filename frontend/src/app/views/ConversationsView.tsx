import { useState } from 'react';
import { Chip } from '../components/ui';
import type { Service } from '../service/service';
import type { Filter, Row, TimelinePage, TimelineQuery } from '../service/types';
import { formatBytes, formatCount, formatDuration, formatTime, type TimeMode } from '../state/format';
import { TimelineTable } from '../table/TimelineTable';
import { SelectRow, Status, TABLE, Th, useLoad } from './common';

interface Props {
  service: Service;
  fileId: string;
  filter: Filter;
  timeMode: TimeMode;
  firstNs: number;
  fetchPage: (q: TimelineQuery, signal: AbortSignal) => Promise<TimelinePage>;
  selectedSeq: number | null;
  onSelect: (row: Row) => void;
}

/** R9: the TCP connections, and the timeline of the selected one. */
export function ConversationsView({ service, fileId, filter, timeMode, firstNs, fetchPage, selectedSeq, onSelect }: Props) {
  const { data, error, loading } = useLoad(() => service.connections(fileId, filter), [service, fileId, JSON.stringify(filter)]);
  const [sel, setSel] = useState<number | null>(null);
  if (!data) return <Status loading={loading} error={error} />;
  if (data.length === 0) return <Status loading={false} error={null} empty="No connections match the filter." />;
  const c = data.find((x) => x.id === sel) ?? data[0];
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="max-h-[45%] shrink-0 overflow-auto border-b border-line">
        <table className={TABLE} aria-label="Connections">
          <thead>
            <tr>
              <Th right>conn</Th>
              <Th>client</Th>
              <Th>server</Th>
              <Th>role</Th>
              <Th>open</Th>
              <Th>close</Th>
              <Th>first</Th>
              <Th right>duration</Th>
              <Th right>transactions</Th>
              <Th right>bytes</Th>
              <Th right>gaps</Th>
              <Th>flags</Th>
            </tr>
          </thead>
          <tbody>
            {data.map((x) => (
              <SelectRow key={x.id} selected={x === c} onSelect={() => setSel(x.id)}>
                <td className="text-right">{x.id}</td>
                <td>{x.client}</td>
                <td>{x.server}</td>
                <td>
                  {x.role_guessed ? (
                    <Chip tone="wire" title={`Inferred from ${x.role_source}, confidence ${Math.round(x.role_confidence * 100)}%`}>
                      guessed
                    </Chip>
                  ) : (
                    x.role_source
                  )}
                </td>
                <td>{x.open}</td>
                <td>{x.close}</td>
                <td>{formatTime(x.first_ns, timeMode, firstNs)}</td>
                <td className="text-right">{formatDuration(x.last_ns - x.first_ns)}</td>
                <td className="text-right">{formatCount(x.transactions)}</td>
                <td className="text-right">{formatBytes(x.bytes)}</td>
                <td className={`text-right ${x.gaps ? 'text-wire' : ''}`}>{formatCount(x.gaps)}</td>
                <td>{x.flags.join(' ')}</td>
              </SelectRow>
            ))}
          </tbody>
        </table>
      </div>
      {c && (
        <section aria-label={`Timeline of connection ${c.id}`} className="flex min-h-0 flex-1 flex-col">
          <TimelineTable
            fetchPage={fetchPage}
            filter={{ ...filter, conns: [c.id] }}
            order="time"
            desc={false}
            timeMode={timeMode}
            firstNs={firstNs}
            selectedSeq={selectedSeq}
            onSelect={(_rank, row) => onSelect(row)}
          />
        </section>
      )}
    </div>
  );
}

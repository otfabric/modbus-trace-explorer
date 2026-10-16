import { Button } from '../components/ui';
import { CheckIcon, InfoIcon, TimelineIcon, WarningIcon } from '../icons';
import type { Service } from '../service/service';
import type { Condition, Filter } from '../service/types';
import { formatCount } from '../state/format';
import { Facts, Heading, Status, useLoad } from './common';

interface Props {
  service: Service;
  fileId: string;
  onShow: (f: Filter) => void;
}

function Mark({ c }: { c: Condition }) {
  if (c.count === 0) return <CheckIcon size={16} className="text-muted" />;
  if (c.severity === 'info') return <InfoIcon size={16} className="text-blue" />;
  return <WarningIcon size={16} className={c.severity === 'problem' ? 'text-danger' : 'text-wire'} />;
}

/** R12: what may make the results incomplete, each with what it means. */
export function QualityView({ service, fileId, onShow }: Props) {
  const { data, error, loading } = useLoad(() => service.quality(fileId), [service, fileId]);
  if (!data) return <Status loading={loading} error={error} />;
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-auto p-4">
      <section aria-label="Conditions">
        <Heading>Conditions</Heading>
        <ul className="flex flex-col divide-y divide-line">
          {data.conditions.map((c) => (
            <li key={c.code} className="flex items-start gap-3 py-2">
              <Mark c={c} />
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="flex items-baseline gap-3">
                  <span className="text-fg-strong">{c.label}</span>
                  <span className={`font-mono text-[13px] ${c.count === 0 ? 'text-muted' : ''}`}>{c.count === 0 ? 'none' : formatCount(c.count)}</span>
                </span>
                <span className="text-[13px] text-muted">{c.meaning}</span>
              </div>
              {c.filter && c.count > 0 && (
                <Button icon={<TimelineIcon size={14} />} onClick={() => c.filter && onShow(c.filter)}>
                  Show in timeline
                </Button>
              )}
            </li>
          ))}
        </ul>
      </section>
      <section aria-label="Facts">
        <Heading>Facts</Heading>
        <Facts items={data.facts.map((f) => [f.label, f.value])} />
      </section>
    </div>
  );
}

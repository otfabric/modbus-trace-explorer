import type { JobState } from '../service/types';
import { formatBytes, formatCount } from '../state/format';
import { Button } from './ui';

interface Props {
  title: string;
  name: string;
  job: JobState | null;
  onCancel: () => void;
}

const PHASES: Record<string, string> = {
  decompress: 'Decompressing',
  scan: 'Scanning',
  analyze: 'Analysing',
  index: 'Writing the index',
};

/** A scan or analysis in progress (UX.md section 2): bytes, rate, rows, Cancel. */
export function ImportProgress({ title, name, job, onCancel }: Props) {
  const total = job && job.total > 0 ? job.total : 0;
  const share = total > 0 ? Math.min(1, (job?.read ?? 0) / total) : 0;
  return (
    <main className="flex flex-1 items-center justify-center p-8">
      <section aria-label={title} className="flex w-full max-w-xl flex-col gap-4 rounded border border-control bg-surface p-6">
        <h1 className="font-display text-xl font-semibold text-fg-strong">{title}</h1>
        <p className="font-mono text-[13px] break-all">{name}</p>
        <div
          role="progressbar"
          aria-label={PHASES[job?.phase ?? ''] ?? 'Starting'}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(share * 100)}
          className="h-2 overflow-hidden rounded bg-bg"
        >
          <div className="h-full bg-accent" style={{ width: `${share * 100}%` }} />
        </div>
        <p className="font-mono text-[12px] text-muted">
          {PHASES[job?.phase ?? ''] ?? 'Starting'}
          {job && (
            <>
              {' '}
              · {formatBytes(job.read)}
              {total > 0 && ` of ${formatBytes(total)}`}
              {job.rate > 0 && ` · ${formatBytes(job.rate)}/s`}
              {job.rows > 0 && ` · ${formatCount(job.rows)} rows`}
            </>
          )}
        </p>
        <div className="flex justify-end">
          <Button onClick={onCancel}>Cancel</Button>
        </div>
      </section>
    </main>
  );
}

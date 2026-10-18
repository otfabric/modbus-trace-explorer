import { useEffect, useState } from 'react';
import { CloseIcon } from '../icons';
import type { Service } from '../service/service';
import type { CacheInfo, JobKind, JobState } from '../service/types';
import { formatBytes } from '../state/format';

const LABELS: Partial<Record<JobKind, string>> = {
  mbcap: 'MBCAP export',
  timeline: 'Timeline export',
  subtrace: 'Sub-trace export',
  history: 'Address history',
  prepare: 'Decompressing',
};

/** "local (UTC+02:00)": the offset of the computer's time zone at that moment (UX.md section 10). */
export function zoneLabel(ns: number): string {
  const off = -new Date(ns / 1e6).getTimezoneOffset();
  const sign = off < 0 ? '-' : '+';
  const a = Math.abs(off);
  return `UTC${sign}${String(Math.floor(a / 60)).padStart(2, '0')}:${String(a % 60).padStart(2, '0')}`;
}

/**
 * The status bar's right side (UX.md section 1): the jobs that run beside
 * the open trace (exports, address histories), each with Cancel, and the
 * cache's size, read again whenever a job ends.
 */
export function StatusJobs({ service, onEnded }: { service: Service; onEnded?: (text: string) => void }) {
  const [jobs, setJobs] = useState<Map<string, JobState>>(new Map());
  const [cache, setCache] = useState<CacheInfo | null>(null);
  useEffect(() => {
    const refresh = () => service.cacheInfo().then(setCache, () => setCache(null));
    refresh();
    return service.subscribe((ev) => {
      const job = ev.job;
      if (!job || !LABELS[job.kind]) return;
      setJobs((m) => {
        const next = new Map(m);
        if (job.state === 'running') next.set(job.id, job);
        else next.delete(job.id);
        return next;
      });
      if (job.state !== 'running') {
        refresh();
        if (job.kind !== 'history') {
          const what = LABELS[job.kind] ?? job.kind;
          onEnded?.(job.state === 'done' ? `${what} finished.` : job.state === 'canceled' ? `${what} cancelled.` : `${what} failed: ${job.error?.message ?? ''}`);
        }
      }
    });
  }, [service, onEnded]);
  return (
    <span className="ml-auto flex items-center gap-4">
      {[...jobs.values()].map((j) => (
        <span key={j.id} role="status" aria-label={`${LABELS[j.kind]} running`} className="inline-flex items-center gap-1 text-fg">
          {LABELS[j.kind]}
          {j.total > 0 ? ` ${Math.round((100 * j.read) / j.total)}%` : '…'}
          <button type="button" aria-label={`Cancel the ${LABELS[j.kind]?.toLowerCase()}`} className="hover:text-accent" onClick={() => void service.cancelJob(j.id)}>
            <CloseIcon size={12} />
          </button>
        </span>
      ))}
      {cache && <span title={cache.dir}>cache: {formatBytes(cache.bytes)}</span>}
    </span>
  );
}

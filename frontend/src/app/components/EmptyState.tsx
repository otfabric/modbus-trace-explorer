import { DropIcon, ErrorIcon, OpenFileIcon } from '../icons';
import type { RecentFile } from '../service/types';
import { Button } from './ui';

interface Props {
  onOpen: () => void;
  onOpenRecent: (paths: string[]) => void;
  recent: RecentFile[];
  error: string | null;
}

/** UX.md section 2: what to open, how, the recent files, and where recordings come from. */
export function EmptyState({ onOpen, onOpenRecent, recent, error }: Props) {
  return (
    <main className="flex flex-1 items-center justify-center p-8">
      <div
        data-file-drop-target
        className="flex w-full max-w-2xl flex-col items-center gap-4 rounded border-2 border-dashed border-control px-8 py-12 text-center"
      >
        <DropIcon size={40} className="text-accent" />
        <h1 className="font-display text-2xl font-semibold text-fg-strong">Drop a pcap or pcapng here, or open one</h1>
        <p className="text-muted">pcap and pcapng from tcpdump, Wireshark, dumpcap and tshark; .gz too. Several files are read as one trace. An MBCAP capture (.mbcap, .mbcap.jsonl, .mbcap.csv) opens on its own, to see what it holds.</p>
        <Button variant="primary" icon={<OpenFileIcon size={16} />} onClick={onOpen}>
          Open…
        </Button>
        {error && (
          <p role="alert" className="flex items-center gap-1 text-danger">
            <ErrorIcon size={14} /> {error}
          </p>
        )}
        {recent.length > 0 && (
          <nav aria-label="Recent traces" className="flex w-full flex-col items-stretch gap-1 text-left">
            <span className="ot-label">Recent</span>
            {recent.map((r) => (
              <button
                key={r.paths.join('\n')}
                type="button"
                disabled={!r.exists}
                title={r.exists ? r.paths.join('\n') : 'Not found at this path any more'}
                onClick={() => onOpenRecent(r.paths)}
                className="truncate rounded px-2 py-1 text-left font-mono text-[13px] hover:bg-surface hover:text-accent disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-fg"
              >
                {r.name}
                {!r.exists && <span className="text-muted"> (not found)</span>}
              </button>
            ))}
          </nav>
        )}
        <p className="mt-4 text-[13px] text-muted">
          This tool reads recordings. To record, use tcpdump or Wireshark, for example on a gateway:{' '}
          <code className="font-mono text-fg">tcpdump -i eth0 -s 0 -w site.pcap &apos;tcp port 502&apos;</code> (<code className="font-mono">-s 0</code>{' '}
          keeps whole frames; other ports need their own filter).
        </p>
      </div>
    </main>
  );
}

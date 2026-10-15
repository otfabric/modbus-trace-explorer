import { DropIcon, OpenFileIcon } from '../icons';
import { Button } from './ui';

interface Props {
  onOpen: () => void;
  notice: string | null;
}

/** UX.md section 2: what to open, how, and where recordings come from. */
export function EmptyState({ onOpen, notice }: Props) {
  return (
    <main className="flex flex-1 items-center justify-center p-8">
      <div
        data-file-drop-target
        className="flex w-full max-w-2xl flex-col items-center gap-4 rounded border-2 border-dashed border-control px-8 py-12 text-center"
      >
        <DropIcon size={40} className="text-accent" />
        <h1 className="font-display text-2xl font-semibold text-fg-strong">Drop a pcap or pcapng here, or open one</h1>
        <p className="text-muted">pcap and pcapng from tcpdump, Wireshark, dumpcap and tshark; .gz too. Several files are read as one trace.</p>
        <Button variant="primary" icon={<OpenFileIcon size={16} />} onClick={onOpen}>
          Open…
        </Button>
        {notice && (
          <p role="status" className="max-w-full font-mono text-[13px] break-all text-fg">
            {notice}
          </p>
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

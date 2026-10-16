import { useEffect, useState } from 'react';
import type { Platform } from '../platform/platform';
import { toServiceError, type Service } from '../service/service';
import type { About } from '../service/types';
import { Dialog } from './Dialog';
import { Button } from './ui';

/** Help > About: the versions, and "Copy diagnostics" for a bug report (R23). */
export function AboutDialog({ service, platform, fileId, onClose }: { service: Service; platform: Platform; fileId: string; onClose: () => void }) {
  const [about, setAbout] = useState<About | null>(null);
  const [status, setStatus] = useState<string | null>(null);
  useEffect(() => {
    service.about().then(setAbout, () => setAbout(null));
  }, [service]);
  const copy = async () => {
    try {
      await platform.copyText(await service.diagnostics(fileId));
      setStatus('Diagnostics copied: versions and the trace\'s facts, without paths or addresses.');
    } catch (err) {
      setStatus(toServiceError(err).message);
    }
  };
  return (
    <Dialog title="About Modbus Trace Explorer" onClose={onClose}>
      {about && (
        <dl className="mt-3 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 font-mono text-[12px]">
          <dt className="text-muted">version</dt>
          <dd>{about.version}</dd>
          <dt className="text-muted">go-modbus-trace</dt>
          <dd>{about.go_modbus_trace}</dd>
          <dt className="text-muted">Wails</dt>
          <dd>{about.wails}</dd>
          <dt className="text-muted">Go</dt>
          <dd>{about.go}</dd>
          <dt className="text-muted">system</dt>
          <dd>
            {about.os}/{about.arch}
          </dd>
        </dl>
      )}
      <p className="mt-3 text-[13px] text-muted">MIT licensed. Part of OT Fabric.</p>
      {status && (
        <p role="status" className="mt-3 text-[13px]">
          {status}
        </p>
      )}
      <div className="mt-5 flex justify-end gap-2">
        <Button onClick={() => void copy()}>Copy diagnostics</Button>
        <Button variant="primary" onClick={onClose} autoFocus>
          Close
        </Button>
      </div>
    </Dialog>
  );
}

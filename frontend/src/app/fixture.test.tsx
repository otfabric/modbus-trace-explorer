import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import engineSite from '../test/fixtures/engine-site.json';
import { loadSchema, validate } from '../test/schema';
import { App } from './App';
import type { Platform } from './platform/platform';
import { MockService } from './service/mock';
import type { ConnStats, Detail, DeviceStats, FileInfo, Overview, Pattern, Quality, TimelinePage, TimelineQuery } from './service/types';

// The engine's answers for the site trace (written by make golden from the
// real engine), served to the real UI: what the mock taught the UI must
// work on what the engine sends (A17).
const fx = engineSite as unknown as {
  file_info: FileInfo;
  overview: Overview;
  timeline: TimelinePage;
  details: Record<string, Detail>;
  devices: DeviceStats[];
  connections: ConnStats[];
  patterns: Pattern[];
  quality: Quality;
};

class FixtureService extends MockService {
  override async open(): Promise<FileInfo> {
    return { ...fx.file_info, analyzed: true, prepare: false };
  }
  override async overview(): Promise<Overview> {
    return fx.overview;
  }
  override async timeline(_id: string, q: TimelineQuery): Promise<TimelinePage> {
    const rows = q.desc ? [...fx.timeline.rows].reverse() : fx.timeline.rows;
    return { offset: q.offset, rows: rows.slice(q.offset, q.offset + q.limit), total: fx.timeline.total };
  }
  override async detail(_id: string, seq: number): Promise<Detail> {
    const d = fx.details[String(seq)];
    if (!d) throw new Error(`no detail ${seq}`);
    return d;
  }
  override async devices(): Promise<DeviceStats[]> {
    return fx.devices;
  }
  override async connections(): Promise<ConnStats[]> {
    return fx.connections;
  }
  override async patterns(): Promise<Pattern[]> {
    return fx.patterns;
  }
  override async quality(): Promise<Quality> {
    return fx.quality;
  }
}

function platform(): Platform {
  return {
    chooseTraces: async () => ['/traces/site.pcapng'],
    chooseSave: async (name: string) => `/out/${name}`,
    onOpen: () => () => {},
    smokeMode: async () => false,
    smokeDone: () => {},
    onMenu: () => () => {},
    openExternal: () => {},
    persistTheme: () => {},
    loadTheme: () => 'dark' as const,
    copyText: async () => {},
  };
}

describe("the engine's own answers in the UI (A17)", () => {
  it('validate against the schemas the mock is held to', () => {
    const pairs: [string, unknown][] = [
      ['file-info', fx.file_info],
      ['overview', fx.overview],
      ['timeline-page', fx.timeline],
      ['devices', fx.devices],
      ['connections', fx.connections],
      ['patterns', fx.patterns],
      ['quality', fx.quality],
      ['scan-result', engineSite.scan_result],
      ['mbcap-preview', engineSite.mbcap_preview],
      ['export-result', engineSite.mbcap_export],
      ...Object.values(fx.details).map((d): [string, unknown] => ['detail', d]),
    ];
    for (const [name, v] of pairs) expect(validate(loadSchema(name), v).join('\n'), name).toBe('');
  });

  it('show the timeline, a detail and the views', async () => {
    const user = userEvent.setup();
    render(<App service={new FixtureService({ rows: 10, analyzed: true, step: 1 })} platform={platform()} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', String(fx.timeline.total + 1)));
    expect(screen.getByText(fx.overview.file)).toBeInTheDocument();
    const txn = fx.timeline.rows.find((r) => r.kind === 'txn');
    if (!txn) throw new Error('no transaction in the fixture');
    expect(within(grid).getAllByText(txn.summary).length).toBeGreaterThan(0);
    await user.click(within(grid).getAllByText(txn.summary)[0] as HTMLElement);
    const pane = screen.getByRole('complementary', { name: 'Detail' });
    expect(await within(pane).findByText(fx.details[String(txn.seq)]?.text ?? '?')).toBeInTheDocument();
    await user.click(within(pane).getByRole('tab', { name: 'Raw' }));
    expect(within(pane).getAllByRole('button', { name: /Request byte/ }).length).toBeGreaterThan(7);

    const nav = screen.getByRole('navigation', { name: 'Views' });
    await user.click(within(nav).getByRole('button', { name: /Devices/ }));
    const devices = await screen.findByRole('table', { name: 'Devices' });
    expect(within(devices).getAllByRole('row')).toHaveLength(fx.devices.length + 1);
    await user.click(within(nav).getByRole('button', { name: /Patterns/ }));
    expect(await screen.findByRole('table', { name: 'Patterns' })).toBeInTheDocument();
    await user.click(within(nav).getByRole('button', { name: /Quality/ }));
    expect(await screen.findByRole('region', { name: 'Conditions' })).toBeInTheDocument();
  });
});

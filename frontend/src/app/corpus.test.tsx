import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { loadSchema, validate } from '../test/schema';
import { App } from './App';
import { DetailPane } from './components/DetailPane';
import type { Platform } from './platform/platform';
import { MockService } from './service/mock';
import type { ConnStats, Detail, DeviceStats, FileInfo, Overview, Pattern, Quality, TimelinePage, TimelineQuery } from './service/types';

// The engine's answers for corpus files (testdata/corpus, written into
// src/test/fixtures by make golden): the function tour (every decoder), the
// pairing-trouble trace (orphans, malformed PDUs, odd units) and an unusual
// capture (serial and TLS targets, long labels in several scripts). The UI
// must render every row's detail on every tab and every view.
interface Fixture {
  file_info: FileInfo;
  overview: Overview;
  timeline: TimelinePage;
  details: Record<string, Detail>;
  devices: DeviceStats[];
  connections: ConnStats[];
  patterns: Pattern[];
  quality: Quality;
}

const fixtures = import.meta.glob<Fixture>('../test/fixtures/engine-{tour,pairing,odd}.json', { eager: true, import: 'default' });

class FixtureService extends MockService {
  constructor(private readonly fx: Fixture) {
    super({ rows: 10, analyzed: true, step: 1 });
  }
  override async open(): Promise<FileInfo> {
    return { ...this.fx.file_info, analyzed: true, prepare: false };
  }
  override async overview(): Promise<Overview> {
    return this.fx.overview;
  }
  override async timeline(_id: string, q: TimelineQuery): Promise<TimelinePage> {
    const rows = q.desc ? [...this.fx.timeline.rows].reverse() : this.fx.timeline.rows;
    return { offset: q.offset, rows: rows.slice(q.offset, q.offset + q.limit), total: this.fx.timeline.total };
  }
  override async detail(_id: string, seq: number): Promise<Detail> {
    const d = this.fx.details[String(seq)];
    if (!d) throw new Error(`no detail ${seq}`);
    return d;
  }
  override async devices(): Promise<DeviceStats[]> {
    return this.fx.devices;
  }
  override async connections(): Promise<ConnStats[]> {
    return this.fx.connections;
  }
  override async patterns(): Promise<Pattern[]> {
    return this.fx.patterns;
  }
  override async quality(): Promise<Quality> {
    return this.fx.quality;
  }
}

const platform: Platform = {
  chooseTraces: async () => ['/traces/x'],
  chooseSave: async (name: string) => `/out/${name}`,
  onOpen: () => () => {},
  onMenu: () => () => {},
  smokeMode: async () => false,
  smokeDone: () => {},
  openExternal: () => {},
  persistTheme: () => {},
  loadTheme: () => 'dark' as const,
  copyText: async () => {},
};

describe.each(Object.entries(fixtures))('corpus fixture %s', (_name, fx) => {
  it('validates against the schemas', () => {
    for (const [schema, v] of [
      ['file-info', fx.file_info],
      ['overview', fx.overview],
      ['timeline-page', fx.timeline],
      ['devices', fx.devices],
      ['connections', fx.connections],
      ['patterns', fx.patterns],
      ['quality', fx.quality],
      ...Object.values(fx.details).map((d) => ['detail', d] as const),
    ] as const) {
      expect(validate(loadSchema(schema), v).join('\n'), schema).toBe('');
    }
  });

  it("renders every row's detail on every tab", () => {
    for (const d of Object.values(fx.details)) {
      render(<DetailPane detail={d} error={null} loading={false} timeMode="relative" firstNs={fx.overview.first_ns} copy={() => {}} />);
      const tabs = screen.getAllByRole('tab');
      for (const tab of tabs) {
        fireEvent.click(tab);
        expect(screen.getByRole('tabpanel')).toBeInTheDocument();
      }
      // Field rows select their bytes in Raw.
      fireEvent.click(screen.getByRole('tab', { name: 'Summary' }));
      if (d.request?.fields.length) {
        fireEvent.click(screen.getByRole('tab', { name: 'Request' }));
        fireEvent.click(screen.getAllByText((d.request.fields[d.request.fields.length - 1]?.name ?? '').replaceAll('_', ' '))[0] as HTMLElement);
        expect(screen.getByRole('tabpanel', { name: 'Raw' })).toBeInTheDocument();
      }
      cleanup();
    }
  });

  it('shows the timeline and every view', async () => {
    const user = userEvent.setup();
    render(<App service={new FixtureService(fx)} platform={platform} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', String(fx.timeline.total + 1)));
    grid.focus();
    await user.keyboard('{Home}');
    const pane = screen.getByRole('complementary', { name: 'Detail' });
    await waitFor(() => expect(within(pane).queryByText('Select a row.')).toBeNull());
    const nav = screen.getByRole('navigation', { name: 'Views' });
    for (const [view, region] of [
      ['Devices', 'Devices'],
      ['Conversations', 'Connections'],
      ['Patterns', 'Patterns'],
    ] as const) {
      await user.click(within(nav).getByRole('button', { name: new RegExp(view) }));
      const table = await screen.findByRole('table', { name: region });
      expect(within(table).getAllByRole('row').length).toBeGreaterThan(1);
    }
    await user.click(within(nav).getByRole('button', { name: /Quality/ }));
    expect(await screen.findByRole('region', { name: 'Conditions' })).toBeInTheDocument();
  });
});

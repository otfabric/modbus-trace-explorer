import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import type { MenuAction, Platform } from './platform/platform';
import { act } from 'react';
import { App } from './App';
import { MockService } from './service/mock';
import type { JobRef } from './service/types';

const copied: string[] = [];
let menu: ((a: MenuAction) => void) | null = null;

function platform(paths: string[] = []): Platform {
  return {
    chooseTraces: async () => paths,
    chooseSave: async (name: string) => `/out/${name}`,
    onOpen: () => () => {},
    smokeMode: async () => false,
    smokeDone: () => {},
    onMenu: (h: (a: MenuAction) => void) => {
      menu = h;
      return () => {};
    },
    openExternal: () => {},
    persistTheme: () => {},
    loadTheme: () => 'dark' as const,
    copyText: async (t: string) => {
      copied.push(t);
    },
  };
}

describe('App', () => {
  it('imports a trace: open, scan, the Flows step, analysis, timeline', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 5000, step: 1 })} platform={platform(['/x/site.pcapng'])} />);
    expect(screen.getByText(/Drop a pcap or pcapng here/)).toBeInTheDocument();
    expect(document.querySelector('[data-file-drop-target]')).not.toBeNull();
    expect(await screen.findByRole('navigation', { name: 'Recent traces' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /Open/ }));
    // The Flows step: Modbus flows preselected, TLS explained.
    const analyze = await screen.findByRole('button', { name: 'Analyze 2 flows' });
    expect(screen.getByText(/cannot be decoded/)).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Analyze flow 3' })).not.toBeChecked();
    await user.click(screen.getByRole('checkbox', { name: 'Analyze flow 1' }));
    expect(screen.getByRole('button', { name: 'Analyze 1 flow' })).toBeInTheDocument();
    await user.click(screen.getByRole('checkbox', { name: 'Analyze flow 1' }));
    await user.click(analyze);
    const grid = await screen.findByRole('grid', { name: 'Timeline' }, { timeout: 3000 });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '5001'));
    expect(screen.getByText('site.pcapng')).toBeInTheDocument();
    expect(within(screen.getByRole('navigation', { name: 'Views' })).getAllByRole('button')).toHaveLength(6);
  });

  it('filters with the text box and shows the detail of a row', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 5000, analyzed: true, step: 0 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '5001'));
    const box = screen.getByRole('textbox', { name: 'Filter' });
    await user.type(box, 'class:write{Enter}');
    await waitFor(() => expect(Number(grid.getAttribute('aria-rowcount'))).toBeLessThan(500));
    expect(screen.getByRole('button', { name: 'Remove class:write' })).toBeInTheDocument();
    await user.type(box, 'color:red{Enter}');
    expect(await screen.findByRole('alert')).toHaveTextContent('color:red');
    await user.click(screen.getByRole('button', { name: 'Remove class:write' }));
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '5001'));
    const rows = within(grid).getAllByRole('row');
    await user.click(rows[3] as HTMLElement);
    const pane = screen.getByRole('complementary', { name: 'Detail' });
    expect(await within(pane).findByText(/asked/)).toBeInTheDocument();
    await user.click(within(pane).getByRole('tab', { name: 'Request' }));
    await user.click(within(pane).getByText('quantity'));
    expect(within(pane).getByRole('tabpanel', { name: 'Raw' })).toBeInTheDocument();
    await user.click(within(pane).getByRole('tab', { name: 'Packets' }));
    expect(within(pane).getAllByRole('button', { name: 'Copy Wireshark filter' }).length).toBeGreaterThan(0);
    grid.focus();
    await user.keyboard('{End}');
    await waitFor(() => expect(grid.querySelector('[aria-selected=true]')?.getAttribute('aria-rowindex')).toBe('5001'));
  });

  it('cancels a scan back to the empty state', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 100, step: 500 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    await user.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(await screen.findByText(/Drop a pcap or pcapng here/)).toBeInTheDocument();
  });

  it('shows devices, connections, patterns, addresses and quality, and jumps to the timeline', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 3000, analyzed: true, step: 1 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    await screen.findByRole('grid', { name: 'Timeline' });
    const nav = screen.getByRole('navigation', { name: 'Views' });

    await user.click(within(nav).getByRole('button', { name: /Devices/ }));
    const devices = await screen.findByRole('table', { name: 'Devices' });
    expect(within(devices).getAllByRole('row').length).toBeGreaterThan(1);
    const detail = screen.getByRole('region', { name: 'Device detail' });
    expect(within(detail).getByText('Latency')).toBeInTheDocument();
    await user.click(within(detail).getByRole('button', { name: 'Show table' }));
    expect(within(detail).getByRole('table')).toBeInTheDocument();
    await user.click(within(detail).getByRole('button', { name: 'Show in timeline' }));
    expect(await screen.findByRole('grid', { name: 'Timeline' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Remove server:/ })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /Remove server:/ }));
    await user.click(screen.getByRole('button', { name: /Remove unit:/ }));

    await user.click(within(nav).getByRole('button', { name: /Conversations/ }));
    const conns = await screen.findByRole('table', { name: 'Connections' });
    expect(within(conns).getAllByRole('row').length).toBeGreaterThan(1);
    expect(screen.getByRole('region', { name: /Timeline of connection/ })).toBeInTheDocument();

    await user.click(within(nav).getByRole('button', { name: /Patterns/ }));
    const patterns = await screen.findByRole('table', { name: 'Patterns' });
    expect(within(patterns).getAllByRole('row').length).toBeGreaterThan(1);
    await user.click(within(screen.getByRole('region', { name: 'Pattern detail' })).getByRole('button', { name: 'Show in timeline' }));
    expect(await screen.findByRole('button', { name: /Remove request:/ })).toBeInTheDocument();

    await user.click(within(nav).getByRole('button', { name: /Addresses/ }));
    expect(await screen.findByText(/Observed, not a complete register map/)).toBeInTheDocument();
    await user.type(screen.getByRole('textbox', { name: 'Address' }), '40');
    await user.click(screen.getByRole('button', { name: 'Show values' }));
    expect(await screen.findByRole('img', { name: 'Values over time' }, { timeout: 3000 })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull(), { timeout: 5000 });
    await user.click(screen.getByRole('button', { name: 'hex' }));
    await user.click(within(screen.getByRole('figure', { name: 'Values' })).getByRole('button', { name: 'Show table' }));
    expect(within(screen.getByRole('table', { name: 'Values' })).getAllByText(/^0x/).length).toBeGreaterThan(0);

    await user.click(within(nav).getByRole('button', { name: /Quality/ }));
    const conditions = await screen.findByRole('region', { name: 'Conditions' });
    expect(within(conditions).getAllByText('none').length).toBeGreaterThan(0);
    expect(screen.getByRole('region', { name: 'Facts' })).toHaveTextContent('pcapng');
  });

  it('exports an MBCAP capture with a live preview, the report and the replay commands', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 3000, analyzed: true, step: 1 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    await screen.findByRole('grid', { name: 'Timeline' });
    await user.click(await screen.findByRole('button', { name: /^Export$/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Export' });
    const preview = await within(dialog).findByRole('region', { name: 'Preview' });
    expect(await within(preview).findByText(/samples of .* transactions/)).toBeInTheDocument();
    expect(within(preview).getByText('write_function')).toBeInTheDocument();
    const label = within(preview).getAllByRole('textbox')[0] as HTMLInputElement;
    await user.clear(label);
    await user.type(label, 'Meter');
    await user.click(within(dialog).getByRole('checkbox', { name: /Change-only/ }));
    expect(await within(preview).findAllByText(/up to/)).not.toHaveLength(0);
    await user.click(within(dialog).getByRole('button', { name: 'Export…' }));
    const done = await screen.findByRole('dialog', { name: 'Export written' }, { timeout: 3000 });
    expect(within(done).getByText('site.mbcap')).toBeInTheDocument();
    expect(within(done).getByText(/1: Meter/)).toBeInTheDocument();
    expect(within(done).getByText(/^mbcapctl verify \/out\/site\.mbcap$/)).toBeInTheDocument();
    expect(within(done).getAllByText(/modbusctl server replay/).length).toBeGreaterThan(0);
    expect(within(done).getByText('stale_after_write')).toBeInTheDocument();
    await user.click(within(done).getByRole('button', { name: 'Save report…' }));
    expect(await within(done).findByText('Report saved as site-report.json')).toBeInTheDocument();
    await user.click(within(done).getByRole('button', { name: 'Done' }));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('exports the timeline, and explains a refused sub-trace', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 3000, analyzed: true, step: 1 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await user.type(screen.getByRole('textbox', { name: 'Filter' }), 'unit:1{Enter}');
    await waitFor(() => expect(Number(grid.getAttribute('aria-rowcount'))).toBeLessThan(3000));
    await user.keyboard('{Control>}e{/Control}');
    const dialog = await screen.findByRole('dialog', { name: 'Export' });
    await user.click(within(dialog).getByRole('tab', { name: 'Timeline' }));
    await user.click(within(dialog).getByRole('radio', { name: 'JSON Lines' }));
    await user.click(within(dialog).getByRole('button', { name: 'Export…' }));
    const done = await screen.findByRole('dialog', { name: 'Export written' }, { timeout: 3000 });
    expect(within(done).getByText(/site-timeline\.jsonl/)).toBeInTheDocument();
    expect(within(done).getByText(/rows/)).toBeInTheDocument();
    await user.click(within(done).getByRole('button', { name: 'Done' }));

    await user.click(screen.getByRole('button', { name: /^Export$/ }));
    const again = await screen.findByRole('dialog', { name: 'Export' });
    await user.click(within(again).getByRole('tab', { name: /Sub-trace/ }));
    expect(within(again).getByText(/writes and vendor functions are included verbatim/)).toBeInTheDocument();
    await user.click(within(again).getByRole('button', { name: 'Export…' }));
    expect(await within(again).findByRole('alert')).toHaveTextContent(/not transactions/);
    await user.click(within(again).getByRole('radio', { name: 'Every connection' }));
    await user.click(within(again).getByRole('button', { name: 'Export…' }));
    const sub = await screen.findByRole('dialog', { name: 'Export written' }, { timeout: 3000 });
    expect(within(sub).getByText(/packets of 4 connections/)).toBeInTheDocument();
  });

  it('keeps settings: time display, server ports, address labels, cache and recent files', async () => {
    const user = userEvent.setup();
    const service = new MockService({ rows: 3000, analyzed: true, step: 1 });
    render(<App service={service} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: 'Settings' }));
    const dialog = await screen.findByRole('dialog', { name: 'Settings' });
    await within(dialog).findByRole('combobox', { name: 'Time display' });
    await user.selectOptions(within(dialog).getByRole('combobox', { name: 'Time display' }), 'utc');
    await user.type(within(dialog).getByRole('textbox', { name: 'Server ports' }), '1502, x');
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(/1 to 65535/);
    await user.clear(within(dialog).getByRole('textbox', { name: 'Server ports' }));
    await user.type(within(dialog).getByRole('textbox', { name: 'Server ports' }), '1502 503');
    await user.click(within(dialog).getByRole('checkbox', { name: /1-based/ }));
    await user.click(within(dialog).getByRole('button', { name: 'Clear recent files' }));
    expect(await within(dialog).findByRole('status')).toHaveTextContent('Recent files cleared.');
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(await service.settings()).toMatchObject({ time_mode: 'utc', server_ports: [503, 1502], one_based: true });

    await user.click(screen.getByRole('button', { name: /Open/ }));
    await screen.findByRole('grid', { name: 'Timeline' });
    expect(screen.getByText('time: utc')).toBeInTheDocument();
    await user.keyboard('{Control>},{/Control}');
    const again = await screen.findByRole('dialog', { name: 'Settings' });
    expect(await within(again).findByText(/in 1 entry/)).toBeInTheDocument();
    await user.click(within(again).getByRole('button', { name: 'Clear cache…' }));
    await user.click(await screen.findByRole('button', { name: 'Clear cache' }));
    expect(await within(again).findByText(/in 0 entries/)).toBeInTheDocument();
  });

  it('decompresses an analysed gzip trace again before showing it (A16)', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 2000, analyzed: true, step: 20 })} platform={platform(['/x/site.pcapng.gz'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    expect(await screen.findByText('Decompressing the trace')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Analyze/ })).toBeNull();
    const grid = await screen.findByRole('grid', { name: 'Timeline' }, { timeout: 3000 });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '2001'));
  });

  it('goes to a time and a frame, walks a connection with N/P, and copies the row', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 5000, analyzed: true, step: 1 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '5001'));
    grid.focus();
    await user.keyboard('{Control>}g{/Control}');
    const time = await screen.findByRole('dialog', { name: 'Go to time' });
    await user.type(within(time).getByRole('textbox', { name: 'Time' }), '+30s');
    await user.click(within(time).getByRole('button', { name: 'Go' }));
    // 100 rows a second: the row 30 s in is rank 3000.
    await waitFor(() => expect(grid.querySelector('[aria-selected=true]')?.getAttribute('aria-rowindex')).toBe('3002'));

    await user.keyboard('{Control>}{Shift>}g{/Shift}{/Control}');
    const frame = await screen.findByRole('dialog', { name: 'Go to frame' });
    await user.type(within(frame).getByRole('textbox', { name: 'Frame' }), '21');
    await user.click(within(frame).getByRole('button', { name: 'Go' }));
    // Frame 21 is the request of row 10 (rank 9).
    await waitFor(() => expect(grid.querySelector('[aria-selected=true]')?.getAttribute('aria-rowindex')).toBe('11'));

    const before = grid.querySelector('[aria-selected=true]')?.textContent ?? '';
    grid.focus();
    await user.keyboard('n');
    await waitFor(() => expect(grid.querySelector('[aria-selected=true]')?.textContent).not.toBe(before));
    await user.keyboard('p');
    await waitFor(() => expect(grid.querySelector('[aria-selected=true]')?.getAttribute('aria-rowindex')).toBe('11'));

    const pane = screen.getByRole('complementary', { name: 'Detail' });
    await within(pane).findByText(/asked/);
    copied.length = 0;
    grid.focus();
    await user.keyboard('{Control>}c{/Control}');
    await waitFor(() => expect(copied[0]).toMatch(/asked/));
    await user.keyboard('{Control>}{Shift>}c{/Shift}{/Control}');
    await waitFor(() => expect(copied[1]).toMatch(/^[0-9a-f]+/));
    grid.focus();
    await user.keyboard('{Enter}');
    expect(document.activeElement).toBe(pane);
  });

  it('filters with the controls, shows capture facts, selects raw bytes, and answers the menu', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 3000, analyzed: true, step: 1 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await user.click(screen.getByRole('button', { name: 'Filter controls' }));
    const pop = await screen.findByRole('dialog', { name: 'Filter controls' });
    await user.click(within(pop).getByRole('checkbox', { name: '03 Read Holding' }));
    await user.type(within(pop).getByRole('textbox', { name: 'Units' }), '1');
    await user.click(within(pop).getByRole('button', { name: 'Apply' }));
    expect(await screen.findByRole('button', { name: 'Remove unit:1' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Remove fc:3' })).toBeInTheDocument();
    await waitFor(() => expect(Number(grid.getAttribute('aria-rowcount'))).toBeLessThan(3000));

    await user.click(screen.getByRole('button', { name: 'Capture facts' }));
    const facts = await screen.findByRole('dialog', { name: 'Capture facts' });
    expect(within(facts).getByText(/9f2c4e0d/)).toBeInTheDocument();
    expect(within(facts).getByText(/not_tcp 12/)).toBeInTheDocument();
    await user.keyboard('{Escape}');

    await user.click(within(grid).getAllByRole('row')[2] as HTMLElement);
    const pane = screen.getByRole('complementary', { name: 'Detail' });
    await within(pane).findByText(/asked/);
    await user.click(within(pane).getByRole('tab', { name: 'Raw' }));
    await user.click(within(pane).getByRole('button', { name: 'Request byte 7' }));
    await user.keyboard('{Shift>}');
    await user.click(within(pane).getByRole('button', { name: 'Request byte 9' }));
    await user.keyboard('{/Shift}');
    copied.length = 0;
    await user.click(within(pane).getByRole('button', { name: 'Copy bytes 7–9 as Python' }));
    expect(copied[0]).toMatch(/^b"\\x03\\x[0-9a-f]{2}\\x[0-9a-f]{2}"$/);

    act(() => menu?.({ action: 'about' }));
    const about = await screen.findByRole('dialog', { name: 'About Modbus Trace Explorer' });
    await user.click(within(about).getByRole('button', { name: 'Copy diagnostics' }));
    expect(await within(about).findByRole('status')).toHaveTextContent('without paths or addresses');
    expect(copied.at(-1)).toMatch(/index: 3000 rows/);
    await user.click(within(about).getByRole('button', { name: 'Close' }));
    act(() => menu?.({ action: 'close' }));
    expect(await screen.findByText(/Drop a pcap or pcapng here/)).toBeInTheDocument();
  });

  it('shows a scan that fails before its job is known (a broken file)', async () => {
    class Broken extends MockService {
      override async scan(): Promise<JobRef> {
        const job = { id: 'scan-x', kind: 'scan' as const, file: 'mock', state: 'failed' as const, phase: 'scan', read: 0, total: 0, rows: 0, rate: 0,
          error: { code: 'invalid_argument', message: 'not a pcap or pcapng file' } };
        for (const l of this.listeners) l({ kind: 'job', job });
        return { id: 'scan-x', kind: 'scan' };
      }
    }
    const user = userEvent.setup();
    render(<App service={new Broken({ rows: 10 })} platform={platform(['/x/broken.pcap'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    expect(await screen.findByText(/not a pcap or pcapng file/)).toBeInTheDocument();
  });

  it('resizes the detail pane, and shows background exports and the cache in the status bar', async () => {
    const user = userEvent.setup();
    localStorage.removeItem('modbus-trace-explorer.detail-width');
    render(<App service={new MockService({ rows: 3000, analyzed: true, step: 200 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    await screen.findByRole('grid', { name: 'Timeline' });
    const pane = screen.getByRole('complementary', { name: 'Detail' });
    const handle = screen.getByRole('separator', { name: 'Resize the detail pane' });
    expect(pane.style.width).toBe('384px');
    handle.focus();
    await user.keyboard('{ArrowLeft}{Shift>}{ArrowLeft}{/Shift}');
    expect(pane.style.width).toBe('464px');
    await user.keyboard('{End}');
    expect(pane.style.width).toBe('280px');
    expect(localStorage.getItem('modbus-trace-explorer.detail-width')).toBe('280');
    expect(await screen.findByText(/^cache: /)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /^Export$/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Export' });
    await user.click(within(dialog).getByRole('tab', { name: 'Timeline' }));
    await user.click(within(dialog).getByRole('button', { name: 'Export…' }));
    await user.click(await within(dialog).findByRole('button', { name: 'Continue in the background' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    const job = await screen.findByRole('status', { name: 'Timeline export running' });
    await user.click(within(job).getByRole('button', { name: 'Cancel the timeline export' }));
    expect(await screen.findByText('Timeline export cancelled.')).toBeInTheDocument();
    expect(screen.queryByRole('status', { name: 'Timeline export running' })).toBeNull();
  });

  it('opens an MBCAP capture: imported without a Flows step, its samples in the timeline, no exports', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 2000, step: 1 })} platform={platform(['/x/site.mbcap'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    const grid = await screen.findByRole('grid', { name: 'Timeline' }, { timeout: 3000 });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '2001'));
    expect(screen.queryByRole('button', { name: /^Analyze/ })).toBeNull();
    expect(screen.getByText('MBCAP capture')).toBeInTheDocument();
    expect(screen.getByText(/2,000 samples/)).toBeInTheDocument();
    expect(screen.getByText(/streams/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Export$/ })).toBeDisabled();
    await user.keyboard('{Control>}e{/Control}');
    expect(screen.queryByRole('dialog', { name: 'Export' })).toBeNull();
  });

  it('cancels a capture import back to the empty state, and refuses to import a trace', async () => {
    const user = userEvent.setup();
    render(<App service={new MockService({ rows: 100, step: 500 })} platform={platform(['/x/site.mbcap'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    expect(await screen.findByText('Importing the capture')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(await screen.findByText(/Drop a pcap or pcapng here/)).toBeInTheDocument();
    const trace = new MockService({ rows: 10 });
    await trace.open(['/x/site.pcapng']);
    await expect(trace.importCapture()).rejects.toThrow(/this is a trace/);
  });

  it('answers the menu Export for a trace and not for a capture', async () => {
    const user = userEvent.setup();
    const { unmount } = render(<App service={new MockService({ rows: 100, analyzed: true, step: 1 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    await screen.findByRole('grid', { name: 'Timeline' });
    act(() => menu?.({ action: 'export' }));
    expect(await screen.findByRole('dialog', { name: 'Export' })).toBeInTheDocument();
    unmount();
    render(<App service={new MockService({ rows: 100, analyzed: true, step: 1 })} platform={platform(['/x/site.mbcap'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    await screen.findByRole('grid', { name: 'Timeline' });
    act(() => menu?.({ action: 'export' }));
    expect(screen.queryByRole('dialog', { name: 'Export' })).toBeNull();
  });

  it('reports the smoke test: rows shown, or an empty timeline', async () => {
    for (const [rows, want] of [
      [50, [50, '']],
      [0, [0, 'the timeline is empty']],
    ] as const) {
      const reports: [number, string][] = [];
      const user = userEvent.setup();
      const p = { ...platform(['/x/site.pcapng']), smokeMode: async () => true, smokeDone: (n: number, why: string) => void reports.push([n, why]) };
      const { unmount } = render(<App service={new MockService({ rows, analyzed: true, step: 1 })} platform={p} />);
      await user.click(screen.getByRole('button', { name: /Open/ }));
      await waitFor(() => expect(reports).toEqual([want]));
      unmount();
    }
  });

  it('explains a disabled Export when the trace is not at its path', async () => {
    class Moved extends MockService {
      override async overview() {
        return { ...(await super.overview()), trace_missing: true };
      }
    }
    const user = userEvent.setup();
    render(<App service={new Moved({ rows: 100, analyzed: true, step: 1 })} platform={platform(['/x/site.pcapng'])} />);
    await user.click(screen.getByRole('button', { name: /Open/ }));
    const button = await screen.findByRole('button', { name: /^Export$/ });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('title', 'Exports read the trace, which is not at its path');
  });
});

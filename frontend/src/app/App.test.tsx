import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import type { Platform } from './platform/platform';
import { App } from './App';
import { MockService } from './service/mock';

function platform(paths: string[] = []): Platform {
  return {
    chooseTraces: async () => paths,
    chooseSave: async (name: string) => `/out/${name}`,
    onOpen: () => () => {},
    openExternal: () => {},
    persistTheme: () => {},
    loadTheme: () => 'dark' as const,
    copyText: async () => {},
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
});

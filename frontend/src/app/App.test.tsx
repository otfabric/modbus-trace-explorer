import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import type { Platform } from './platform/platform';
import { App } from './App';
import { MockService } from './service/mock';

function platform(paths: string[] = []): Platform {
  return {
    chooseTraces: async () => paths,
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
});

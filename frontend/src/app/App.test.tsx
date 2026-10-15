import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import type { Platform } from './platform/platform';
import { App } from './App';
import { MockService } from './service/mock';

function platform(paths: string[] = []): Platform & { opened: string[][] } {
  const p = {
    opened: [] as string[][],
    chooseTraces: async () => paths,
    onOpen: () => () => {},
    openExternal: () => {},
    persistTheme: () => {},
    loadTheme: () => 'dark' as const,
    copyText: async () => {},
  };
  return p;
}

describe('App', () => {
  it('shows the empty state with a drop target and opens with the dialog', async () => {
    render(<App service={new MockService(100, true)} platform={platform(['/x/site.pcapng'])} />);
    expect(screen.getByText(/Drop a pcap or pcapng here/)).toBeInTheDocument();
    expect(document.querySelector('[data-file-drop-target]')).not.toBeNull();
    await userEvent.click(screen.getByRole('button', { name: /Open/ }));
    expect(await screen.findByRole('status')).toHaveTextContent('/x/site.pcapng');
  });

  it('shows the file strip, the views and the timeline of an open trace', async () => {
    render(<App service={new MockService(5000)} platform={platform()} />);
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '5001'));
    expect(screen.getByText('site-2026-10-14.pcapng')).toBeInTheDocument();
    expect(within(screen.getByRole('navigation', { name: 'Views' })).getAllByRole('button')).toHaveLength(6);
    const rows = within(grid).getAllByRole('row');
    expect(rows.length).toBeGreaterThan(10);
    await userEvent.click(rows[3] as HTMLElement);
    expect(within(screen.getByRole('complementary', { name: 'Detail' })).getByText(/Row 3/)).toBeInTheDocument();
  });

  it('filters with a quick filter and moves with the keyboard', async () => {
    render(<App service={new MockService(5000)} platform={platform()} />);
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '5001'));
    await userEvent.click(screen.getByRole('button', { name: 'class:write' }));
    await waitFor(() => expect(Number(grid.getAttribute('aria-rowcount'))).toBeLessThan(500));
    grid.focus();
    await userEvent.keyboard('{End}');
    await waitFor(() => expect(grid.querySelector('[aria-selected=true]')).not.toBeNull());
    expect(grid.querySelector('[aria-selected=true]')?.getAttribute('aria-rowindex')).toBe(grid.getAttribute('aria-rowcount'));
  });
});

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { MockService } from '../service/mock';
import type { TimelinePage, TimelineQuery } from '../service/types';
import { ROW_HEIGHT, TimelineTable } from './TimelineTable';

describe('TimelineTable', () => {
  it('fetches again only outside the loaded rows, or on a refresh', async () => {
    const svc = new MockService({ rows: 2000, analyzed: true, step: 0 });
    const fetchPage = vi.fn((q: TimelineQuery, signal: AbortSignal): Promise<TimelinePage> => svc.timeline('mock', q, signal));
    const props = { fetchPage, filter: {}, order: 'time' as const, desc: false, timeMode: 'relative' as const, firstNs: 0, selectedSeq: null, onSelect: () => {} };
    const { rerender } = render(<TimelineTable {...props} />);
    const grid = await screen.findByRole('grid', { name: 'Timeline' });
    await waitFor(() => expect(grid).toHaveAttribute('aria-rowcount', '2001'));
    const calls = fetchPage.mock.calls.length;
    // A scroll by a few rows stays within the loaded window.
    for (const top of [0, 3 * ROW_HEIGHT]) {
      grid.scrollTop = top;
      fireEvent.scroll(grid);
    }
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchPage.mock.calls.length).toBe(calls);
    // A refresh fetches the same rows again.
    rerender(<TimelineTable {...props} refresh={1} />);
    await waitFor(() => expect(fetchPage.mock.calls.length).toBe(calls + 1));
  });
});

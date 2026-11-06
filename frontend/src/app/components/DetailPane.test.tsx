import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { MockService } from '../service/mock';
import type { Detail } from '../service/types';
import { DetailPane } from './DetailPane';

const service = new MockService({ rows: 3000, step: 0 });

function pane(detail: Detail, seq: number | null = detail.row.seq) {
  return <DetailPane detail={detail} seq={seq} error={null} loading={false} timeMode="relative" firstNs={0} copy={() => {}} />;
}

describe('DetailPane follows the selected row', () => {
  it('names the row it shows', async () => {
    const d = await service.detail('f', 3);
    render(pane(d));
    expect(screen.getByText(`row 3 · TID ${d.row.tid}`)).toBeInTheDocument();
  });

  it('marks the previous row as loading until the selected one arrives', async () => {
    const d = await service.detail('f', 3);
    const { container, rerender } = render(pane(d, 4));
    expect(container.firstChild).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByText(/loading…/)).toBeInTheDocument();
    rerender(pane(await service.detail('f', 4)));
    expect(container.firstChild).toHaveAttribute('aria-busy', 'false');
    expect(screen.queryByText(/loading…/)).toBeNull();
  });

  it('falls back to Summary when the new row has no such tab, and keeps the tab otherwise', async () => {
    const user = userEvent.setup();
    const txn = await service.detail('f', 3);
    const event = await service.detail('f', 1);
    expect(event.row.kind).not.toBe('txn');
    const { rerender } = render(pane(txn));
    await user.click(screen.getByRole('tab', { name: 'Request' }));
    rerender(pane(event));
    expect(screen.getByRole('tabpanel', { name: 'Summary' })).toHaveTextContent(event.text);
    expect(screen.getByRole('tab', { name: 'Summary' })).toHaveAttribute('aria-selected', 'true');
    rerender(pane(await service.detail('f', 4)));
    expect(screen.getByRole('tab', { name: 'Request' })).toHaveAttribute('aria-selected', 'true');
    await user.click(screen.getByRole('tab', { name: 'Notes' }));
    rerender(pane(event));
    expect(screen.getByRole('tabpanel', { name: 'Notes' })).toHaveTextContent('Nothing to note.');
  });

  it('drops the byte selection of the previous row', async () => {
    const user = userEvent.setup();
    const { rerender } = render(pane(await service.detail('f', 3)));
    await user.click(screen.getByRole('tab', { name: 'Raw' }));
    const raw = screen.getByRole('tabpanel', { name: 'Raw' });
    await user.click(within(raw).getByRole('button', { name: 'Request byte 7' }));
    expect(within(raw).getByText('1 selected')).toBeInTheDocument();
    rerender(pane(await service.detail('f', 4)));
    expect(screen.queryByText('1 selected')).toBeNull();
  });
});

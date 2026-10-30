import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import type { AddressInterval, CoverageMap } from '../service/types';
import { CoverageStrip } from './AddressesView';

const iv = (start: number, count: number, end = start): AddressInterval => ({ start, end, count, first_ns: 0, last_ns: 0 });
const map = (read: AddressInterval[], written: AddressInterval[]): CoverageMap => ({ server: 'd', unit: 1, space: 'holding', spaces: ['holding'], read, written });
// Ten blocks: more than get a slice of their own.
const many = Array.from({ length: 10 }, (_, i) => iv(i * 1000, 100 - i));

describe('coverage pies', () => {
  it('draws one pie per kind, and a click picks a block', async () => {
    const onPick = vi.fn();
    const user = userEvent.setup();
    const { container } = render(<CoverageStrip map={map([iv(10, 3), iv(5000, 1)], [iv(5000, 2)])} base={0} shape="pie" onPick={onPick} />);
    const read = screen.getByRole('img', { name: 'Address coverage, read' });
    const written = screen.getByRole('img', { name: 'Address coverage, written' });
    // Two wedges, and a whole circle for the only slice.
    expect(read.querySelectorAll('path')).toHaveLength(2);
    expect(written.querySelectorAll('circle')).toHaveLength(1);
    expect(read.querySelector('title')).toHaveTextContent('read 10: 3 times, 75.0%');
    expect(container).toHaveTextContent('25.0%');
    await user.click(read.querySelector('path') as Element);
    expect(onPick).toHaveBeenLastCalledWith(10);
    await user.click(screen.getAllByText('5000')[1] as HTMLElement);
    expect(onPick).toHaveBeenLastCalledWith(5000);
  });

  it('folds the rest into other blocks, which picks nothing', async () => {
    const onPick = vi.fn();
    const user = userEvent.setup();
    render(<CoverageStrip map={map(many, [])} base={0} shape="pie" onPick={onPick} />);
    expect(screen.queryByRole('img', { name: 'Address coverage, written' })).toBeNull();
    const pie = screen.getByRole('img', { name: 'Address coverage, read' });
    expect(pie.querySelectorAll('path')).toHaveLength(9);
    await user.click(screen.getByText('other blocks'));
    await user.click(pie.querySelectorAll('path')[8] as Element);
    expect(onPick).not.toHaveBeenCalled();
    expect(pie.querySelectorAll('path')[8]).toHaveClass('fill-muted');
    expect(pie.querySelectorAll('path')[0]).toHaveClass('fill-chart-1', 'cursor-pointer');
  });

  it('draws only the written pie when nothing was read, with 1-based labels', () => {
    render(<CoverageStrip map={map([], [iv(9, 1)])} base={1} shape="pie" onPick={() => {}} />);
    expect(screen.queryByRole('img', { name: 'Address coverage, read' })).toBeNull();
    expect(within(screen.getByRole('figure')).getByRole('img', { name: 'Address coverage, written' }).querySelector('title')).toHaveTextContent('written 10: 1 times, 100.0%');
  });

  it('keeps the bars and the table, and says when nothing was touched', async () => {
    const user = userEvent.setup();
    const { rerender } = render(<CoverageStrip map={map([iv(10, 3)], [])} base={0} shape="bars" onPick={() => {}} />);
    expect(screen.getByRole('img', { name: 'Address coverage' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Show table' }));
    expect(screen.getByRole('table')).toHaveTextContent('read');
    rerender(<CoverageStrip map={map([], [])} base={0} shape="pie" onPick={() => {}} />);
    expect(screen.getByText(/Nothing of this space/)).toBeInTheDocument();
  });
});

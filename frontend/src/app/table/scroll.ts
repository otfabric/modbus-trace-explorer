// The arithmetic of the scaled scrollbar (ARCHITECTURE.md section 11).
//
// Browsers cap the height of an element (on the order of 10^7 px, less in
// some engines), so a list of ten million 24 px rows cannot be one tall
// element. The table therefore keeps a spacer of at most MAX_SPACER pixels
// and maps the scroll position to a row rank by proportion. Below the cap
// the mapping is exact (one pixel is one pixel); above it, dragging the
// thumb jumps through ranks, while the wheel and the keyboard move by rows
// relative to the current rank.

/** Tallest spacer the table creates: well below every engine's cap. */
export const MAX_SPACER = 4_000_000;

export interface Geometry {
  /** Rows in the (filtered) set. */
  total: number;
  /** Height of one row, px. */
  rowHeight: number;
  /** Height of the visible area, px. */
  viewport: number;
}

/** Rows that fit in the viewport (at least one). */
export function visibleRows(g: Geometry): number {
  return Math.max(1, Math.floor(g.viewport / g.rowHeight));
}

/** The highest rank the first visible row can have. */
export function maxTop(g: Geometry): number {
  return Math.max(0, g.total - visibleRows(g));
}

/** Height of the spacer that gives the scrollbar its range. */
export function spacerHeight(g: Geometry): number {
  return Math.min(g.total * g.rowHeight, MAX_SPACER);
}

/** Whether the scrollbar is scaled (the rows do not fit in MAX_SPACER). */
export function scaled(g: Geometry): boolean {
  return g.total * g.rowHeight > MAX_SPACER;
}

/** The scroll range in pixels. */
function range(g: Geometry): number {
  return Math.max(0, spacerHeight(g) - g.viewport);
}

/** The rank of the first visible row for a scroll position. */
export function rankAt(g: Geometry, scrollTop: number): number {
  const top = maxTop(g);
  if (top === 0) return 0;
  if (!scaled(g)) return Math.min(top, Math.max(0, Math.floor(scrollTop / g.rowHeight)));
  const r = range(g);
  if (r <= 0) return 0;
  return Math.min(top, Math.max(0, Math.round((scrollTop / r) * top)));
}

/** The scroll position that shows rank as the first visible row. */
export function scrollFor(g: Geometry, rank: number): number {
  const top = maxTop(g);
  const k = Math.min(top, Math.max(0, rank));
  if (!scaled(g)) return k * g.rowHeight;
  if (top === 0) return 0;
  return (k / top) * range(g);
}

/**
 * The first visible rank after the user asks to see row `rank`: unchanged
 * when it is already on screen, otherwise the nearest position that shows
 * it.
 */
export function reveal(g: Geometry, first: number, rank: number): number {
  const v = visibleRows(g);
  if (rank < first) return Math.max(0, rank);
  if (rank >= first + v) return Math.min(maxTop(g), rank - v + 1);
  return first;
}

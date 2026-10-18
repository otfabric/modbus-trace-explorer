import { useCallback, useState, type KeyboardEvent, type PointerEvent } from 'react';

const KEY = 'modbus-trace-explorer.detail-width';
export const DETAIL_MIN = 280;
export const DETAIL_MAX = 800;
export const DETAIL_DEFAULT = 384;

const clamp = (w: number) => Math.round(Math.min(DETAIL_MAX, Math.max(DETAIL_MIN, w)));

/** The detail pane's width, remembered in the window's storage (a per-viewer convenience). */
export function useDetailWidth(): [number, (w: number) => void] {
  const [width, setWidth] = useState(() => {
    try {
      const v = Number(localStorage.getItem(KEY));
      return Number.isFinite(v) && v > 0 ? clamp(v) : DETAIL_DEFAULT;
    } catch {
      return DETAIL_DEFAULT;
    }
  });
  const set = useCallback((w: number) => {
    const c = clamp(w);
    setWidth(c);
    try {
      localStorage.setItem(KEY, String(c));
    } catch {
      /* not remembered */
    }
  }, []);
  return [width, set];
}

/**
 * The handle on the detail pane's left edge (UX.md section 1): drag it, or
 * focus it and use the arrow keys (Shift for bigger steps, Home and End for
 * the limits).
 */
export function Resizer({ width, onWidth }: { width: number; onWidth: (w: number) => void }) {
  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const startX = e.clientX;
    const start = width;
    const el = e.currentTarget;
    el.setPointerCapture(e.pointerId);
    const move = (ev: globalThis.PointerEvent) => onWidth(start + (startX - ev.clientX));
    const up = () => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
  };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = e.shiftKey ? 64 : 16;
    const next: Record<string, number> = { ArrowLeft: width + step, ArrowRight: width - step, Home: DETAIL_MAX, End: DETAIL_MIN };
    const w = next[e.key];
    if (w === undefined) return;
    e.preventDefault();
    onWidth(w);
  };
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the detail pane"
      aria-valuemin={DETAIL_MIN}
      aria-valuemax={DETAIL_MAX}
      aria-valuenow={width}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onDoubleClick={() => onWidth(DETAIL_DEFAULT)}
      title="Drag to resize; double-click for the default width"
      className="w-1.5 shrink-0 cursor-col-resize bg-line/0 outline-none hover:bg-accent/40 focus-visible:bg-accent"
    />
  );
}

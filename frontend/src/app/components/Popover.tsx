import { useEffect, useRef, type ReactNode } from 'react';

/** A panel under its button; Escape or a click outside closes it. */
export function Popover({ label, onClose, children, align = 'left' }: { label: string; onClose: () => void; children: ReactNode; align?: 'left' | 'right' }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    // Escape closes it wherever the focus is (focus often stays on the button).
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [onClose]);
  return (
    <div
      ref={ref}
      role="dialog"
      aria-label={label}
      className={`absolute top-full z-30 mt-1 w-[30rem] max-w-[calc(100vw-2rem)] rounded border border-control bg-surface p-4 text-[13px] shadow-lg ${align === 'right' ? 'right-0' : 'left-0'}`}
    >
      {children}
    </div>
  );
}

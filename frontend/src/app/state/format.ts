// Display formats (UX.md section 10).

export type TimeMode = 'local' | 'utc' | 'relative';

const pad = (n: number, w = 2) => String(n).padStart(w, '0');

/** A trace time (ns since 1970) at microseconds, in the chosen mode. */
export function formatTime(ns: number, mode: TimeMode, firstNs = 0): string {
  if (mode === 'relative') {
    const d = Math.max(0, ns - firstNs);
    const us = Math.floor(d / 1000);
    const s = Math.floor(us / 1e6);
    return `+${pad(Math.floor(s / 3600))}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}.${pad(us % 1e6, 6)}`;
  }
  const ms = Math.floor(ns / 1e6);
  const us = Math.floor(ns / 1000) % 1e6;
  const d = new Date(ms);
  const [h, m, s] = mode === 'utc' ? [d.getUTCHours(), d.getUTCMinutes(), d.getUTCSeconds()] : [d.getHours(), d.getMinutes(), d.getSeconds()];
  return `${pad(h)}:${pad(m)}:${pad(s)}.${pad(us, 6)}`;
}

/** A latency: ms with one decimal, µs below 1 ms. */
export function formatLatency(ns: number | undefined): string {
  if (ns === undefined) return '';
  if (ns < 1e6) return `${Math.round(ns / 1000)} µs`;
  return `${(ns / 1e6).toFixed(1)} ms`;
}

/** Thousands separators, the same in every locale the tests run in. */
export function formatCount(n: number): string {
  return n.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',');
}

/** Bytes in IEC units. */
export function formatBytes(n: number): string {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let v = n;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  return u === 0 ? `${v} B` : `${v.toFixed(1)} ${units[u]}`;
}

/** A duration in words: "2 h 37 min", "14 s". */
export function formatDuration(ns: number): string {
  const s = Math.round(ns / 1e9);
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min ${s % 60} s`;
  return `${Math.floor(m / 60)} h ${m % 60} min`;
}

const FUNCTION_NAMES: Record<number, string> = {
  1: 'Read Coils',
  2: 'Read Discrete',
  3: 'Read Holding',
  4: 'Read Input',
  5: 'Write Coil',
  6: 'Write Register',
  7: 'Exception Status',
  8: 'Diagnostics',
  11: 'Comm Counter',
  12: 'Comm Log',
  15: 'Write Coils',
  16: 'Write Registers',
  17: 'Report Server ID',
  20: 'Read File',
  21: 'Write File',
  22: 'Mask Write',
  23: 'Read/Write',
  24: 'Read FIFO',
  43: 'Device ID',
};

/** "03 Read Holding"; an unknown code as hex. */
export function formatFunction(fc: number | undefined): string {
  if (fc === undefined) return '';
  const name = FUNCTION_NAMES[fc];
  return name ? `${pad(fc)} ${name}` : `0x${fc.toString(16).toUpperCase().padStart(2, '0')} user-defined`;
}

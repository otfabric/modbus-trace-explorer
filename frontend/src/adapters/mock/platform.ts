// The browser stand-ins for the desktop's platform, for `npm run dev`
// against the mock Service. Never shipped.

import type { Platform } from '../../app/platform/platform';
import { loadTheme, persistTheme } from '../../app/platform/themeStorage';

export class MockPlatform implements Platform {
  persistTheme = persistTheme;
  loadTheme = loadTheme;

  async chooseTraces(): Promise<string[]> {
    return ['/traces/site-2026-10-14.pcapng'];
  }

  async chooseSave(name: string): Promise<string | null> {
    return `/exports/${name}`;
  }

  onMenu(): () => void {
    return () => {};
  }

  onOpen(): () => void {
    return () => {};
  }

  openExternal(url: string): void {
    window.open(url, '_blank', 'noopener');
  }

  async smokeMode(): Promise<boolean> {
    return new URLSearchParams(window.location.search).has('smoke');
  }

  smokeDone(rows: number, problem: string): void {
    console.info(`smoke test: ${problem || `${rows} rows`}`);
  }

  copyText(text: string): Promise<void> {
    return navigator.clipboard.writeText(text);
  }
}

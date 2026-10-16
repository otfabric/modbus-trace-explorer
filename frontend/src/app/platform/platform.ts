export type Theme = 'dark' | 'light';

/** The kinds of file the Save dialog offers (its file-type filter). */
export type SaveKind = 'mbcap' | 'jsonl' | 'csv' | 'pcapng' | 'json';

/** An item of the native menu (gui.MenuAction); the browser build has no menu. */
export interface MenuAction {
  action: 'open' | 'recent' | 'clear-recent' | 'close' | 'export' | 'settings' | 'theme' | 'time' | 'detail' | 'about';
  paths?: string[];
}

/** What the user interface needs from its host besides the Service. */
export interface Platform {
  /** Opens the native Open dialog for trace files; resolves to [] when cancelled. */
  chooseTraces(): Promise<string[]>;
  /** Opens the native Save dialog proposing name; resolves to null when cancelled. */
  chooseSave(name: string, kind: SaveKind): Promise<string | null>;
  /**
   * Calls back with trace paths the operating system hands over (a drop on
   * the window, "open with", the command line). Returns the unsubscribe.
   */
  onOpen(handler: (paths: string[]) => void): () => void;
  openExternal(url: string): void;
  /** Calls back with the native menu's actions. Returns the unsubscribe. */
  onMenu(handler: (action: MenuAction) => void): () => void;
  persistTheme(theme: Theme): void;
  /** The theme the user chose earlier, or null on first run. */
  loadTheme(): Theme | null;
  copyText(text: string): Promise<void>;
  /** The smoke test (A15): whether this run is one, and its report. */
  smokeMode(): Promise<boolean>;
  smokeDone(rows: number, problem: string): void;
}

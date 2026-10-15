export type Theme = 'dark' | 'light';

/** What the user interface needs from its host besides the Service. */
export interface Platform {
  /** Opens the native Open dialog for trace files; resolves to [] when cancelled. */
  chooseTraces(): Promise<string[]>;
  /**
   * Calls back with trace paths the operating system hands over (a drop on
   * the window, "open with", the command line). Returns the unsubscribe.
   */
  onOpen(handler: (paths: string[]) => void): () => void;
  openExternal(url: string): void;
  persistTheme(theme: Theme): void;
  /** The theme the user chose earlier, or null on first run. */
  loadTheme(): Theme | null;
  copyText(text: string): Promise<void>;
}

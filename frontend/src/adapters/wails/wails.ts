// The only file that touches the Wails runtime. It calls the Go service App
// in internal/gui; method names here must match its exported methods.

import { Call, Clipboard, Events } from '@wailsio/runtime';
import type { Platform } from '../../app/platform/platform';
import { loadTheme, persistTheme } from '../../app/platform/themeStorage';
import { ServiceError, toServiceError, type ErrorCode, type Service } from '../../app/service/service';
import type { About, Overview, TimelinePage, TimelineQuery } from '../../app/service/types';

const BINDING = 'github.com/otfabric/modbus-trace-explorer/internal/gui.App';
const OPEN_EVENT = 'explorer:open';

interface ErrorBody {
  code: ErrorCode;
  message: string;
}

const isErrorBody = (value: unknown): value is ErrorBody =>
  typeof value === 'object' && value !== null && 'code' in value && 'message' in value;

/** A Go service.Error arrives as the rejection's cause or as JSON in its message. */
export function fromGo(err: unknown): ServiceError {
  const candidates: unknown[] = [];
  if (err instanceof Error) {
    candidates.push(err.cause);
    try {
      candidates.push(JSON.parse(err.message));
    } catch {
      /* a plain message */
    }
  }
  const body = candidates.find(isErrorBody);
  return body ? new ServiceError(body.code, body.message) : toServiceError(err);
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  try {
    return (await Call.ByName(`${BINDING}.${method}`, ...args)) as T;
  } catch (err) {
    throw fromGo(err);
  }
}

export class WailsService implements Service {
  about = () => call<About>('About');
  // Opening a trace arrives with M1: until then nothing is open.
  overview = async (): Promise<Overview | null> => null;
  timeline = async (q: TimelineQuery): Promise<TimelinePage> => ({ offset: q.offset, rows: [], total: 0 });
}

export class WailsPlatform implements Platform {
  persistTheme = persistTheme;
  loadTheme = loadTheme;

  // The Go side shows the native dialog; the paths come from it, not from the page.
  chooseTraces = () => call<string[]>('ChooseTraces');

  onOpen(handler: (paths: string[]) => void): () => void {
    // Paths handed over before the window was ready (command line, "open with").
    void call<string[]>('PendingFiles').then((paths) => {
      if (paths.length > 0) handler(paths);
    });
    return Events.On(OPEN_EVENT, (event) => handler(event.data as string[]));
  }

  openExternal(url: string): void {
    void call<void>('OpenExternal', url);
  }

  copyText(text: string): Promise<void> {
    return Clipboard.SetText(text);
  }
}

import type { About, Overview, TimelinePage, TimelineQuery } from './types';

export type ErrorCode = 'invalid_argument' | 'not_found' | 'canceled' | 'internal';

/** The error of a Service call, as the Go side reports it (service.Error). */
export class ServiceError extends Error {
  readonly code: ErrorCode;
  constructor(code: ErrorCode, message: string) {
    super(message);
    this.name = 'ServiceError';
    this.code = code;
  }
}

export function toServiceError(err: unknown): ServiceError {
  if (err instanceof ServiceError) {
    return err;
  }
  return new ServiceError('internal', err instanceof Error ? err.message : String(err));
}

/**
 * What the user interface can ask of the application (ARCHITECTURE.md
 * section 2). The Wails adapter calls the Go engine; the mock answers from a
 * generated dataset. M0 covers the shell and the timeline window; opening,
 * analysis, detail, the views and the exports follow with M1 to M3.
 */
export interface Service {
  about(): Promise<About>;
  /** The open trace's facts, or null when none is open. */
  overview(signal?: AbortSignal): Promise<Overview | null>;
  /** A window of the filtered timeline; abort stale requests with signal. */
  timeline(query: TimelineQuery, signal?: AbortSignal): Promise<TimelinePage>;
}

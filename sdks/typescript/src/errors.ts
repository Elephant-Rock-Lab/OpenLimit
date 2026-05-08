import type { ErrorResponse } from './types';

/**
 * Error thrown when the API returns a non-2xx response.
 */
export class APIError extends Error {
  readonly status: number;
  readonly type: string;
  readonly code?: string;
  readonly requestId?: string;

  constructor(status: number, body: ErrorResponse) {
    super(body.error?.message ?? `API error ${status}`);
    this.name = 'APIError';
    this.status = status;
    this.type = body.error?.type ?? 'unknown';
    this.code = body.error?.code;
    this.requestId = body.error?.request_id;
  }
}

/**
 * Error thrown when a request times out.
 */
export class TimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Request timed out after ${timeoutMs}ms`);
    this.name = 'TimeoutError';
  }
}

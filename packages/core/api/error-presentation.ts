import { ApiError } from './client';

export type ApiFailureKind =
  | 'unreachable'
  /** The server answered, but with no sentence of its own. */
  | 'server_error'
  /** Not a request failure at all — a thrown non-Error, a bug in the caller. */
  | 'unknown';

export interface ApiFailure {
  kind: ApiFailureKind;
  message: string | null;
  status: number | null;
  detail: string;
}

const TRANSPORT_FAILURE =
  /failed to fetch|network ?error|load failed|networkerror|err_[a-z_]+|econnrefused|enotfound|etimedout|socket hang up/i;

export function isTransportFailure(err: unknown): boolean {
  if (err instanceof ApiError) return false;
  if (err instanceof TypeError) return true;
  const text = err instanceof Error ? err.message : String(err ?? '');
  return TRANSPORT_FAILURE.test(text);
}

export function describeApiFailure(err: unknown): ApiFailure {
  if (err instanceof ApiError) {
    return {
      kind: 'server_error',
      message: err.serverMessage ?? null,
      status: err.status,
      detail: err.message,
    };
  }

  const detail = err instanceof Error ? err.message : String(err ?? '').trim() || 'unknown';

  if (isTransportFailure(err)) {
    return { kind: 'unreachable', message: null, status: null, detail };
  }

  return { kind: 'unknown', message: null, status: null, detail };
}

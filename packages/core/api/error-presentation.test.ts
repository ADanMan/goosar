import { describe, expect, it } from 'vitest';
import { ApiError } from './client';
import { describeApiFailure, isTransportFailure } from './error-presentation';

describe('describeApiFailure', () => {
  it('passes through a sentence the server actually wrote', () => {
    const err = new ApiError(
      'user registration is disabled on this self-hosted instance',
      403,
      'Forbidden',
      { error: 'user registration is disabled on this self-hosted instance' },
      'user registration is disabled on this self-hosted instance',
    );

    expect(describeApiFailure(err)).toEqual({
      kind: 'server_error',
      message: 'user registration is disabled on this self-hosted instance',
      status: 403,
      detail: 'user registration is disabled on this self-hosted instance',
    });
  });

  it('refuses to present the synthesized status line as a sentence', () => {
    const err = new ApiError(
      'API error: 500 Internal Server Error',
      500,
      'Internal Server Error',
      undefined,
    );

    const failure = describeApiFailure(err);
    expect(failure.message).toBeNull();
    expect(failure.kind).toBe('server_error');
    expect(failure.detail).toBe('API error: 500 Internal Server Error');
  });

  it('recognises a host that never answered', () => {
    const failure = describeApiFailure(new TypeError('Failed to fetch'));

    expect(failure.kind).toBe('unreachable');
    expect(failure.message).toBeNull();
    expect(failure.detail).toBe('Failed to fetch');
  });

  it('recognises transport wordings that reached us as plain Errors', () => {
    expect(describeApiFailure(new Error('Load failed')).kind).toBe('unreachable');
    expect(describeApiFailure(new Error('request to https://x failed, ECONNREFUSED')).kind).toBe(
      'unreachable',
    );
  });

  it('never returns an empty detail, even for a thrown non-Error', () => {
    expect(describeApiFailure(undefined).detail).toBe('unknown');
    expect(describeApiFailure('').detail).toBe('unknown');
    expect(describeApiFailure({ nope: true }).detail).toBe('[object Object]');
  });
});

describe('isTransportFailure', () => {
  it('is false for anything the server answered', () => {
    expect(isTransportFailure(new ApiError('API error: 502 Bad Gateway', 502, 'Bad Gateway'))).toBe(
      false,
    );
  });

  it('does not mistake ordinary application errors for transport failures', () => {
    expect(isTransportFailure(new Error('token missing'))).toBe(false);
  });
});

import { describe, expect, it } from 'vitest';

import { classifyAuthProbe, isAuthStatusError } from './daemon-auth-probe';

describe('classifyAuthProbe', () => {
  it('treats a 401 as expired login', () => {
    expect(classifyAuthProbe({ status: 401 })).toBe('auth_expired');
  });

  it('treats a missing token as expired login', () => {
    expect(classifyAuthProbe({ noToken: true })).toBe('auth_expired');
  });

  it('treats a 2xx as a valid token (failure is non-auth)', () => {
    expect(classifyAuthProbe({ status: 200 })).toBe('ok');
    expect(classifyAuthProbe({ status: 204 })).toBe('ok');
  });

  it('does NOT classify a network error as expired login', () => {
    expect(classifyAuthProbe({ networkError: true })).toBe('unknown');
  });

  it('leaves 5xx and other statuses inconclusive', () => {
    expect(classifyAuthProbe({ status: 500 })).toBe('unknown');
    expect(classifyAuthProbe({ status: 503 })).toBe('unknown');
    expect(classifyAuthProbe({ status: 403 })).toBe('unknown');
  });

  it('is inconclusive when nothing is known', () => {
    expect(classifyAuthProbe({})).toBe('unknown');
  });
});

describe('isAuthStatusError', () => {
  it('is true only for a 401-tagged error (session token is dead)', () => {
    expect(isAuthStatusError(Object.assign(new Error('x'), { status: 401 }))).toBe(true);
  });

  it('is false for transient / non-401 failures', () => {
    expect(isAuthStatusError(Object.assign(new Error('x'), { status: 503 }))).toBe(false);
    expect(isAuthStatusError(new Error('network down'))).toBe(false);
    expect(isAuthStatusError(new Error('EACCES: write failed'))).toBe(false);
    expect(isAuthStatusError(undefined)).toBe(false);
    expect(isAuthStatusError(null)).toBe(false);
    expect(isAuthStatusError('401')).toBe(false);
  });
});

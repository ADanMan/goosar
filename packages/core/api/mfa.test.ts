import { describe, expect, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  DEFAULT_SESSION_POLICY,
  EMPTY_LOGIN_RESULT,
  EMPTY_MFA_CONFIRMATION,
  EMPTY_MFA_STATUS,
  EMPTY_SESSION_LIST,
  LoginResultSchema,
  MFAConfirmSchema,
  MFAStatusSchema,
  SessionListSchema,
  readSessionPolicy,
  writeSessionPolicy,
} from './mfa';

const opts = { endpoint: 'test' };

describe('login result', () => {
  test('parses the MFA challenge, which carries no user', () => {
    const parsed = parseWithFallback(
      { mfa_required: true, mfa_token: 'ticket' },
      LoginResultSchema,
      EMPTY_LOGIN_RESULT,
      opts,
    );
    expect(parsed.mfa_required).toBe(true);
    expect(parsed.mfa_token).toBe('ticket');
    expect(parsed.token).toBe('');
  });

  test('parses a finished login and its enrollment flag', () => {
    const parsed = parseWithFallback(
      {
        token: 'jwt',
        user: { id: 'u1', name: 'N', email: 'n@example.com' },
        mfa_enrollment_required: true,
      },
      LoginResultSchema,
      EMPTY_LOGIN_RESULT,
      opts,
    );
    expect(parsed.token).toBe('jwt');
    expect(parsed.user?.id).toBe('u1');
    expect(parsed.mfa_enrollment_required).toBe(true);
  });

  test('a malformed response invents no authentication state', () => {
    for (const malformed of [null, 'nope', 42, { token: 7 }, { user: 3 }]) {
      const parsed = parseWithFallback(malformed, LoginResultSchema, EMPTY_LOGIN_RESULT, opts);
      expect(parsed.token).toBe('');
      expect(parsed.mfa_required).toBe(false);
      expect(parsed.mfa_token).toBe('');
    }
  });

  test('keeps parsing when the backend grows a field', () => {
    const parsed = parseWithFallback(
      { token: 'jwt', user: { id: 'u1', name: 'N', email: 'n@e.com' }, webauthn: true },
      LoginResultSchema,
      EMPTY_LOGIN_RESULT,
      opts,
    );
    expect(parsed.token).toBe('jwt');
  });
});

describe('MFA status', () => {
  test('parses a live enrollment', () => {
    const parsed = parseWithFallback(
      {
        enabled: true,
        pending_enrollment: false,
        enabled_at: '2026-01-01T00:00:00Z',
        recovery_codes_remaining: 7,
        required: true,
        available: true,
      },
      MFAStatusSchema,
      EMPTY_MFA_STATUS,
      opts,
    );
    expect(parsed.enabled).toBe(true);
    expect(parsed.recovery_codes_remaining).toBe(7);
  });

  test('a malformed status never claims a factor is protecting the account', () => {
    for (const malformed of [null, [], { enabled: 'yes' }]) {
      const parsed = parseWithFallback(malformed, MFAStatusSchema, EMPTY_MFA_STATUS, opts);
      expect(parsed.enabled).toBe(false);
      expect(parsed.available).toBe(false);
    }
  });
});

describe('recovery codes', () => {
  test('parses the one-time list', () => {
    const parsed = parseWithFallback(
      { enabled: true, recovery_codes: ['AAAA', 'BBBB'] },
      MFAConfirmSchema,
      EMPTY_MFA_CONFIRMATION,
      opts,
    );
    expect(parsed.recovery_codes).toEqual(['AAAA', 'BBBB']);
  });

  test('a malformed confirmation yields no codes to show', () => {
    const parsed = parseWithFallback(
      { enabled: true, recovery_codes: 'AAAA' },
      MFAConfirmSchema,
      EMPTY_MFA_CONFIRMATION,
      opts,
    );
    expect(parsed.recovery_codes).toEqual([]);
    expect(parsed.enabled).toBe(false);
  });
});

describe('session list', () => {
  test('parses rows and defaults the missing fields', () => {
    const parsed = parseWithFallback(
      [{ id: 's1', current: true }],
      SessionListSchema,
      EMPTY_SESSION_LIST,
      opts,
    );
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.user_agent).toBe('');
    expect(parsed[0]?.current).toBe(true);
  });

  test('a malformed list renders empty rather than throwing', () => {
    for (const malformed of [null, { sessions: [] }, 'nope']) {
      expect(parseWithFallback(malformed, SessionListSchema, EMPTY_SESSION_LIST, opts)).toEqual([]);
    }
  });
});

describe('session policy block', () => {
  test('returns the documented defaults for an absent block', () => {
    expect(readSessionPolicy({})).toEqual(DEFAULT_SESSION_POLICY);
    expect(readSessionPolicy(null)).toEqual(DEFAULT_SESSION_POLICY);
    expect(readSessionPolicy('nope')).toEqual(DEFAULT_SESSION_POLICY);
  });

  test('a partial block keeps the other knobs at their defaults', () => {
    const got = readSessionPolicy({ session: { idle_timeout_hours: 4 } });
    expect(got.idle_timeout_hours).toBe(4);
    expect(got.absolute_lifetime_days).toBe(DEFAULT_SESSION_POLICY.absolute_lifetime_days);
    expect(got.require_mfa).toBe('none');
  });

  test('an unknown require_mfa value falls back instead of being rendered', () => {
    expect(readSessionPolicy({ session: { require_mfa: 'sometimes' } }).require_mfa).toBe('none');
  });

  test('writing the block leaves the rest of the policy document intact', () => {
    const merged = writeSessionPolicy(
      { llm: { model: 'gpt-x', locked: true }, mcp: { '*': { enabled: false } } },
      { ...DEFAULT_SESSION_POLICY, idle_timeout_hours: 6 },
    );
    expect(merged.llm).toEqual({ model: 'gpt-x', locked: true });
    expect(merged.mcp).toEqual({ '*': { enabled: false } });
    expect(merged.session).toEqual({
      ...DEFAULT_SESSION_POLICY,
      idle_timeout_hours: 6,
    });
  });
});

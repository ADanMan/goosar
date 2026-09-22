import { describe, expect, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  DeploymentUserSchema,
  EMPTY_DEPLOYMENT_USER,
  type DeploymentUser,
} from './deployment-user';

function parseUser(data: unknown): DeploymentUser {
  return parseWithFallback(data, DeploymentUserSchema, EMPTY_DEPLOYMENT_USER, {
    endpoint: 'POST /api/deployment/users/{userId}/deactivate',
  });
}

describe('DeploymentUserSchema', () => {
  test('parses a deactivation result', () => {
    const parsed = parseUser({
      user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
      email: 'leaver@corp.example',
      name: 'Leaver',
      deactivated: true,
      deactivated_at: '2026-09-08T09:00:00Z',
      token_version: 3,
      revoked_tokens: 2,
      closed_connections: 1,
    });
    expect(parsed.deactivated).toBe(true);
    expect(parsed.revoked_tokens).toBe(2);
    expect(parsed.closed_connections).toBe(1);
    expect(parsed.deactivated_at).toBe('2026-09-08T09:00:00Z');
  });

  test('treats a null deactivated_at as an active account', () => {
    const parsed = parseUser({
      user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
      email: 'back@corp.example',
      name: 'Back',
      deactivated: false,
      deactivated_at: null,
      token_version: 4,
      revoked_tokens: 0,
      closed_connections: 0,
    });
    expect(parsed.deactivated).toBe(false);
    expect(parsed.deactivated_at).toBeNull();
  });

  test('defaults the counters when the backend omits them', () => {
    const parsed = parseUser({
      user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
      email: 'old@corp.example',
      name: 'Old',
      deactivated: true,
    });
    expect(parsed.revoked_tokens).toBe(0);
    expect(parsed.closed_connections).toBe(0);
    expect(parsed.token_version).toBe(0);
  });

  test('falls back on a malformed response', () => {
    expect(parseUser({ deactivated: 'yes' })).toEqual(EMPTY_DEPLOYMENT_USER);
    expect(parseUser('deactivated')).toEqual(EMPTY_DEPLOYMENT_USER);
    expect(parseUser(null)).toEqual(EMPTY_DEPLOYMENT_USER);
  });

  test('the fallback never claims an account was deactivated', () => {
    expect(EMPTY_DEPLOYMENT_USER.deactivated === true).toBe(false);
  });
});

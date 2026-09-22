import { describe, it, expect } from 'vitest';
import { ApiError } from '@goosar/core/api';
import { describeServerFailure, serverErrorMessage } from './server-error';
import enCommon from '../locales/en/common.json';
import ruCommon from '../locales/ru/common.json';

type Bundle = Record<string, unknown>;
function fakeT(bundle: Bundle) {
  return ((selector: (b: never) => string) => selector(bundle as never)) as never;
}

const enT = fakeT(enCommon);
const ruT = fakeT(ruCommon);

function apiError(status: number, body: unknown, message: string): ApiError {
  const serverMessage =
    body !== null &&
    typeof body === 'object' &&
    typeof (body as { error?: unknown }).error === 'string'
      ? (body as { error: string }).error
      : undefined;
  return new ApiError(message, status, '', body, serverMessage);
}

describe('serverErrorMessage', () => {
  it('localizes every code the server can send', () => {
    const codes = [
      'email_not_configured',
      'signup_disabled',
      'email_domain_not_allowed',
      'code_rate_limited',
      'invalid_or_expired_code',
      'account_deactivated',
      'invitation_not_found',
      'invitation_not_yours',
      'invitation_expired',
      'invitation_not_pending',
      'invitation_already_pending',
      'already_member',
      'already_member_self',
      'workspace_creation_disabled',
      'workspace_slug_taken',
      'workspace_slug_reserved',
      'workspace_slug_invalid',
      'insufficient_permissions',
    ];
    for (const code of codes) {
      expect(serverErrorMessage(enT, code), code).toBeTruthy();
      const ru = serverErrorMessage(ruT, code);
      expect(ru, code).toBeTruthy();
      expect(ru, code).not.toBe(serverErrorMessage(enT, code));
    }
  });

  it('returns null for a code this build does not know', () => {
    expect(serverErrorMessage(enT, 'some_future_refusal')).toBeNull();
    expect(serverErrorMessage(enT, undefined)).toBeNull();
  });
});

describe('describeServerFailure', () => {
  it('prefers the localized copy and files the server sentence under details', () => {
    const err = apiError(
      403,
      {
        error: 'email address or domain not allowed on this instance',
        code: 'email_domain_not_allowed',
      },
      'email address or domain not allowed on this instance',
    );

    const shown = describeServerFailure(ruT, err, 'fallback');

    expect(shown.text).toBe(serverErrorMessage(ruT, 'email_domain_not_allowed'));
    expect(shown.detail).toBe('email address or domain not allowed on this instance');
  });

  it('tells the invitee about themselves, not about a third person', () => {
    expect(serverErrorMessage(ruT, 'already_member_self')).not.toBe(
      serverErrorMessage(ruT, 'already_member'),
    );
    expect(serverErrorMessage(ruT, 'already_member_self')).toMatch(/^Вы /);
  });

  it('keeps the server sentence when the refusal carries no code', () => {
    const err = apiError(
      409,
      { error: 'something specific happened' },
      'something specific happened',
    );

    const shown = describeServerFailure(ruT, err, 'fallback');

    expect(shown.text).toBe('something specific happened');
    expect(shown.detail).toBeUndefined();
  });

  it("falls back to the caller's copy when the server wrote no sentence", () => {
    const err = apiError(500, null, 'API error: 500 Internal Server Error');

    const shown = describeServerFailure(ruT, err, 'не получилось');

    expect(shown.text).toBe('не получилось');
    expect(shown.detail).toBe('API error: 500 Internal Server Error');
  });

  it('survives a malformed envelope', () => {
    const err = apiError(400, { code: { slug: 'nope' } }, 'API error: 400 Bad Request');

    const shown = describeServerFailure(ruT, err, 'не получилось');

    expect(shown.text).toBe('не получилось');
    expect(shown.detail).toBe('API error: 400 Bad Request');
  });
});

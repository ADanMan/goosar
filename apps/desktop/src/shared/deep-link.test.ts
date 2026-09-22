import { describe, expect, it } from 'vitest';
import { parseDeepLink } from './deep-link';

describe('parseDeepLink', () => {
  it('parses the historical auth callback with a session token', () => {
    expect(parseDeepLink('goosar://auth/callback?token=jwt-abc')).toEqual({
      kind: 'auth-token',
      token: 'jwt-abc',
    });
  });

  it('parses the emailed login link into a link-token action', () => {
    expect(parseDeepLink('goosar://auth/callback?link_token=one-time-tok')).toEqual({
      kind: 'auth-link-token',
      linkToken: 'one-time-tok',
    });
  });

  it('decodes url-escaped link tokens', () => {
    expect(parseDeepLink('goosar://auth/callback?link_token=a%2Bb')).toEqual({
      kind: 'auth-link-token',
      linkToken: 'a+b',
    });
  });

  it('prefers a ready session token when both parameters are present', () => {
    expect(parseDeepLink('goosar://auth/callback?token=jwt&link_token=lt')).toEqual({
      kind: 'auth-token',
      token: 'jwt',
    });
  });

  it('parses a pending second-factor ticket', () => {
    expect(parseDeepLink('goosar://auth/callback?mfa_token=ticket-1')).toEqual({
      kind: 'auth-mfa-token',
      mfaToken: 'ticket-1',
    });
  });

  it('prefers a session token over a pending ticket', () => {
    expect(parseDeepLink('goosar://auth/callback?token=jwt&mfa_token=ticket-1')).toEqual({
      kind: 'auth-token',
      token: 'jwt',
    });
  });

  it('parses invite links', () => {
    expect(parseDeepLink('goosar://invite/inv-1')).toEqual({
      kind: 'invite',
      invitationId: 'inv-1',
    });
  });

  it('returns null for other protocols, hosts, and malformed URLs', () => {
    expect(parseDeepLink('https://auth/callback?token=jwt')).toBeNull();
    expect(parseDeepLink('goosar://other/callback?token=jwt')).toBeNull();
    expect(parseDeepLink('goosar://auth/callback')).toBeNull();
  });

  it('parses a corporate sign-in refusal', () => {
    expect(parseDeepLink('goosar://auth/callback?error=oidc_provider_unavailable')).toEqual({
      kind: 'auth-error',
      code: 'oidc_provider_unavailable',
    });
  });

  it('prefers a session token over an error on the same link', () => {
    expect(parseDeepLink('goosar://auth/callback?error=oidc_state_invalid&token=jwt')).toEqual({
      kind: 'auth-token',
      token: 'jwt',
    });
    expect(parseDeepLink('goosar://invite/')).toBeNull();
    expect(parseDeepLink('not a url')).toBeNull();
  });
});

import { describe, expect, it } from 'vitest';

import {
  KERBEROS_DEFAULT_THRESHOLD_MS,
  KERBEROS_PROACTIVE_REKINIT_MS,
  decideKerberosAction,
  resolveKerberosPrincipal,
  validateKerberosPrincipalInput,
} from './kerberos-identity';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

describe('validateKerberosPrincipalInput', () => {
  it('accepts user@REALM and reports no case warning', () => {
    const result = validateKerberosPrincipalInput('ivanov@CORP.EXAMPLE');
    expect(result.ok).toBe(true);
    expect(result.realmNotUppercase).toBe(false);
    expect(result.value).toBe('ivanov@CORP.EXAMPLE');
  });

  it('trims surrounding whitespace before judging', () => {
    expect(validateKerberosPrincipalInput('  ivanov@CORP.EXAMPLE \n').value).toBe(
      'ivanov@CORP.EXAMPLE',
    );
  });

  it('accepts a principal with an instance', () => {
    expect(validateKerberosPrincipalInput('ivanov/admin@CORP.EXAMPLE').ok).toBe(true);
  });

  it('warns, but accepts, when the realm is not upper-case', () => {
    const result = validateKerberosPrincipalInput('ivanov@corp.example');
    expect(result.ok).toBe(true);
    expect(result.realmNotUppercase).toBe(true);
  });

  it('reports an empty value as empty, not as a shape error', () => {
    expect(validateKerberosPrincipalInput('   ').issue).toBe('empty');
  });

  it('reports whitespace inside the value', () => {
    expect(validateKerberosPrincipalInput('iv anov@CORP.EXAMPLE').issue).toBe('whitespace');
  });

  it('rejects a value without a realm', () => {
    expect(validateKerberosPrincipalInput('ivanov').issue).toBe('shape');
  });

  it('rejects a value with two realms', () => {
    expect(validateKerberosPrincipalInput('ivanov@A@B').issue).toBe('shape');
  });
});

describe('resolveKerberosPrincipal', () => {
  it('uses the override when it is valid, ignoring the e-mail entirely', () => {
    expect(
      resolveKerberosPrincipal({
        override: 'ivanov@CORP.EXAMPLE',
        email: 'another.name@goosar.example',
        realm: 'GOOSAR.EXAMPLE',
      }),
    ).toBe('ivanov@CORP.EXAMPLE');
  });

  it('falls back to the derived principal when no override is set', () => {
    expect(
      resolveKerberosPrincipal({
        override: null,
        email: 'ivanov@goosar.example',
        realm: 'CORP.EXAMPLE',
      }),
    ).toBe('ivanov@CORP.EXAMPLE');
  });

  it('falls back when the override is blank', () => {
    expect(
      resolveKerberosPrincipal({
        override: '   ',
        email: 'ivanov@goosar.example',
      }),
    ).toBe('ivanov@GOOSAR.EXAMPLE');
  });

  it('falls back rather than passing an ill-formed override to kinit', () => {
    expect(
      resolveKerberosPrincipal({
        override: 'not a principal',
        email: 'ivanov@goosar.example',
      }),
    ).toBe('ivanov@GOOSAR.EXAMPLE');
  });

  it('returns null when neither source yields a principal', () => {
    expect(resolveKerberosPrincipal({ override: null, email: null })).toBeNull();
  });
});

describe('decideKerberosAction', () => {
  const now = Date.UTC(2026, 8, 8, 12, 0, 0);
  const base = {
    nowMs: now,
    expiresAt: now + 8 * HOUR,
    renewUntil: now + 3 * 24 * HOUR,
    lastKinitAt: now - HOUR,
  };

  it('does nothing for a comfortably valid ticket', () => {
    expect(decideKerberosAction(base)).toBe('none');
  });

  it('prompts when there is no ticket at all', () => {
    expect(decideKerberosAction({ ...base, expiresAt: null })).toBe('prompt');
  });

  it('prompts instead of renewing when the principal is mismatched', () => {
    const nearExpiry = { ...base, expiresAt: now + MINUTE };
    expect(decideKerberosAction(nearExpiry)).toBe('renew');
    expect(decideKerberosAction({ ...nearExpiry, principalMismatch: true })).toBe('prompt');
  });

  it('renews an expired but still renewable ticket', () => {
    expect(decideKerberosAction({ ...base, expiresAt: now - MINUTE })).toBe('renew');
  });

  it('prompts for an expired ticket past its renewable life', () => {
    expect(
      decideKerberosAction({
        ...base,
        expiresAt: now - HOUR,
        renewUntil: now - MINUTE,
      }),
    ).toBe('prompt');
  });

  it('renews inside the default threshold', () => {
    expect(
      decideKerberosAction({
        ...base,
        expiresAt: now + KERBEROS_DEFAULT_THRESHOLD_MS - MINUTE,
      }),
    ).toBe('renew');
  });

  it('honours a configured threshold wider than the default', () => {
    const input = { ...base, expiresAt: now + 2 * HOUR };
    expect(decideKerberosAction(input)).toBe('none');
    expect(decideKerberosAction({ ...input, thresholdMs: 3 * HOUR })).toBe('renew');
  });

  it('acts proactively once the last kinit is older than the TGT life', () => {
    expect(
      decideKerberosAction({
        ...base,
        lastKinitAt: now - KERBEROS_PROACTIVE_REKINIT_MS - MINUTE,
      }),
    ).toBe('renew');
  });

  it('prompts proactively when renewal is no longer possible', () => {
    expect(
      decideKerberosAction({
        ...base,
        lastKinitAt: now - KERBEROS_PROACTIVE_REKINIT_MS - MINUTE,
        renewUntil: now - MINUTE,
      }),
    ).toBe('prompt');
  });

  it('treats an unknown renew-until as worth attempting', () => {
    expect(decideKerberosAction({ ...base, expiresAt: now - MINUTE, renewUntil: null })).toBe(
      'renew',
    );
  });

  it('suppresses a prompt while snoozed', () => {
    expect(
      decideKerberosAction({
        ...base,
        expiresAt: null,
        snoozedUntil: now + 30 * MINUTE,
      }),
    ).toBe('none');
  });

  it('prompts again once the snooze has passed', () => {
    expect(
      decideKerberosAction({
        ...base,
        expiresAt: null,
        snoozedUntil: now - MINUTE,
      }),
    ).toBe('prompt');
  });

  it('never lets a snooze suppress a pre-flight check', () => {
    expect(
      decideKerberosAction({
        ...base,
        expiresAt: null,
        snoozedUntil: now + 30 * MINUTE,
        preflight: true,
      }),
    ).toBe('prompt');
  });

  it('still renews while snoozed — a renewal shows no dialog', () => {
    expect(
      decideKerberosAction({
        ...base,
        expiresAt: now - MINUTE,
        snoozedUntil: now + 30 * MINUTE,
      }),
    ).toBe('renew');
  });
});

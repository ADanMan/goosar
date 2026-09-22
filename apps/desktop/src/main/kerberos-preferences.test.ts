import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { KERBEROS_DEFAULT_THRESHOLD_MS } from '../shared/kerberos-identity';
import {
  DEFAULT_KERBEROS_PREFERENCES,
  kerberosPreferencesPath,
  loadKerberosPreferences,
  parseKerberosPreferences,
  saveKerberosPreferences,
} from './kerberos-preferences';

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'kerberos-prefs-'));
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe('parseKerberosPreferences', () => {
  it('keeps a valid principal, trimmed', () => {
    expect(parseKerberosPreferences({ principal: ' ivanov@CORP.EXAMPLE ' }).principal).toBe(
      'ivanov@CORP.EXAMPLE',
    );
  });

  it('drops an ill-formed principal rather than storing a future kinit argv', () => {
    expect(parseKerberosPreferences({ principal: 'not a principal' }).principal).toBeNull();
  });

  it('falls back to the default threshold for out-of-range values', () => {
    expect(parseKerberosPreferences({ thresholdMs: 5 }).thresholdMs).toBe(
      KERBEROS_DEFAULT_THRESHOLD_MS,
    );
    expect(parseKerberosPreferences({ thresholdMs: 99 * 60 * 60_000 }).thresholdMs).toBe(
      KERBEROS_DEFAULT_THRESHOLD_MS,
    );
    expect(parseKerberosPreferences({ thresholdMs: '30' }).thresholdMs).toBe(
      KERBEROS_DEFAULT_THRESHOLD_MS,
    );
  });

  it('keeps an in-range threshold', () => {
    expect(parseKerberosPreferences({ thresholdMs: 15 * 60_000 }).thresholdMs).toBe(15 * 60_000);
  });

  it('degrades each field independently', () => {
    const prefs = parseKerberosPreferences({
      principal: 'ivanov@CORP.EXAMPLE',
      thresholdMs: 'nonsense',
      snoozedUntil: 'later',
    });
    expect(prefs.principal).toBe('ivanov@CORP.EXAMPLE');
    expect(prefs.thresholdMs).toBe(KERBEROS_DEFAULT_THRESHOLD_MS);
    expect(prefs.snoozedUntil).toBeNull();
  });

  it('returns the defaults for a non-object', () => {
    expect(parseKerberosPreferences(null)).toEqual(DEFAULT_KERBEROS_PREFERENCES);
    expect(parseKerberosPreferences([1, 2])).toEqual(DEFAULT_KERBEROS_PREFERENCES);
  });
});

describe('load/save', () => {
  it('round-trips through the file', async () => {
    const path = kerberosPreferencesPath(dir);
    const saved = await saveKerberosPreferences(path, {
      principal: 'ivanov@CORP.EXAMPLE',
      thresholdMs: 10 * 60_000,
      snoozedUntil: 1_800_000_000_000,
    });
    expect(saved.principal).toBe('ivanov@CORP.EXAMPLE');
    expect(await loadKerberosPreferences(path)).toEqual(saved);
  });

  it('returns the defaults when the file is missing', async () => {
    expect(await loadKerberosPreferences(join(dir, 'absent.json'))).toEqual(
      DEFAULT_KERBEROS_PREFERENCES,
    );
  });

  it('returns the defaults when the file is corrupt', async () => {
    const path = kerberosPreferencesPath(dir);
    writeFileSync(path, '{ not json', 'utf-8');
    expect(await loadKerberosPreferences(path)).toEqual(DEFAULT_KERBEROS_PREFERENCES);
  });

  it('normalizes on write, so a bad principal never reaches the file', async () => {
    const path = kerberosPreferencesPath(dir);
    await saveKerberosPreferences(path, {
      principal: 'iv anov@CORP.EXAMPLE',
      thresholdMs: KERBEROS_DEFAULT_THRESHOLD_MS,
      snoozedUntil: null,
    });
    expect((await loadKerberosPreferences(path)).principal).toBeNull();
  });
});

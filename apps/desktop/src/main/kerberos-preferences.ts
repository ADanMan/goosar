import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';

import {
  KERBEROS_DEFAULT_THRESHOLD_MS,
  validateKerberosPrincipalInput,
} from '../shared/kerberos-identity';
import type { KerberosPreferences } from '../shared/kerberos-preferences-types';

export const DEFAULT_KERBEROS_PREFERENCES: KerberosPreferences = {
  principal: null,
  thresholdMs: KERBEROS_DEFAULT_THRESHOLD_MS,
  snoozedUntil: null,
};

export function kerberosPreferencesPath(userDataPath: string): string {
  return join(userDataPath, 'kerberos-preferences.json');
}

const MIN_THRESHOLD_MS = 60_000;
const MAX_THRESHOLD_MS = 4 * 60 * 60_000;

export function parseKerberosPreferences(value: unknown): KerberosPreferences {
  const record =
    value !== null && typeof value === 'object' && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : {};

  const principalInput = typeof record.principal === 'string' ? record.principal : '';
  const principalCheck = validateKerberosPrincipalInput(principalInput);

  const rawThreshold = record.thresholdMs;
  const thresholdMs =
    typeof rawThreshold === 'number' &&
    Number.isFinite(rawThreshold) &&
    rawThreshold >= MIN_THRESHOLD_MS &&
    rawThreshold <= MAX_THRESHOLD_MS
      ? rawThreshold
      : DEFAULT_KERBEROS_PREFERENCES.thresholdMs;

  const rawSnooze = record.snoozedUntil;
  const snoozedUntil =
    typeof rawSnooze === 'number' && Number.isFinite(rawSnooze) && rawSnooze > 0 ? rawSnooze : null;

  return {
    principal: principalCheck.ok ? principalCheck.value : null,
    thresholdMs,
    snoozedUntil,
  };
}

export async function loadKerberosPreferences(filePath: string): Promise<KerberosPreferences> {
  try {
    return parseKerberosPreferences(JSON.parse(await readFile(filePath, 'utf-8')));
  } catch {
    return { ...DEFAULT_KERBEROS_PREFERENCES };
  }
}

export async function saveKerberosPreferences(
  filePath: string,
  preferences: unknown,
): Promise<KerberosPreferences> {
  const normalized = parseKerberosPreferences(preferences);
  await mkdir(dirname(filePath), { recursive: true });
  const temporaryPath = `${filePath}.tmp`;
  await writeFile(temporaryPath, JSON.stringify(normalized, null, 2), 'utf-8');
  await rename(temporaryPath, filePath);
  return normalized;
}

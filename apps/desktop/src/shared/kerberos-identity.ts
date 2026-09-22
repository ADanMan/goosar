import { deriveKinitPrincipal, isValidKerberosPrincipal } from './perimeter-config';

export type KerberosPrincipalIssue =
  | 'empty'
  /** Whitespace inside the value; kinit takes one argv token. */
  | 'whitespace'
  /** Not `primary[/instance]@REALM`. */
  | 'shape';

export interface KerberosPrincipalValidation {
  ok: boolean;
  issue?: KerberosPrincipalIssue;
  realmNotUppercase: boolean;
  value: string;
}

export function validateKerberosPrincipalInput(raw: string): KerberosPrincipalValidation {
  const value = typeof raw === 'string' ? raw.trim() : '';
  if (value.length === 0) {
    return { ok: false, issue: 'empty', realmNotUppercase: false, value };
  }
  if (/\s/.test(value)) {
    return { ok: false, issue: 'whitespace', realmNotUppercase: false, value };
  }
  if (!isValidKerberosPrincipal(value)) {
    return { ok: false, issue: 'shape', realmNotUppercase: false, value };
  }
  const realm = value.slice(value.lastIndexOf('@') + 1);
  return { ok: true, realmNotUppercase: realm !== realm.toUpperCase(), value };
}

export interface KerberosPrincipalSources {
  override?: string | null;
  email?: string | null;
  realm?: string | null;
  principalDomain?: string | null;
}

export function resolveKerberosPrincipal(sources: KerberosPrincipalSources): string | null {
  const override = validateKerberosPrincipalInput(sources.override ?? '');
  if (override.ok) return override.value;
  const domain = sources.principalDomain?.trim();
  return deriveKinitPrincipal(
    sources.email ?? '',
    domain && domain.length > 0 ? domain : (sources.realm ?? null),
  );
}

export const KERBEROS_DEFAULT_THRESHOLD_MS = 30 * 60_000;

export const KERBEROS_PROACTIVE_REKINIT_MS = 10 * 60 * 60_000;

export const KERBEROS_SNOOZE_MS = 60 * 60_000;

export type KerberosLifecycleAction =
  | 'none'
  /** Try `kinit -R` — passwordless, silent, no dialog. */
  | 'renew'
  /** Only a password gets a ticket: open the kinit dialog. */
  | 'prompt';

export interface KerberosLifecycleInput {
  nowMs: number;
  expiresAt: number | null;
  renewUntil: number | null;
  lastKinitAt: number | null;
  thresholdMs?: number;
  snoozedUntil?: number | null;
  preflight?: boolean;
  principalMismatch?: boolean;
}

function isRenewable(input: KerberosLifecycleInput): boolean {
  if (input.renewUntil === null) return true;
  return input.nowMs < input.renewUntil;
}

export function decideKerberosAction(input: KerberosLifecycleInput): KerberosLifecycleAction {
  const threshold = input.thresholdMs ?? KERBEROS_DEFAULT_THRESHOLD_MS;
  const snoozed =
    input.preflight !== true &&
    input.snoozedUntil !== null &&
    input.snoozedUntil !== undefined &&
    input.nowMs < input.snoozedUntil;

  const prompt = (): KerberosLifecycleAction => (snoozed ? 'none' : 'prompt');

  if (input.expiresAt === null) return prompt();

  const expired = input.nowMs >= input.expiresAt;
  const closeToExpiry = input.expiresAt - input.nowMs <= threshold;
  const stale =
    input.lastKinitAt !== null && input.nowMs - input.lastKinitAt >= KERBEROS_PROACTIVE_REKINIT_MS;

  if (!expired && !closeToExpiry && !stale) return 'none';
  if (input.principalMismatch === true) return prompt();
  return isRenewable(input) ? 'renew' : prompt();
}

export const KERBEROS_BACKED_PACKAGES: readonly string[] = ['ews-mcp'];

export function isKerberosBackedInstalled(packageNames: readonly string[]): boolean {
  return packageNames.some((name) => KERBEROS_BACKED_PACKAGES.includes(name));
}

import { execFile } from 'child_process';

import { isValidKerberosPrincipal } from '../shared/perimeter-config';

export const KINIT_BIN = '/usr/bin/kinit';
const KINIT_TIMEOUT_MS = 30_000;

export interface KinitInvocation {
  bin: string;
  args: string[];
}

export function buildKinitInvocation(principal?: string | null): KinitInvocation {
  const args =
    typeof principal === 'string' && principal.length > 0
      ? ['--password-file=STDIN', principal]
      : ['--password-file=STDIN'];
  return { bin: KINIT_BIN, args };
}

export function buildKinitRenewInvocation(principal?: string | null): KinitInvocation {
  const args =
    typeof principal === 'string' && isValidKerberosPrincipal(principal)
      ? ['-R', principal]
      : ['-R'];
  return { bin: KINIT_BIN, args };
}

export type KinitFailureReason =
  | 'invalid_principal'
  | 'wrong_password'
  | 'principal_unknown'
  | 'kdc_unreachable'
  | 'kinit_missing'
  | 'renew_rejected'
  | 'unknown';

export type KinitResult = { ok: true } | { ok: false; reason: KinitFailureReason; message: string };

export interface KinitSpawnOutcome {
  code: number | null;
  stderr: string;
  spawnError?: string;
}

export type KinitRunner = (
  invocation: KinitInvocation,
  password: string,
) => Promise<KinitSpawnOutcome>;

const MESSAGE_BY_REASON: Record<KinitFailureReason, string> = {
  invalid_principal: 'Could not build a valid Kerberos principal to sign in with.',
  wrong_password: 'The password was not accepted by the domain.',
  principal_unknown:
    'The domain does not know this principal. Enter the Kerberos account (UPN) to use.',
  kdc_unreachable:
    'Could not reach the Kerberos server. Check the perimeter connection and try again.',
  kinit_missing: 'kinit is not available on this machine.',
  renew_rejected: 'The ticket could not be renewed. Sign in with your password to get a new one.',
  unknown: 'Could not obtain a Kerberos ticket.',
};

function classifyFailure(outcome: KinitSpawnOutcome): KinitFailureReason {
  if (outcome.spawnError) {
    return /ENOENT|not found|no such file/i.test(outcome.spawnError) ? 'kinit_missing' : 'unknown';
  }
  const stderr = outcome.stderr.toLowerCase();
  if (
    /c_principal_unknown|client principal unknown|client\b[^\n]*\bnot found in kerberos database|client not found|client \([^)]*\) unknown/.test(
      stderr,
    )
  ) {
    return 'principal_unknown';
  }
  if (/password incorrect|preauth|pre-authentication|bad password|failed to verify/.test(stderr)) {
    return 'wrong_password';
  }
  if (
    /unreachable|cannot contact|cannot resolve|unable to reach|connection refused|timed out|time out|no such host|cannot find kdc|generic error/.test(
      stderr,
    )
  ) {
    return 'kdc_unreachable';
  }
  return 'unknown';
}

export const spawnKinit: KinitRunner = (invocation, password) =>
  new Promise<KinitSpawnOutcome>((resolve) => {
    let settled = false;
    const finish = (outcome: KinitSpawnOutcome) => {
      if (settled) return;
      settled = true;
      resolve(outcome);
    };

    let child: ReturnType<typeof execFile>;
    try {
      child = execFile(
        invocation.bin,
        invocation.args,
        { timeout: KINIT_TIMEOUT_MS },
        (err, _stdout, stderr) => {
          if (err) {
            const code = typeof err.code === 'number' ? err.code : null;
            const spawnError = typeof err.code === 'string' ? err.code : undefined;
            finish({ code, stderr: stderr ?? '', spawnError });
            return;
          }
          finish({ code: 0, stderr: stderr ?? '' });
        },
      );
    } catch (err) {
      finish({
        code: null,
        stderr: '',
        spawnError: err instanceof Error ? err.message : String(err),
      });
      return;
    }

    child.stdin?.on('error', () => {});
    child.stdin?.end(`${password}\n`);
  });

export async function runKinit(
  principal: string | null | undefined,
  password: string,
  run: KinitRunner = spawnKinit,
): Promise<KinitResult> {
  const hasPrincipal = typeof principal === 'string' && principal.length > 0;
  if (hasPrincipal && !isValidKerberosPrincipal(principal)) {
    return {
      ok: false,
      reason: 'invalid_principal',
      message: MESSAGE_BY_REASON.invalid_principal,
    };
  }

  const outcome = await run(buildKinitInvocation(hasPrincipal ? principal : null), password);
  if (outcome.spawnError === undefined && outcome.code === 0) {
    return { ok: true };
  }
  const reason = classifyFailure(outcome);
  return { ok: false, reason, message: MESSAGE_BY_REASON[reason] };
}

export type KinitRenewRunner = (invocation: KinitInvocation) => Promise<KinitSpawnOutcome>;

export const spawnKinitRenew: KinitRenewRunner = (invocation) =>
  new Promise<KinitSpawnOutcome>((resolve) => {
    try {
      const child = execFile(
        invocation.bin,
        invocation.args,
        { timeout: KINIT_TIMEOUT_MS },
        (err, _stdout, stderr) => {
          if (err) {
            resolve({
              code: typeof err.code === 'number' ? err.code : null,
              stderr: stderr ?? '',
              spawnError: typeof err.code === 'string' ? err.code : undefined,
            });
            return;
          }
          resolve({ code: 0, stderr: stderr ?? '' });
        },
      );
      child.stdin?.on('error', () => {});
      child.stdin?.end();
    } catch (err) {
      resolve({
        code: null,
        stderr: '',
        spawnError: err instanceof Error ? err.message : String(err),
      });
    }
  });

export async function runKinitRenew(
  run: KinitRenewRunner = spawnKinitRenew,
  principal?: string | null,
): Promise<KinitResult> {
  const outcome = await run(buildKinitRenewInvocation(principal));
  if (outcome.spawnError === undefined && outcome.code === 0) {
    return { ok: true };
  }
  const classified = classifyFailure(outcome);
  const reason =
    classified === 'kinit_missing' || classified === 'kdc_unreachable'
      ? classified
      : 'renew_rejected';
  return { ok: false, reason, message: MESSAGE_BY_REASON[reason] };
}

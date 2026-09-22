import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { afterAll, describe, expect, it } from 'vitest';

import { resolveKerberosPrincipal } from '../shared/kerberos-identity';
import {
  buildKinitInvocation,
  runKinit,
  buildKinitRenewInvocation,
  spawnKinit,
  spawnKinitRenew,
} from './perimeter-kinit';

const PRINCIPAL = 'worker@EXAMPLE.TEST';
const PASSWORD = 'not-a-real-password';

const dir = mkdtempSync(join(tmpdir(), 'kinit-fake-'));
afterAll(() => rmSync(dir, { recursive: true, force: true }));

function writeFakeKinit(
  name: string,
  { code = 0, stderr = '' }: { code?: number; stderr?: string } = {},
): { bin: string; argvFile: string; stdinFile: string } {
  const bin = join(dir, name);
  const argvFile = `${bin}.argv`;
  const stdinFile = `${bin}.stdin`;
  writeFileSync(
    bin,
    [
      '#!/bin/sh',
      `printf '%s\\n' "$@" > "${argvFile}"`,
      `cat > "${stdinFile}"`,
      stderr ? `printf '%s' '${stderr}' >&2` : '',
      `exit ${code}`,
      '',
    ].join('\n'),
    'utf-8',
  );
  chmodSync(bin, 0o755);
  return { bin, argvFile, stdinFile };
}

const readOrEmpty = (path: string): string => {
  try {
    return readFileSync(path, 'utf-8');
  } catch {
    return '';
  }
};

describe('spawnKinit against a fake kinit binary', () => {
  it('passes only the principal on argv and the password only on stdin', async () => {
    const fake = writeFakeKinit('kinit-ok');
    const outcome = await spawnKinit(
      { ...buildKinitInvocation(PRINCIPAL), bin: fake.bin },
      PASSWORD,
    );

    expect(outcome.code).toBe(0);
    expect(outcome.spawnError).toBeUndefined();

    const argv = readOrEmpty(fake.argvFile);
    expect(argv).toContain('--password-file=STDIN');
    expect(argv).toContain(PRINCIPAL);
    expect(argv).not.toContain(PASSWORD);
    expect(readOrEmpty(fake.stdinFile)).toBe(`${PASSWORD}\n`);
  });

  it('reports the exit code and stderr of a refused sign-in', async () => {
    const fake = writeFakeKinit('kinit-bad-password', {
      code: 1,
      stderr: 'kinit: Password incorrect',
    });
    const outcome = await spawnKinit(
      { ...buildKinitInvocation(PRINCIPAL), bin: fake.bin },
      PASSWORD,
    );

    expect(outcome.code).toBe(1);
    expect(outcome.stderr).toContain('Password incorrect');
    expect(outcome.stderr).not.toContain(PASSWORD);
  });

  it('reports a spawn error for a path that does not exist', async () => {
    const outcome = await spawnKinit(
      { bin: join(dir, 'kinit-does-not-exist'), args: [] },
      PASSWORD,
    );
    expect(outcome.spawnError).toBe('ENOENT');
  });
});

describe('spawnKinitRenew against a fake kinit binary (#466)', () => {
  it("renews the named principal's cache and writes nothing to stdin", async () => {
    const fake = writeFakeKinit('kinit-renew');
    const outcome = await spawnKinitRenew({
      ...buildKinitRenewInvocation(PRINCIPAL),
      bin: fake.bin,
    });

    expect(outcome.code).toBe(0);
    expect(readOrEmpty(fake.argvFile).split('\n').filter(Boolean)).toEqual(['-R', PRINCIPAL]);
    expect(readOrEmpty(fake.stdinFile)).toBe('');
  });

  it('falls back to a bare -R when no principal is known', async () => {
    const fake = writeFakeKinit('kinit-renew-bare');
    await spawnKinitRenew({
      ...buildKinitRenewInvocation(null),
      bin: fake.bin,
    });
    expect(readOrEmpty(fake.argvFile).split('\n').filter(Boolean)).toEqual(['-R']);
  });

  it('never puts an ill-formed principal on argv', async () => {
    const fake = writeFakeKinit('kinit-renew-junk');
    await spawnKinitRenew({
      ...buildKinitRenewInvocation('not a principal; rm -rf /'),
      bin: fake.bin,
    });
    expect(readOrEmpty(fake.argvFile).split('\n').filter(Boolean)).toEqual(['-R']);
  });
});

describe('principal in another domain (#531, fake kinit)', () => {
  it('spawns kinit for the override, not for the sign-in address', async () => {
    const fake = writeFakeKinit('kinit-other-domain');
    const principal = resolveKerberosPrincipal({
      override: 'ivanov@EXAMPLE.NET',
      email: 'ivanov@example.com',
      realm: 'EXAMPLE.COM',
    });

    await spawnKinit({ ...buildKinitInvocation(principal!), bin: fake.bin }, PASSWORD);

    expect(readOrEmpty(fake.argvFile).split('\n').filter(Boolean)).toEqual([
      '--password-file=STDIN',
      'ivanov@EXAMPLE.NET',
    ]);
    expect(readOrEmpty(fake.argvFile)).not.toContain('example.com');
  });

  it('classifies a KDC that does not know the principal', async () => {
    const fake = writeFakeKinit('kinit-unknown-principal', {
      code: 1,
      stderr: 'kinit: krb5_get_init_creds: Client (ivanov@EXAMPLE.COM) unknown',
    });

    const result = await runKinit('ivanov@EXAMPLE.COM', PASSWORD, (inv, pw) =>
      spawnKinit({ ...inv, bin: fake.bin }, pw),
    );

    if (result.ok) throw new Error('expected failure');
    expect(result.reason).toBe('principal_unknown');
    expect(readOrEmpty(fake.stdinFile)).toBe(`${PASSWORD}\n`);
    expect(result.message).not.toContain(PASSWORD);
  });
});

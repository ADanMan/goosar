import { afterEach, describe, expect, it, vi } from 'vitest';

const PASSWORD = 'S3cret-corp-pass!';
const PRINCIPAL = 'user@EXAMPLE.TEST';

describe('buildKinitInvocation', () => {
  it('puts the principal on argv and NEVER the password', async () => {
    const { buildKinitInvocation, KINIT_BIN } = await import('./perimeter-kinit');
    const invocation = buildKinitInvocation(PRINCIPAL);
    expect(invocation.bin).toBe(KINIT_BIN);
    expect(invocation.args).toEqual(['--password-file=STDIN', PRINCIPAL]);
    expect(JSON.stringify(invocation)).not.toContain(PASSWORD);
  });

  it('omits the principal argument for an empty string', async () => {
    const { buildKinitInvocation } = await import('./perimeter-kinit');
    const invocation = buildKinitInvocation('');
    expect(invocation.args).toEqual(['--password-file=STDIN']);
    expect(JSON.stringify(invocation)).not.toContain(PASSWORD);
  });

  it('omits the principal argument for null/undefined', async () => {
    const { buildKinitInvocation } = await import('./perimeter-kinit');
    expect(buildKinitInvocation(null).args).toEqual(['--password-file=STDIN']);
    expect(buildKinitInvocation(undefined).args).toEqual(['--password-file=STDIN']);
    expect(buildKinitInvocation().args).toEqual(['--password-file=STDIN']);
  });
});

describe('runKinit (injected runner)', () => {
  it('refuses an invalid principal WITHOUT spawning anything', async () => {
    const { runKinit } = await import('./perimeter-kinit');
    const run = vi.fn();
    const result = await runKinit('not a principal', PASSWORD, run);
    expect(run).not.toHaveBeenCalled();
    expect(result).toEqual({
      ok: false,
      reason: 'invalid_principal',
      message: expect.any(String),
    });
  });

  it("hands the password to the runner's stdin channel, not the argv", async () => {
    const { runKinit } = await import('./perimeter-kinit');
    const run = vi.fn(async (invocation, password) => {
      expect(invocation.args).toEqual(['--password-file=STDIN', PRINCIPAL]);
      expect(invocation.args).not.toContain(password);
      expect(password).toBe(PASSWORD);
      return { code: 0, stderr: '' };
    });
    const result = await runKinit(PRINCIPAL, PASSWORD, run);
    expect(result).toEqual({ ok: true });
  });

  it.each([
    ['', 'empty'],
    [null, 'null'],
    [undefined, 'undefined'],
  ] as const)(
    'runs a bare kinit for a %s principal, password never on argv',
    async (principal, _label) => {
      const { runKinit } = await import('./perimeter-kinit');
      const run = vi.fn(async (invocation: { args: string[] }, password: string) => {
        expect(invocation.args).toEqual(['--password-file=STDIN']);
        expect(invocation.args).not.toContain(password);
        expect(invocation.args.every((arg) => arg !== PASSWORD)).toBe(true);
        return { code: 0, stderr: '' };
      });
      const result = await runKinit(principal, PASSWORD, run);
      expect(run).toHaveBeenCalledTimes(1);
      expect(result).toEqual({ ok: true });
    },
  );

  it('still refuses a non-empty invalid principal as invalid_principal', async () => {
    const { runKinit } = await import('./perimeter-kinit');
    const run = vi.fn();
    const result = await runKinit('not a principal', PASSWORD, run);
    expect(run).not.toHaveBeenCalled();
    expect(result).toEqual({
      ok: false,
      reason: 'invalid_principal',
      message: expect.any(String),
    });
  });

  it('classifies a bad password without leaking it in the message', async () => {
    const { runKinit } = await import('./perimeter-kinit');
    const run = vi.fn(async () => ({
      code: 1,
      stderr: 'kinit: krb5_get_init_creds: Password incorrect',
    }));
    const result = await runKinit(PRINCIPAL, PASSWORD, run);
    expect(result.ok).toBe(false);
    if (result.ok) throw new Error('expected failure');
    expect(result.reason).toBe('wrong_password');
    expect(result.message).not.toContain(PASSWORD);
  });

  it('classifies an unreachable KDC', async () => {
    const { runKinit } = await import('./perimeter-kinit');
    const run = vi.fn(async () => ({
      code: 1,
      stderr: 'kinit: krb5_get_init_creds: unable to reach any KDC in realm',
    }));
    const result = await runKinit(PRINCIPAL, PASSWORD, run);
    if (result.ok) throw new Error('expected failure');
    expect(result.reason).toBe('kdc_unreachable');
  });

  it('classifies a missing kinit binary', async () => {
    const { runKinit } = await import('./perimeter-kinit');
    const run = vi.fn(async () => ({
      code: null,
      stderr: '',
      spawnError: 'ENOENT',
    }));
    const result = await runKinit(PRINCIPAL, PASSWORD, run);
    if (result.ok) throw new Error('expected failure');
    expect(result.reason).toBe('kinit_missing');
  });
});

describe('runKinitRenew (#298)', () => {
  it('builds a bare `kinit -R` argv — no principal, no password channel', async () => {
    const { buildKinitRenewInvocation, KINIT_BIN } = await import('./perimeter-kinit');
    const invocation = buildKinitRenewInvocation();
    expect(invocation.bin).toBe(KINIT_BIN);
    expect(invocation.args).toEqual(['-R']);
  });

  it('reports success when kinit -R exits 0', async () => {
    const { runKinitRenew } = await import('./perimeter-kinit');
    const run = vi.fn(async () => ({ code: 0, stderr: '' }));
    expect(await runKinitRenew(run)).toEqual({ ok: true });
  });

  it('classifies a non-renewable/expired ticket as a failure (→ re-kinit)', async () => {
    const { runKinitRenew } = await import('./perimeter-kinit');
    const run = vi.fn(async () => ({
      code: 1,
      stderr: "kinit: krb5_get_kdc_cred: KDC can't fulfill requested option",
    }));
    const result = await runKinitRenew(run);
    if (result.ok) throw new Error('expected failure');
    expect(result.reason).toBe('renew_rejected');
    expect(result.message).toEqual(expect.any(String));
  });

  it('classifies a missing kinit binary', async () => {
    const { runKinitRenew } = await import('./perimeter-kinit');
    const run = vi.fn(async () => ({
      code: null,
      stderr: '',
      spawnError: 'ENOENT',
    }));
    const result = await runKinitRenew(run);
    if (result.ok) throw new Error('expected failure');
    expect(result.reason).toBe('kinit_missing');
  });

  it('classifies an unreachable KDC', async () => {
    const { runKinitRenew } = await import('./perimeter-kinit');
    const run = vi.fn(async () => ({
      code: 1,
      stderr: 'kinit: krb5_get_kdc_cred: unable to reach any KDC in realm',
    }));
    const result = await runKinitRenew(run);
    if (result.ok) throw new Error('expected failure');
    expect(result.reason).toBe('kdc_unreachable');
  });
});

describe('default runner (child_process.execFile mocked)', () => {
  afterEach(() => {
    vi.resetModules();
    vi.doUnmock('child_process');
  });

  it("writes the password to the child's stdin and never to execFile's argv", async () => {
    const endSpy = vi.fn();
    const execFileSpy = vi.fn(
      (
        _bin: string,
        _args: string[],
        _opts: unknown,
        cb: (err: Error | null, stdout: string, stderr: string) => void,
      ) => {
        queueMicrotask(() => cb(null, '', ''));
        return { stdin: { on: vi.fn(), end: endSpy } };
      },
    );
    vi.doMock('child_process', () => ({
      execFile: execFileSpy,
      default: { execFile: execFileSpy },
    }));
    vi.resetModules();

    const { runKinit } = await import('./perimeter-kinit');
    const result = await runKinit(PRINCIPAL, PASSWORD);

    expect(result).toEqual({ ok: true });
    const [, args] = execFileSpy.mock.calls[0];
    expect(args).toEqual(['--password-file=STDIN', PRINCIPAL]);
    expect(args).not.toContain(PASSWORD);
    expect(endSpy).toHaveBeenCalledWith(`${PASSWORD}\n`);
  });
});

describe('principal in a different domain than the login address (#531)', () => {
  const LOGIN_EMAIL = 'ivanov@example.com';

  it('derives the principal from perimeter.principalDomain, not from the login domain', async () => {
    const { resolveKerberosPrincipal } = await import('../shared/kerberos-identity');
    const { buildKinitInvocation } = await import('./perimeter-kinit');

    const principal = resolveKerberosPrincipal({
      override: null,
      email: LOGIN_EMAIL,
      realm: 'EXAMPLE.COM',
      principalDomain: 'example.net',
    });

    expect(principal).toBe('ivanov@EXAMPLE.NET');
    expect(buildKinitInvocation(principal!).args).toEqual([
      '--password-file=STDIN',
      'ivanov@EXAMPLE.NET',
    ]);
  });

  it('prefers the saved override over the configured principal domain', async () => {
    const { resolveKerberosPrincipal } = await import('../shared/kerberos-identity');
    expect(
      resolveKerberosPrincipal({
        override: 'i.ivanov@EXAMPLE.NET',
        email: LOGIN_EMAIL,
        realm: 'EXAMPLE.COM',
        principalDomain: 'example.net',
      }),
    ).toBe('i.ivanov@EXAMPLE.NET');
  });

  it('falls back to the realm when no principal domain is configured', async () => {
    const { resolveKerberosPrincipal } = await import('../shared/kerberos-identity');
    expect(
      resolveKerberosPrincipal({
        override: null,
        email: LOGIN_EMAIL,
        realm: 'EXAMPLE.COM',
        principalDomain: null,
      }),
    ).toBe('ivanov@EXAMPLE.COM');
  });

  it("does not mistake an unknown SERVER principal for the user's account", async () => {
    const { runKinit } = await import('./perimeter-kinit');
    const result = await runKinit(PRINCIPAL, PASSWORD, async () => ({
      code: 1,
      stderr: 'kinit: krb5_get_init_creds: Server (krbtgt/EXAMPLE.NET@EXAMPLE.COM) unknown',
    }));
    if (result.ok) throw new Error('expected failure');
    expect(result.reason).not.toBe('principal_unknown');
  });

  it('classifies an unknown principal as its own reason so the UI can ask for one', async () => {
    const { runKinit } = await import('./perimeter-kinit');
    for (const stderr of [
      'kinit: krb5_get_init_creds: Client (ivanov@EXAMPLE.COM) unknown',
      'kinit: Client not found in Kerberos database while getting initial credentials',
      'kinit: KDC_ERR_C_PRINCIPAL_UNKNOWN',
      "kinit: Client 'ivanov@EXAMPLE.NET' not found in Kerberos database while getting initial credentials",
    ]) {
      const result = await runKinit(PRINCIPAL, PASSWORD, async () => ({
        code: 1,
        stderr,
      }));
      if (result.ok) throw new Error('expected failure');
      expect(result.reason).toBe('principal_unknown');
      expect(result.message).not.toContain(PASSWORD);
    }
  });
});

describe('classifying an unknown SERVER principal (#531)', () => {
  const PRINCIPAL = 'ivanov@EXAMPLE.COM';
  const PASSWORD = 's3cret';

  it('does not ask for an account when the SERVER principal is unknown', async () => {
    const { runKinit } = await import('./perimeter-kinit');
    for (const stderr of [
      'kinit: Server principal unknown while getting initial credentials',
      'kinit: KRB5KDC_ERR_S_PRINCIPAL_UNKNOWN',
    ]) {
      const result = await runKinit(PRINCIPAL, PASSWORD, async () => ({
        code: 1,
        stderr,
      }));
      if (result.ok) throw new Error('expected failure');
      expect(result.reason).not.toBe('principal_unknown');
    }
  });
});

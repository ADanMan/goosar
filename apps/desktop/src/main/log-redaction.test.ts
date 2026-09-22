import { describe, expect, it } from 'vitest';
import { REDACTED_VALUE, redactSecretsDeep, redactSecretsInString } from './log-redaction';
import { redactionHook } from './main-logging';

describe('redactSecretsDeep', () => {
  it('redacts a fake secret before it can reach the log', () => {
    const fakeSecret = 'sk-fake-1234567890';
    const redacted = redactSecretsDeep({
      llm: { api_key: fakeSecret, model: 'gpt-test' },
    });
    expect(JSON.stringify(redacted)).not.toContain(fakeSecret);
    expect(redacted).toEqual({
      llm: { api_key: REDACTED_VALUE, model: 'gpt-test' },
    });
  });

  it('matches key/token/secret/password case-insensitively and as substrings', () => {
    const redacted = redactSecretsDeep({
      apiKey: 'a',
      CLI_TOKEN: 'b',
      clientSecret: 'c',
      UserPassword: 'd',
      safeField: 'keep',
    });
    expect(redacted).toEqual({
      apiKey: REDACTED_VALUE,
      CLI_TOKEN: REDACTED_VALUE,
      clientSecret: REDACTED_VALUE,
      UserPassword: REDACTED_VALUE,
      safeField: 'keep',
    });
  });

  it('redacts nested objects and objects inside arrays', () => {
    const redacted = redactSecretsDeep({
      servers: [{ url: 'https://x', token: 't-1' }, { headers: { 'X-Api-Key': 'k' } }],
    });
    expect(redacted).toEqual({
      servers: [
        { url: 'https://x', token: REDACTED_VALUE },
        { headers: { 'X-Api-Key': REDACTED_VALUE } },
      ],
    });
  });

  it('redacts non-string secret values too', () => {
    expect(redactSecretsDeep({ token: { inner: 'leak' } })).toEqual({
      token: REDACTED_VALUE,
    });
  });

  it('does not mutate the input', () => {
    const input = { nested: { password: 'p' } };
    redactSecretsDeep(input);
    expect(input.nested.password).toBe('p');
  });

  it('passes non-string primitives through untouched', () => {
    expect(redactSecretsDeep(42)).toBe(42);
    expect(redactSecretsDeep(null)).toBeNull();
    expect(redactSecretsDeep(undefined)).toBeUndefined();
  });

  it('scrubs strings too, at the top level and as leaves', () => {
    expect(redactSecretsDeep('token=visible-in-plain-string')).toBe(`token=${REDACTED_VALUE}`);
  });

  it('passes class instances through untouched', () => {
    const error = new Error('boom');
    expect(redactSecretsDeep(error)).toBe(error);
    const date = new Date(0);
    expect(redactSecretsDeep(date)).toBe(date);
  });

  it('survives circular structures', () => {
    const input: Record<string, unknown> = { name: 'a' };
    input.self = input;
    expect(redactSecretsDeep(input)).toEqual({ name: 'a', self: '[circular]' });
  });

  it('prints a shared (non-cyclic) reference in both branches', () => {
    const shared = { token: 't' };
    const redacted = redactSecretsDeep({ a: shared, b: shared });
    expect(redacted).toEqual({
      a: { token: REDACTED_VALUE },
      b: { token: REDACTED_VALUE },
    });
  });

  it('caps pathological nesting depth', () => {
    let value: Record<string, unknown> = { leaf: true };
    for (let i = 0; i < 20; i += 1) value = { child: value };
    const redacted = JSON.stringify(redactSecretsDeep(value));
    expect(redacted).toContain('[max-depth]');
  });
});

describe('redactSecretsInString — credentials inside an address', () => {
  it('redacts userinfo in an address with no credential-shaped field name', () => {
    expect(
      redactSecretsInString(
        '[network] routes resolved (focus): entry http://alice:s3cret@isa.corp:8080, local proxy unchanged',
      ),
    ).toBe(
      '[network] routes resolved (focus): entry http://***@isa.corp:8080, local proxy unchanged',
    );
  });

  it('leaves an address that carries no credential alone', () => {
    expect(redactSecretsInString('[network] entry http://127.0.0.1:3128')).toBe(
      '[network] entry http://127.0.0.1:3128',
    );
  });

  it('redacts a credential whose password contains a slash', () => {
    expect(redactSecretsInString('entry http://alice:pa/ss@isa.corp:8080')).not.toContain('pa/ss');
  });
});

describe('redactSecretsInString (issue #215 — flat strings from renderer and template literals)', () => {
  it('redacts named assignments while keeping the name readable', () => {
    const line = 'auth failed: apiKey=sk-FAKE-123 retry with token: tok-FAKE-9';
    expect(redactSecretsInString(line)).toBe(
      'auth failed: apiKey=<redacted> retry with token: <redacted>',
    );
  });

  it('redacts an address credential nested inside a logged object', () => {
    const redacted = redactSecretsDeep({
      route: { entry: 'http://alice:s3cret@isa.corp:8080' },
      offered: ['http://alice:s3cret@isa.corp:8080', 'DIRECT'],
    }) as { route: { entry: string }; offered: string[] };

    expect(redacted.route.entry).not.toContain('s3cret');
    expect(redacted.offered[0]).not.toContain('s3cret');
    expect(redacted.offered[1]).toBe('DIRECT');
  });

  it('redacts a Bearer credential', () => {
    expect(redactSecretsInString('Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.x.y')).toBe(
      'Authorization: Bearer <redacted>',
    );
  });

  it('leaves ordinary prose untouched', () => {
    const line = 'keyboard shortcuts loaded; press any key to continue';
    expect(redactSecretsInString(line)).toBe(line);
  });

  it('is applied by the hook to string arguments', () => {
    const message = { data: ['JIRA_PERSONAL_TOKEN=MjYx-FAKE'], level: 'error' } as never;
    const out = redactionHook(message) as unknown as { data: string[] };
    expect(out.data[0]).toBe('JIRA_PERSONAL_TOKEN=<redacted>');
  });
});

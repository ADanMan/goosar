import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { managedAgentPath, userBinPath } from './agent-bootstrap';
import {
  ensureBashFullFlag,
  sanitizeAgentConfigPatch,
  getAgentConfigValue,
  saveAgentConfig,
  type AgentCommandOutcome,
  type AgentConfigCommand,
  type AgentPathContext,
} from './agent-config';

let home: string;

function ctxFor(overrides: Partial<AgentPathContext> = {}): AgentPathContext {
  return { home, env: { HOME: home }, ...overrides };
}

function fakeManagedBinary(ctx: AgentPathContext, body: string): string {
  const bin = managedAgentPath(ctx);
  mkdirSync(join(home, '.hermes', 'runtime', 'current', 'bin'), {
    recursive: true,
  });
  writeFileSync(bin, body);
  chmodSync(bin, 0o755);
  return bin;
}

function recordingRunner(outcomes: AgentCommandOutcome[]) {
  const calls: AgentConfigCommand[] = [];
  const run = vi.fn(async (command: AgentConfigCommand) => {
    calls.push(command);
    const next = outcomes[calls.length - 1];
    if (!next) throw new Error(`unexpected extra invocation #${calls.length}`);
    return next;
  });
  return { calls, run };
}

function okEnvelope(key: string, shadowed = false): AgentCommandOutcome {
  return {
    code: 0,
    stdout:
      JSON.stringify({
        ok: true,
        key,
        layer: 'user',
        path: join(home, '.hermes', 'config.user.yaml'),
        shadowed_by_runtime: shadowed,
      }) + '\n',
    stderr: '',
  };
}

function errEnvelope(
  key: string,
  kind: string,
  message: string,
  code: number,
): AgentCommandOutcome {
  return {
    code,
    stdout: JSON.stringify({ ok: false, key, error: { kind, message } }) + '\n',
    stderr: `config set ${key}: ${message}\n`,
  };
}

const SECRET = 'sk-super-secret-value';

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'hermes-config-'));
});

afterEach(() => {
  rmSync(home, { recursive: true, force: true });
});

describe('saveAgentConfig', () => {
  it('writes every requested field and reports where it landed', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([
      okEnvelope('llm.api_base'),
      okEnvelope('llm.model'),
      okEnvelope('llm.api_key'),
    ]);

    const result = await saveAgentConfig(
      ctx,
      {
        'llm.api_base': 'https://gw.corp.example/v1',
        'llm.model': 'openai/glm-4.6',
        'llm.api_key': SECRET,
      },
      run,
    );

    expect(result).toEqual({
      ok: true,
      path: join(home, '.hermes', 'config.user.yaml'),
      shadowed: [],
    });
    expect(calls.map((c) => c.args)).toEqual([
      ['config', 'set', 'llm.api_base', '--stdin'],
      ['config', 'set', 'llm.model', '--stdin'],
      ['config', 'set', 'llm.api_key', '--stdin'],
    ]);
  });

  it('puts every value on stdin and never on argv', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([okEnvelope('llm.api_base'), okEnvelope('llm.api_key')]);

    await saveAgentConfig(
      ctx,
      { 'llm.api_base': 'https://gw.corp.example/v1', 'llm.api_key': SECRET },
      run,
    );

    for (const call of calls) {
      expect(call.args).toContain('--stdin');
      expect(call.args.join(' ')).not.toContain(SECRET);
      expect(call.args.join(' ')).not.toContain('gw.corp.example');
    }
    expect(calls.map((c) => c.stdin)).toEqual(['https://gw.corp.example/v1', SECRET]);
  });

  it('keeps the secret off argv when it really spawns the binary', async () => {
    const ctx = ctxFor();
    const argvLog = join(home, 'argv.txt');
    const stdinLog = join(home, 'stdin.txt');
    fakeManagedBinary(
      ctx,
      '#!/bin/sh\n' +
        `printf '%s\\n' "$@" > ${argvLog}\n` +
        `cat > ${stdinLog}\n` +
        `printf '{"ok":true,"key":"llm.api_key","layer":"user","path":"${join(
          home,
          '.hermes',
          'config.user.yaml',
        )}","shadowed_by_runtime":false}\\n'\n`,
    );

    const result = await saveAgentConfig(ctx, { 'llm.api_key': SECRET });

    expect(result).toMatchObject({ ok: true });
    expect(readFileSync(argvLog, 'utf-8')).not.toContain(SECRET);
    expect(readFileSync(argvLog, 'utf-8').split('\n').filter(Boolean)).toEqual([
      'config',
      'set',
      'llm.api_key',
      '--stdin',
    ]);
    expect(readFileSync(stdinLog, 'utf-8')).toBe(SECRET);
  });

  it('surfaces a write shadowed by config.runtime.yaml', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([
      okEnvelope('llm.api_base', true),
      okEnvelope('llm.api_key', false),
    ]);

    const result = await saveAgentConfig(
      ctx,
      { 'llm.api_base': 'https://gw.corp.example/v1', 'llm.api_key': SECRET },
      run,
    );

    expect(result).toEqual({
      ok: true,
      path: join(home, '.hermes', 'config.user.yaml'),
      shadowed: ['llm.api_base'],
    });
  });

  it.each([
    [1, 'config_unavailable', 'path not found: /home/x/.hermes/config.user.yaml'],
    [2, 'bad_usage', 'empty value on stdin'],
    [3, 'bad_key', 'key is not part of the config schema: llm.bogus'],
    [4, 'invalid_value', 'llm.api_base: api_base must be a valid URL'],
    [5, 'write_failed', 'OSError: read-only file system'],
  ])('maps exit code %i to %s', async (code, kind, message) => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([errEnvelope('llm.api_base', kind, message, code)]);

    const result = await saveAgentConfig(
      ctx,
      { 'llm.api_base': 'https://gw.corp.example/v1', 'llm.api_key': SECRET },
      run,
    );

    expect(result).toEqual({
      ok: false,
      field: 'llm.api_base',
      kind,
      message,
    });
  });

  it('stops at the first failure instead of writing the rest', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([
      errEnvelope('llm.api_base', 'invalid_value', 'api_base must be a valid URL', 4),
    ]);

    const result = await saveAgentConfig(
      ctx,
      { 'llm.api_base': 'not-a-url', 'llm.api_key': SECRET },
      run,
    );

    expect(result).toMatchObject({ ok: false, field: 'llm.api_base' });
    expect(calls).toHaveLength(1);
  });

  it('writes every field before the rejected one and none after it', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([
      okEnvelope('llm.api_base'),
      errEnvelope('llm.model', 'invalid_value', 'llm.model: unknown model id', 4),
    ]);

    const result = await saveAgentConfig(
      ctx,
      {
        'llm.api_base': 'https://gw.corp.example/v1',
        'llm.model': 'bogus/model',
        'llm.api_key': SECRET,
      },
      run,
    );

    expect(result).toMatchObject({ ok: false, field: 'llm.model' });
    expect(calls.map((c) => c.args[2])).toEqual(['llm.api_base', 'llm.model']);
  });

  it('never claims the whole config is untouched in its fallback message', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([
      okEnvelope('llm.api_base'),
      { code: 4, stdout: '', stderr: '' },
    ]);

    const result = await saveAgentConfig(
      ctx,
      { 'llm.api_base': 'https://gw.corp.example/v1', 'llm.model': 'bogus' },
      run,
    );

    expect(result).toMatchObject({ ok: false, field: 'llm.model' });
    expect(result.ok === false && result.message).not.toMatch(/nothing was changed/i);
  });

  it('classifies a nonzero exit from its code when the envelope is unreadable', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([{ code: 4, stdout: 'not json at all', stderr: 'boom\n' }]);

    const result = await saveAgentConfig(ctx, { 'llm.model': 'gpt-4' }, run);

    expect(result).toMatchObject({ ok: false, field: 'llm.model', kind: 'invalid_value' });
  });

  it('refuses to claim success when a zero exit carries no readable envelope', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([{ code: 0, stdout: '', stderr: '' }]);

    const result = await saveAgentConfig(ctx, { 'llm.model': 'gpt-4' }, run);

    expect(result).toMatchObject({ ok: false, field: 'llm.model', kind: 'unknown' });
  });

  it('reports an unmapped exit code as unknown rather than crashing', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([{ code: 137, stdout: '', stderr: 'Killed\n' }]);

    const result = await saveAgentConfig(ctx, { 'llm.model': 'gpt-4' }, run);

    expect(result).toMatchObject({ ok: false, kind: 'unknown' });
  });

  it('does nothing when this app installed no runtime', async () => {
    const ctx = ctxFor();
    const { calls, run } = recordingRunner([]);

    const result = await saveAgentConfig(ctx, { 'llm.api_key': SECRET }, run);

    expect(result).toMatchObject({
      ok: false,
      field: null,
      kind: 'runtime_unavailable',
    });
    expect(calls).toHaveLength(0);
  });

  it('never calls a hermes this app does not manage', async () => {
    const ctx = ctxFor();
    mkdirSync(join(home, '.local', 'bin'), { recursive: true });
    const foreign = userBinPath(ctx);
    writeFileSync(foreign, '#!/bin/sh\nexec /repo/.venv/bin/hermes "$@"\n');
    chmodSync(foreign, 0o755);
    const { calls, run } = recordingRunner([]);

    const result = await saveAgentConfig(ctx, { 'llm.api_key': SECRET }, run);

    expect(result).toMatchObject({ ok: false, kind: 'runtime_unavailable' });
    expect(calls).toHaveLength(0);
  });

  it('treats a spawn failure as an unusable runtime, not a crash', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([
      { code: null, stdout: '', stderr: '', spawnError: 'spawn ENOENT' },
    ]);

    const result = await saveAgentConfig(ctx, { 'llm.api_key': SECRET }, run);

    expect(result).toMatchObject({ ok: false, kind: 'runtime_unavailable' });
  });

  it('rejects an empty patch without touching the runtime', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([]);

    const result = await saveAgentConfig(ctx, { 'llm.model': '   ', 'llm.api_key': '' }, run);

    expect(result).toMatchObject({ ok: false, field: null, kind: 'bad_usage' });
    expect(calls).toHaveLength(0);
  });

  it('never leaks a value into the failure message it returns', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([
      { code: 5, stdout: '', stderr: `disk full while writing ${SECRET}\n` },
    ]);

    const result = await saveAgentConfig(ctx, { 'llm.api_key': SECRET }, run);

    expect(result.ok).toBe(false);
    expect(JSON.stringify(result)).not.toContain(SECRET);
  });
});

describe('sanitizeAgentConfigPatch', () => {
  it('accepts a valid llm.* patch', () => {
    expect(sanitizeAgentConfigPatch({ 'llm.model': 'gpt-4' })).toEqual({
      'llm.model': 'gpt-4',
    });
  });

  it('accepts security.bash_full: true', () => {
    expect(sanitizeAgentConfigPatch({ 'security.bash_full': true })).toEqual({
      'security.bash_full': true,
    });
  });

  it('accepts security.bash_full: false', () => {
    expect(sanitizeAgentConfigPatch({ 'security.bash_full': false })).toEqual({
      'security.bash_full': false,
    });
  });

  it('rejects a string value for security.bash_full', () => {
    expect(sanitizeAgentConfigPatch({ 'security.bash_full': 'true' })).toBeNull();
  });

  it('rejects a number value for security.bash_full', () => {
    expect(sanitizeAgentConfigPatch({ 'security.bash_full': 1 })).toBeNull();
  });

  it('drops an attempt to smuggle an arbitrary security.* key through the same patch', () => {
    const patch = sanitizeAgentConfigPatch({
      'llm.model': 'gpt-4',
      'security.bash_full': true,
      'security.approval.require_approval_for': ['irreversible'],
    }) as Record<string, unknown>;
    expect(patch).toEqual({ 'llm.model': 'gpt-4', 'security.bash_full': true });
    expect(patch['security.approval.require_approval_for']).toBeUndefined();
  });
});

describe('ensureBashFullFlag', () => {
  it('gets first, and skips the write when the field is already explicitly set', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([{ code: 0, stdout: 'false\n', stderr: '' }]);

    const result = await ensureBashFullFlag(ctx, run);

    expect(calls).toHaveLength(1);
    expect(calls[0].args).toEqual(['config', 'get', 'security.bash_full']);
    expect(result).toEqual({ ok: true, path: null, skipped: true });
  });

  it('writes true when the get comes back unset', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([
      { code: 3, stdout: '', stderr: 'no such key\n' },
      okEnvelope('security.bash_full'),
    ]);

    const result = await ensureBashFullFlag(ctx, run);

    expect(calls).toHaveLength(2);
    expect(calls[0].args).toEqual(['config', 'get', 'security.bash_full']);
    expect(calls[1].args).toEqual(['config', 'set', 'security.bash_full', '--stdin']);
    expect(calls[1].stdin).toBe('true');
    expect(result.ok).toBe(true);
  });

  it('proceeds to write when the get command errors outright', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([
      { code: null, stdout: '', stderr: '', spawnError: 'not found' },
      okEnvelope('security.bash_full'),
    ]);

    const result = await ensureBashFullFlag(ctx, run);

    expect(calls).toHaveLength(2);
    expect(result.ok).toBe(true);
  });

  it('falls back to a direct file write when config set fails on a foreign block', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    mkdirSync(join(home, '.hermes'), { recursive: true });
    const configPath = join(home, '.hermes', 'config.user.yaml');
    writeFileSync(
      configPath,
      'llm:\n  api_base: https://example/v1\nmcp_servers:\n  ews-mcp:\n    stdio: {}\n',
    );

    const { calls, run } = recordingRunner([
      { code: 3, stdout: '', stderr: 'no such key\n' },
      errEnvelope(
        'security.bash_full',
        'invalid_value',
        "mcp_servers.ews-mcp.stdio: Value error, mcp stdio server requires 'command' or 'docker_profile'",
        4,
      ),
    ]);

    const result = await ensureBashFullFlag(ctx, run);

    expect(calls).toHaveLength(2);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.path).toBe(configPath);

    const text = readFileSync(configPath, 'utf-8');
    expect(text).toContain('bash_full: true');
    expect(text).toContain('api_base: https://example/v1');
    expect(text).toContain('ews-mcp');
  });

  it('surfaces the failure when the fallback write also fails', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([
      { code: 3, stdout: '', stderr: 'no such key\n' },
      errEnvelope(
        'security.bash_full',
        'invalid_value',
        "mcp_servers.ews-mcp.stdio: Value error, mcp stdio server requires 'command' or 'docker_profile'",
        4,
      ),
    ]);

    const result = await ensureBashFullFlag(ctx, run);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toMatch(/fallback also failed/);
    }
  });
});

describe('getAgentConfigValue', () => {
  it('returns the value from a {value: ...} JSON envelope', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { calls, run } = recordingRunner([
      { code: 0, stdout: JSON.stringify({ ok: true, value: SECRET }) + '\n', stderr: '' },
    ]);

    const value = await getAgentConfigValue(ctx, 'llm.api_key', run);

    expect(value).toBe(SECRET);
    expect(calls).toEqual([
      {
        bin: managedAgentPath(ctx),
        args: ['config', 'get', 'llm.api_key'],
        stdin: '',
        env: ctx.env,
      },
    ]);
  });

  it('falls back to trimmed raw stdout when the output is not a JSON envelope', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([{ code: 0, stdout: `${SECRET}\n`, stderr: '' }]);

    expect(await getAgentConfigValue(ctx, 'llm.api_key', run)).toBe(SECRET);
  });

  it('returns null when nothing is configured (empty output)', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([{ code: 0, stdout: '\n', stderr: '' }]);

    expect(await getAgentConfigValue(ctx, 'llm.api_key', run)).toBeNull();
  });

  it('returns null on a non-zero exit, never throwing', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([{ code: 3, stdout: '', stderr: 'no such key\n' }]);

    expect(await getAgentConfigValue(ctx, 'llm.api_key', run)).toBeNull();
  });

  it('returns null on a spawn failure', async () => {
    const ctx = ctxFor();
    fakeManagedBinary(ctx, '#!/bin/sh\n');
    const { run } = recordingRunner([{ code: null, stdout: '', stderr: '', spawnError: 'ENOENT' }]);

    expect(await getAgentConfigValue(ctx, 'llm.api_key', run)).toBeNull();
  });

  it('returns null when no managed runtime is installed', async () => {
    const ctx = ctxFor();
    const run = vi.fn();

    expect(await getAgentConfigValue(ctx, 'llm.api_key', run)).toBeNull();
    expect(run).not.toHaveBeenCalled();
  });
});

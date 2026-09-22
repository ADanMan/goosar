import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { managedAgentPath } from './agent-bootstrap';
import { readConfigFieldsFromFile, writeLlmFieldsDirectly } from './agent-config-yaml-fallback';
import {
  EMPTY_CLIENT_SECRETS_SYNC_STATE,
  fetchDeploymentClientSecrets,
  sha256Hex,
  syncDeploymentClientSecrets,
  toAgentModelId,
  type ClientSecretsSyncState,
} from './agent-client-secrets';
import type { AgentCommandOutcome, AgentConfigCommand, AgentPathContext } from './agent-config';
import type { ProvisioningAuth } from './provisioning';

const auth: ProvisioningAuth = {
  apiBaseUrl: 'https://goosar.example',
  token: 'gsl_test-token',
  workspaceId: '11111111-1111-1111-1111-111111111111',
};

function jsonResponse(status: number, body: unknown): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  } as unknown as Response;
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

function getValueOutcome(value: string | null): AgentCommandOutcome {
  return {
    code: value === null ? 1 : 0,
    stdout: value === null ? '' : JSON.stringify({ value }) + '\n',
    stderr: '',
  };
}

function setOkOutcome(key: string): AgentCommandOutcome {
  return {
    code: 0,
    stdout: JSON.stringify({ ok: true, key, path: '/tmp/config.user.yaml' }) + '\n',
    stderr: '',
  };
}

function invalidValueOutsideLlmOutcome(key: string): AgentCommandOutcome {
  const message = "mcp_servers.ews-mcp.stdio: field required 'command'";
  return {
    code: 4,
    stdout: JSON.stringify({ ok: false, key, error: { kind: 'invalid_value', message } }) + '\n',
    stderr: '',
  };
}

vi.mock('./agent-config-yaml-fallback', () => ({
  writeLlmFieldsDirectly: vi.fn(),
  readConfigFieldsFromFile: vi.fn(),
}));

let home: string;
let ctx: AgentPathContext;

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'hermes-client-secrets-'));
  ctx = { home, env: { HOME: home } };
  const bin = managedAgentPath(ctx);
  mkdirSync(join(home, '.hermes', 'runtime', 'current', 'bin'), {
    recursive: true,
  });
  writeFileSync(bin, '#!/bin/sh\nexit 0\n');
  chmodSync(bin, 0o755);
});

afterEach(() => {
  rmSync(home, { recursive: true, force: true });
});

describe('fetchDeploymentClientSecrets', () => {
  it('reports forbidden on 403', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(403, { error: 'no' }));
    const result = await fetchDeploymentClientSecrets(auth, fetchImpl);
    expect(result).toEqual({ kind: 'forbidden' });
  });

  it('reports error on network failure', async () => {
    const fetchImpl = vi.fn(async () => {
      throw new Error('ECONNREFUSED');
    });
    const result = await fetchDeploymentClientSecrets(auth, fetchImpl);
    expect(result.kind).toBe('error');
  });

  it('reports error on a malformed llm block', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(200, { llm: { api_base: 123 } }));
    const result = await fetchDeploymentClientSecrets(auth, fetchImpl);
    expect(result.kind).toBe('error');
  });

  it('parses a valid response, including a null llm block', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(200, { llm: null }));
    const result = await fetchDeploymentClientSecrets(auth, fetchImpl);
    expect(result).toEqual({ kind: 'ok', llm: null });
  });

  it('sends the managed-runtime header and auth', async () => {
    const fetchImpl = vi.fn(async (_input: string, _init?: RequestInit) =>
      jsonResponse(200, { llm: null }),
    );
    await fetchDeploymentClientSecrets(auth, fetchImpl);
    const [, init] = fetchImpl.mock.calls[0];
    const headers = (init as RequestInit).headers as Record<string, string>;
    expect(headers['X-Goosar-Launched-By']).toBe('desktop');
    expect(headers.Authorization).toBe('Bearer gsl_test-token');
    expect(headers['X-Workspace-ID']).toBe(auth.workspaceId);
  });
});

function fileReader(responses: Array<Record<string, string | null>>) {
  let call = 0;
  return vi.fn((_ctx: AgentPathContext, fields: readonly string[]) => {
    const next = responses[call];
    call += 1;
    if (!next) throw new Error(`unexpected extra readConfigFieldsFromFile call #${call}`);
    const result: Record<string, string | null> = {};
    for (const f of fields) result[f] = next[f] ?? null;
    return result;
  });
}

describe('syncDeploymentClientSecrets', () => {
  beforeEach(() => {
    vi.mocked(readConfigFieldsFromFile).mockReset();
    vi.mocked(writeLlmFieldsDirectly).mockReset();
  });

  it('does not throw and reports unavailability on 403', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(403, {}));
    const { run } = recordingRunner([]);
    const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
    expect(result.fieldsWritten).toEqual([]);
    expect(result.note).toMatch(/недоступны/);
    expect(run).not.toHaveBeenCalled();
  });

  it('does not throw and reports unavailability on a network error', async () => {
    const fetchImpl = vi.fn(async () => {
      throw new Error('offline');
    });
    const { run } = recordingRunner([]);
    const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
    expect(result.fieldsWritten).toEqual([]);
    expect(result.note).toMatch(/недоступны/);
  });

  it('writes all three fields on a clean install with nothing set', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, {
        llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
      }),
    );
    vi.mocked(readConfigFieldsFromFile).mockImplementation(
      fileReader([{ 'llm.api_key': null }, { 'llm.api_key': 'sk-abc' }]),
    );
    const { run, calls } = recordingRunner([
      getValueOutcome(null), // get llm.api_base
      getValueOutcome(null), // get llm.model
      setOkOutcome('llm.api_base'),
      setOkOutcome('llm.model'),
      setOkOutcome('llm.api_key'),
      getValueOutcome('https://gw.example/v1'), // read-back verification
      getValueOutcome('openai/gpt-5'),
    ]);
    const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
    expect(result.fieldsWritten).toEqual(['llm.api_base', 'llm.model', 'llm.api_key']);
    expect(result.nextState.issuedApiKeyHash).toBe(sha256Hex('sk-abc'));
    expect(calls.filter((c) => c.args.includes('set')).map((c) => c.args[2])).toEqual([
      'llm.api_base',
      'llm.model',
      'llm.api_key',
    ]);
  });

  it('never overwrites a value the user already set', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, {
        llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
      }),
    );
    vi.mocked(readConfigFieldsFromFile).mockImplementation(
      fileReader([{ 'llm.api_key': 'sk-user-own-key' }]),
    );
    const { run, calls } = recordingRunner([
      getValueOutcome('https://user-own-gateway.example/v1'),
      getValueOutcome('user-own-model'),
    ]);
    const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
    expect(result.fieldsWritten).toEqual([]);
    expect(calls.every((c) => c.args[1] === 'get')).toBe(true);
  });

  it('is idempotent: a second run against already-written values changes nothing', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, {
        llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
      }),
    );
    const state: ClientSecretsSyncState = { issuedApiKeyHash: sha256Hex('sk-abc') };
    vi.mocked(readConfigFieldsFromFile).mockImplementation(
      fileReader([{ 'llm.api_key': 'sk-abc' }]),
    );
    const { run } = recordingRunner([
      getValueOutcome('https://gw.example/v1'),
      getValueOutcome('gpt-5'),
    ]);
    const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run, state);
    expect(result.fieldsWritten).toEqual([]);
    expect(result.nextState).toEqual(state);
  });

  it('overwrites a previously-issued api key on rotation', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, {
        llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-NEW' },
      }),
    );
    const state: ClientSecretsSyncState = { issuedApiKeyHash: sha256Hex('sk-OLD') };
    vi.mocked(readConfigFieldsFromFile).mockImplementation(
      fileReader([{ 'llm.api_key': 'sk-OLD' }, { 'llm.api_key': 'sk-NEW' }]),
    );
    const { run, calls } = recordingRunner([
      getValueOutcome('https://gw.example/v1'),
      getValueOutcome('gpt-5'),
      setOkOutcome('llm.api_key'),
    ]);
    const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run, state);
    expect(result.fieldsWritten).toEqual(['llm.api_key']);
    expect(result.nextState.issuedApiKeyHash).toBe(sha256Hex('sk-NEW'));
    const setCall = calls.find((c) => c.args[1] === 'set');
    expect(setCall?.stdin).toBe('sk-NEW');
  });

  it('does not treat an unrelated user-typed key as ours to rotate', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, {
        llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-NEW' },
      }),
    );
    vi.mocked(readConfigFieldsFromFile).mockImplementation(
      fileReader([{ 'llm.api_key': 'sk-user-typed-this-themselves' }]),
    );
    const { run } = recordingRunner([
      getValueOutcome('https://gw.example/v1'),
      getValueOutcome('gpt-5'),
    ]);
    const result = await syncDeploymentClientSecrets(
      ctx,
      auth,
      fetchImpl,
      run,
      EMPTY_CLIENT_SECRETS_SYNC_STATE,
    );
    expect(result.fieldsWritten).toEqual([]);
  });

  it('writes the key when the file value is masked/unreadable and none has ever been issued', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, {
        llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
      }),
    );
    vi.mocked(readConfigFieldsFromFile).mockImplementation(
      fileReader([{ 'llm.api_key': '***' }, { 'llm.api_key': 'sk-abc' }]),
    );
    const { run } = recordingRunner([
      getValueOutcome('https://gw.example/v1'),
      getValueOutcome('gpt-5'),
      setOkOutcome('llm.api_key'),
    ]);
    const result = await syncDeploymentClientSecrets(
      ctx,
      auth,
      fetchImpl,
      run,
      EMPTY_CLIENT_SECRETS_SYNC_STATE,
    );
    expect(result.fieldsWritten).toEqual(['llm.api_key']);
    expect(result.note).toMatch(/llm\.api_key/);
  });

  it('writes the placeholder key from a clean install even though `config get` would mask it', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, {
        llm: {
          api_base: 'https://gw.example/v1',
          model: 'gpt-5',
          api_key: 'sk-issued-by-deployment',
        },
      }),
    );
    vi.mocked(readConfigFieldsFromFile).mockImplementation(
      fileReader([
        { 'llm.api_key': 'REPLACE_WITH_YOUR_KEY' },
        { 'llm.api_key': 'sk-issued-by-deployment' },
      ]),
    );
    const { run } = recordingRunner([
      getValueOutcome('https://your-gateway.example/v1'),
      getValueOutcome('openai/glm-4.6'),
      setOkOutcome('llm.api_base'),
      setOkOutcome('llm.model'),
      setOkOutcome('llm.api_key'),
      getValueOutcome('https://gw.example/v1'),
      getValueOutcome('openai/gpt-5'),
    ]);
    const result = await syncDeploymentClientSecrets(
      ctx,
      auth,
      fetchImpl,
      run,
      EMPTY_CLIENT_SECRETS_SYNC_STATE,
    );
    expect(result.fieldsWritten).toEqual(['llm.api_base', 'llm.model', 'llm.api_key']);
    expect(result.nextState.issuedApiKeyHash).toBe(sha256Hex('sk-issued-by-deployment'));
  });

  it('reports a message and leaves state untouched when the deployment has no llm key', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(200, { llm: null }));
    const { run } = recordingRunner([]);
    const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
    expect(result.fieldsWritten).toEqual([]);
    expect(run).not.toHaveBeenCalled();
  });

  describe('placeholder-aware sync (#723)', () => {
    it('overwrites the placeholder api_base host, model, and key all at once', async () => {
      const fetchImpl = vi.fn(async () =>
        jsonResponse(200, {
          llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
        }),
      );
      vi.mocked(readConfigFieldsFromFile).mockImplementation(
        fileReader([{ 'llm.api_key': 'REPLACE_WITH_YOUR_KEY' }, { 'llm.api_key': 'sk-abc' }]),
      );
      const { run, calls } = recordingRunner([
        getValueOutcome('https://your-gateway.example/v1'),
        getValueOutcome('openai/glm-4.6'),
        setOkOutcome('llm.api_base'),
        setOkOutcome('llm.model'),
        setOkOutcome('llm.api_key'),
        getValueOutcome('https://gw.example/v1'),
        getValueOutcome('openai/gpt-5'),
      ]);
      const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
      expect(result.fieldsWritten).toEqual(['llm.api_base', 'llm.model', 'llm.api_key']);
      expect(calls.filter((c) => c.args.includes('set')).map((c) => c.args[2])).toEqual([
        'llm.api_base',
        'llm.model',
        'llm.api_key',
      ]);
    });

    it('does not treat a real gateway host that merely contains the placeholder as a placeholder', async () => {
      const fetchImpl = vi.fn(async () =>
        jsonResponse(200, {
          llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
        }),
      );
      vi.mocked(readConfigFieldsFromFile).mockImplementation(
        fileReader([{ 'llm.api_key': 'sk-user-own-key' }]),
      );
      const { run } = recordingRunner([
        getValueOutcome('https://your-gateway.example.corp.com/v1'),
        getValueOutcome('user-own-model'),
      ]);
      const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
      expect(result.fieldsWritten).toEqual([]);
    });
  });

  describe('write verification (#723)', () => {
    it('does not report a field as written if the read-back value does not match', async () => {
      const fetchImpl = vi.fn(async () =>
        jsonResponse(200, {
          llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
        }),
      );
      vi.mocked(readConfigFieldsFromFile).mockImplementation(
        fileReader([{ 'llm.api_key': null }, { 'llm.api_key': 'sk-abc' }]),
      );
      const { run } = recordingRunner([
        getValueOutcome(null), // get llm.api_base
        getValueOutcome(null), // get llm.model
        setOkOutcome('llm.api_base'),
        setOkOutcome('llm.model'),
        setOkOutcome('llm.api_key'),
        getValueOutcome('https://your-gateway.example/v1'),
        getValueOutcome('openai/gpt-5'),
      ]);
      const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
      expect(result.fieldsWritten).toEqual(['llm.model', 'llm.api_key']);
      expect(result.note).toMatch(/llm\.model, llm\.api_key/);
    });
  });

  describe('YAML fallback on validation failure outside llm.* (#723)', () => {
    beforeEach(() => {
      vi.mocked(writeLlmFieldsDirectly).mockReset();
      vi.mocked(readConfigFieldsFromFile).mockReset();
    });

    it('falls back to a direct YAML write when config set fails on an unrelated block', async () => {
      const fetchImpl = vi.fn(async () =>
        jsonResponse(200, {
          llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
        }),
      );
      vi.mocked(writeLlmFieldsDirectly).mockResolvedValue({ ok: true });
      vi.mocked(readConfigFieldsFromFile).mockImplementation(
        fileReader([
          { 'llm.api_key': null },
          {
            'llm.api_base': 'https://gw.example/v1',
            'llm.model': 'openai/gpt-5',
            'llm.api_key': 'sk-abc',
          },
        ]),
      );
      const { run } = recordingRunner([
        getValueOutcome(null),
        getValueOutcome(null),
        invalidValueOutsideLlmOutcome('llm.api_base'),
      ]);
      const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
      expect(writeLlmFieldsDirectly).toHaveBeenCalledTimes(1);
      expect(readConfigFieldsFromFile).toHaveBeenCalledTimes(2);
      expect(result.fieldsWritten).toEqual(['llm.api_base', 'llm.model', 'llm.api_key']);
      expect(result.note).toMatch(/напрямую в YAML \(проверено по файлу\)/);
    });

    it('confirms the fallback write via the file even when `config get` would also fail on the foreign block (#730)', async () => {
      const fetchImpl = vi.fn(async () =>
        jsonResponse(200, {
          llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
        }),
      );
      vi.mocked(writeLlmFieldsDirectly).mockResolvedValue({ ok: true });
      vi.mocked(readConfigFieldsFromFile).mockImplementation(
        fileReader([
          { 'llm.api_key': null },
          {
            'llm.api_base': 'https://gw.example/v1',
            'llm.model': 'openai/gpt-5',
            'llm.api_key': 'sk-abc',
          },
        ]),
      );
      const { run, calls } = recordingRunner([
        getValueOutcome(null),
        getValueOutcome(null),
        invalidValueOutsideLlmOutcome('llm.api_base'),
      ]);
      const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
      expect(calls).toHaveLength(3);
      expect(result.fieldsWritten).toEqual(['llm.api_base', 'llm.model', 'llm.api_key']);
      expect(result.note).not.toMatch(/не удалось подтвердить/);
      expect(result.note).toMatch(/проверено по файлу/);
    });

    it('reports failure when both config set and the YAML fallback fail', async () => {
      const fetchImpl = vi.fn(async () =>
        jsonResponse(200, {
          llm: { api_base: 'https://gw.example/v1', model: 'gpt-5', api_key: 'sk-abc' },
        }),
      );
      vi.mocked(writeLlmFieldsDirectly).mockResolvedValue({
        ok: false,
        message: 'could not write config file: EACCES',
      });
      vi.mocked(readConfigFieldsFromFile).mockImplementation(fileReader([{ 'llm.api_key': null }]));
      const { run } = recordingRunner([
        getValueOutcome(null),
        getValueOutcome(null),
        invalidValueOutsideLlmOutcome('llm.api_base'),
      ]);
      const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
      expect(result.fieldsWritten).toEqual([]);
      expect(result.note).toMatch(/EACCES/);
    });

    it('does not use the YAML fallback when the rejection is about the llm.* field itself', async () => {
      const fetchImpl = vi.fn(async () =>
        jsonResponse(200, {
          llm: { api_base: 'not-a-valid-url', model: 'gpt-5', api_key: 'sk-abc' },
        }),
      );
      vi.mocked(readConfigFieldsFromFile).mockImplementation(fileReader([{ 'llm.api_key': null }]));
      const { run } = recordingRunner([
        getValueOutcome(null),
        getValueOutcome(null),
        {
          code: 4,
          stdout:
            JSON.stringify({
              ok: false,
              key: 'llm.api_base',
              error: { kind: 'invalid_value', message: 'llm.api_base: must be a valid URL' },
            }) + '\n',
          stderr: '',
        },
      ]);
      const result = await syncDeploymentClientSecrets(ctx, auth, fetchImpl, run);
      expect(writeLlmFieldsDirectly).not.toHaveBeenCalled();
      expect(result.fieldsWritten).toEqual([]);
      expect(result.note).toMatch(/не удалось записать секреты деплоя/);
    });
  });
});

describe('toAgentModelId', () => {
  it('prefixes a bare model with openai/', () => {
    expect(toAgentModelId('gpt-5')).toBe('openai/gpt-5');
  });

  it('leaves an already-prefixed model unchanged', () => {
    expect(toAgentModelId('anthropic/claude-4')).toBe('anthropic/claude-4');
  });

  it('leaves an empty model unchanged', () => {
    expect(toAgentModelId('')).toBe('');
  });
});

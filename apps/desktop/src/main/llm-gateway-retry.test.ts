import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

vi.mock('electron', () => ({
  app: {
    getAppPath: () => '/nonexistent-app-path',
    isPackaged: false,
    on: vi.fn(),
    quit: vi.fn(),
  },
  ipcMain: { handle: vi.fn(), on: vi.fn() },
  BrowserWindow: class {},
  shell: { openPath: vi.fn() },
  session: { fromPartition: vi.fn(), defaultSession: {} },
  powerMonitor: { on: vi.fn() },
}));

let tempDir = '';
let configPath = '';

function writeConfig(apiBase: string, apiKey = 'sk-test') {
  writeFileSync(
    configPath,
    `llm:\n  api_base: "${apiBase}"\n  api_key: "${apiKey}"\n  model: "gpt-test"\n`,
    'utf-8',
  );
}

beforeEach(() => {
  tempDir = mkdtempSync(join(tmpdir(), 'llm-gateway-retry-'));
  configPath = join(tempDir, 'config.user.yaml');
  process.env.HERMES_CONFIG_PATH = configPath;
  vi.resetModules();
});

afterEach(() => {
  delete process.env.HERMES_CONFIG_PATH;
  rmSync(tempDir, { recursive: true, force: true });
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('retryLlmGatewayRowStatus (T-14, #698)', () => {
  it('re-fetches client-secrets BEFORE reprobing, in that order', async () => {
    writeConfig('https://llm.example.com/v1');
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('', { status: 200 })),
    );
    const { retryLlmGatewayRowStatus, setResyncClientSecrets } = await import('./perimeter');

    const calls: string[] = [];
    setResyncClientSecrets(async () => {
      calls.push('client-secrets');
      return { deploymentApiBase: 'https://api.deepseek.com/v1' };
    });
    const fetchSpy = globalThis.fetch as ReturnType<typeof vi.fn>;
    fetchSpy.mockImplementation(async () => {
      calls.push('probe');
      return new Response('', { status: 200 });
    });

    await retryLlmGatewayRowStatus();

    expect(calls).toEqual(['client-secrets', 'probe']);
  });

  it('names the host that was actually probed', async () => {
    writeConfig('https://llm.example.com/v1');
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new Error('network unreachable');
      }),
    );
    const { retryLlmGatewayRowStatus, setResyncClientSecrets } = await import('./perimeter');
    setResyncClientSecrets(async () => ({ deploymentApiBase: null }));

    const status = await retryLlmGatewayRowStatus();

    expect(status.verdict).toBe('unreachable');
    expect(status.host).toBe('llm.example.com');
  });

  it("hints switching to the stand's address when the agent's failing host differs from the deployment's", async () => {
    writeConfig('https://llm.example.com/v1');
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new Error('timeout');
      }),
    );
    const { retryLlmGatewayRowStatus, setResyncClientSecrets } = await import('./perimeter');
    setResyncClientSecrets(async () => ({
      deploymentApiBase: 'https://api.deepseek.com/v1',
    }));

    const status = await retryLlmGatewayRowStatus();

    expect(status.verdict).toBe('unreachable');
    expect(status.host).toBe('llm.example.com');
    expect(status.standApiBaseHint).toBe('api.deepseek.com');
  });

  it("does not hint when the agent's host already matches the deployment's", async () => {
    writeConfig('https://api.deepseek.com/v1');
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('', { status: 401 })),
    );
    const { retryLlmGatewayRowStatus, setResyncClientSecrets } = await import('./perimeter');
    setResyncClientSecrets(async () => ({
      deploymentApiBase: 'https://api.deepseek.com/v1',
    }));

    const status = await retryLlmGatewayRowStatus();

    expect(status.verdict).toBe('auth_rejected');
    expect(status.standApiBaseHint).toBeUndefined();
  });

  it('works with no resync callback wired (falls back to a plain reprobe)', async () => {
    writeConfig('https://api.deepseek.com/v1');
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('', { status: 200 })),
    );
    const { retryLlmGatewayRowStatus } = await import('./perimeter');

    const status = await retryLlmGatewayRowStatus();

    expect(status.verdict).toBe('ok');
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

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
}));

vi.mock('./cli-bootstrap', () => ({
  managedCliPath: () => '/nonexistent/goosar',
  ensureManagedCli: vi.fn(async () => '/nonexistent/goosar'),
}));

vi.mock('./agent-runner-preference', () => ({
  loadAgentRunner: vi.fn(async () => 'hermes'),
  saveAgentRunner: vi.fn(async () => {}),
}));

vi.mock('./agent-bootstrap', () => ({
  readCaBundleStatus: () => ({ caBundlePresent: false, corpCaPresent: false }),
  caBundleFilePath: () => '/nonexistent/ca-bundle.pem',
  corpCaFilePath: () => '/nonexistent/corp-ca.pem',
  agentHomeDir: () => '/nonexistent/.hermes',
  resolveAgentConfigPath: () => null,
  readLlmProfileScalars: () => ({ apiBase: null, model: null, apiKey: null }),
  ensureAgentRuntime: vi.fn(async () => ({ state: 'needs_config' })),
  readAgentRuntimeStatus: vi.fn(async () => ({ state: 'needs_config' })),
  agentPathEnvValue: () => null,
  runtimeRoot: () => '/nonexistent/runtime',
}));

const secrets = vi.hoisted(() => ({
  sync: vi.fn(),
}));

vi.mock('./agent-client-secrets', () => ({
  syncDeploymentClientSecrets: secrets.sync,
}));

const provisioningState = vi.hoisted(() => ({
  apiBaseUrl: null as string | null,
  token: null as string | null,
  workspaceId: null as string | null,
}));

vi.mock('./provisioning', () => ({
  bindProvisioningCredentials: vi.fn(),
  provisioningAuthGap: () => {
    if (!provisioningState.apiBaseUrl || !provisioningState.token) return 'no auth';
    if (!provisioningState.workspaceId) return 'no workspace';
    return null;
  },
  resolvedProvisioningAuth: () => {
    if (
      !provisioningState.apiBaseUrl ||
      !provisioningState.token ||
      !provisioningState.workspaceId
    ) {
      return null;
    }
    return {
      apiBaseUrl: provisioningState.apiBaseUrl,
      token: provisioningState.token,
      workspaceId: provisioningState.workspaceId,
    };
  },
  setProvisioningCredentials: vi.fn(),
  setProvisioningRestartPending: vi.fn(),
  syncProvisionedPackages: vi.fn(async () => {}),
  resolveProvisionedRuntimeDir: vi.fn(() => null),
}));

let daemon: typeof import('./daemon-manager');

beforeEach(async () => {
  vi.resetModules();
  provisioningState.apiBaseUrl = null;
  provisioningState.token = null;
  provisioningState.workspaceId = null;
  secrets.sync.mockReset();
  secrets.sync.mockResolvedValue({
    note: 'секреты деплоя записаны: llm.api_base, llm.model, llm.api_key',
    fieldsWritten: ['llm.api_base', 'llm.model', 'llm.api_key'],
    nextState: { issuedApiKeyHash: 'hash' },
    deploymentApiBase: 'https://gw.example/v1',
  });
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => {
      throw new Error('unexpected fetch in test');
    }),
  );
  daemon = await import('./daemon-manager');
  daemon.setupDaemonManager(() => null);
  await new Promise((r) => setTimeout(r, 0));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('client-secrets sync ordering (#734)', () => {
  it('writes llm.* once the workspace binds, without needing a restart', async () => {
    provisioningState.apiBaseUrl = 'https://goosar.example';
    provisioningState.token = 'gsl_test-token';

    let resolveBind!: () => void;
    const bindPromise = new Promise<void>((resolve) => {
      resolveBind = () => {
        provisioningState.workspaceId = '11111111-1111-1111-1111-111111111111';
        resolve();
      };
    });

    const pending = daemon.bindProvisioningThenSyncSecrets(bindPromise);

    expect(secrets.sync).not.toHaveBeenCalled();

    resolveBind();
    const result = await pending;

    expect(secrets.sync).toHaveBeenCalledTimes(1);
    expect(result?.fieldsWritten).toEqual(['llm.api_base', 'llm.model', 'llm.api_key']);
  });

  it('logs each skip reason only once', async () => {
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
    expect(daemon.clientSecretsSyncSkipReason()).toBe('no auth');

    provisioningState.apiBaseUrl = 'https://goosar.example';
    provisioningState.token = 'gsl_test-token';
    expect(daemon.clientSecretsSyncSkipReason()).toBe('no workspace');

    logSpy.mockRestore();
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

type ExecFileCallback = (err: Error | null, stdout: string, stderr: string) => void;

const harness = vi.hoisted(() => ({
  ipcHandlers: new Map<string, (...args: unknown[]) => unknown>(),
  execCalls: [] as string[][],
  storedRunner: 'none' as string,
  saved: [] as string[],
  ensureAgentRuntime: vi.fn(),
  bundledVersion: null as string | null,
}));

vi.mock('electron', () => ({
  app: {
    getAppPath: () => '/nonexistent-app-path',
    isPackaged: false,
    on: vi.fn(),
    quit: vi.fn(),
  },
  ipcMain: {
    handle: vi.fn((channel: string, fn: (...args: unknown[]) => unknown) => {
      harness.ipcHandlers.set(channel, fn);
    }),
    on: vi.fn(),
  },
  BrowserWindow: class {},
  shell: { openPath: vi.fn() },
}));

vi.mock('child_process', () => {
  const execFile = vi.fn((bin: string, args: string[], _opts: unknown, cb: ExecFileCallback) => {
    harness.execCalls.push([bin, ...args]);
    if (args[0] === 'version') {
      cb(null, JSON.stringify({ version: '9.9.9' }), '');
      return;
    }
    cb(null, '', '');
  });
  return { execFile, default: { execFile } };
});

vi.mock('./cli-bootstrap', () => ({
  managedCliPath: () => '/nonexistent/goosar',
  ensureManagedCli: vi.fn(async () => '/nonexistent/goosar'),
}));

vi.mock('./agent-runner-preference', async () => {
  const actual = await vi.importActual<typeof import('./agent-runner-preference')>(
    './agent-runner-preference',
  );
  return {
    loadAgentRunner: vi.fn(async () => actual.normalizeAgentRunner(harness.storedRunner)),
    saveAgentRunner: vi.fn(async (_path: string, choice: string) => {
      harness.saved.push(choice);
      harness.storedRunner = choice;
    }),
  };
});

vi.mock('./agent-bootstrap', () => ({
  readCaBundleStatus: () => ({ caBundlePresent: false, corpCaPresent: false }),
  caBundleFilePath: () => '/nonexistent/ca-bundle.pem',
  corpCaFilePath: () => '/nonexistent/corp-ca.pem',
  agentHomeDir: () => '/nonexistent/.hermes',
  resolveAgentConfigPath: () => null,
  readLlmProfileScalars: () => ({ apiBase: null, model: null, apiKey: null }),
  ensureAgentRuntime: (...args: unknown[]) => harness.ensureAgentRuntime(...args),
  bundledAgentVersion: vi.fn(async () => harness.bundledVersion),
  readAgentRuntimeStatus: vi.fn(async () => ({ state: 'checking' })),
  agentPathEnvValue: () => null,
  runtimeRoot: () => '/nonexistent/runtime',
}));

vi.mock('./mcp-servers', () => ({
  ensureBundledMcpServers: vi.fn(async () => ({ staged: [], refused: [] })),
  appendStagedMcpServerPath: (path: string | undefined) => path,
}));

vi.mock('./skills', () => ({
  ensureBundledSkills: vi.fn(async () => ({ staged: [], refused: [] })),
  playwrightBrowsersEnv: () => ({}),
}));

vi.mock('./provisioning', () => ({
  bindProvisioningCredentials: vi.fn(),
  provisioningAuthGap: () => 'no auth',
  resolvedProvisioningAuth: () => null,
  resolveProvisionedRuntimeDir: () => null,
  setProvisioningCredentials: vi.fn(),
  setProvisioningRestartPending: vi.fn(),
  syncProvisionedPackages: vi.fn(async () => {}),
}));

const READY = { state: 'ready', version: '9.9.9', binPath: '/x', configPath: '/y' };

type SetResult = {
  success: boolean;
  error?: string;
  choice: string;
  runtime: { state: string };
  daemonRestart?: string;
};

let daemon: typeof import('./daemon-manager');

async function boot(): Promise<void> {
  vi.resetModules();
  daemon = await import('./daemon-manager');
  daemon.setupDaemonManager(() => null);
  await harness.ipcHandlers.get('daemon:get-agent-runtime')?.();
}

beforeEach(() => {
  harness.ipcHandlers.clear();
  harness.execCalls.length = 0;
  harness.saved.length = 0;
  harness.storedRunner = 'none';
  harness.bundledVersion = null;
  harness.ensureAgentRuntime.mockReset();
  harness.ensureAgentRuntime.mockResolvedValue(READY);
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => {
      throw new Error('no network in tests');
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('agent runner preference at launch', () => {
  it('does not run ensureAgentRuntime by default and reports a neutral status', async () => {
    await boot();

    expect(harness.ensureAgentRuntime).not.toHaveBeenCalled();
    const status = (await harness.ipcHandlers.get('daemon:get-agent-runtime')?.()) as {
      state: string;
    };
    expect(status.state).toBe('not_installed');
  });

  it('still syncs provisioned packages when no runner is selected', async () => {
    await boot();

    const provisioning = await import('./provisioning');
    expect(provisioning.syncProvisionedPackages).toHaveBeenCalled();
    expect(harness.ensureAgentRuntime).not.toHaveBeenCalled();
  });

  it('starts the daemon without a runner when the preference is none', async () => {
    await boot();

    const result = (await harness.ipcHandlers.get('daemon:start')?.()) as { success: boolean };

    expect(result.success).toBe(true);
    expect(harness.execCalls.some((call) => call.includes('start'))).toBe(true);
    expect(harness.ensureAgentRuntime).not.toHaveBeenCalled();
  });

  it('treats an unknown persisted value as none', async () => {
    harness.storedRunner = 'openclaw-legacy';
    await boot();

    expect(harness.ensureAgentRuntime).not.toHaveBeenCalled();
    const state = (await harness.ipcHandlers.get('daemon:get-agent-runner')?.()) as {
      choice: string;
    };
    expect(state.choice).toBe('none');
  });

  it('runs ensureAgentRuntime at launch when the preference is hermes', async () => {
    harness.storedRunner = 'hermes';
    await boot();

    expect(harness.ensureAgentRuntime).toHaveBeenCalledTimes(1);
    const status = (await harness.ipcHandlers.get('daemon:get-agent-runtime')?.()) as {
      state: string;
    };
    expect(status.state).toBe('ready');
  });
});

describe('daemon:get-agent-runner', () => {
  it('returns the choice and whether the bundled runner payload is available', async () => {
    harness.bundledVersion = '1.2.3';
    await boot();

    const state = await harness.ipcHandlers.get('daemon:get-agent-runner')?.();

    expect(state).toEqual({ choice: 'none', bundledAvailable: false, bundledVersion: '1.2.3' });
  });
});

describe('daemon:set-agent-runner', () => {
  it('persists hermes, runs ensureAgentRuntime and reports the runtime and daemon outcome', async () => {
    await boot();
    expect(harness.ensureAgentRuntime).not.toHaveBeenCalled();

    const result = (await harness.ipcHandlers.get('daemon:set-agent-runner')?.(
      {},
      'hermes',
    )) as SetResult;

    expect(harness.saved).toEqual(['hermes']);
    expect(harness.ensureAgentRuntime).toHaveBeenCalledTimes(1);
    expect(result.success).toBe(true);
    expect(result.choice).toBe('hermes');
    expect(result.runtime.state).toBe('ready');
    expect(result.daemonRestart).toBe('not_running');
    expect(harness.execCalls.some((call) => call.includes('stop'))).toBe(false);
  });

  it('persists none without running ensureAgentRuntime and drops a previously ready status', async () => {
    harness.storedRunner = 'hermes';
    await boot();
    harness.ensureAgentRuntime.mockClear();

    const result = (await harness.ipcHandlers.get('daemon:set-agent-runner')?.(
      {},
      'none',
    )) as SetResult;

    expect(harness.saved).toEqual(['none']);
    expect(harness.ensureAgentRuntime).not.toHaveBeenCalled();
    expect(result.success).toBe(true);
    expect(result.runtime.state).toBe('not_installed');
    expect(result.daemonRestart).toBeUndefined();
    const status = (await harness.ipcHandlers.get('daemon:get-agent-runtime')?.()) as {
      state: string;
    };
    expect(status.state).toBe('not_installed');
  });

  it('rejects an unknown choice without persisting it', async () => {
    await boot();

    const result = (await harness.ipcHandlers.get('daemon:set-agent-runner')?.(
      {},
      'something-else',
    )) as SetResult;

    expect(result.success).toBe(false);
    expect(result.choice).toBe('none');
    expect(harness.saved).toEqual([]);
    expect(harness.ensureAgentRuntime).not.toHaveBeenCalled();
  });
});

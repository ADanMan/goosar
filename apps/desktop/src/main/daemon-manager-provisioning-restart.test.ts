import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

type ExecFileCallback = (err: Error | null, stdout: string, stderr: string) => void;

const harness = vi.hoisted(() => ({
  execCalls: [] as string[][],
  health: { status: 'running', active_task_count: 0 } as {
    status: string;
    active_task_count: number;
    os?: string;
  },
}));

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

vi.mock('child_process', () => {
  const execFile = vi.fn((bin: string, args: string[], _opts: unknown, cb: ExecFileCallback) => {
    harness.execCalls.push([bin, ...args]);
    if (args[0] === 'version') {
      cb(null, JSON.stringify({ version: '9.9.9' }), '');
      return;
    }
    if (args.includes('stop')) {
      harness.health = { ...harness.health, status: 'stopped' };
    }
    cb(null, '', '');
  });
  return { execFile, default: { execFile } };
});

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
  ensureAgentRuntime: vi.fn(async () => ({
    state: 'ready',
    version: '9.9.9',
    binPath: '/x',
    configPath: '/y',
  })),
  readAgentRuntimeStatus: vi.fn(async () => ({ state: 'ready' })),
  agentPathEnvValue: () => null,
  runtimeRoot: () => '/nonexistent/runtime',
}));

let daemon: typeof import('./daemon-manager');
let provisioning: typeof import('./provisioning');

beforeEach(async () => {
  harness.execCalls.length = 0;
  harness.health = { status: 'running', active_task_count: 0 };
  vi.resetModules();
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (String(url).includes('/health')) {
        return new Response(JSON.stringify(harness.health), { status: 200 });
      }
      throw new Error(`unexpected fetch in test: ${url}`);
    }),
  );
  daemon = await import('./daemon-manager');
  provisioning = await import('./provisioning');
  daemon.setupDaemonManager(() => null);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('provisioning restart pending (issue #191)', () => {
  it('keeps restartPending true and never shells out to stop/start while the daemon is busy', async () => {
    harness.health = { status: 'running', active_task_count: 3 };

    daemon.notifyProvisioningPackagesChanged();
    await vi.waitFor(() => expect(provisioning.getProvisioningStatus().restartPending).toBe(true));
    await new Promise((r) => setTimeout(r, 10));

    expect(provisioning.getProvisioningStatus().restartPending).toBe(true);
    expect(harness.execCalls.some((call) => call.includes('stop') || call.includes('start'))).toBe(
      false,
    );
  });

  it('restarts (stop, then start) and clears restartPending once the daemon is idle', async () => {
    harness.health = { status: 'running', active_task_count: 0 };

    daemon.notifyProvisioningPackagesChanged();
    await vi.waitFor(() => expect(provisioning.getProvisioningStatus().restartPending).toBe(false));

    expect(harness.execCalls.some((call) => call.includes('stop'))).toBe(true);
    expect(harness.execCalls.some((call) => call.includes('start'))).toBe(true);
  });

  it('requestProvisioningRestartNow reports "deferred" while busy and never kills the running task', async () => {
    harness.health = { status: 'running', active_task_count: 1 };

    const outcome = await daemon.requestProvisioningRestartNow();

    expect(outcome).toBe('deferred');
    expect(provisioning.getProvisioningStatus().restartPending).toBe(true);
    expect(harness.execCalls.some((call) => call.includes('stop') || call.includes('start'))).toBe(
      false,
    );
  });

  it('requestProvisioningRestartNow restarts and clears the pending state once idle', async () => {
    harness.health = { status: 'running', active_task_count: 0 };

    const outcome = await daemon.requestProvisioningRestartNow();

    expect(outcome).toBe('restarted');
    expect(provisioning.getProvisioningStatus().restartPending).toBe(false);
    expect(harness.execCalls.some((call) => call.includes('stop'))).toBe(true);
    expect(harness.execCalls.some((call) => call.includes('start'))).toBe(true);
  });
});

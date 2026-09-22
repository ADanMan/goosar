import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

type ExecFileCallback = (err: Error | null, stdout: string, stderr: string) => void;

const harness = vi.hoisted(() => ({
  ipcHandlers: new Map<string, (...args: unknown[]) => unknown>(),
  execCalls: [] as string[][],
  finishRuntimeInstall: () => {},
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
  ensureAgentRuntime: vi.fn(
    () =>
      new Promise((resolve) => {
        harness.finishRuntimeInstall = () =>
          resolve({ state: 'ready', version: '9.9.9', binPath: '/x', configPath: '/y' });
      }),
  ),
  readAgentRuntimeStatus: vi.fn(async () => ({ state: 'checking' })),
  agentPathEnvValue: () => null,
  runtimeRoot: () => '/nonexistent/runtime',
}));

let daemon: typeof import('./daemon-manager');

beforeEach(async () => {
  harness.ipcHandlers.clear();
  harness.execCalls.length = 0;
  vi.resetModules();
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => {
      throw new Error('no network in tests');
    }),
  );
  daemon = await import('./daemon-manager');
  daemon.setupDaemonManager(() => null);
});

afterEach(() => {
  harness.finishRuntimeInstall();
  vi.unstubAllGlobals();
});

describe('stopDaemonForRemoval', () => {
  it('reports a busy lifecycle guard as not stopped, not as a clean stop', async () => {
    const start = harness.ipcHandlers.get('daemon:start');
    expect(start).toBeTruthy();
    const starting = start?.();
    await vi.waitFor(() =>
      expect(harness.execCalls.some((call) => call.includes('version'))).toBe(true),
    );

    const report = await daemon.stopDaemonForRemoval();

    expect(report.stopped).toBe(false);
    expect(report.detail).toMatch(/in progress/i);
    expect(report.detail).toMatch(/nothing was removed/i);
    expect(harness.execCalls.some((call) => call.includes('stop'))).toBe(false);

    harness.finishRuntimeInstall();
    await starting;
  });

  it('reports a stop as stopped when the guard was actually acquired', async () => {
    const report = await daemon.stopDaemonForRemoval();

    expect(report).toEqual({ stopped: true });
    expect(harness.execCalls.some((call) => call.includes('daemon') && call.includes('stop'))).toBe(
      true,
    );
  });
});

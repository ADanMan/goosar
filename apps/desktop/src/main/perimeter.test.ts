import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { createServer, type AddressInfo, type Server } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type {
  DaemonReachability,
  HttpReachability,
  PerimeterStateView,
  ProvisioningReachability,
} from '../shared/perimeter-config';

let TEST_HOME = '';

const setProxy = vi.fn<(config: unknown) => Promise<void>>(async () => {});
const sessionFetch = vi.fn(async () => new Response('session'));
const ipcHandlers = new Map<string, (...args: unknown[]) => unknown>();
const resolveProxy = vi.fn(async (_url: string) => 'DIRECT');
const partitionSetProxy = vi.fn(async (_config: unknown) => {});
const partitionFetch = vi.fn(async () => new Response('probe'));
const fromPartition = vi.fn((_name: string) => ({
  setProxy: partitionSetProxy,
  fetch: partitionFetch,
}));

const localProxyPlans: unknown[] = [];
const localProxyOutcome = { value: 'unchanged' as string };
const localProxyExecutable = { value: '/opt/px' as string | null };
const localProxyIsOurs = { value: false };
vi.mock('./local-proxy', () => ({
  createLocalProxySupervisor: () => ({
    apply: async (plan: unknown) => {
      localProxyPlans.push(plan);
      return localProxyOutcome.value;
    },
    isOurs: () => localProxyIsOurs.value,
    stop: () => {},
  }),
  findLocalProxyExecutable: () => localProxyExecutable.value,
  localProxyManagementEnabled: () => process.env.GOOSAR_MANAGE_LOCAL_PROXY !== '0',
  REAL_LOCAL_PROXY_DEPS: { spawn: () => ({ kill: () => {}, onExit: () => {} }) },
}));

const klistFixture = { value: '' as string };
vi.mock('child_process', async (importOriginal) => {
  const actual = await importOriginal<typeof import('child_process')>();
  const execFile = ((
    _file: string,
    _args: unknown,
    _opts: unknown,
    callback: (err: unknown, stdout: string, stderr: string) => void,
  ) => {
    callback(null, klistFixture.value, '');
  }) as typeof actual.execFile;
  return { ...actual, execFile, default: { ...actual, execFile } };
});

vi.mock('electron', () => ({
  app: {
    isPackaged: false,
    getAppPath: () => TEST_HOME,
    getPath: () => TEST_HOME,
    on: vi.fn(),
  },
  ipcMain: {
    handle: (channel: string, handler: (...args: unknown[]) => unknown) => {
      ipcHandlers.set(channel, handler);
    },
  },
  session: {
    defaultSession: { setProxy, fetch: sessionFetch, resolveProxy },
    fromPartition,
  },
  powerMonitor: { on: vi.fn() },
}));

async function loadPerimeter() {
  return import('./perimeter');
}

function writeDesktopJson(document: Record<string, unknown>): void {
  mkdirSync(join(TEST_HOME, '.goosar'), { recursive: true });
  writeFileSync(join(TEST_HOME, '.goosar', 'desktop.json'), JSON.stringify(document));
}

function stageCaFiles(): void {
  mkdirSync(join(TEST_HOME, '.hermes'), { recursive: true });
  writeFileSync(join(TEST_HOME, '.hermes', 'ca-bundle.pem'), 'PEM');
  writeFileSync(join(TEST_HOME, '.hermes', 'corp-ca.pem'), 'PEM');
}

async function startFakePx(): Promise<{ port: number; close: () => Promise<void> }> {
  const server: Server = createServer();
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  return {
    port,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

const UNREACHABLE_PORT = 1;

beforeEach(() => {
  TEST_HOME = mkdtempSync(join(tmpdir(), 'goosar-perimeter-'));
  vi.stubEnv('HERMES_HOME', join(TEST_HOME, '.hermes'));
  vi.stubEnv('XDG_CONFIG_HOME', join(TEST_HOME, '.config'));
  vi.stubEnv('HERMES_CONFIG_PATH', '');
  vi.stubEnv('OPENCLAW_CONFIG_PATH', '');
  setProxy.mockClear();
  sessionFetch.mockClear();
  resolveProxy.mockClear().mockImplementation(async () => 'DIRECT');
  partitionSetProxy.mockClear();
  partitionFetch.mockClear().mockImplementation(async () => new Response('probe'));
  fromPartition.mockClear();
  ipcHandlers.clear();
  localProxyPlans.length = 0;
  localProxyOutcome.value = 'unchanged';
  localProxyExecutable.value = '/opt/px';
  localProxyIsOurs.value = false;
  vi.stubEnv('GOOSAR_MANAGE_LOCAL_PROXY', '1');
  vi.resetModules();
});

afterEach(() => {
  rmSync(TEST_HOME, { recursive: true, force: true });
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

const RESTART_OK = () => vi.fn(async () => ({ kind: 'restarted' as const }));

function writeLegacyEnabledConfig(extra: Record<string, unknown> = {}): void {
  writeDesktopJson({
    schemaVersion: 1,
    apiUrl: 'https://goosar.ru',
    proxy: { perimeter: true, ...extra },
  });
}

describe('the app does not configure its own transport (issue #249)', () => {
  it("never touches the default session's proxy, legacy flag or not", async () => {
    writeLegacyEnabledConfig();
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    await perimeter.refreshNetworkRoutes('test');

    expect(setProxy).not.toHaveBeenCalled();
  });

  it('routes main-process API calls through the session stack, unconditionally', async () => {
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });

    const globalFetch = vi.fn(async () => new Response('global'));
    vi.stubGlobal('fetch', globalFetch);
    await perimeter.mainApiFetch('https://goosar.ru/api/me');

    expect(sessionFetch).toHaveBeenCalledTimes(1);
    expect(globalFetch).not.toHaveBeenCalled();
  });

  it('preserves the Authorization header through the session fetch path (issue #108)', async () => {
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });

    await perimeter.mainApiFetch('https://goosar.ru/api/tokens', {
      method: 'POST',
      headers: { Authorization: 'Bearer TOKEN', 'Content-Type': 'application/json' },
    });

    const [, init] = sessionFetch.mock.calls[0] as unknown as [string, RequestInit];
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer TOKEN');
  });
});

describe('subprocess environment comes from the system (issue #249)', () => {
  it('carries the proxy the system resolved for the addresses we need', async () => {
    resolveProxy.mockImplementation(async (url: string) =>
      url.startsWith('https://goosar.ru') ? 'PROXY isa.corp:8080' : 'DIRECT',
    );
    writeDesktopJson({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { port: UNREACHABLE_PORT },
    });
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    perimeter.setPerimeterServerHost('https://goosar.ru');
    await perimeter.refreshNetworkRoutes('test');

    const env = perimeter.networkSpawnEnvVars();
    expect(env.HTTP_PROXY).toBe('http://isa.corp:8080');
    expect(env.https_proxy).toBe('http://isa.corp:8080');
    expect(resolveProxy).toHaveBeenCalledWith('https://goosar.ru');
  });

  it('injects no proxy at all when the system resolved everything direct', async () => {
    writeDesktopJson({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { port: UNREACHABLE_PORT },
    });
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    perimeter.setPerimeterServerHost('https://goosar.ru');
    await perimeter.refreshNetworkRoutes('test');

    const env = perimeter.networkSpawnEnvVars();
    expect(env.HTTP_PROXY).toBeUndefined();
    expect(env.NO_PROXY?.split(',')).toContain('goosar.ru');
  });

  it('produces the same environment with and without the legacy perimeter flag', async () => {
    resolveProxy.mockImplementation(async () => 'PROXY isa.corp:8080');

    writeLegacyEnabledConfig({ port: UNREACHABLE_PORT });
    const withFlag = await loadPerimeter();
    await withFlag.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    withFlag.setPerimeterServerHost('https://goosar.ru');
    await withFlag.refreshNetworkRoutes('test');
    const envWithFlag = withFlag.networkSpawnEnvVars();

    vi.resetModules();
    writeDesktopJson({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { port: UNREACHABLE_PORT },
    });
    const withoutFlag = await loadPerimeter();
    await withoutFlag.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    withoutFlag.setPerimeterServerHost('https://goosar.ru');
    await withoutFlag.refreshNetworkRoutes('test');
    const envWithoutFlag = withoutFlag.networkSpawnEnvVars();

    expect(envWithFlag).toEqual(envWithoutFlag);
  });

  it('injects the CA variables with no toggle in front of them', async () => {
    stageCaFiles();
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });

    const env = perimeter.networkSpawnEnvVars();
    expect(env.REQUESTS_CA_BUNDLE).toBe(join(TEST_HOME, '.hermes', 'ca-bundle.pem'));
    expect(env.SSL_CERT_FILE).toBe(join(TEST_HOME, '.hermes', 'ca-bundle.pem'));
    expect(env.NODE_EXTRA_CA_CERTS).toBe(join(TEST_HOME, '.hermes', 'corp-ca.pem'));
  });

  it("keeps the operator's own bypass entries alongside the resolved answer", async () => {
    writeDesktopJson({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { port: UNREACHABLE_PORT, noProxy: ['.example.test'] },
      perimeter: { noProxy: ['gw.internal'] },
    });
    resolveProxy.mockImplementation(async () => 'PROXY isa.corp:8080');

    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    perimeter.setPerimeterServerHost('https://goosar.ru');
    await perimeter.refreshNetworkRoutes('test');

    const bypass = perimeter.networkSpawnEnvVars().NO_PROXY.split(',');
    expect(bypass).toContain('.example.test');
    expect(bypass).toContain('gw.internal');
    expect(bypass).toContain('127.0.0.1');
  });

  it('recycles the daemon when the route arrives after it was already spawned', async () => {
    writeDesktopJson({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { port: UNREACHABLE_PORT },
    });
    const restart = vi.fn(async () => ({ kind: 'restarted' as const }));
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: restart });

    expect(perimeter.networkSpawnEnvVars().HTTP_PROXY).toBeUndefined();

    resolveProxy.mockImplementation(async () => 'PROXY isa.corp:8080');
    perimeter.setPerimeterServerHost('https://goosar.ru');
    await perimeter.refreshNetworkRoutes('test');

    expect(restart).toHaveBeenCalled();
    expect(perimeter.networkSpawnEnvVars().HTTP_PROXY).toBe('http://isa.corp:8080');
  });

  it('sets no CA variable pointing at a file that is not there', async () => {
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });

    const env = perimeter.networkSpawnEnvVars();
    expect(env.REQUESTS_CA_BUNDLE).toBeUndefined();
    expect(env.NODE_EXTRA_CA_CERTS).toBeUndefined();
  });
});

describe('the local proxy has to actually work (issue #249)', () => {
  it('does not send subprocesses into a proxy that only listens', async () => {
    const px = await startFakePx();
    try {
      partitionFetch.mockImplementation(async () => {
        throw new Error('ERR_TUNNEL_CONNECTION_FAILED');
      });
      resolveProxy.mockImplementation(async () => 'PROXY isa.corp:8080');
      writeLegacyEnabledConfig({ port: px.port });

      const perimeter = await loadPerimeter();
      await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
      perimeter.setPerimeterServerHost('https://goosar.ru');
      await perimeter.refreshNetworkRoutes('test');

      expect(perimeter.networkSpawnEnvVars().HTTP_PROXY).toBe('http://isa.corp:8080');
    } finally {
      await px.close();
    }
  }, 10_000);

  it('probes the local proxy on its own session, never reconfiguring the default one', async () => {
    const px = await startFakePx();
    try {
      writeLegacyEnabledConfig({ port: px.port });
      const perimeter = await loadPerimeter();
      await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
      perimeter.setPerimeterServerHost('https://goosar.ru');
      await perimeter.refreshNetworkRoutes('test');

      expect(partitionSetProxy).toHaveBeenCalledWith({
        proxyRules: `127.0.0.1:${px.port}`,
      });
      expect(setProxy).not.toHaveBeenCalled();
      const [, init] = partitionFetch.mock.calls[0] as unknown as [string, RequestInit];
      expect(init.credentials).toBe('omit');
      expect(init.headers).toBeUndefined();
    } finally {
      await px.close();
    }
  }, 10_000);
});

describe('network status (issues #154, #249)', () => {
  const okServer: HttpReachability = { kind: 'response', status: 200 };

  function liveProbes() {
    return {
      server: vi.fn<() => Promise<HttpReachability>>(async () => okServer),
      llm: vi.fn<() => Promise<HttpReachability>>(async () => okServer),
      daemon: vi.fn<() => Promise<DaemonReachability>>(async () => ({
        kind: 'running',
      })),
      provisioning: vi.fn<() => Promise<ProvisioningReachability>>(async () => ({
        kind: 'status',
        state: 'ok' as const,
        installed: 2,
        total: 2,
        failed: 0,
      })),
    };
  }

  it('get-state names the route the system resolved for each address', async () => {
    resolveProxy.mockImplementation(async (url: string) =>
      url.startsWith('https://goosar.ru') ? 'PROXY isa.corp:8080' : 'DIRECT',
    );
    stageCaFiles();
    const probes = liveProbes();
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({
      restartDaemonIfAlive: RESTART_OK(),
      liveProbes: probes,
    });
    perimeter.setPerimeterServerHost('https://goosar.ru');

    const handler = ipcHandlers.get('perimeter:get-state');
    if (!handler) throw new Error('perimeter:get-state handler not registered');
    const state = (await handler({})) as PerimeterStateView;

    const byId = Object.fromEntries((state.live?.checks ?? []).map((check) => [check.id, check]));
    expect(byId.route_server).toMatchObject({
      state: 'ok',
      reasonCode: 'route_proxy',
      detail: 'http://isa.corp:8080',
    });
    expect(byId.ca?.state).toBe('ok');
    expect(byId.server?.state).toBe('ok');
    expect(byId.daemon?.state).toBe('ok');
    expect(probes.server).toHaveBeenCalled();
    expect(probes.daemon).toHaveBeenCalled();
  }, 10_000);

  it('get-state carries perimeter.principalDomain through to the renderer', async () => {
    writeDesktopJson({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      perimeter: { realm: 'example.com', principalDomain: 'example.net' },
    });
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({
      restartDaemonIfAlive: RESTART_OK(),
      liveProbes: liveProbes(),
    });

    const handler = ipcHandlers.get('perimeter:get-state');
    if (!handler) throw new Error('perimeter:get-state handler not registered');
    const state = (await handler({})) as PerimeterStateView;

    expect(state.realm).toBe('EXAMPLE.COM');
    expect(state.principalDomain).toBe('EXAMPLE.NET');
  }, 10_000);

  it('a stopped daemon drives overall FAIL with a concrete reason', async () => {
    stageCaFiles();
    const probes = liveProbes();
    probes.daemon.mockImplementation(async () => ({ kind: 'stopped' }));
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({
      restartDaemonIfAlive: RESTART_OK(),
      liveProbes: probes,
    });

    const handler = ipcHandlers.get('perimeter:get-state');
    if (!handler) throw new Error('perimeter:get-state handler not registered');
    const state = (await handler({})) as PerimeterStateView;

    expect(state.live?.overall).toBe('fail');
    expect(state.live?.checks.find((check) => check.id === 'daemon')).toMatchObject({
      state: 'fail',
      reasonCode: 'live_daemon_stopped',
    });
  }, 10_000);

  it('answers a status read before any login, carrying no credential-shaped field', async () => {
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });

    const handler = ipcHandlers.get('perimeter:get-state');
    if (!handler) throw new Error('perimeter:get-state handler not registered');
    const state = (await handler({})) as PerimeterStateView;

    expect(state.live).not.toBeNull();
    const serialized = JSON.stringify(state).toLowerCase();
    for (const word of ['authorization', 'token', 'password', 'apikey', 'api_key']) {
      expect(serialized).not.toContain(word);
    }
  }, 10_000);

  it('recheck re-resolves the routes on demand', async () => {
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    perimeter.setPerimeterServerHost('https://goosar.ru');
    resolveProxy.mockClear();

    const handler = ipcHandlers.get('perimeter:recheck');
    if (!handler) throw new Error('perimeter:recheck handler not registered');
    await handler({});

    expect(resolveProxy).toHaveBeenCalled();
  }, 10_000);
});

describe('Kerberos principal mismatch and ticket source (T-31, ADR-0022)', () => {
  const okServer: HttpReachability = { kind: 'response', status: 200 };
  function liveProbes() {
    return {
      server: vi.fn<() => Promise<HttpReachability>>(async () => okServer),
      llm: vi.fn<() => Promise<HttpReachability>>(async () => okServer),
      daemon: vi.fn<() => Promise<DaemonReachability>>(async () => ({
        kind: 'running',
      })),
      provisioning: vi.fn<() => Promise<ProvisioningReachability>>(async () => ({
        kind: 'status',
        state: 'ok' as const,
        installed: 2,
        total: 2,
        failed: 0,
      })),
    };
  }

  const MONTHS = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec',
  ];
  function fmt(d: Date): string {
    const mon = MONTHS[d.getMonth()];
    const day = String(d.getDate()).padStart(2, ' ');
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    const ss = String(d.getSeconds()).padStart(2, '0');
    return `${mon} ${day} ${hh}:${mm}:${ss} ${d.getFullYear()}`;
  }
  function fakeKlist(principal: string, expiresInMs: number): string {
    const now = new Date();
    const expires = new Date(now.getTime() + expiresInMs);
    return [
      'Credentials cache: API:501:9',
      `        Principal: ${principal}`,
      '',
      '  Issued                Expires               Principal',
      `${fmt(now)}  ${fmt(expires)}  krbtgt/CORP.EXAMPLE@CORP.EXAMPLE`,
      '',
    ].join('\n');
  }

  let originalPlatform: PropertyDescriptor | undefined;
  beforeEach(() => {
    originalPlatform = Object.getOwnPropertyDescriptor(process, 'platform');
    Object.defineProperty(process, 'platform', {
      value: 'darwin',
      configurable: true,
    });
  });
  afterEach(() => {
    if (originalPlatform) {
      Object.defineProperty(process, 'platform', originalPlatform);
    }
  });

  it('fails as principal_mismatch when the klist cache principal differs from the resolved one', async () => {
    klistFixture.value = fakeKlist('wrong@CORP.EXAMPLE', 8 * 3_600_000);
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({
      restartDaemonIfAlive: RESTART_OK(),
      liveProbes: liveProbes(),
    });
    perimeter.setResolvedKerberosPrincipal('right@CORP.EXAMPLE');

    const handler = ipcHandlers.get('perimeter:get-state');
    if (!handler) throw new Error('perimeter:get-state handler not registered');
    const state = (await handler({})) as PerimeterStateView;

    const kerberos = state.live?.checks.find((c) => c.id === 'kerberos');
    expect(kerberos).toMatchObject({ state: 'fail', reasonCode: 'principal_mismatch' });
    expect(state.kerberosCachePrincipal).toBe('wrong@CORP.EXAMPLE');
  }, 10_000);

  it('keeps the prior verdict when the cache principal matches the resolved one', async () => {
    klistFixture.value = fakeKlist('right@CORP.EXAMPLE', 8 * 3_600_000);
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({
      restartDaemonIfAlive: RESTART_OK(),
      liveProbes: liveProbes(),
    });
    perimeter.setResolvedKerberosPrincipal('right@CORP.EXAMPLE');

    const handler = ipcHandlers.get('perimeter:get-state');
    if (!handler) throw new Error('perimeter:get-state handler not registered');
    const state = (await handler({})) as PerimeterStateView;

    const kerberos = state.live?.checks.find((c) => c.id === 'kerberos');
    expect(kerberos?.reasonCode).not.toBe('principal_mismatch');
  }, 10_000);

  it('reports ticketSource as cache when no in-app kinit/renew happened this session', async () => {
    klistFixture.value = fakeKlist('user@CORP.EXAMPLE', 8 * 3_600_000);
    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({
      restartDaemonIfAlive: RESTART_OK(),
      liveProbes: liveProbes(),
    });

    const handler = ipcHandlers.get('perimeter:get-state');
    if (!handler) throw new Error('perimeter:get-state handler not registered');
    const state = (await handler({})) as PerimeterStateView;

    expect(state.ticketSource).toBe('cache');
  }, 10_000);
});

describe('the local entry point follows the same resolution (issue #249)', () => {
  it('runs it on the upstream the system resolved, never on a remembered one', async () => {
    resolveProxy.mockImplementation(async () => 'PROXY isa.corp:8080');
    writeDesktopJson({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { port: UNREACHABLE_PORT },
    });

    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    perimeter.setPerimeterServerHost('https://goosar.ru');
    await perimeter.refreshNetworkRoutes('test');

    expect(localProxyPlans.at(-1)).toMatchObject({
      action: 'run',
      upstream: { host: 'isa.corp', port: 8080 },
      listen: { host: '127.0.0.1', port: UNREACHABLE_PORT },
    });
  });

  it('stops it when the route stops resolving', async () => {
    resolveProxy.mockImplementation(async () => {
      throw new Error('resolver unavailable');
    });

    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    perimeter.setPerimeterServerHost('https://goosar.ru');
    await perimeter.refreshNetworkRoutes('test');

    expect(localProxyPlans.at(-1)).toEqual({
      action: 'stop',
      reason: 'route_unknown',
    });
  });

  it('leaves a working proxy it did not start alone', async () => {
    const px = await startFakePx();
    try {
      resolveProxy.mockImplementation(async () => 'PROXY isa.corp:8080');
      writeDesktopJson({
        schemaVersion: 1,
        apiUrl: 'https://goosar.ru',
        proxy: { port: px.port },
      });

      const perimeter = await loadPerimeter();
      await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
      perimeter.setPerimeterServerHost('https://goosar.ru');
      await perimeter.refreshNetworkRoutes('test');

      expect(localProxyPlans.at(-1)).toEqual({
        action: 'leave_foreign',
        reason: 'foreign_proxy_works',
      });
    } finally {
      await px.close();
    }
  }, 10_000);

  it('says so in the panel when there is no local proxy to run at all', async () => {
    localProxyExecutable.value = null;
    resolveProxy.mockImplementation(async () => 'PROXY isa.corp:8080');
    writeDesktopJson({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { port: UNREACHABLE_PORT },
    });

    const perimeter = await loadPerimeter();
    await perimeter.initPerimeter({ restartDaemonIfAlive: RESTART_OK() });
    perimeter.setPerimeterServerHost('https://goosar.ru');

    const handler = ipcHandlers.get('perimeter:get-state');
    if (!handler) throw new Error('perimeter:get-state handler not registered');
    const state = (await handler({})) as PerimeterStateView;

    expect(state.live?.checks.find((check) => check.id === 'px')).toMatchObject({
      state: 'fail',
      reasonCode: 'live_px_executable_missing',
    });
  }, 10_000);
});

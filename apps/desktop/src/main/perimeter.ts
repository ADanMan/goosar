import {
  app,
  ipcMain,
  powerMonitor,
  session as electronSession,
  type BrowserWindow,
} from 'electron';
import { execFile } from 'child_process';
import { existsSync, readFileSync } from 'fs';
import { readFile } from 'fs/promises';
import { connect as netConnect } from 'net';
import { homedir } from 'os';
import { join } from 'path';
import {
  buildPerimeterLiveStatus,
  DEFAULT_PERIMETER_EXTRAS,
  DEFAULT_PERIMETER_SETTINGS,
  KERBEROS_EXPIRING_SOON_MS,
  parseKlistCacheIdentity,
  parseKlistRenewUntil,
  parseKlistTicket,
  parsePerimeterExtras,
  parsePerimeterSettings,
  perimeterProxyUrl,
  deriveTicketSource,
  llmGatewayRowVerdict,
  type DaemonReachability,
  type HttpReachability,
  type LlmAuthReachability,
  type LlmGatewayVerdict,
  type KerberosTicketStatus,
  type LastKinitOutcome,
  type PerimeterDaemonRestartOutcome,
  type PerimeterExtras,
  type PerimeterKinitResult,
  type PerimeterLiveStatus,
  type PerimeterSettings,
  type PerimeterStateView,
  type ProvisioningReachability,
} from '../shared/perimeter-config';
import {
  createLocalProxySupervisor,
  findLocalProxyExecutable,
  localProxyManagementEnabled,
  REAL_LOCAL_PROXY_DEPS,
} from './local-proxy';
import { classifyProxyProbe } from '../shared/system-proxy';
import { planLocalProxy } from '../shared/local-proxy';
import {
  describeEntry,
  resolveNetworkRoutes,
  type NetworkRouteSnapshot,
  type RouteResolutionDeps,
} from './system-proxy';
import { desktopConfigPath } from './runtime-config-loader';
import {
  caBundleFilePath,
  corpCaFilePath,
  ensureCaBundleFiles,
  readCaBundleStatus,
  readLlmProfileScalars,
  resolveAgentConfigPath,
  type AgentPathContext,
} from './agent-bootstrap';
import { runKinit, runKinitRenew } from './perimeter-kinit';
import {
  kerberosPreferencesPath,
  loadKerberosPreferences,
  saveKerberosPreferences,
} from './kerberos-preferences';
import type { KerberosPreferences } from '../shared/kerberos-preferences-types';

const KLIST_TIMEOUT_MS = 2_000;
const PX_PROBE_TIMEOUT_MS = 1_500;
const LIVE_PROBE_TIMEOUT_MS = 2_500;

export interface PerimeterLiveProbes {
  server: () => Promise<HttpReachability>;
  llm: () => Promise<LlmAuthReachability>;
  daemon: () => Promise<DaemonReachability>;
  provisioning: () => Promise<ProvisioningReachability>;
}

const SKIPPED_LIVE_PROBES: PerimeterLiveProbes = {
  server: async () => ({ kind: 'skipped' }),
  llm: async () => ({ kind: 'skipped' }),
  daemon: async () => ({ kind: 'skipped' }),
  provisioning: async () => ({ kind: 'skipped' }),
};

let liveProbesRef: PerimeterLiveProbes = SKIPPED_LIVE_PROBES;

let cachedSettings: PerimeterSettings = { ...DEFAULT_PERIMETER_SETTINGS };
let cachedExtras: PerimeterExtras = { ...DEFAULT_PERIMETER_EXTRAS };

let lastKinitOutcome: LastKinitOutcome | null = null;

function recordKinitOutcome(kind: LastKinitOutcome['kind'], ok: boolean, reason?: string): void {
  lastKinitOutcome = { at: Date.now(), kind, ok, ...(ok ? {} : { reason }) };
}

function agentPathCtx(): AgentPathContext {
  return { home: homedir(), env: process.env };
}

function bundledCaDir(): string | null {
  const dir = app.isPackaged
    ? join(process.resourcesPath, 'ca')
    : join(app.getAppPath(), 'resources-ca');
  return existsSync(dir) ? dir : null;
}

async function readDesktopRaw(): Promise<string | null> {
  try {
    return await readFile(desktopConfigPath(), 'utf-8');
  } catch {
    return null;
  }
}

function activeLlmApiBase(): string | null {
  const ctx = agentPathCtx();
  const path = resolveAgentConfigPath(ctx);
  if (!path) return null;
  try {
    const scalars = readLlmProfileScalars(readFileSync(path, 'utf-8'));
    return scalars.apiBase ?? null;
  } catch {
    return null;
  }
}

let perimeterServerBaseUrl: string | null = null;

let resolvedKerberosPrincipal: string | null = null;

export function setResolvedKerberosPrincipal(principal: string | null): void {
  resolvedKerberosPrincipal =
    typeof principal === 'string' && principal.trim().length > 0 ? principal : null;
}

export function setPerimeterServerHost(url: string | null): void {
  const trimmed = typeof url === 'string' ? url.trim() : '';
  const next = trimmed.length > 0 ? trimmed : null;
  if (next === perimeterServerBaseUrl) return;
  perimeterServerBaseUrl = next;
  void refreshNetworkRoutes('server address changed');
}

const ROUTE_SNAPSHOT_TTL_MS = 60_000;

const LOCAL_PROXY_PROBE_TIMEOUT_MS = 4_000;

let routeSnapshot: NetworkRouteSnapshot | null = null;
let lastSpawnProxyEnv: Record<string, string> | null = null;

const localProxySupervisor = createLocalProxySupervisor(REAL_LOCAL_PROXY_DEPS);
let localProxyExecutableMissing = false;

const LOCAL_PROXY_START_POLL_MS = 300;
const LOCAL_PROXY_START_POLL_ATTEMPTS = 5;

async function waitForLocalProxy(host: string, port: number): Promise<void> {
  for (let attempt = 0; attempt < LOCAL_PROXY_START_POLL_ATTEMPTS; attempt++) {
    if (await probePxReachable(host, port)) return;
    await new Promise((resolve) => setTimeout(resolve, LOCAL_PROXY_START_POLL_MS));
  }
}
let routeSnapshotAtMs = 0;
let routeRefreshInFlight: Promise<NetworkRouteSnapshot> | null = null;
let routeRefreshPending: Promise<NetworkRouteSnapshot> | null = null;

const REAL_ROUTE_DEPS: RouteResolutionDeps = {
  resolveProxy: (url) => electronSession.defaultSession.resolveProxy(url),
  probeListening: (host, port) => probePxReachable(host, port),
  probeThroughProxy: async (proxyUrl, targetUrl) => {
    const probeSession = electronSession.fromPartition('perimeter-route-probe');
    try {
      const endpoint = new URL(proxyUrl).host;
      await probeSession.setProxy({ proxyRules: endpoint });
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), LOCAL_PROXY_PROBE_TIMEOUT_MS);
      try {
        const res = await probeSession.fetch(targetUrl, {
          method: 'HEAD',
          signal: controller.signal,
          credentials: 'omit',
        });
        return classifyProxyProbe(res.status);
      } finally {
        clearTimeout(timer);
      }
    } catch {
      return 'no_response';
    }
  },
};

export async function refreshNetworkRoutes(reason: string): Promise<NetworkRouteSnapshot> {
  if (routeRefreshInFlight) {
    routeRefreshPending ??= routeRefreshInFlight
      .catch(() => undefined)
      .then(() => {
        routeRefreshPending = null;
        return runRouteRefresh(reason);
      });
    return routeRefreshPending;
  }
  return runRouteRefresh(reason);
}

async function runRouteRefresh(reason: string): Promise<NetworkRouteSnapshot> {
  const pending = (async () => {
    let snapshot = await resolveOnce();

    const outcome = await applyLocalProxyPlan(snapshot);
    if (outcome === 'started' || outcome === 'restarted') {
      await waitForLocalProxy(cachedSettings.proxyHost, cachedSettings.proxyPort);
      snapshot = await resolveOnce();
    }

    routeSnapshot = snapshot;
    routeSnapshotAtMs = Date.now();
    console.log(
      `[network] routes resolved (${reason}): entry ${describeEntry(snapshot.entry)}, local proxy ${outcome}`,
    );
    if (lastSpawnProxyEnv && !sameProxyEnv(lastSpawnProxyEnv, snapshot.env)) {
      const restart = await safeRestartDaemon();
      console.log(`[network] route changed — daemon ${restart.kind}`);
    }
    return snapshot;
  })();
  routeRefreshInFlight = pending;
  try {
    return await pending;
  } finally {
    if (routeRefreshInFlight === pending) routeRefreshInFlight = null;
  }
}

function resolveOnce(): Promise<NetworkRouteSnapshot> {
  return resolveNetworkRoutes(
    { goosarUrl: perimeterServerBaseUrl, llmApiBase: activeLlmApiBase() },
    { host: cachedSettings.proxyHost, port: cachedSettings.proxyPort },
    REAL_ROUTE_DEPS,
    [...cachedSettings.noProxy, ...(cachedExtras.noProxy ?? [])],
  );
}

async function applyLocalProxyPlan(snapshot: NetworkRouteSnapshot): Promise<string> {
  const executable = findLocalProxyExecutable();
  localProxyExecutableMissing = executable === null;
  const plan = planLocalProxy({
    managementEnabled: localProxyManagementEnabled(),
    upstream: snapshot.goosar.route,
    listen: { host: cachedSettings.proxyHost, port: cachedSettings.proxyPort },
    executable,
    foreignProxyWorks:
      snapshot.localProxy?.upstreamWorks === true && !localProxySupervisor.isOurs(),
    noProxy: [...cachedSettings.noProxy, ...(cachedExtras.noProxy ?? [])],
  });
  return localProxySupervisor.apply(plan);
}

function sameProxyEnv(a: Record<string, string>, b: Record<string, string>): boolean {
  const keys = ['HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY'];
  return keys.every((key) => a[key] === b[key]);
}

export function networkSpawnEnvVars(): Record<string, string> {
  if (Date.now() - routeSnapshotAtMs > ROUTE_SNAPSHOT_TTL_MS) {
    void refreshNetworkRoutes('spawn env stale').catch(() => {});
  }
  const ctx = agentPathCtx();
  const status = readCaBundleStatus(ctx);
  const env: Record<string, string> = { ...(routeSnapshot?.env ?? {}) };
  if (status.caBundlePresent) {
    const bundle = caBundleFilePath(ctx);
    env.REQUESTS_CA_BUNDLE = bundle;
    env.SSL_CERT_FILE = bundle;
  }
  if (status.corpCaPresent) {
    env.NODE_EXTRA_CA_CERTS = corpCaFilePath(ctx);
  }
  lastSpawnProxyEnv = { ...(routeSnapshot?.env ?? {}) };
  return env;
}

export function mainApiFetch(input: string, init?: RequestInit): Promise<Response> {
  return electronSession.defaultSession.fetch(input, init);
}

export function probePxReachable(
  host: string,
  port: number,
  timeoutMs: number = PX_PROBE_TIMEOUT_MS,
): Promise<boolean> {
  return new Promise((resolve) => {
    let settled = false;
    const socket = netConnect({ host, port });
    const finish = (result: boolean) => {
      if (settled) return;
      settled = true;
      socket.removeAllListeners();
      socket.destroy();
      resolve(result);
    };
    socket.setTimeout(timeoutMs);
    socket.once('connect', () => finish(true));
    socket.once('timeout', () => finish(false));
    socket.once('error', () => finish(false));
  });
}

let lastKlistIdentityLine: string | null = null;

function logKlistIdentityChange(output: string): void {
  const identity = parseKlistCacheIdentity(output);
  const line = `cache ${identity.cache ?? 'none'}, realm ${identity.realm ?? 'none'}`;
  if (line === lastKlistIdentityLine) return;
  lastKlistIdentityLine = line;
  console.log(`[perimeter] kerberos ${line}`);
}

interface KerberosTicketFacts extends KerberosTicketStatus {
  renewUntil: number | null;
  cachePrincipal: string | null;
}

function checkKerberosTicket(): Promise<KerberosTicketFacts> {
  if (process.platform !== 'darwin') {
    return Promise.resolve({
      state: 'unknown',
      expiresAt: null,
      renewUntil: null,
      cachePrincipal: null,
    });
  }
  return new Promise((resolve) => {
    execFile(
      '/usr/bin/klist',
      [],
      { timeout: KLIST_TIMEOUT_MS, env: { ...process.env, LC_ALL: 'C', LANG: 'C' } },
      (err, stdout, stderr) => {
        const output = `${stdout ?? ''}\n${stderr ?? ''}`;
        if (err && output.trim().length === 0) {
          resolve({
            state: 'unknown',
            expiresAt: null,
            renewUntil: null,
            cachePrincipal: null,
          });
          return;
        }
        logKlistIdentityChange(output);
        resolve({
          ...parseKlistTicket(output, Date.now(), KERBEROS_EXPIRING_SOON_MS),
          renewUntil: parseKlistRenewUntil(output),
          cachePrincipal: parseKlistCacheIdentity(output).principal,
        });
      },
    );
  });
}

async function httpRoundTrip(
  url: string,
  doFetch: (input: string, init?: RequestInit) => Promise<Response>,
): Promise<HttpReachability> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), LIVE_PROBE_TIMEOUT_MS);
  try {
    const res = await doFetch(url, {
      method: 'GET',
      signal: controller.signal,
    });
    return { kind: 'response', status: res.status };
  } catch {
    return { kind: 'unreachable' };
  } finally {
    clearTimeout(timer);
  }
}

export function probePerimeterServerReachability(): Promise<HttpReachability> {
  if (!perimeterServerBaseUrl) return Promise.resolve({ kind: 'unconfigured' });
  const url = `${perimeterServerBaseUrl.replace(/\/+$/, '')}/health`;
  return httpRoundTrip(url, mainApiFetch);
}

function activeLlmApiKey(): string | null {
  const ctx = agentPathCtx();
  const path = resolveAgentConfigPath(ctx);
  if (!path) return null;
  try {
    const scalars = readLlmProfileScalars(readFileSync(path, 'utf-8'));
    return scalars.apiKey ?? null;
  } catch {
    return null;
  }
}

export function probePerimeterLlmReachability(): Promise<LlmAuthReachability> {
  const base = activeLlmApiBase();
  if (!base) return Promise.resolve({ kind: 'unconfigured' });
  const key = activeLlmApiKey();
  if (!key) return Promise.resolve({ kind: 'no_key' });
  const url = `${base.replace(/\/+$/, '')}/models`;
  return httpRoundTrip(url, (input, init) =>
    fetch(input, {
      ...init,
      headers: { ...(init?.headers ?? {}), Authorization: `Bearer ${key}` },
    }),
  );
}

export interface LlmGatewayRowStatusResult {
  verdict: LlmGatewayVerdict;
  source: 'agent' | 'server';
  host: string | null;
  standApiBaseHint?: string | null;
}

function hostOf(url: string | null): string | null {
  if (!url) return null;
  try {
    return new URL(url).host || null;
  } catch {
    return null;
  }
}

export async function getLlmGatewayRowStatus(): Promise<LlmGatewayRowStatusResult> {
  const fact = await probePerimeterLlmReachability();
  return {
    verdict: llmGatewayRowVerdict(fact),
    source: 'agent',
    host: hostOf(activeLlmApiBase()),
  };
}

export type ClientSecretsResyncResult = { deploymentApiBase: string | null } | null;
export type ResyncClientSecrets = () => Promise<ClientSecretsResyncResult>;

let resyncClientSecretsImpl: ResyncClientSecrets | null = null;

export function setResyncClientSecrets(fn: ResyncClientSecrets): void {
  resyncClientSecretsImpl = fn;
}

export async function retryLlmGatewayRowStatus(): Promise<LlmGatewayRowStatusResult> {
  const resync = resyncClientSecretsImpl ? await resyncClientSecretsImpl().catch(() => null) : null;

  const status = await getLlmGatewayRowStatus();
  if (status.verdict !== 'ok' && status.host && resync?.deploymentApiBase) {
    const standHost = hostOf(resync.deploymentApiBase);
    if (standHost && standHost !== status.host) {
      return { ...status, standApiBaseHint: standHost };
    }
  }
  return status;
}

export function getPxProxyRowStatus(): {
  state: 'not_required' | 'installed' | 'not_found';
} {
  const inPlay =
    routeSnapshot?.goosar.route.kind === 'proxy' || routeSnapshot?.localProxy?.listening === true;
  if (!inPlay) return { state: 'not_required' };
  return { state: findLocalProxyExecutable() ? 'installed' : 'not_found' };
}

async function gatherLiveFacts(): Promise<{
  caStatus: ReturnType<typeof readCaBundleStatus>;
  routes: NetworkRouteSnapshot;
  ticket: KerberosTicketFacts;
  server: HttpReachability;
  llm: LlmAuthReachability;
  daemon: DaemonReachability;
  provisioning: ProvisioningReachability;
}> {
  const caStatus = readCaBundleStatus(agentPathCtx());
  const [routes, ticket, server, llm, daemon, provisioning] = await Promise.all([
    refreshNetworkRoutes('status read'),
    checkKerberosTicket(),
    liveProbesRef.server(),
    liveProbesRef.llm(),
    liveProbesRef.daemon(),
    liveProbesRef.provisioning(),
  ]);
  return { caStatus, routes, ticket, server, llm, daemon, provisioning };
}

async function buildStateView(): Promise<PerimeterStateView> {
  const facts = await gatherLiveFacts();
  const live: PerimeterLiveStatus = buildPerimeterLiveStatus({
    caBundlePresent: facts.caStatus.caBundlePresent,
    routes: {
      goosar: {
        host: facts.routes.goosar.host,
        route: facts.routes.goosar.route,
        candidates: facts.routes.goosar.candidates,
      },
      llm: {
        host: facts.routes.llm.host,
        route: facts.routes.llm.route,
        candidates: facts.routes.llm.candidates,
      },
      localProxy: facts.routes.localProxy,
      entry: facts.routes.entry,
      conflict: facts.routes.conflict,
      localProxyExecutableMissing,
    },
    kerberosSupported: process.platform === 'darwin',
    kerberosTicket: facts.ticket.state,
    kerberosPrincipals: {
      cachePrincipal: facts.ticket.cachePrincipal,
      resolvedPrincipal: resolvedKerberosPrincipal,
    },
    server: facts.server,
    llm: facts.llm,
    daemon: facts.daemon,
    provisioning: facts.provisioning,
    checkedAt: Date.now(),
  });
  return {
    localProxyUrl: perimeterProxyUrl(cachedSettings),
    caBundlePresent: facts.caStatus.caBundlePresent,
    corpCaPresent: facts.caStatus.corpCaPresent,
    kerberosTicket: facts.ticket.state,
    kerberosExpiresAt: facts.ticket.expiresAt,
    kerberosRenewUntil: facts.ticket.renewUntil,
    kerberosCachePrincipal: facts.ticket.cachePrincipal,
    lastKinit: lastKinitOutcome,
    ticketSource: deriveTicketSource(lastKinitOutcome),
    kerberosSupported: process.platform === 'darwin',
    realm: cachedExtras.realm,
    principalDomain: cachedExtras.principalDomain,
    live,
  };
}

const KERBEROS_RECHECK_INTERVAL_MS = 5 * 60_000;
const KERBEROS_FOCUS_DEBOUNCE_MS = 60_000;

let getMainWindowRef: () => BrowserWindow | null = () => null;
let restartDaemonRef: () => Promise<PerimeterDaemonRestartOutcome> = async () => ({
  kind: 'not_running',
});
let lastKerberosCheckMs = 0;

async function safeRestartDaemon(): Promise<PerimeterDaemonRestartOutcome> {
  try {
    return await restartDaemonRef();
  } catch (err) {
    console.warn('[perimeter] daemon restart failed:', err);
    return {
      kind: 'failed',
      message: err instanceof Error ? err.message : String(err),
    };
  }
}
let kerberosTimer: ReturnType<typeof setInterval> | null = null;
let lifecycleInstalled = false;

function pushPerimeterState(state: PerimeterStateView): void {
  const win = getMainWindowRef();
  if (win && !win.isDestroyed()) {
    win.webContents.send('perimeter:state', state);
  }
}

async function recheckAndPushPerimeter(opts: { force?: boolean } = {}): Promise<void> {
  const now = Date.now();
  if (!opts.force && now - lastKerberosCheckMs < KERBEROS_FOCUS_DEBOUNCE_MS) {
    return;
  }
  lastKerberosCheckMs = now;
  try {
    pushPerimeterState(await buildStateView());
  } catch (err) {
    console.warn('[perimeter] state re-check failed:', err);
  }
}

function installKerberosLifecycle(): void {
  if (lifecycleInstalled) return;
  lifecycleInstalled = true;
  kerberosTimer = setInterval(
    () => void recheckAndPushPerimeter({ force: true }),
    KERBEROS_RECHECK_INTERVAL_MS,
  );
  kerberosTimer.unref?.();
  app.on('browser-window-focus', () => void recheckAndPushPerimeter());
  powerMonitor.on('resume', () => void recheckAndPushPerimeter({ force: true }));
}

export async function initPerimeter(options: {
  restartDaemonIfAlive: () => Promise<PerimeterDaemonRestartOutcome>;
  getMainWindow?: () => BrowserWindow | null;
  liveProbes?: PerimeterLiveProbes;
}): Promise<void> {
  if (options.getMainWindow) getMainWindowRef = options.getMainWindow;
  if (options.liveProbes) liveProbesRef = options.liveProbes;
  restartDaemonRef = options.restartDaemonIfAlive;
  const raw = await readDesktopRaw();
  cachedSettings = parsePerimeterSettings(raw);
  cachedExtras = parsePerimeterExtras(raw);

  void ensureCaBundleFiles(agentPathCtx(), bundledCaDir())
    .then((status) => {
      if (!status.caBundlePresent) {
        console.warn(
          '[perimeter] ~/.hermes/ca-bundle.pem is missing — perimeter connections will not work on this machine',
        );
      }
    })
    .catch((err) => {
      console.warn('[perimeter] CA materialization check failed:', err);
    });

  void refreshNetworkRoutes('boot').catch((err) => {
    console.warn('[network] initial route resolution failed:', err);
  });

  installKerberosLifecycle();

  app.on('will-quit', () => localProxySupervisor.stop());

  ipcMain.handle('perimeter:get-state', () => buildStateView());
  ipcMain.handle('perimeter:recheck', async () => {
    await refreshNetworkRoutes('user requested');
    return buildStateView();
  });

  ipcMain.handle('llm-gateway:get-status', () => getLlmGatewayRowStatus());
  ipcMain.handle('llm-gateway:retry', () => retryLlmGatewayRowStatus());

  ipcMain.handle('px-proxy:get-status', () => getPxProxyRowStatus());

  ipcMain.handle(
    'perimeter:kinit',
    async (_event, payload: unknown): Promise<PerimeterKinitResult> => {
      if (
        payload === null ||
        typeof payload !== 'object' ||
        !(
          (payload as { principal?: unknown }).principal === undefined ||
          (payload as { principal?: unknown }).principal === null ||
          typeof (payload as { principal?: unknown }).principal === 'string'
        ) ||
        typeof (payload as { password?: unknown }).password !== 'string'
      ) {
        return {
          ok: false,
          reason: 'bad_request',
          message: 'kinit: a password is required.',
        };
      }
      const { principal, password } = payload as {
        principal: string | null | undefined;
        password: string;
      };
      const result = await runKinit(principal, password);
      recordKinitOutcome('kinit', result.ok, result.ok ? undefined : result.reason);
      if (!result.ok) {
        return { ok: false, reason: result.reason, message: result.message };
      }
      return { ok: true, state: await buildStateView() };
    },
  );

  ipcMain.handle(
    'perimeter:kinit-renew',
    async (_event, payload?: unknown): Promise<PerimeterKinitResult> => {
      const principal =
        payload !== null &&
        typeof payload === 'object' &&
        typeof (payload as { principal?: unknown }).principal === 'string'
          ? (payload as { principal: string }).principal
          : null;
      const result = await runKinitRenew(undefined, principal);
      recordKinitOutcome('renew', result.ok, result.ok ? undefined : result.reason);
      if (!result.ok) {
        return { ok: false, reason: result.reason, message: result.message };
      }
      return { ok: true, state: await buildStateView() };
    },
  );

  ipcMain.handle('kerberos:set-resolved-principal', (_event, payload: unknown): void => {
    setResolvedKerberosPrincipal(typeof payload === 'string' ? payload : null);
  });

  ipcMain.handle('kerberos:get-preferences', (): Promise<KerberosPreferences> =>
    loadKerberosPreferences(kerberosPreferencesPath(app.getPath('userData'))),
  );
  ipcMain.handle(
    'kerberos:set-preferences',
    (_event, payload: unknown): Promise<KerberosPreferences> =>
      saveKerberosPreferences(kerberosPreferencesPath(app.getPath('userData')), payload),
  );
}

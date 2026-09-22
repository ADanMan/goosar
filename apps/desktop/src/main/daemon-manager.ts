import { app, ipcMain, BrowserWindow, Notification, shell } from 'electron';
import { execFile } from 'child_process';
import { readFile, writeFile, mkdir, rm, open, stat } from 'fs/promises';
import { existsSync, readdirSync, watchFile, unwatchFile, type StatsListener } from 'fs';
import { join } from 'path';
import { homedir, hostname } from 'os';
import type {
  DaemonStatus,
  DaemonPrefs,
  LocalRuntimeProbe,
  DoctorReport,
} from '../shared/daemon-types';
import { daemonStatusAlive } from '../shared/daemon-types';
import { ensureManagedCli, managedCliPath } from './cli-bootstrap';
import { describeCliPathResult, installCliOnPath } from './cli-path';
import { isDaemonReadinessTimeout } from './daemon-start-verdict';
import { decideIdleRestartAction, decideVersionAction } from './version-decision';
import {
  daemonLifecycleUnreachable,
  isDaemonExternallyManaged,
  normalizeHostOS,
} from './daemon-os';
import { classifyAuthProbe, isAuthStatusError, type AuthProbeResult } from './daemon-auth-probe';
import {
  agentPathEnvValue,
  bundledAgentVersion,
  ensureAgentRuntime,
  ensureBashProbe,
  readAgentRuntimeStatus,
  type AgentRuntimeContext,
} from './agent-bootstrap';
import { ensureBashFullFlag } from './agent-config';
import { loadAgentRunner, saveAgentRunner } from './agent-runner-preference';
import { appendStagedMcpServerPath, ensureBundledMcpServers } from './mcp-servers';
import { ensureBundledSkills, playwrightBrowsersEnv, type PlaywrightBundleInfo } from './skills';
import {
  bindProvisioningCredentials,
  provisioningAuthGap,
  resolveProvisionedRuntimeDir,
  resolvedProvisioningAuth,
  setProvisioningCredentials,
  setProvisioningRestartPending,
  syncProvisionedPackages,
} from './provisioning';
import {
  syncDeploymentClientSecrets,
  type ClientSecretsSyncResult,
  type ClientSecretsSyncState,
} from './agent-client-secrets';
import type { ProvisioningRestartOutcome } from '../shared/provisioning-status';
import type { AgentPathContext } from './agent-bootstrap';
import {
  AGENT_BASH_FULL_FIELD,
  AGENT_RUNNER_CHOICES,
  type AgentRunnerChoice,
  type AgentRunnerSetResult,
  type AgentRunnerState,
  type AgentRuntimeStatus,
} from '../shared/agent-runtime-types';
import type { DaemonStopReport } from '../shared/uninstall-types';
import { mainApiFetch, networkSpawnEnvVars, setPerimeterServerHost } from './perimeter';
import type { DaemonReachability, PerimeterDaemonRestartOutcome } from '../shared/perimeter-config';

const PLAYWRIGHT_RUNTIME_PACKAGE_NAME = 'playwright-browsers';

const DEFAULT_HEALTH_PORT = 19514;
const POLL_INTERVAL_MS = 5_000;
const PREFS_PATH = join(homedir(), '.goosar', 'desktop_prefs.json');
const CLIENT_SECRETS_STATE_PATH = join(homedir(), '.goosar', 'client_secrets_state.json');
const LOG_TAIL_RETRY_MS = 2_000;
const LOG_TAIL_MAX_RETRIES = 5;
const AUTH_PROBE_GRACE_MS = 10_000;
const DAEMON_START_EXEC_TIMEOUT_MS = 60_000;

const DEFAULT_PREFS: DaemonPrefs = { autoStart: true, autoStop: false };

interface ActiveProfile {
  name: string; 
  port: number;
}

let statusPollTimer: ReturnType<typeof setInterval> | null = null;
let logTailWatcher: { path: string; listener: StatsListener } | null = null;
let currentState: DaemonStatus['state'] = 'installing_cli';
let getMainWindow: () => BrowserWindow | null = () => null;
let operationInProgress = false;
let cachedCliBinary: string | null | undefined = undefined;
let cliResolvePromise: Promise<string | null> | null = null;
let cachedCliBinaryVersion: string | null | undefined = undefined;

export async function getCliBinaryPath(): Promise<string | null> {
  return resolveCliBinary();
}

let cliPathAttempted = false;
async function exposeCliOnPathOnce(): Promise<void> {
  if (cliPathAttempted) return;
  cliPathAttempted = true;
  const bin = await resolveCliBinary();
  if (!bin) return;
  const result = await installCliOnPath({ target: bin });
  console.log(`[cli-path] ${result.state}`);
  if (result.state === 'installed' || result.state === 'already') return;
  const lang = app.getPreferredSystemLanguages()[0]?.startsWith('ru') ? 'ru' : 'en';
  if (Notification.isSupported()) {
    new Notification({
      title: lang === 'ru' ? 'Команда goosar не установлена' : 'goosar command not installed',
      body: describeCliPathResult(result, lang),
    }).show();
  }
}
let pendingVersionRestart = false;
let pendingProvisioningRestart = false;
let targetApiBaseUrl: string | null = null;
let activeProfile: ActiveProfile | null = null;

let startingSince: number | null = null;
let authProbeDone = false;
let authExpired = false;

let configWriteChain: Promise<void> = Promise.resolve();

function healthPortForProfile(profile: string): number {
  if (!profile) return DEFAULT_HEALTH_PORT;
  let sum = 0;
  for (const b of Buffer.from(profile, 'utf-8')) sum += b;
  return DEFAULT_HEALTH_PORT + 1 + (sum % 1000);
}

function profileDir(profile: string): string {
  return profile ? join(homedir(), '.goosar', 'profiles', profile) : join(homedir(), '.goosar');
}

function profileConfigPath(profile: string): string {
  return join(profileDir(profile), 'config.json');
}

function profileLogPath(profile: string): string {
  return join(profileDir(profile), 'daemon.log');
}

function profileUserIdPath(profile: string): string {
  return join(profileDir(profile), '.desktop-user-id');
}

async function readProfileUserId(profile: string): Promise<string | null> {
  try {
    const raw = await readFile(profileUserIdPath(profile), 'utf-8');
    const trimmed = raw.trim();
    return trimmed || null;
  } catch {
    return null;
  }
}

async function writeProfileUserId(profile: string, userId: string): Promise<void> {
  await mkdir(profileDir(profile), { recursive: true });
  await writeFile(profileUserIdPath(profile), userId, 'utf-8');
}

async function removeProfileUserId(profile: string): Promise<void> {
  try {
    await rm(profileUserIdPath(profile));
  } catch {
    // Already gone — nothing to do.
  }
}

function normalizeUrl(u: string): string {
  if (!u) return '';
  try {
    const parsed = new URL(u);
    return `${parsed.protocol}//${parsed.host}`.toLowerCase();
  } catch {
    return u.replace(/\/+$/, '').toLowerCase();
  }
}

function urlsMatch(a: string, b: string): boolean {
  const na = normalizeUrl(a);
  const nb = normalizeUrl(b);
  return na.length > 0 && na === nb;
}

function sendStatus(status: DaemonStatus): void {
  const win = getMainWindow();
  win?.webContents.send('daemon:status', status);
}

interface HealthPayload {
  status?: string;
  pid?: number;
  os?: string;
  uptime?: string;
  daemon_id?: string;
  device_name?: string;
  server_url?: string;
  cli_version?: string;
  active_task_count?: number;
  agents?: string[];
  workspaces?: unknown[];
}

async function fetchHealthAtPort(port: number): Promise<HealthPayload | null> {
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 2_000);
    const res = await fetch(`http://127.0.0.1:${port}/health`, {
      signal: controller.signal,
    });
    clearTimeout(timeout);
    if (!res.ok) return null;
    return (await res.json()) as HealthPayload;
  } catch {
    return null;
  }
}

async function probeTokenValidity(profile: string): Promise<AuthProbeResult> {
  if (!targetApiBaseUrl) return 'unknown';
  const cfg = await readProfileConfig(profile);
  const token = typeof cfg.token === 'string' ? cfg.token : '';
  if (!token) return classifyAuthProbe({ noToken: true });
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 4_000);
    const res = await mainApiFetch(`${targetApiBaseUrl.replace(/\/+$/, '')}/api/me`, {
      headers: { Authorization: `Bearer ${token}` },
      signal: controller.signal,
    });
    clearTimeout(timeout);
    return classifyAuthProbe({ status: res.status });
  } catch {
    return classifyAuthProbe({ networkError: true });
  }
}

function deriveProfileName(targetUrl: string): string {
  try {
    const url = new URL(targetUrl);
    const host = url.host.replace(/:/g, '-').toLowerCase();
    return `desktop-${host}`;
  } catch {
    return 'desktop';
  }
}

async function readProfileConfig(profile: string): Promise<Record<string, unknown>> {
  try {
    const raw = await readFile(profileConfigPath(profile), 'utf-8');
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch {
    return {};
  }
}

async function writeProfileConfig(profile: string, cfg: Record<string, unknown>): Promise<void> {
  const op = async () => {
    await mkdir(profileDir(profile), { recursive: true });
    await writeFile(profileConfigPath(profile), JSON.stringify(cfg, null, 2), 'utf-8');
  };
  const next = configWriteChain.catch(() => {}).then(op);
  configWriteChain = next.catch(() => {});
  return next;
}

async function resolveActiveProfile(): Promise<ActiveProfile> {
  const target = targetApiBaseUrl;
  if (!target) return { name: '', port: DEFAULT_HEALTH_PORT };

  const name = deriveProfileName(target);
  const cfg = await readProfileConfig(name);

  if (cfg.server_url !== target) {
    cfg.server_url = target;
    await writeProfileConfig(name, cfg);
    console.log(`[daemon] initialized profile "${name}" → ${target}`);
  }

  return { name, port: healthPortForProfile(name) };
}

async function ensureActiveProfile(): Promise<ActiveProfile> {
  if (activeProfile) return activeProfile;
  activeProfile = await resolveActiveProfile();
  return activeProfile;
}

function invalidateActiveProfile(): void {
  activeProfile = null;
}

async function fetchHealth(): Promise<DaemonStatus> {
  if (currentState === 'installing_cli' || currentState === 'cli_not_found') {
    return { state: currentState };
  }

  const active = await ensureActiveProfile();
  const data = await fetchHealthAtPort(active.port);

  if (!data || data.status !== 'running') {
    if (
      currentState === 'starting' &&
      !authExpired &&
      !authProbeDone &&
      startingSince !== null &&
      Date.now() - startingSince >= AUTH_PROBE_GRACE_MS
    ) {
      authProbeDone = true;
      if ((await probeTokenValidity(active.name)) === 'auth_expired') {
        authExpired = true;
      }
    }
    if (authExpired) {
      return { state: 'auth_expired', profile: active.name };
    }
    if (data?.status === 'starting') {
      return { state: 'starting', profile: active.name };
    }
    return {
      state: currentState === 'starting' ? 'starting' : 'stopped',
      profile: active.name,
    };
  }

  authExpired = false;
  startingSince = null;

  const externallyManaged = isDaemonExternallyManaged(data.os, normalizeHostOS(process.platform));

  if (targetApiBaseUrl && data.server_url && !urlsMatch(data.server_url, targetApiBaseUrl)) {
    invalidateActiveProfile();
    return { state: 'stopped' };
  }

  return {
    state: 'running',
    pid: data.pid,
    uptime: data.uptime,
    daemonId: data.daemon_id,
    deviceName: data.device_name,
    agents: data.agents ?? [],
    workspaceCount: Array.isArray(data.workspaces) ? data.workspaces.length : 0,
    profile: active.name,
    serverUrl: data.server_url,
    externallyManaged,
  };
}

function findCliOnPath(): string | null {
  const candidates = process.platform === 'win32' ? ['goosar.exe'] : ['goosar'];
  const paths = (process.env['PATH'] ?? '').split(process.platform === 'win32' ? ';' : ':');
  if (process.platform === 'darwin') {
    paths.push('/opt/homebrew/bin', '/usr/local/bin');
  }
  for (const name of candidates) {
    for (const dir of paths) {
      const full = join(dir, name);
      if (existsSync(full)) return full;
    }
  }
  return null;
}

function bundledCliPath(): string {
  const binName = process.platform === 'win32' ? 'goosar.exe' : 'goosar';
  return join(app.getAppPath(), 'resources', 'bin', binName).replace(
    'app.asar',
    'app.asar.unpacked',
  );
}

async function probeCliBinary(
  bin: string,
  source: 'bundled' | 'managed' | 'path',
): Promise<string | null> {
  try {
    const stdout = await new Promise<string>((resolve, reject) => {
      execFile(bin, ['version', '--output', 'json'], { timeout: 5_000 }, (err, out) => {
        if (err) reject(err);
        else resolve(out);
      });
    });
    const parsed = JSON.parse(stdout) as { version?: string };
    if (typeof parsed.version === 'string' && parsed.version.length > 0) {
      return parsed.version;
    }
    console.warn(
      `[daemon] ignoring ${source} CLI at ${bin}: version output was missing or invalid`,
    );
    return null;
  } catch (err) {
    console.warn(`[daemon] ignoring ${source} CLI at ${bin}:`, err);
    return null;
  }
}

async function resolveCliBinary(): Promise<string | null> {
  if (cachedCliBinary !== undefined) return cachedCliBinary;
  if (cliResolvePromise) return cliResolvePromise;

  cliResolvePromise = (async () => {
    const bundled = bundledCliPath();
    if (existsSync(bundled)) {
      const version = await probeCliBinary(bundled, 'bundled');
      if (version) {
        console.log(`[daemon] using bundled CLI at ${bundled}`);
        cachedCliBinary = bundled;
        cachedCliBinaryVersion = version;
        return bundled;
      }
    }

    const managed = managedCliPath();
    if (existsSync(managed)) {
      const version = await probeCliBinary(managed, 'managed');
      if (version) {
        cachedCliBinary = managed;
        cachedCliBinaryVersion = version;
        return managed;
      }
    }

    try {
      const installed = await ensureManagedCli({
        forceInstall: existsSync(managed),
      });
      const version = await probeCliBinary(installed, 'managed');
      if (version) {
        cachedCliBinary = installed;
        cachedCliBinaryVersion = version;
        return installed;
      }
      console.warn(`[daemon] managed CLI at ${installed} failed validation after install`);
    } catch (err) {
      console.warn('[daemon] CLI auto-install failed, falling back to PATH:', err);
    }

    const onPath = findCliOnPath();
    if (onPath) {
      const version = await probeCliBinary(onPath, 'path');
      if (version) {
        cachedCliBinary = onPath;
        cachedCliBinaryVersion = version;
        return onPath;
      }
    }

    cachedCliBinary = null;
    cachedCliBinaryVersion = null;
    return null;
  })();

  try {
    return await cliResolvePromise;
  } finally {
    cliResolvePromise = null;
  }
}

async function getCliBinaryVersion(): Promise<string | null> {
  if (cachedCliBinaryVersion !== undefined) return cachedCliBinaryVersion;
  const bin = await resolveCliBinary();
  if (!bin) {
    cachedCliBinaryVersion = null;
    return null;
  }
  cachedCliBinaryVersion = await probeCliBinary(bin, 'path');
  return cachedCliBinaryVersion;
}

async function ensureRunningDaemonVersionMatches(): Promise<
  'restarted' | 'deferred' | 'ok' | 'not_running'
> {
  const active = await ensureActiveProfile();
  const running = await fetchHealthAtPort(active.port);

  if (isDaemonExternallyManaged(running?.os, normalizeHostOS(process.platform))) {
    pendingVersionRestart = false;
    return 'ok';
  }

  const bundled = await getCliBinaryVersion();
  const action = decideVersionAction(bundled, running);

  switch (action) {
    case 'not_running':
      pendingVersionRestart = false;
      return 'not_running';
    case 'ok':
      pendingVersionRestart = false;
      return 'ok';
    case 'defer': {
      if (!pendingVersionRestart) {
        const activeTasks = running?.active_task_count ?? 0;
        console.log(
          `[daemon] CLI version mismatch (bundled=${bundled} running=${running?.cli_version}); deferring restart until ${activeTasks} active task(s) finish`,
        );
      }
      pendingVersionRestart = true;
      return 'deferred';
    }
    case 'restart':
      console.log(
        `[daemon] CLI version mismatch (bundled=${bundled} running=${running?.cli_version}) — restarting daemon`,
      );
      pendingVersionRestart = false;
      await restartDaemon();
      return 'restarted';
  }
}

let provisioningRestartInFlight: Promise<ProvisioningRestartOutcome> | null = null;

async function ensureProvisioningRestartIfNeeded(): Promise<ProvisioningRestartOutcome> {
  if (provisioningRestartInFlight) return provisioningRestartInFlight;
  const pending = ensureProvisioningRestartIfNeededUncached();
  provisioningRestartInFlight = pending;
  try {
    return await pending;
  } finally {
    if (provisioningRestartInFlight === pending) {
      provisioningRestartInFlight = null;
    }
  }
}

async function ensureProvisioningRestartIfNeededUncached(): Promise<ProvisioningRestartOutcome> {
  if (!pendingProvisioningRestart) return 'ok';

  const active = await ensureActiveProfile();
  const running = await fetchHealthAtPort(active.port);

  if (isDaemonExternallyManaged(running?.os, normalizeHostOS(process.platform))) {
    pendingProvisioningRestart = false;
    return 'ok';
  }

  const action = decideIdleRestartAction(running);
  switch (action) {
    case 'not_running':
      pendingProvisioningRestart = false;
      return 'not_running';
    case 'defer': {
      const activeTasks = running?.active_task_count ?? 0;
      console.log(
        `[daemon] provisioning installed/upgraded package(s) while the daemon was busy; deferring restart until ${activeTasks} active task(s) finish`,
      );
      return 'deferred';
    }
    case 'restart':
      console.log(
        '[daemon] provisioning installed/upgraded package(s) — restarting daemon so the new PATH/env takes effect',
      );
      pendingProvisioningRestart = false;
      await restartDaemon();
      void syncAgentClientSecretsIfPossible();
      return 'restarted';
  }
}

async function settleProvisioningRestartPending(): Promise<ProvisioningRestartOutcome> {
  const outcome = await ensureProvisioningRestartIfNeeded();
  if (outcome !== 'deferred') {
    setProvisioningRestartPending(false);
  }
  return outcome;
}

export function notifyProvisioningPackagesChanged(): void {
  pendingProvisioningRestart = true;
  setProvisioningRestartPending(true);
  void settleProvisioningRestartPending();
}

export async function requestProvisioningRestartNow(): Promise<ProvisioningRestartOutcome> {
  pendingProvisioningRestart = true;
  setProvisioningRestartPending(true);
  return settleProvisioningRestartPending();
}

export async function isDaemonBusyForProvisioningSwap(): Promise<boolean> {
  const active = await ensureActiveProfile();
  const running = await fetchHealthAtPort(active.port);
  if (isDaemonExternallyManaged(running?.os, normalizeHostOS(process.platform))) {
    return false;
  }
  return decideIdleRestartAction(running) === 'defer';
}

async function mintPat(jwt: string): Promise<string> {
  if (!targetApiBaseUrl) {
    throw new Error('mint PAT: target API URL not set');
  }
  const url = `${targetApiBaseUrl.replace(/\/+$/, '')}/api/tokens`;
  const res = await mainApiFetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${jwt}`,
    },
    body: JSON.stringify({ name: 'Goosar Desktop' }),
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw Object.assign(new Error(`mint PAT failed: ${res.status} ${res.statusText} ${body}`), {
      status: res.status,
    });
  }
  const data = (await res.json()) as { token?: unknown };
  if (typeof data.token !== 'string' || !data.token.startsWith('gsl_')) {
    throw new Error('mint PAT: response missing token');
  }
  return data.token;
}

async function syncToken(tokenFromRenderer: string, userId: string): Promise<void> {
  const active = await ensureActiveProfile();
  const config = await readProfileConfig(active.name);
  const previousUserId = await readProfileUserId(active.name);
  const userChanged = Boolean(previousUserId) && previousUserId !== userId;
  const sameUserWithCachedPat =
    !userChanged &&
    previousUserId === userId &&
    typeof config.token === 'string' &&
    config.token.startsWith('gsl_');

  let finalToken: string;
  if (tokenFromRenderer.startsWith('gsl_')) {
    finalToken = tokenFromRenderer;
  } else if (sameUserWithCachedPat) {
    finalToken = config.token as string;
  } else {
    try {
      finalToken = await mintPat(tokenFromRenderer);
      console.log(`[daemon] minted PAT for profile "${active.name}" (user_changed=${userChanged})`);
    } catch (err) {
      console.error('[daemon] failed to mint PAT:', err);
      throw err;
    }
  }

  config.token = finalToken;
  if (targetApiBaseUrl) config.server_url = targetApiBaseUrl;
  await writeProfileConfig(active.name, config);
  await writeProfileUserId(active.name, userId);

  void exposeCliOnPathOnce().catch((err) => console.warn('[cli-path] failed:', err));

  const provisioningBound = bindProvisioningCredentials(targetApiBaseUrl, finalToken, {
    ctx: { home: homedir(), env: process.env },
  });
  void syncAgentClientSecretsIfPossible();
  void bindProvisioningThenSyncSecrets(provisioningBound);

  if (userChanged) {
    try {
      const existing = await fetchHealthAtPort(active.port);
      if (daemonStatusAlive(existing?.status)) {
        console.log('[daemon] user switched — restarting daemon with new credentials');
        void restartDaemon();
      }
    } catch (err) {
      console.warn('[daemon] restart-on-user-switch failed:', err);
    }
  }
}

async function loadPrefs(): Promise<DaemonPrefs> {
  try {
    const raw = await readFile(PREFS_PATH, 'utf-8');
    const parsed = JSON.parse(raw);
    return { ...DEFAULT_PREFS, ...parsed };
  } catch {
    return { ...DEFAULT_PREFS };
  }
}

async function savePrefs(prefs: DaemonPrefs): Promise<void> {
  const dir = join(homedir(), '.goosar');
  await mkdir(dir, { recursive: true });
  await writeFile(PREFS_PATH, JSON.stringify(prefs, null, 2), 'utf-8');
}

async function clearToken(): Promise<void> {
  const active = await ensureActiveProfile();
  const config = await readProfileConfig(active.name);
  if ('token' in config) {
    delete config.token;
    await writeProfileConfig(active.name, config);
  }
  await removeProfileUserId(active.name);
  setProvisioningCredentials(null, null);
}

export type ReauthResult =
  | { ok: true }
  | { ok: false; reason: 'session_invalid' }
  | { ok: false; reason: 'transient'; message: string };

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

async function reauthenticate(token: string, userId: string): Promise<ReauthResult> {
  try {
    await clearToken();
    await syncToken(token, userId);
  } catch (err) {
    if (isAuthStatusError(err)) return { ok: false, reason: 'session_invalid' };
    return { ok: false, reason: 'transient', message: errorMessage(err) };
  }
  const restart = await restartDaemon();
  if (!restart.success) {
    return {
      ok: false,
      reason: 'transient',
      message: restart.error ?? 'failed to restart daemon',
    };
  }
  return { ok: true };
}

async function withGuard<T>(fn: () => Promise<T>): Promise<T | { success: false; error: string }> {
  if (operationInProgress) {
    return { success: false, error: 'Another daemon operation is in progress' };
  }
  operationInProgress = true;
  try {
    return await fn();
  } finally {
    operationInProgress = false;
  }
}

function profileArgs(active: ActiveProfile): string[] {
  return active.name ? ['--profile', active.name] : [];
}

function successfulRuntimeProbe(
  providers: string[],
  daemonRunning: boolean,
): Extract<LocalRuntimeProbe, { probeResult: 'success' }> {
  const providerSummary: Record<string, number> = {};
  for (const rawProvider of providers) {
    const provider = rawProvider.trim().toLowerCase();
    if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(provider)) continue;
    providerSummary[provider] = (providerSummary[provider] ?? 0) + 1;
  }
  const runtimeCount = Object.values(providerSummary).reduce((sum, count) => sum + count, 0);
  return {
    probeResult: 'success',
    runtimeCount,
    providerSummary,
    onlineCount: daemonRunning ? runtimeCount : 0,
    offlineCount: daemonRunning ? 0 : runtimeCount,
  };
}

const DOCTOR_CACHE_MS = 5 * 60 * 1000;
const DOCTOR_TIMEOUT_MS = 20_000;
let doctorCache: DoctorReport | null = null;
let doctorInFlight: Promise<DoctorReport | null> | null = null;

async function runDoctor(opts: { refresh?: boolean } = {}): Promise<DoctorReport | null> {
  if (!opts.refresh && doctorCache && Date.now() - doctorCache.checkedAt < DOCTOR_CACHE_MS) {
    return doctorCache;
  }
  if (doctorInFlight) return doctorInFlight;
  doctorInFlight = (async () => {
    const bin = await resolveCliBinary();
    if (!bin) return null;
    const active = await ensureActiveProfile();
    return new Promise<DoctorReport | null>((resolve) => {
      execFile(
        bin,
        ['doctor', '--server', '--output', 'json', ...profileArgs(active)],
        { timeout: DOCTOR_TIMEOUT_MS, env: desktopSpawnEnv(), maxBuffer: 256 * 1024 },
        (_error, stdout) => {
          try {
            const parsed = JSON.parse(stdout) as { ok?: unknown; results?: unknown };
            if (!Array.isArray(parsed.results)) {
              resolve(null);
              return;
            }
            const results = parsed.results.filter(
              (r): r is DoctorReport['results'][number] =>
                !!r && typeof r === 'object' && typeof (r as { id?: unknown }).id === 'string',
            );
            doctorCache = { ok: parsed.ok === true, results, checkedAt: Date.now() };
            resolve(doctorCache);
          } catch {
            resolve(null);
          }
        },
      );
    });
  })();
  try {
    return await doctorInFlight;
  } finally {
    doctorInFlight = null;
  }
}

async function probeLocalRuntimes(): Promise<LocalRuntimeProbe> {
  const health = await fetchHealth();
  if (health.state === 'running') {
    return successfulRuntimeProbe(health.agents ?? [], true);
  }

  const bin = await resolveCliBinary();
  if (!bin) return { probeResult: 'error' };
  const active = await ensureActiveProfile();
  return new Promise((resolve) => {
    execFile(
      bin,
      ['daemon', 'probe-runtimes', ...profileArgs(active)],
      { timeout: 15_000, env: desktopSpawnEnv(), maxBuffer: 64 * 1024 },
      (error, stdout) => {
        if (error) {
          resolve({ probeResult: 'error' });
          return;
        }
        try {
          const parsed = JSON.parse(stdout) as {
            probe_result?: unknown;
            runtime_count?: unknown;
            provider_summary?: unknown;
          };
          if (
            parsed.probe_result !== 'success' ||
            typeof parsed.runtime_count !== 'number' ||
            !parsed.provider_summary ||
            typeof parsed.provider_summary !== 'object' ||
            Array.isArray(parsed.provider_summary)
          ) {
            resolve({ probeResult: 'error' });
            return;
          }
          const providers: string[] = [];
          for (const [provider, count] of Object.entries(
            parsed.provider_summary as Record<string, unknown>,
          )) {
            if (!Number.isInteger(count) || (count as number) < 0 || (count as number) > 1000) {
              resolve({ probeResult: 'error' });
              return;
            }
            providers.push(...Array<string>(count as number).fill(provider));
          }
          const probe = successfulRuntimeProbe(providers, false);
          resolve(probe.runtimeCount === parsed.runtime_count ? probe : { probeResult: 'error' });
        } catch {
          resolve({ probeResult: 'error' });
        }
      },
    );
  });
}

function bundledAgentArtifactPath(): string | null {
  const dir = app.isPackaged
    ? join(process.resourcesPath, 'hermes')
    : join(app.getAppPath(), 'resources-agent', 'hermes');
  return existsSync(join(dir, 'bin', 'hermes')) ? dir : null;
}

function bundledMcpServersDir(): string | null {
  const dir = app.isPackaged
    ? join(process.resourcesPath, 'mcp-servers')
    : join(app.getAppPath(), 'resources-mcp-servers');
  return existsSync(dir) ? dir : null;
}

function bundledSkillsDir(): string | null {
  const dir = app.isPackaged
    ? join(process.resourcesPath, 'skills')
    : join(app.getAppPath(), 'resources-skills');
  return existsSync(dir) ? dir : null;
}

function bundledPlaywrightHasBrowsers(dir: string): boolean {
  try {
    return readdirSync(dir, { withFileTypes: true }).some((entry) => entry.isDirectory());
  } catch {
    return false;
  }
}

function bundledPlaywrightBundleInfo(): PlaywrightBundleInfo | null {
  const dir = app.isPackaged
    ? join(process.resourcesPath, 'playwright')
    : join(app.getAppPath(), 'resources-playwright');
  if (!existsSync(dir)) return null;
  return { dir, hasBrowsers: bundledPlaywrightHasBrowsers(dir) };
}

function playwrightRuntimeEnv(): Record<string, string> {
  const provisioned = resolveProvisionedRuntimeDir(
    { home: homedir(), env: process.env } satisfies AgentPathContext,
    PLAYWRIGHT_RUNTIME_PACKAGE_NAME,
  );
  return playwrightBrowsersEnv(bundledPlaywrightBundleInfo(), provisioned, process.env);
}

function agentRuntimeContext(): AgentRuntimeContext {
  return {
    home: homedir(),
    env: process.env,
    bundledArtifactDir: bundledAgentArtifactPath(),
    platform: process.platform,
  };
}

let agentBootstrapPromise: Promise<AgentRuntimeStatus> | null = null;
let lastAgentStatus: AgentRuntimeStatus = { state: 'checking' };

let mcpServersStagePromise: Promise<void> | null = null;

function stageBundledMcpServers(): Promise<void> {
  if (!mcpServersStagePromise) {
    mcpServersStagePromise = ensureBundledMcpServers(
      { home: homedir(), env: process.env },
      bundledMcpServersDir(),
    )
      .then((status) => {
        if (status.staged.length > 0) {
          console.log(`[mcp-servers] staged ${status.staged.join(', ')}`);
        }
        if (status.refused.length > 0) {
          console.error(`[mcp-servers] refused (manifest mismatch): ${status.refused.join(', ')}`);
        }
      })
      .catch((err) => {
        console.warn('[mcp-servers] staging failed:', err);
      });
  }
  return mcpServersStagePromise;
}

let skillsStagePromise: Promise<void> | null = null;

function stageBundledSkills(): Promise<void> {
  if (!skillsStagePromise) {
    skillsStagePromise = ensureBundledSkills(
      { home: homedir(), env: process.env },
      bundledSkillsDir(),
    )
      .then((status) => {
        if (status.staged.length > 0) {
          console.log(`[skills] staged ${status.staged.join(', ')}`);
        }
        if (status.refused.length > 0) {
          console.error(`[skills] refused (manifest mismatch): ${status.refused.join(', ')}`);
        }
      })
      .catch((err) => {
        console.warn('[skills] staging failed:', err);
      });
  }
  return skillsStagePromise;
}

let provisioningSyncPromise: Promise<void> | null = null;

function stageProvisionedPackages(): Promise<void> {
  if (!provisioningSyncPromise) {
    provisioningSyncPromise = syncProvisionedPackages({
      ctx: { home: homedir(), env: process.env },
    }).catch((err) => {
      console.warn('[provisioning] sync failed:', err);
    });
  }
  return provisioningSyncPromise;
}

async function finishAgentInstall(status: AgentRuntimeStatus): Promise<void> {
  if (status.state !== 'ready' && status.state !== 'needs_config') return;
  const ctx: AgentPathContext = { home: homedir(), env: process.env };
  try {
    const bashFull = await ensureBashFullFlag(ctx);
    if (!bashFull.ok) {
      lastBashFullFailure = describeBashFullFailure(bashFull.message);
      console.warn(`[agent-bootstrap] security.bash_full write failed: ${lastBashFullFailure}`);
    } else {
      lastBashFullFailure = null;
    }
  } catch (err) {
    console.warn('[agent-bootstrap] security.bash_full write threw:', err);
  }
  try {
    const probe = await ensureBashProbe(ctx, status.binPath);
    if (!probe.skipped) {
      lastBashProbeResult = probe;
      console.log(
        probe.ok
          ? '[agent-bootstrap] bash probe: ok'
          : `[agent-bootstrap] bash probe failed: ${probe.reason}`,
      );
    }
  } catch (err) {
    console.warn('[agent-bootstrap] bash probe threw:', err);
  }
}

let lastBashProbeResult: { ok: boolean; reason?: string } | null = null;

export function getLastBashProbeResult(): { ok: boolean; reason?: string } | null {
  return lastBashProbeResult;
}

export function describeBashFullFailure(message: string): string {
  const match = message.match(/^([a-zA-Z0-9_.-]+):\s*(.+)$/s);
  if (match && match[1] !== AGENT_BASH_FULL_FIELD) {
    return `config.user.yaml невалиден: ${match[1]}: ${match[2]}`;
  }
  return message;
}

let lastBashFullFailure: string | null = null;

export function getLastBashFullFailure(): string | null {
  return lastBashFullFailure;
}

async function readClientSecretsState(): Promise<ClientSecretsSyncState> {
  try {
    const raw = await readFile(CLIENT_SECRETS_STATE_PATH, 'utf-8');
    const parsed = JSON.parse(raw) as Partial<ClientSecretsSyncState>;
    return { issuedApiKeyHash: parsed.issuedApiKeyHash ?? null };
  } catch {
    return { issuedApiKeyHash: null };
  }
}

async function writeClientSecretsState(state: ClientSecretsSyncState): Promise<void> {
  const dir = join(homedir(), '.goosar');
  await mkdir(dir, { recursive: true });
  await writeFile(CLIENT_SECRETS_STATE_PATH, JSON.stringify(state), 'utf-8');
}

export function clientSecretsSyncSkipReason(): string | null {
  const gap = provisioningAuthGap();
  if (gap) return gap;
  if (lastAgentStatus?.state !== 'ready' && lastAgentStatus?.state !== 'needs_config') {
    return `runtime not ready (state=${lastAgentStatus?.state ?? 'unknown'})`;
  }
  return null;
}

const loggedClientSecretsSkipReasons = new Set<string>();

export async function bindProvisioningThenSyncSecrets(
  bindPromise: Promise<void>,
): Promise<ClientSecretsSyncResult | null> {
  await bindPromise;
  return syncAgentClientSecretsIfPossible();
}

async function syncAgentClientSecretsIfPossible(): Promise<ClientSecretsSyncResult | null> {
  const skipReason = clientSecretsSyncSkipReason();
  if (skipReason) {
    if (!loggedClientSecretsSkipReasons.has(skipReason)) {
      loggedClientSecretsSkipReasons.add(skipReason);
      console.log(`[client-secrets] skipped: ${skipReason}`);
    }
    return null;
  }
  const auth = resolvedProvisioningAuth();
  if (!auth) return null;
  const state = await readClientSecretsState();
  const result = await syncDeploymentClientSecrets(
    { home: homedir(), env: process.env },
    auth,
    mainApiFetch,
    undefined,
    state,
  );
  console.log(`[client-secrets] ${result.note}`);
  if (result.nextState !== state) {
    await writeClientSecretsState(result.nextState).catch((err) =>
      console.warn('[client-secrets] failed to persist sync state:', err),
    );
  }
  return result;
}

export async function resyncClientSecretsForRetry(): Promise<{
  deploymentApiBase: string | null;
} | null> {
  const result = await syncAgentClientSecretsIfPossible();
  return result ? { deploymentApiBase: result.deploymentApiBase } : null;
}

const NO_RUNNER_STATUS: AgentRuntimeStatus = {
  state: 'not_installed',
  detail: 'No agent runner is selected.',
};

async function runAgentBootstrap(): Promise<AgentRuntimeStatus> {
  void stageBundledMcpServers();
  void stageBundledSkills();
  void stageProvisionedPackages();
  if ((await loadAgentRunner(PREFS_PATH)) !== 'hermes') {
    lastAgentStatus = NO_RUNNER_STATUS;
    return NO_RUNNER_STATUS;
  }
  return runBundledAgentBootstrap();
}

function runBundledAgentBootstrap(): Promise<AgentRuntimeStatus> {
  return ensureAgentRuntime(agentRuntimeContext())
    .then(async (status) => {
      lastAgentStatus = status;
      console.log(`[agent-bootstrap] runtime state: ${status.state}`);
      await finishAgentInstall(status);
      void syncAgentClientSecretsIfPossible();
      return status;
    })
    .catch((err) => {
      console.warn('[agent-bootstrap] bootstrap threw:', err);
      const status: AgentRuntimeStatus = {
        state: 'not_installed',
        detail: err instanceof Error ? err.message : String(err),
      };
      lastAgentStatus = status;
      return status;
    });
}

function bootstrapAgentRuntime(): Promise<AgentRuntimeStatus> {
  if (!agentBootstrapPromise) {
    agentBootstrapPromise = runAgentBootstrap();
  }
  return agentBootstrapPromise;
}

export function retryAgentRuntime(): Promise<AgentRuntimeStatus> {
  agentBootstrapPromise = runAgentBootstrap();
  return agentBootstrapPromise;
}

async function getAgentRunnerState(): Promise<AgentRunnerState> {
  const ctx = agentRuntimeContext();
  const bundledVersion = await bundledAgentVersion(ctx).catch(() => null);
  return {
    choice: await loadAgentRunner(PREFS_PATH),
    bundledAvailable: ctx.bundledArtifactDir !== null,
    bundledVersion,
  };
}

async function setAgentRunner(input: unknown): Promise<AgentRunnerSetResult> {
  const requested = AGENT_RUNNER_CHOICES.find((choice) => choice === input);
  if (!requested) {
    return {
      success: false,
      error: `Unknown agent runner: ${String(input)}`,
      choice: await loadAgentRunner(PREFS_PATH),
      runtime: lastAgentStatus,
    };
  }
  try {
    await saveAgentRunner(PREFS_PATH, requested);
  } catch (err) {
    return {
      success: false,
      error: err instanceof Error ? err.message : String(err),
      choice: await loadAgentRunner(PREFS_PATH),
      runtime: lastAgentStatus,
    };
  }
  return applyAgentRunnerChoice(requested);
}

async function applyAgentRunnerChoice(choice: AgentRunnerChoice): Promise<AgentRunnerSetResult> {
  if (choice === 'none') {
    lastAgentStatus = NO_RUNNER_STATUS;
    agentBootstrapPromise = Promise.resolve(NO_RUNNER_STATUS);
    return { success: true, choice, runtime: NO_RUNNER_STATUS };
  }
  const runtime = await retryAgentRuntime();
  const restart = await restartDaemonIfAlive(
    '[daemon] agent runner selected — restarting daemon so it picks up the runtime',
  );
  return { success: true, choice, runtime, daemonRestart: restart.kind };
}

function desktopSpawnEnv(): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {
    ...process.env,
    GOOSAR_LAUNCHED_BY: 'desktop',
  };
  const agentPath = agentPathEnvValue(lastAgentStatus);
  if (agentPath && !process.env['GOOSAR_HERMES_PATH']?.trim()) {
    env['GOOSAR_HERMES_PATH'] = agentPath;
  }
  for (const [key, value] of Object.entries(networkSpawnEnvVars())) {
    if (!process.env[key]?.trim()) env[key] = value;
  }
  Object.assign(env, playwrightRuntimeEnv());
  env.PATH = appendStagedMcpServerPath(
    env.PATH,
    { home: homedir(), env: process.env },
    process.platform,
  );
  return env;
}

async function startDaemon(): Promise<{ success: boolean; error?: string }> {
  const bin = await resolveCliBinary();
  if (!bin) return { success: false, error: 'goosar CLI is not installed' };

  await bootstrapAgentRuntime();

  const active = await ensureActiveProfile();
  const existing = await fetchHealthAtPort(active.port);
  if (daemonStatusAlive(existing?.status)) {
    pollOnce();
    return { success: true };
  }

  currentState = 'starting';
  startingSince = Date.now();
  authProbeDone = false;
  authExpired = false;
  sendStatus({ state: 'starting' });

  const args = ['daemon', 'start', ...profileArgs(active)];

  return new Promise((resolve) => {
    execFile(
      bin,
      args,
      { timeout: DAEMON_START_EXEC_TIMEOUT_MS, env: desktopSpawnEnv() },
      (err, _stdout, stderr) => {
        if (err && !isDaemonReadinessTimeout(err, stderr)) {
          currentState = 'stopped';
          sendStatus({ state: 'stopped' });
          resolve({ success: false, error: err.message });
          return;
        }
        pollOnce();
        resolve({ success: true });
      },
    );
  });
}

async function lifecycleBlockedByForeignDaemon(): Promise<boolean> {
  const active = await ensureActiveProfile();
  return daemonLifecycleUnreachable(
    async () => (await fetchHealthAtPort(active.port))?.os,
    normalizeHostOS(process.platform),
  );
}

async function stopDaemon(): Promise<{ success: boolean; error?: string }> {
  if (await lifecycleBlockedByForeignDaemon()) return { success: true };

  const bin = await resolveCliBinary();
  if (!bin) return { success: false, error: 'goosar CLI is not installed' };

  const active = await ensureActiveProfile();
  currentState = 'stopping';
  authExpired = false;
  startingSince = null;
  sendStatus({ state: 'stopping' });

  const args = ['daemon', 'stop', ...profileArgs(active)];

  return new Promise((resolve) => {
    execFile(bin, args, { timeout: 15_000 }, (err) => {
      if (err) {
        resolve({ success: false, error: err.message });
      } else {
        resolve({ success: true });
      }
      currentState = 'stopped';
      sendStatus({ state: 'stopped' });
    });
  });
}

export async function restartDaemonIfAlive(
  reason = '[daemon] perimeter mode changed — restarting daemon so the new transport env applies',
): Promise<PerimeterDaemonRestartOutcome> {
  try {
    const active = await ensureActiveProfile();
    const existing = await fetchHealthAtPort(active.port);
    if (!daemonStatusAlive(existing?.status)) return { kind: 'not_running' };
    if (await lifecycleBlockedByForeignDaemon()) return { kind: 'foreign' };
    console.log(reason);
    const result = await restartDaemon();
    return result.success
      ? { kind: 'restarted' }
      : { kind: 'failed', message: result.error ?? 'daemon restart failed' };
  } catch (err) {
    return {
      kind: 'failed',
      message: err instanceof Error ? err.message : String(err),
    };
  }
}

export async function probeDaemonReachability(): Promise<DaemonReachability> {
  try {
    const active = await ensureActiveProfile();
    if (!active.port) return { kind: 'unknown' };
    const existing = await fetchHealthAtPort(active.port);
    return daemonStatusAlive(existing?.status) ? { kind: 'running' } : { kind: 'stopped' };
  } catch {
    return { kind: 'unknown' };
  }
}

async function restartDaemon(): Promise<{ success: boolean; error?: string }> {
  if (await lifecycleBlockedByForeignDaemon()) return { success: true };
  const stopResult = await stopDaemon();
  if (!stopResult.success) return stopResult;
  return startDaemon();
}

const DAEMON_STOP_VERIFY_TIMEOUT_MS = 10_000;
const DAEMON_STOP_VERIFY_INTERVAL_MS = 250;

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export async function stopDaemonForRemoval(): Promise<DaemonStopReport> {
  let guardAcquired = false;
  const result = await withGuard(() => {
    guardAcquired = true;
    return stopDaemon();
  });
  if (!guardAcquired) {
    return {
      stopped: false,
      detail:
        `${result.error ?? 'Another daemon operation is in progress'}. ` +
        'Nothing was removed: the app may still be installing or starting the ' +
        'agent runtime, and those files are the ones this would delete. Wait ' +
        'for it to finish and try again.',
    };
  }
  const active = await ensureActiveProfile();

  const deadline = Date.now() + DAEMON_STOP_VERIFY_TIMEOUT_MS;
  let health = await fetchHealthAtPort(active.port);
  while (daemonStatusAlive(health?.status) && Date.now() < deadline) {
    await wait(DAEMON_STOP_VERIFY_INTERVAL_MS);
    health = await fetchHealthAtPort(active.port);
  }
  if (!daemonStatusAlive(health?.status)) return { stopped: true };

  const pid = typeof health?.pid === 'number' ? ` (pid ${health.pid})` : '';
  const reason = result.error ? `: ${result.error}` : '';
  const profileFlag = active.name ? ` --profile ${active.name}` : '';
  return {
    stopped: false,
    detail:
      `A daemon is still answering on port ${active.port}${pid}${reason}. ` +
      `Stop it where it runs — \`goosar daemon stop${profileFlag}\` — and try again.`,
  };
}

async function pollOnce(): Promise<void> {
  const status = await fetchHealth();
  currentState = status.state;
  sendStatus(status);
  if (pendingVersionRestart && status.state === 'running') {
    void ensureRunningDaemonVersionMatches();
  }
  if (pendingProvisioningRestart && status.state === 'running') {
    void settleProvisioningRestartPending();
  }
}

function startPolling(): void {
  if (statusPollTimer) return;
  pollOnce();
  statusPollTimer = setInterval(pollOnce, POLL_INTERVAL_MS);
}

async function bootstrapCli(): Promise<void> {
  const bin = await resolveCliBinary();
  if (!bin) {
    currentState = 'cli_not_found';
    sendStatus({ state: 'cli_not_found' });
    return;
  }
  currentState = 'stopped';
  sendStatus({ state: 'stopped' });
  startPolling();
}

function stopPolling(): void {
  if (statusPollTimer) {
    clearInterval(statusPollTimer);
    statusPollTimer = null;
  }
}

const LOG_TAIL_INITIAL_WINDOW_BYTES = 32 * 1024;
const LOG_TAIL_INITIAL_LINES = 200;
const LOG_TAIL_POLL_MS = 500;

async function readLogRange(path: string, startAt: number, length: number): Promise<string> {
  const handle = await open(path, 'r');
  try {
    const buffer = Buffer.alloc(length);
    const { bytesRead } = await handle.read(buffer, 0, length, startAt);
    return buffer.subarray(0, bytesRead).toString('utf-8');
  } finally {
    await handle.close();
  }
}

function sendLines(win: BrowserWindow, text: string): void {
  const lines = text.split('\n').filter((line) => line.length > 0);
  for (const line of lines) {
    win.webContents.send('daemon:log-line', line);
  }
}

function startLogTail(win: BrowserWindow, retryCount = 0): void {
  stopLogTail();

  void ensureActiveProfile().then(async (active) => {
    const logPath = profileLogPath(active.name);
    if (!existsSync(logPath)) {
      if (retryCount < LOG_TAIL_MAX_RETRIES) {
        setTimeout(() => startLogTail(win, retryCount + 1), LOG_TAIL_RETRY_MS);
      }
      return;
    }

    let position = 0;
    try {
      const initialStats = await stat(logPath);
      const windowBytes = Math.min(initialStats.size, LOG_TAIL_INITIAL_WINDOW_BYTES);
      const startAt = initialStats.size - windowBytes;
      if (windowBytes > 0) {
        const text = await readLogRange(logPath, startAt, windowBytes);
        const lines = text
          .split('\n')
          .filter((line) => line.length > 0)
          .slice(-LOG_TAIL_INITIAL_LINES);
        for (const line of lines) {
          win.webContents.send('daemon:log-line', line);
        }
      }
      position = initialStats.size;
    } catch (err) {
      console.warn('[daemon] log tail initial read failed:', err);
      return;
    }

    const listener: StatsListener = (curr) => {
      const target = getMainWindow();
      if (!target) return;
      if (curr.size < position) position = 0;
      if (curr.size === position) return;
      const from = position;
      const length = curr.size - from;
      position = curr.size;
      readLogRange(logPath, from, length)
        .then((text) => sendLines(target, text))
        .catch((err) => {
          console.warn('[daemon] log tail read failed:', err);
        });
    };

    watchFile(logPath, { interval: LOG_TAIL_POLL_MS }, listener);
    logTailWatcher = { path: logPath, listener };
  });
}

function stopLogTail(): void {
  if (logTailWatcher) {
    unwatchFile(logTailWatcher.path, logTailWatcher.listener);
    logTailWatcher = null;
  }
}

export function setupDaemonManager(windowGetter: () => BrowserWindow | null): void {
  getMainWindow = windowGetter;

  ipcMain.handle('daemon:set-target-api-url', async (_e, url: string) => {
    const normalized = url || null;
    if (targetApiBaseUrl !== normalized) {
      console.log(`[daemon] target API URL set to ${normalized ?? '(none)'}`);
      targetApiBaseUrl = normalized;
      setPerimeterServerHost(normalized);
      invalidateActiveProfile();
      await pollOnce();
    }
  });
  ipcMain.handle('daemon:start', () => withGuard(() => startDaemon()));
  ipcMain.handle('daemon:stop', () => withGuard(() => stopDaemon()));
  ipcMain.handle('daemon:restart', () => withGuard(() => restartDaemon()));
  ipcMain.handle('provisioning:restart-now', () => requestProvisioningRestartNow());
  ipcMain.handle('daemon:get-status', () => fetchHealth());
  ipcMain.handle('daemon:probe-runtimes', () => probeLocalRuntimes());
  ipcMain.handle('daemon:doctor', (_event, opts?: { refresh?: boolean }) => runDoctor(opts ?? {}));
  ipcMain.handle('daemon:get-host-name', () => hostname());
  ipcMain.handle('daemon:sync-token', (_event, token: string, userId: string) =>
    syncToken(token, userId),
  );
  ipcMain.handle('daemon:clear-token', () => clearToken());
  ipcMain.handle('daemon:reauthenticate', (_event, token: string, userId: string) =>
    reauthenticate(token, userId),
  );
  ipcMain.handle('daemon:is-cli-installed', async () => {
    const bin = await resolveCliBinary();
    return bin !== null;
  });
  ipcMain.handle('daemon:retry-install', async () => {
    cachedCliBinary = undefined;
    cliResolvePromise = null;
    cachedCliBinaryVersion = undefined;
    await bootstrapCli();
  });
  ipcMain.handle('daemon:get-agent-runtime', async () => {
    if (agentBootstrapPromise) return agentBootstrapPromise;
    return readAgentRuntimeStatus(agentRuntimeContext());
  });
  ipcMain.handle('daemon:retry-agent-runtime', () => retryAgentRuntime());
  ipcMain.handle('daemon:get-agent-runner', () => getAgentRunnerState());
  ipcMain.handle('daemon:set-agent-runner', (_event, choice: unknown) => setAgentRunner(choice));
  ipcMain.handle('daemon:get-prefs', () => loadPrefs());
  ipcMain.handle('daemon:set-prefs', (_event, prefs: Partial<DaemonPrefs>) =>
    loadPrefs().then((cur) => {
      const incoming: Record<string, unknown> = { ...prefs };
      delete incoming['agentRunner'];
      const merged = { ...cur, ...incoming };
      return savePrefs(merged).then(() => merged);
    }),
  );
  ipcMain.handle('daemon:auto-start', async () => {
    const prefs = await loadPrefs();
    if (!prefs.autoStart) return;
    const bin = await resolveCliBinary();
    if (!bin) return;
    const health = await fetchHealth();
    if (health.state === 'running') {
      await ensureRunningDaemonVersionMatches();
      return;
    }
    await startDaemon();
  });

  ipcMain.on('daemon:start-log-stream', () => {
    const win = getMainWindow();
    if (win) startLogTail(win);
  });

  ipcMain.on('daemon:stop-log-stream', () => {
    stopLogTail();
  });

  ipcMain.handle('daemon:open-log-file', async () => {
    const active = await ensureActiveProfile();
    const logPath = profileLogPath(active.name);
    if (!existsSync(logPath)) {
      return { success: false, error: 'Log file not found yet' };
    }
    const error = await shell.openPath(logPath);
    return error === '' ? { success: true } : { success: false, error };
  });

  currentState = 'installing_cli';
  sendStatus({ state: 'installing_cli' });
  void bootstrapCli();

  void bootstrapAgentRuntime();

  let isQuitting = false;
  app.on('before-quit', (event) => {
    if (isQuitting) return;
    stopPolling();
    stopLogTail();

    loadPrefs().then(async (prefs) => {
      if (prefs.autoStop) {
        isQuitting = true;
        event.preventDefault();
        try {
          await stopDaemon();
        } catch {
          // Best-effort stop on quit
        }
        app.quit();
      }
    });
  });
}

import { contextBridge, ipcRenderer } from 'electron';
import { electronAPI } from '@electron-toolkit/preload';
import type {
  RuntimeConfigPatch,
  RuntimeConfigResult,
  RuntimeConfigSaveResult,
  ServerProbeResult,
} from '../shared/runtime-config';
import type {
  PerimeterKinitResult,
  PerimeterStateView,
  LlmGatewayVerdict,
} from '../shared/perimeter-config';
import type { ProvisioningRestartOutcome, ProvisioningStatus } from '../shared/provisioning-status';
import type { FreezeBreadcrumb } from '../shared/freeze-breadcrumb';
import type { KerberosPreferences } from '../shared/kerberos-preferences-types';
import type { ManualUpdateCheckResult, UpdaterPreferences } from '../shared/updater-types';
import {
  RENDERER_ROUTE_CONTEXT_CHANNEL,
  type RendererRouteContextInput,
} from '../shared/renderer-route-context';
import {
  DIAGNOSTICS_CONTROL_CHANNEL,
  type DiagnosticsControl,
} from '../shared/diagnostics-control';
import { UI_LOCALE_CHANNEL, type DesktopUiLocale } from '../shared/ui-locale';
import { UPDATE_CONTROL_CHANNEL, type UpdateControl } from '../shared/update-control';
import {
  isNavigationGesture,
  NAVIGATION_GESTURE_CHANNEL,
  type NavigationGesture,
} from '../shared/navigation-gestures';
import { readDesktopWindowContext, type IssueWindowRequest } from '../shared/issue-window';
import { AUTH_SESSION_STATE_CHANNEL } from '../shared/auth-session';
import type {
  AgentConfigPatch,
  AgentConfigSaveResult,
  AgentRunnerChoice,
  AgentRunnerSetResult,
  AgentRunnerState,
  AgentRuntimeStatus,
} from '../shared/agent-runtime-types';
import type {
  LlmRuntimeConfigView,
  LlmRuntimeSaveInput,
  LlmRuntimeSaveResult,
} from '../shared/llm-runtime-settings';
import type { DaemonStatus, LocalRuntimeProbe, DoctorReport } from '../shared/daemon-types';
import type { UninstallOptions, UninstallOutcome, UninstallPlan } from '../shared/uninstall-types';
import {
  MAIN_RENDERER_CHANNEL_STATE_CHANNEL,
  type MainRendererMessageChannel,
} from '../shared/main-renderer-messages';

function fetchAppInfo(): { version: string; os: 'macos' | 'windows' | 'linux' | 'unknown' } {
  try {
    const info = ipcRenderer.sendSync('app:get-info') as
      { version: string; os: 'macos' | 'windows' | 'linux' | 'unknown' } | undefined;
    if (info && typeof info.version === 'string' && typeof info.os === 'string') return info;
  } catch {
    // fall through
  }
  const p = process.platform;
  const os: 'macos' | 'windows' | 'linux' | 'unknown' =
    p === 'darwin' ? 'macos' : p === 'win32' ? 'windows' : p === 'linux' ? 'linux' : 'unknown';
  return { version: 'unknown', os };
}

function fetchRuntimeConfig(): RuntimeConfigResult {
  try {
    const result = ipcRenderer.sendSync('runtime-config:get') as RuntimeConfigResult | undefined;
    if (result && typeof result === 'object' && 'ok' in result) return result;
  } catch (err) {
    return {
      ok: false,
      error: {
        message: err instanceof Error ? err.message : String(err),
      },
    };
  }
  return { ok: false, error: { message: 'Runtime config unavailable' } };
}

function fetchPresetOverlay(): Record<string, unknown> | null {
  try {
    const result = ipcRenderer.sendSync('preset-overlay:get') as unknown;
    return result && typeof result === 'object' && !Array.isArray(result)
      ? (result as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

function fetchDeploymentHosts(): Record<string, unknown> | null {
  try {
    const result = ipcRenderer.sendSync('deployment-hosts:get') as unknown;
    return result && typeof result === 'object' && !Array.isArray(result)
      ? (result as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

const appInfo = fetchAppInfo();
const runtimeConfig = fetchRuntimeConfig();
const presetOverlay = fetchPresetOverlay();
const deploymentHosts = fetchDeploymentHosts();
const windowContext = readDesktopWindowContext(process.argv);

function subscribeToMainRendererChannel<T>(
  channel: MainRendererMessageChannel,
  callback: (payload: T) => void,
): () => void {
  const handler = (_event: Electron.IpcRendererEvent, payload: T) => callback(payload);
  ipcRenderer.on(channel, handler);
  ipcRenderer.send(MAIN_RENDERER_CHANNEL_STATE_CHANNEL, {
    channel,
    ready: true,
  });
  return () => {
    ipcRenderer.removeListener(channel, handler);
    ipcRenderer.send(MAIN_RENDERER_CHANNEL_STATE_CHANNEL, {
      channel,
      ready: false,
    });
  };
}

const desktopAPI = {
  appInfo,
  runtimeConfig,
  presetOverlay,
  deploymentHosts,
  probeRuntimeServer: (apiUrl: string): Promise<ServerProbeResult> =>
    ipcRenderer.invoke('runtime-config:probe', apiUrl),
  setRuntimeConfig: (patch: RuntimeConfigPatch): Promise<RuntimeConfigSaveResult> =>
    ipcRenderer.invoke('runtime-config:set', patch),
  windowContext,
  getLastFreeze: (): FreezeBreadcrumb | null => {
    try {
      return ipcRenderer.sendSync('freeze:get-last') as FreezeBreadcrumb | null;
    } catch {
      return null;
    }
  },
  ackFreeze: (ts: number) => ipcRenderer.send('freeze:ack', ts),
  reportAuthSession: (userId: string | null) =>
    ipcRenderer.send(AUTH_SESSION_STATE_CHANNEL, userId),
  onAuthToken: (callback: (token: string) => void) =>
    subscribeToMainRendererChannel('auth:token', callback),
  onAuthLinkToken: (callback: (linkToken: string) => void) =>
    subscribeToMainRendererChannel('auth:link-token', callback),

  onAuthMfaToken: (callback: (mfaToken: string) => void) =>
    subscribeToMainRendererChannel('auth:mfa-token', callback),

  onAuthError: (callback: (code: string) => void) =>
    subscribeToMainRendererChannel('auth:error', callback),
  onInviteOpen: (callback: (invitationId: string) => void) =>
    subscribeToMainRendererChannel('invite:open', callback),
  openExternal: (url: string) => ipcRenderer.invoke('shell:openExternal', url),
  downloadURL: (url: string) => ipcRenderer.invoke('file:download-url', url),
  setImmersiveMode: (immersive: boolean) => ipcRenderer.invoke('window:setImmersive', immersive),
  showNotification: (payload: {
    slug: string;
    itemId: string;
    issueKey: string;
    title: string;
    body: string;
  }) => ipcRenderer.send('notification:show', payload),
  setUnreadBadge: (count: number) => ipcRenderer.send('badge:set', Math.max(0, Math.floor(count))),
  onInboxOpen: (callback: (payload: { slug: string; itemId: string; issueKey: string }) => void) =>
    subscribeToMainRendererChannel('inbox:open', callback),
  onNavigationGesture: (callback: (gesture: NavigationGesture) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, gesture: unknown) => {
      if (isNavigationGesture(gesture)) callback(gesture);
    };
    ipcRenderer.on(NAVIGATION_GESTURE_CHANNEL, handler);
    return () => {
      ipcRenderer.removeListener(NAVIGATION_GESTURE_CHANNEL, handler);
    };
  },
  setRendererRouteContext: (context: RendererRouteContextInput) =>
    ipcRenderer.send(RENDERER_ROUTE_CONTEXT_CHANNEL, context),
  setDiagnosticsControl: (control: DiagnosticsControl) =>
    ipcRenderer.send(DIAGNOSTICS_CONTROL_CHANNEL, control),
  setUiLocale: (locale: DesktopUiLocale) => ipcRenderer.send(UI_LOCALE_CHANNEL, locale),
  setUpdateControl: (control: UpdateControl) => ipcRenderer.send(UPDATE_CONTROL_CHANNEL, control),
  pickDirectory: (defaultPath?: string) => ipcRenderer.invoke('local-directory:pick', defaultPath),
  validateLocalDirectory: (path: string) => ipcRenderer.invoke('local-directory:validate', path),
  onCloseActiveTab: (callback: () => void) => {
    const handler = () => callback();
    ipcRenderer.on('tab:close-active', handler);
    return () => {
      ipcRenderer.removeListener('tab:close-active', handler);
    };
  },
  closeWindow: () => ipcRenderer.send('window:close'),
  openIssueWindow: (request: IssueWindowRequest) =>
    ipcRenderer.invoke('window:open-issue', request),
  planUninstall: (): Promise<UninstallPlan> => ipcRenderer.invoke('uninstall:plan'),
  performUninstall: (options: UninstallOptions): Promise<UninstallOutcome> =>
    ipcRenderer.invoke('uninstall:perform', options),
};

type DaemonReauthResult =
  | { ok: true }
  | { ok: false; reason: 'session_invalid' }
  | { ok: false; reason: 'transient'; message: string };

const daemonAPI = {
  start: (): Promise<{ success: boolean; error?: string }> => ipcRenderer.invoke('daemon:start'),
  stop: (): Promise<{ success: boolean; error?: string }> => ipcRenderer.invoke('daemon:stop'),
  restart: (): Promise<{ success: boolean; error?: string }> =>
    ipcRenderer.invoke('daemon:restart'),
  restartProvisioningNow: (): Promise<ProvisioningRestartOutcome> =>
    ipcRenderer.invoke('provisioning:restart-now'),
  getStatus: (): Promise<DaemonStatus> => ipcRenderer.invoke('daemon:get-status'),
  probeRuntimes: (): Promise<LocalRuntimeProbe> => ipcRenderer.invoke('daemon:probe-runtimes'),
  doctor: (opts?: { refresh?: boolean }): Promise<DoctorReport | null> =>
    ipcRenderer.invoke('daemon:doctor', opts ?? {}),
  getHostName: (): Promise<string> => ipcRenderer.invoke('daemon:get-host-name'),
  onStatusChange: (callback: (status: DaemonStatus) => void) => {
    const handler = (_: unknown, status: DaemonStatus) => callback(status);
    ipcRenderer.on('daemon:status', handler);
    return () => ipcRenderer.removeListener('daemon:status', handler);
  },
  setTargetApiUrl: (url: string): Promise<void> =>
    ipcRenderer.invoke('daemon:set-target-api-url', url),
  syncToken: (token: string, userId: string): Promise<void> =>
    ipcRenderer.invoke('daemon:sync-token', token, userId),
  clearToken: (): Promise<void> => ipcRenderer.invoke('daemon:clear-token'),
  reauthenticate: (token: string, userId: string): Promise<DaemonReauthResult> =>
    ipcRenderer.invoke('daemon:reauthenticate', token, userId),
  isCliInstalled: (): Promise<boolean> => ipcRenderer.invoke('daemon:is-cli-installed'),
  getAgentRuntime: (): Promise<AgentRuntimeStatus> =>
    ipcRenderer.invoke('daemon:get-agent-runtime'),
  retryAgentRuntime: (): Promise<AgentRuntimeStatus> =>
    ipcRenderer.invoke('daemon:retry-agent-runtime'),
  getAgentRunner: (): Promise<AgentRunnerState> => ipcRenderer.invoke('daemon:get-agent-runner'),
  setAgentRunner: (choice: AgentRunnerChoice): Promise<AgentRunnerSetResult> =>
    ipcRenderer.invoke('daemon:set-agent-runner', choice),
  setAgentRuntimeConfig: (patch: AgentConfigPatch): Promise<AgentConfigSaveResult> =>
    ipcRenderer.invoke('agent-runtime:set-config', patch),
  getLlmRuntimeConfig: (): Promise<LlmRuntimeConfigView> =>
    ipcRenderer.invoke('llm-runtime:get-config'),
  saveLlmRuntimeConfig: (input: LlmRuntimeSaveInput): Promise<LlmRuntimeSaveResult> =>
    ipcRenderer.invoke('llm-runtime:save', input),
  getPrefs: (): Promise<{ autoStart: boolean; autoStop: boolean }> =>
    ipcRenderer.invoke('daemon:get-prefs'),
  setPrefs: (
    prefs: Partial<{ autoStart: boolean; autoStop: boolean }>,
  ): Promise<{ autoStart: boolean; autoStop: boolean }> =>
    ipcRenderer.invoke('daemon:set-prefs', prefs),
  autoStart: (): Promise<void> => ipcRenderer.invoke('daemon:auto-start'),
  retryInstall: (): Promise<void> => ipcRenderer.invoke('daemon:retry-install'),
  startLogStream: () => ipcRenderer.send('daemon:start-log-stream'),
  stopLogStream: () => ipcRenderer.send('daemon:stop-log-stream'),
  onLogLine: (callback: (line: string) => void) => {
    const handler = (_: unknown, line: string) => callback(line);
    ipcRenderer.on('daemon:log-line', handler);
    return () => ipcRenderer.removeListener('daemon:log-line', handler);
  },
  openLogFile: (): Promise<{ success: boolean; error?: string }> =>
    ipcRenderer.invoke('daemon:open-log-file'),
  getPerimeter: (): Promise<PerimeterStateView> => ipcRenderer.invoke('perimeter:get-state'),
  recheckPerimeter: (): Promise<PerimeterStateView> => ipcRenderer.invoke('perimeter:recheck'),
  onPerimeterState: (callback: (state: PerimeterStateView) => void) => {
    const handler = (_: unknown, state: PerimeterStateView) => callback(state);
    ipcRenderer.on('perimeter:state', handler);
    return () => ipcRenderer.removeListener('perimeter:state', handler);
  },
  perimeterKinit: (
    principal: string | null | undefined,
    password: string,
  ): Promise<PerimeterKinitResult> =>
    ipcRenderer.invoke('perimeter:kinit', { principal: principal ?? null, password }),
  perimeterKinitRenew: (principal?: string | null): Promise<PerimeterKinitResult> =>
    ipcRenderer.invoke('perimeter:kinit-renew', { principal: principal ?? null }),
  getLlmGatewayStatus: (): Promise<{
    verdict: LlmGatewayVerdict;
    source: 'agent' | 'server';
    host: string | null;
  }> => ipcRenderer.invoke('llm-gateway:get-status'),
  retryLlmGatewayStatus: (): Promise<{
    verdict: LlmGatewayVerdict;
    source: 'agent' | 'server';
    host: string | null;
    standApiBaseHint?: string | null;
  }> => ipcRenderer.invoke('llm-gateway:retry'),
  getPxProxyStatus: (): Promise<{
    state: 'not_required' | 'installed' | 'not_found';
  }> => ipcRenderer.invoke('px-proxy:get-status'),
  kerberosGetPreferences: (): Promise<KerberosPreferences> =>
    ipcRenderer.invoke('kerberos:get-preferences'),
  kerberosSetPreferences: (preferences: KerberosPreferences): Promise<KerberosPreferences> =>
    ipcRenderer.invoke('kerberos:set-preferences', preferences),
  kerberosSetResolvedPrincipal: (principal: string | null): Promise<void> =>
    ipcRenderer.invoke('kerberos:set-resolved-principal', principal),
  getProvisioningStatus: (): Promise<ProvisioningStatus> =>
    ipcRenderer.invoke('provisioning:status'),
  setProvisioningWorkspace: (workspaceId: string | null): Promise<void> =>
    ipcRenderer.invoke('provisioning:set-workspace', workspaceId),
  retryProvisioning: (): Promise<void> => ipcRenderer.invoke('provisioning:retry'),
  notifyProvisioningAccessRevoked: (workspaceId: string): Promise<void> =>
    ipcRenderer.invoke('provisioning:access-revoked', workspaceId),
};

const updaterAPI = {
  onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => {
    const handler = (_: unknown, info: { version: string; releaseNotes?: string }) =>
      callback(info);
    ipcRenderer.on('updater:update-available', handler);
    return () => ipcRenderer.removeListener('updater:update-available', handler);
  },
  onDownloadProgress: (callback: (progress: { percent: number }) => void) => {
    const handler = (_: unknown, progress: { percent: number }) => callback(progress);
    ipcRenderer.on('updater:download-progress', handler);
    return () => ipcRenderer.removeListener('updater:download-progress', handler);
  },
  onUpdateDownloaded: (callback: (info: { version: string; releaseNotes?: string }) => void) => {
    const handler = (_: unknown, info: { version: string; releaseNotes?: string }) =>
      callback(info);
    ipcRenderer.on('updater:update-downloaded', handler);
    return () => ipcRenderer.removeListener('updater:update-downloaded', handler);
  },
  downloadUpdate: () => ipcRenderer.invoke('updater:download'),
  installUpdate: () => ipcRenderer.invoke('updater:install'),
  getPreferences: (): Promise<UpdaterPreferences> => ipcRenderer.invoke('updater:get-preferences'),
  setAutomaticUpdates: (enabled: boolean): Promise<UpdaterPreferences> =>
    ipcRenderer.invoke('updater:set-automatic-updates', enabled),
  checkForUpdates: (): Promise<ManualUpdateCheckResult> => ipcRenderer.invoke('updater:check'),
};

if (process.contextIsolated) {
  contextBridge.exposeInMainWorld('electron', electronAPI);
  contextBridge.exposeInMainWorld('desktopAPI', desktopAPI);
  contextBridge.exposeInMainWorld('daemonAPI', daemonAPI);
  contextBridge.exposeInMainWorld('updater', updaterAPI);
} else {
  // @ts-expect-error - fallback for non-isolated context
  window.electron = electronAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.desktopAPI = desktopAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.daemonAPI = daemonAPI;
  // @ts-expect-error - fallback for non-isolated context
  window.updater = updaterAPI;
}

import { ElectronAPI } from '@electron-toolkit/preload';
import type {
  RuntimeConfigPatch,
  RuntimeConfigResult,
  RuntimeConfigSaveResult,
  ServerProbeResult,
} from '../shared/runtime-config';
import type { NavigationGesture } from '../shared/navigation-gestures';
import type { RendererRouteContextInput } from '../shared/renderer-route-context';
import type { DiagnosticsControl } from '../shared/diagnostics-control';
import type { DesktopUiLocale } from '../shared/ui-locale';
import type { UpdateControl } from '../shared/update-control';
import type { FreezeBreadcrumb } from '../shared/freeze-breadcrumb';
import type { KerberosPreferences } from '../shared/kerberos-preferences-types';
import type { DesktopWindowContext, IssueWindowRequest } from '../shared/issue-window';
import type { ManualUpdateCheckResult, UpdaterPreferences } from '../shared/updater-types';
import type {
  DaemonStatus,
  DaemonPrefs,
  LocalRuntimeProbe,
  DoctorReport,
} from '../shared/daemon-types';
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
import type { UninstallOptions, UninstallOutcome, UninstallPlan } from '../shared/uninstall-types';
import type { ProvisioningRestartOutcome } from '../shared/provisioning-status';
import type {
  PerimeterKinitResult,
  PerimeterStateView,
  LlmGatewayVerdict,
} from '../shared/perimeter-config';
import type { ProvisioningStatus } from '../shared/provisioning-status';

interface DesktopAPI {
  /** App version + normalized OS, captured synchronously at preload time. */
  appInfo: {
    version: string;
    os: 'macos' | 'windows' | 'linux' | 'unknown';
  };
  /** Validated runtime endpoint config, or a blocking config error. Captured
   *  once at preload time; changes require a renderer reload. */
  runtimeConfig: RuntimeConfigResult;
  /** MCP preset provisioning overlay (issue #161), or null when this build
   *  carries none. Captured once at preload time; the renderer sets it into
   *  configStore so buildHelperMcpConfig merges it over the placeholder
   *  presets. */
  presetOverlay: Record<string, unknown> | null;
  deploymentHosts: Record<string, unknown> | null;
  /** Ask a server whether it answers. Every outcome carries the normalized
   *  address so failures can name it. */
  probeRuntimeServer: (apiUrl: string) => Promise<ServerProbeResult>;
  /** Persist a new server address; on success main reloads the renderers. */
  setRuntimeConfig: (patch: RuntimeConfigPatch) => Promise<RuntimeConfigSaveResult>;
  /** Main tabbed window or a dedicated issue-only window. */
  windowContext: DesktopWindowContext;
  /** Read any freeze/crash breadcrumb from a previous session, so the renderer
   *  can flush it to telemetry on boot. Null when nothing's pending. Reading
   *  does not consume it — acknowledge with `ackFreeze`. */
  getLastFreeze: () => FreezeBreadcrumb | null;
  /** Retire the breadcrumb with this exact timestamp once its event has been
   *  handed to analytics. Unacknowledged breadcrumbs are retried next boot. */
  ackFreeze: (ts: number) => void;
  /** Report the resolved account identity so stale issue windows can close. */
  reportAuthSession: (userId: string | null) => void;
  /** Listen for auth token delivered via deep link. Returns an unsubscribe function. */
  onAuthToken: (callback: (token: string) => void) => () => void;
  /** Listen for one-time login-link tokens delivered via deep link (#226).
   *  Returns an unsubscribe function. */
  onAuthLinkToken: (callback: (linkToken: string) => void) => () => void;
  /** A sign-in that proved the first factor and still owes a second one (#391).
   *  Returns an unsubscribe function. */
  onAuthMfaToken: (callback: (mfaToken: string) => void) => () => void;
  /** Corporate sign-in (#394) returned a refusal code rather than a session. */
  onAuthError: (callback: (code: string) => void) => () => void;
  /** Listen for invitation IDs delivered via deep link. Returns an unsubscribe function. */
  onInviteOpen: (callback: (invitationId: string) => void) => () => void;
  /** Open a URL in the default browser. */
  openExternal: (url: string) => Promise<void>;
  /** Download a file by URL through Electron's native download system.
   *  Shows a native save dialog. On non-desktop platforms this is undefined. */
  downloadURL: (url: string) => Promise<void>;
  /** Hide macOS traffic lights for full-screen modals; restore when false. */
  setImmersiveMode: (immersive: boolean) => Promise<void>;
  /** Show a native OS notification for a new inbox item. */
  showNotification: (payload: {
    slug: string;
    itemId: string;
    issueKey: string;
    title: string;
    body: string;
  }) => void;
  /** Update the OS dock / taskbar unread badge. Pass 0 to clear. */
  setUnreadBadge: (count: number) => void;
  /** Listen for "open inbox row" requests from notification clicks. Returns an unsubscribe function. */
  onInboxOpen: (
    callback: (payload: { slug: string; itemId: string; issueKey: string }) => void,
  ) => () => void;
  /** Listen for native macOS back/forward swipe gestures. Returns an unsubscribe function. */
  onNavigationGesture: (callback: (gesture: NavigationGesture) => void) => () => void;
  /** Report the renderer's memory-router path for recovery diagnostics. */
  setRendererRouteContext: (context: RendererRouteContextInput) => void;
  /** Publish server-driven diagnostics flags; main stays fail-closed until then. */
  setDiagnosticsControl: (control: DiagnosticsControl) => void;
  /** Publish the locale this renderer resolved, so native menus built in the
   *  main process speak the same language as the window (#207). */
  setUiLocale: (locale: DesktopUiLocale) => void;
  /** Publish the server's delivery-profile bit; main assumes cloud until told. */
  setUpdateControl: (control: UpdateControl) => void;
  /** Open the OS folder picker and return the chosen absolute path.
   *  Used by the Project settings "Add local directory" flow. */
  pickDirectory: (defaultPath?: string) => Promise<{
    ok: boolean;
    path?: string;
    basename?: string;
    reason?: 'cancelled' | 'no_window' | 'error';
    error?: string;
  }>;
  /** Validate that a path is an existing readable+writable directory.
   *  Mirrors the daemon's runtime check so the user sees errors before submit. */
  validateLocalDirectory: (path: string) => Promise<{
    ok: boolean;
    reason?:
      'not_absolute' | 'not_found' | 'not_a_directory' | 'not_readable' | 'not_writable' | 'error';
    error?: string;
  }>;
  /** Listen for Cmd/Ctrl+W tab-close requests from the main process.
   *  Returns an unsubscribe function. */
  onCloseActiveTab: (callback: () => void) => () => void;
  /** Ask the main process to close the window. */
  closeWindow: () => void;
  /** Open an issue-detail tab in a dedicated native window. */
  openIssueWindow: (
    request: IssueWindowRequest,
  ) => Promise<{ ok: true } | { ok: false; reason: 'invalid_request' }>;
  /** Enumerate everything this app installed, with paths and sizes, plus what
   *  will be left alone and why. Read-only: removes nothing. */
  planUninstall: () => Promise<UninstallPlan>;
  /** Stop the daemon and remove the enumerated set. The user's config, agents,
   *  skills, schedules, and history go only on an explicit opt-in. */
  performUninstall: (options: UninstallOptions) => Promise<UninstallOutcome>;
}

type DaemonReauthResult =
  | { ok: true }
  | { ok: false; reason: 'session_invalid' }
  | { ok: false; reason: 'transient'; message: string };

interface DaemonAPI {
  start: () => Promise<{ success: boolean; error?: string }>;
  stop: () => Promise<{ success: boolean; error?: string }>;
  restart: () => Promise<{ success: boolean; error?: string }>;
  /** Perimeter panel's "restart now" action for a deferred provisioning
   *  install (issue #191). Busy-aware: never kills a running task — it
   *  either restarts (idle) or reports "deferred" (busy) so the UI can say
   *  so honestly, exactly like the automatic post-install restart. */
  restartProvisioningNow: () => Promise<ProvisioningRestartOutcome>;
  getStatus: () => Promise<DaemonStatus>;
  probeRuntimes: () => Promise<LocalRuntimeProbe>;
  doctor: (opts?: { refresh?: boolean }) => Promise<DoctorReport | null>;
  getHostName: () => Promise<string>;
  onStatusChange: (callback: (status: DaemonStatus) => void) => () => void;
  setTargetApiUrl: (url: string) => Promise<void>;
  syncToken: (token: string, userId: string) => Promise<void>;
  clearToken: () => Promise<void>;
  reauthenticate: (token: string, userId: string) => Promise<DaemonReauthResult>;
  isCliInstalled: () => Promise<boolean>;
  /** State of the bundled hermes agent runtime. Read-only: never installs. */
  getAgentRuntime: () => Promise<AgentRuntimeStatus>;
  /** T-05 (#633): "Повторить" button — reruns ensureAgentRuntime in place. */
  retryAgentRuntime: () => Promise<AgentRuntimeStatus>;
  /** The persisted agent runner choice ('none' by default) plus whether this
   *  build carries the bundled runner payload. Read-only. */
  getAgentRunner: () => Promise<AgentRunnerState>;
  /** Persist the agent runner choice. 'hermes' installs the bundled runtime and
   *  recycles a running daemon; 'none' only persists the choice. */
  setAgentRunner: (choice: AgentRunnerChoice) => Promise<AgentRunnerSetResult>;
  /** Write the agent runtime's LLM settings through its own `config set`.
   *  The API key is forwarded to the runtime's stdin and never stored. */
  setAgentRuntimeConfig: (patch: AgentConfigPatch) => Promise<AgentConfigSaveResult>;
  /** The active LLM model/endpoint the agent will load (issue #160). Read-only;
   *  the api_key is reduced to a presence flag. */
  getLlmRuntimeConfig: () => Promise<LlmRuntimeConfigView>;
  /** Change the active model/endpoint (issue #160). Reuses the perimeter
   *  switch's `config set` write (#147) for llm.api_base + llm.model only, then
   *  the #154 reachability probe, so the result says whether it answers. */
  saveLlmRuntimeConfig: (input: LlmRuntimeSaveInput) => Promise<LlmRuntimeSaveResult>;
  getPrefs: () => Promise<DaemonPrefs>;
  setPrefs: (prefs: Partial<DaemonPrefs>) => Promise<DaemonPrefs>;
  autoStart: () => Promise<void>;
  retryInstall: () => Promise<void>;
  startLogStream: () => void;
  stopLogStream: () => void;
  onLogLine: (callback: (line: string) => void) => () => void;
  openLogFile: () => Promise<{ success: boolean; error?: string }>;
  /** Network status (#249): the routes the system resolved for the addresses
   *  this app needs, the local proxy's state, CA presence, Kerberos verdict.
   *  Read-only, credential-free, and readable before login. */
  getPerimeter: () => Promise<PerimeterStateView>;
  /** Re-resolve the routes and re-run every check now. Diagnoses only. */
  recheckPerimeter: () => Promise<PerimeterStateView>;
  /** Subscribe to main-process-pushed state (issue #148): the watcher
   *  re-checks on a timer/focus/resume and pushes fresh state, so the UI
   *  reacts to an expiring ticket or a changed network without polling.
   *  Returns an unsubscribe function. */
  onPerimeterState: (callback: (state: PerimeterStateView) => void) => () => void;
  /** Obtain a Kerberos ticket; the password is piped to kinit's stdin in main,
   *  never on argv, logged, or stored. */
  perimeterKinit: (
    principal: string | null | undefined,
    password: string,
  ) => Promise<PerimeterKinitResult>;
  /** Renew the current TGT via `kinit -R` (#298) — passwordless. */
  perimeterKinitRenew: (principal?: string | null) => Promise<PerimeterKinitResult>;
  /** T-25 (#653): the "AI-шлюз" onboarding row's status — same authenticated
   *  probe as the perimeter panel's "llm" row, mapped to the row's own
   *  verdict vocabulary. Read-only. */
  getLlmGatewayStatus: () => Promise<{
    verdict: LlmGatewayVerdict;
    source: 'agent' | 'server';
    host: string | null;
  }>;
  /** "Повторить" for the AI-шлюз row (T-14, #698): re-fetches client-secrets
   *  (apply-if-empty) before reprobing; may carry `standApiBaseHint`. */
  retryLlmGatewayStatus: () => Promise<{
    verdict: LlmGatewayVerdict;
    source: 'agent' | 'server';
    host: string | null;
    standApiBaseHint?: string | null;
  }>;
  /** T-17 (#645): the "Прокси px" onboarding row's status. Read-only —
   *  never spawns or installs px. */
  getPxProxyStatus: () => Promise<{
    state: 'not_required' | 'installed' | 'not_found';
  }>;
  kerberosGetPreferences: () => Promise<KerberosPreferences>;
  kerberosSetPreferences: (preferences: KerberosPreferences) => Promise<KerberosPreferences>;
  kerberosSetResolvedPrincipal: (principal: string | null) => Promise<void>;
  /** Package-provisioning sync status (issue #184). Read-only. */
  getProvisioningStatus: () => Promise<ProvisioningStatus>;
  /** Tells main which workspace's manifest to sync (issue #184). */
  setProvisioningWorkspace: (workspaceId: string | null) => Promise<void>;
  /** Forces a provisioning sync pass right now — outside the periodic timer
   *  and outside in-flight memoization (issue #188). The recovery path
   *  behind the onboarding step's "Повторить" (retry) button. */
  retryProvisioning: () => Promise<void>;
  /** Reports the current user's own member:removed realtime event (issue
   *  #242, pass-2 finding L) so main runs the marker-gated revocation
   *  cleanup BEFORE the workspace binding is cleared. */
  notifyProvisioningAccessRevoked: (workspaceId: string) => Promise<void>;
}

interface UpdaterAPI {
  onUpdateAvailable: (
    callback: (info: { version: string; releaseNotes?: string }) => void,
  ) => () => void;
  onDownloadProgress: (callback: (progress: { percent: number }) => void) => () => void;
  onUpdateDownloaded: (
    callback: (info: { version: string; releaseNotes?: string }) => void,
  ) => () => void;
  downloadUpdate: () => Promise<void>;
  installUpdate: () => Promise<void>;
  getPreferences: () => Promise<UpdaterPreferences>;
  setAutomaticUpdates: (enabled: boolean) => Promise<UpdaterPreferences>;
  checkForUpdates: () => Promise<ManualUpdateCheckResult>;
}

declare global {
  interface Window {
    electron: ElectronAPI;
    desktopAPI: DesktopAPI;
    daemonAPI: DaemonAPI;
    updater: UpdaterAPI;
  }
}

export {};

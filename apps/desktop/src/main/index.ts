import { describeCliPathResult, installCliOnPath } from './cli-path';
import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  nativeImage,
  Notification,
  screen,
  shell,
} from 'electron';
import { existsSync, readFileSync } from 'fs';
import { homedir } from 'os';
import { dirname, join } from 'path';
import { pathToFileURL } from 'url';
import { electronApp, optimizer, is } from '@electron-toolkit/utils';
import fixPath from 'fix-path';
import { setupAutoUpdater } from './updater';
import {
  isDaemonBusyForProvisioningSwap,
  notifyProvisioningPackagesChanged,
  probeDaemonReachability,
  restartDaemonIfAlive,
  resyncClientSecretsForRetry,
  setupDaemonManager,
  stopDaemonForRemoval,
  getCliBinaryPath,
} from './daemon-manager';
import {
  initPerimeter,
  probePerimeterLlmReachability,
  probePerimeterServerReachability,
  setResyncClientSecrets,
} from './perimeter';
import {
  initProvisioningLifecycle,
  probeProvisioningReachability,
  resolveProvisionedRuntimeDir,
  setProvisioningDaemonBusyHook,
  setProvisioningRestartHook,
} from './provisioning';
import { setLocalProxyRuntimeResolver } from './local-proxy';
import { resolveAgentConfigPath, readLlmProfileScalars } from './agent-bootstrap';
import { parseLlmRuntimeSaveInput, saveLlmRuntimeSettings } from './llm-runtime-settings';
import type { LlmRuntimeConfigView } from '../shared/llm-runtime-settings';
import { setupLocalDirectory } from './local-directory';
import { openExternalSafely, downloadURLSafely } from './external-url';
import { installContextMenu, recordRendererUiLocale } from './context-menu';
import { handleAppShortcut } from './keyboard-shortcuts';
import { installNavigationGestures } from './navigation-gestures';
import { installNavigationGuard } from './navigation-guard';
import { getAppVersion } from './app-version';
import {
  desktopConfigPath,
  loadRuntimeConfig,
  probeRuntimeServer,
  saveDebugLoggingToggle,
  saveRuntimeConfig,
} from './runtime-config-loader';
import {
  initMainLogging,
  isDebugFileLogging,
  mainLogFilePath,
  setDebugFileLogging,
} from './main-logging';
import { installRendererLogCapture } from './renderer-log-capture';
import { watchLoggingConfig } from './logging-config-watch';
import { installAppMenu } from './app-menu';
import { parseLoggingSettings } from '../shared/logging-config';
import type { RuntimeConfigResult } from '../shared/runtime-config';
import { loadPresetOverlay, presetOverlayStagedDir } from './preset-overlay';
import { deploymentDefaultsStagedDir, loadDeploymentDefaults } from './deployment-defaults';
import { sanitizeAgentConfigPatch, saveAgentConfig } from './agent-config';
import type { AgentConfigSaveResult } from '../shared/agent-runtime-types';
import { performUninstall, planUninstall, type UninstallContext } from './uninstall';
import { parseUninstallOptions } from '../shared/uninstall-types';
import {
  RENDERER_ROUTE_CONTEXT_CHANNEL,
  sanitizeRendererRouteContext,
  type RendererRouteContext,
} from '../shared/renderer-route-context';
import {
  createElectronReloadPrompt,
  installRendererRecoveryHandlers,
  type RendererRecoveryWindow,
} from './renderer-recovery';
import { createBestEffortDevLog } from './dev-log';
import {
  writeFreezeBreadcrumb,
  readFreezeBreadcrumb,
  ackFreezeBreadcrumb,
  clearFreezeBreadcrumb,
} from './freeze-breadcrumb';
import {
  captureHangStack,
  coolDebuggerChannel,
  warmDebuggerChannel,
} from './renderer-stack-capture';
import { DIAGNOSTICS_CONTROL_CHANNEL } from '../shared/diagnostics-control';
import { UI_LOCALE_CHANNEL, parseUiLocale } from '../shared/ui-locale';
import { createDiagnosticsControlRegistry } from './diagnostics-control-registry';
import {
  loadWindowState,
  resolveWindowOptions,
  saveWindowStateToFile,
  snapshotWindowState,
  windowStateFilePath,
} from './window-state';
import {
  encodeIssueWindowArgument,
  parseIssueWindowRequest,
  type IssueWindowContext,
} from '../shared/issue-window';
import { AUTH_SESSION_STATE_CHANNEL, parseAuthSessionUserId } from '../shared/auth-session';
import {
  MAIN_RENDERER_CHANNEL_STATE_CHANNEL,
  MainRendererMessageQueue,
  parseMainRendererChannelState,
  type MainRendererMessageChannel,
} from '../shared/main-renderer-messages';
import { DEEP_LINK_PROTOCOL, parseDeepLink } from '../shared/deep-link';
import { AuthSessionCoordinator } from './auth-session-coordinator';
import { NotificationGate, parseNativeNotificationPayload } from './notification-gate';

const downloadDialogSessions = new WeakSet<Electron.Session>();

function installDownloadSaveDialogHandler(window: BrowserWindow): void {
  const { session } = window.webContents;
  if (downloadDialogSessions.has(session)) return;
  downloadDialogSessions.add(session);
  session.on('will-download', (_event, item) => {
    item.setSaveDialogOptions({
      defaultPath: join(app.getPath('downloads'), item.getFilename()),
    });
  });
}

const BUNDLED_ICON_PATH = join(__dirname, '../../resources/icon.png').replace(
  'app.asar',
  'app.asar.unpacked',
);

if (process.platform !== 'win32') {
  fixPath();
  const fallbackPaths = ['/opt/homebrew/bin', '/usr/local/bin', join(homedir(), '.local/bin')];
  process.env.PATH = `${fallbackPaths.join(':')}:${process.env.PATH ?? ''}`;
}

const PROTOCOL = DEEP_LINK_PROTOCOL;
const devLog = is.dev ? createBestEffortDevLog() : undefined;

function freezeBreadcrumbPath(): string {
  return join(app.getPath('userData'), 'last-client-failure.json');
}

function uninstallContext(): UninstallContext {
  return {
    home: homedir(),
    env: process.env,
    platform: process.platform,
    userDataDir: app.getPath('userData'),
    executablePath: app.getPath('exe'),
    isPackaged: app.isPackaged,
    stopDaemon: stopDaemonForRemoval,
  };
}

let mainWindow: BrowserWindow | null = null;
const issueWindows = new Set<BrowserWindow>();
const authSessionCoordinator = new AuthSessionCoordinator<BrowserWindow>((window) => {
  issueWindows.delete(window);
  if (!window.isDestroyed()) window.close();
});
const notificationGate = new NotificationGate();
const mainRendererMessages = new MainRendererMessageQueue();
let desktopInitialized = false;
let authSessionGeneration = 0;
const rendererRouteContexts = new WeakMap<Electron.WebContents, RendererRouteContext>();

const diagnosticsControl = createDiagnosticsControlRegistry<Electron.WebContents>({
  warm: (webContents) => {
    if (is.dev || webContents.isDestroyed()) return;
    void warmDebuggerChannel(webContents.debugger);
  },
  cool: (webContents) => {
    if (webContents.isDestroyed()) return;
    void coolDebuggerChannel(webContents.debugger);
  },
});

async function captureStackIfEnabled(webContents: Electron.WebContents): Promise<unknown> {
  if (is.dev) return null;
  if (!diagnosticsControl.isStackCaptureEnabled(webContents)) return null;
  if (webContents.isDestroyed()) return null;
  return captureHangStack(webContents.debugger);
}
let runtimeConfigResult: RuntimeConfigResult = {
  ok: false,
  error: { message: 'Runtime config has not loaded yet' },
};

let presetOverlay: Record<string, unknown> | null = null;
let deploymentHosts: Record<string, unknown> | null = null;

function sendMainRendererMessage(channel: MainRendererMessageChannel, payload: unknown): void {
  const window = mainWindow;
  if (!window || window.isDestroyed()) return;
  window.webContents.send(channel, payload);
}

function focusMainWindow(window: BrowserWindow): void {
  if (window.isMinimized()) window.restore();
  window.show();
  window.focus();
}

function ensureMainWindow(): BrowserWindow | null {
  if (!desktopInitialized || !app.isReady()) return null;
  if (!mainWindow || mainWindow.isDestroyed()) return createWindow();
  return mainWindow;
}

function dispatchToMainRenderer(channel: MainRendererMessageChannel, payload: unknown): void {
  mainRendererMessages.enqueue(channel, payload, sendMainRendererMessage);
  const window = ensureMainWindow();
  if (window) focusMainWindow(window);
}

function handleDeepLink(url: string): void {
  const action = parseDeepLink(url);
  if (!action) return;

  switch (action.kind) {
    case 'auth-token':
      dispatchToMainRenderer('auth:token', action.token);
      return;
    case 'auth-link-token':
      dispatchToMainRenderer('auth:link-token', action.linkToken);
      return;
    case 'auth-mfa-token':
      dispatchToMainRenderer('auth:mfa-token', action.mfaToken);
      return;
    case 'auth-error':
      dispatchToMainRenderer('auth:error', action.code);
      return;
    case 'invite':
      dispatchToMainRenderer('invite:open', action.invitationId);
      return;
  }
}

function createRendererWebPreferences(additionalArguments: string[] = []): Electron.WebPreferences {
  return {
    preload: join(__dirname, '../preload/index.js'),
    sandbox: false,
    webSecurity: false,
    plugins: true,
    additionalArguments,
  };
}

function loadRenderer(window: BrowserWindow): void {
  const rendererEntry = join(__dirname, '../renderer/index.html');
  const rendererURL =
    is.dev && process.env['ELECTRON_RENDERER_URL']
      ? process.env['ELECTRON_RENDERER_URL']
      : pathToFileURL(rendererEntry).toString();

  installNavigationGuard(window, rendererURL);

  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    void window.loadURL(process.env['ELECTRON_RENDERER_URL']);
  } else {
    void window.loadFile(rendererEntry);
  }
}

function installWindowShortcutHandler(window: BrowserWindow): void {
  window.webContents.on('before-input-event', (event, input) => {
    const result = handleAppShortcut(input, window.webContents);
    if (result === 'close-tab') {
      event.preventDefault();
      window.webContents.send('tab:close-active');
    } else if (result) {
      event.preventDefault();
    }
  });
}

function createWindow(): BrowserWindow {
  mainRendererMessages.resetReady();

  const stateFile = windowStateFilePath(app.getPath('userData'));
  const savedWindowState = loadWindowState(stateFile);
  const windowOpts = resolveWindowOptions(
    savedWindowState,
    screen.getAllDisplays().map((d) => d.workArea),
    screen.getPrimaryDisplay().workArea,
  );

  mainWindow = new BrowserWindow({
    width: windowOpts.width,
    height: windowOpts.height,
    ...(windowOpts.x != null && windowOpts.y != null ? { x: windowOpts.x, y: windowOpts.y } : {}),
    minWidth: 900,
    minHeight: 600,
    titleBarStyle: 'hiddenInset',
    trafficLightPosition: { x: 16, y: 17 },
    show: false,
    autoHideMenuBar: true,
    ...(is.dev || process.platform === 'linux' ? { icon: BUNDLED_ICON_PATH } : {}),
    webPreferences: createRendererWebPreferences(),
  });
  const window = mainWindow;

  let persistTimer: ReturnType<typeof setTimeout> | null = null;
  const persistWindowState = () => {
    const snap = snapshotWindowState(window);
    if (snap) saveWindowStateToFile(stateFile, snap);
  };
  const schedulePersistWindowState = () => {
    if (persistTimer) clearTimeout(persistTimer);
    persistTimer = setTimeout(persistWindowState, 400);
  };
  window.on('resize', schedulePersistWindowState);
  window.on('move', schedulePersistWindowState);
  window.on('close', () => {
    if (persistTimer) clearTimeout(persistTimer);
    persistWindowState();
  });

  window.on('closed', () => {
    if (mainWindow === window) {
      mainWindow = null;
      mainRendererMessages.resetReady();
    }
  });

  window.webContents.session.webRequest.onBeforeSendHeaders(
    { urls: ['wss://*/*', 'ws://*/*'] },
    (details, callback) => {
      delete details.requestHeaders['Origin'];
      callback({ requestHeaders: details.requestHeaders });
    },
  );

  window.on('ready-to-show', () => {
    if (windowOpts.isFullScreen) {
      window.setFullScreen(true);
    } else if (windowOpts.isMaximized) {
      window.maximize();
    }
    window.show();
  });

  installDownloadSaveDialogHandler(window);

  window.webContents.setWindowOpenHandler((details) => {
    openExternalSafely(details.url);
    return { action: 'deny' };
  });

  installWindowShortcutHandler(window);

  if (devLog) {
    window.webContents.on('console-message', (details) => {
      const { level, message, sourceId, lineNumber } = details;
      devLog(level, `${message} (${sourceId}:${lineNumber})`);
    });

    window.webContents.on(
      'did-fail-load',
      (_event, errorCode, errorDescription, validatedURL, isMainFrame) => {
        if (errorCode === -3) return;
        devLog(
          'did-fail-load',
          `code=${errorCode} desc=${errorDescription} url=${validatedURL} mainFrame=${isMainFrame}`,
        );
      },
    );
  }

  installRendererRecoveryHandlers(window as unknown as RendererRecoveryWindow, {
    isDev: is.dev,
    showReloadPrompt: createElectronReloadPrompt((options) =>
      dialog.showMessageBox(window, options),
    ),
    getDiagnosticContext: () => {
      const routeContext = rendererRouteContexts.get(window.webContents);
      return routeContext ? { desktopRoute: routeContext } : {};
    },
    captureStack: () => captureStackIfEnabled(window.webContents),
    persistBreadcrumb: is.dev
      ? undefined
      : (payload) =>
          writeFreezeBreadcrumb(freezeBreadcrumbPath(), {
            ownerId: `main:${window.id}`,
            kind: payload.kind,
            context: payload.context,
            ts: Date.now(),
            version: getAppVersion(),
          }),
    clearBreadcrumb: is.dev
      ? undefined
      : () => clearFreezeBreadcrumb(freezeBreadcrumbPath(), `main:${window.id}`),
    log: devLog,
  });

  installContextMenu(window.webContents);
  installNavigationGestures(window);

  loadRenderer(window);
  return window;
}

function createIssueWindow(context: IssueWindowContext): void {
  const window = new BrowserWindow({
    width: 960,
    height: 760,
    minWidth: 720,
    minHeight: 520,
    title: context.title,
    titleBarStyle: 'hiddenInset',
    trafficLightPosition: { x: 16, y: 17 },
    show: false,
    autoHideMenuBar: true,
    ...(is.dev || process.platform === 'linux' ? { icon: BUNDLED_ICON_PATH } : {}),
    webPreferences: createRendererWebPreferences([encodeIssueWindowArgument(context)]),
  });

  issueWindows.add(window);
  authSessionCoordinator.registerIssueWindow(window);
  window.on('closed', () => {
    issueWindows.delete(window);
    authSessionCoordinator.unregisterIssueWindow(window);
  });

  window.on('ready-to-show', () => window.show());
  installDownloadSaveDialogHandler(window);

  window.webContents.setWindowOpenHandler((details) => {
    void openExternalSafely(details.url);
    return { action: 'deny' };
  });
  installWindowShortcutHandler(window);

  const initialRouteContext = sanitizeRendererRouteContext({
    surface: 'tab',
    path: context.path,
    workspaceSlug: context.workspaceSlug,
  });
  if (initialRouteContext) {
    rendererRouteContexts.set(window.webContents, initialRouteContext);
  }
  installRendererRecoveryHandlers(window as unknown as RendererRecoveryWindow, {
    isDev: is.dev,
    showReloadPrompt: createElectronReloadPrompt((options) =>
      dialog.showMessageBox(window, options),
    ),
    getDiagnosticContext: () => {
      const routeContext = rendererRouteContexts.get(window.webContents);
      return routeContext ? { desktopRoute: routeContext } : {};
    },
    captureStack: () => captureStackIfEnabled(window.webContents),
    persistBreadcrumb: is.dev
      ? undefined
      : (payload) =>
          writeFreezeBreadcrumb(freezeBreadcrumbPath(), {
            ownerId: `issue:${window.id}`,
            kind: payload.kind,
            context: payload.context,
            ts: Date.now(),
            version: getAppVersion(),
          }),
    clearBreadcrumb: is.dev
      ? undefined
      : () => clearFreezeBreadcrumb(freezeBreadcrumbPath(), `issue:${window.id}`),
    log: devLog,
  });

  installContextMenu(window.webContents);
  loadRenderer(window);
}

const DEV_APP_NAME = process.env.DESKTOP_APP_SUFFIX
  ? `Goosar Canary ${process.env.DESKTOP_APP_SUFFIX}`
  : 'Goosar Canary';

if (is.dev) {
  app.setName(DEV_APP_NAME);
  app.setPath('userData', join(app.getPath('appData'), DEV_APP_NAME));
} else {
  app.setName('Goosar');
}

function readBootDebugLogging(): boolean {
  try {
    return parseLoggingSettings(readFileSync(desktopConfigPath(), 'utf-8')).debug;
  } catch {
    return false;
  }
}

initMainLogging({ debug: readBootDebugLogging() });
installRendererLogCapture();
console.log(
  `[boot] Goosar desktop ${getAppVersion()} starting (platform=${process.platform}, dev=${is.dev}, debugLogging=${isDebugFileLogging()})`,
);

if (process.defaultApp) {
  app.setAsDefaultProtocolClient(PROTOCOL, process.execPath, [app.getAppPath()]);
} else {
  app.setAsDefaultProtocolClient(PROTOCOL);
}

const gotTheLock = app.requestSingleInstanceLock();

if (!gotTheLock) {
  app.quit();
} else {
  app.on('open-url', (event, url) => {
    event.preventDefault();
    handleDeepLink(url);
  });

  app.on('second-instance', (_event, argv) => {
    const window = ensureMainWindow();
    if (window) focusMainWindow(window);

    const deepLinkUrl = argv.find((arg) => arg.startsWith(`${PROTOCOL}://`));
    if (deepLinkUrl) handleDeepLink(deepLinkUrl);
  });

  const coldStartDeepLink = process.argv.find((arg) => arg.startsWith(`${PROTOCOL}://`));
  if (coldStartDeepLink) handleDeepLink(coldStartDeepLink);

  app.whenReady().then(async () => {
    const viteEnv = import.meta.env as ImportMetaEnv & {
      readonly VITE_API_URL?: string;
      readonly VITE_WS_URL?: string;
      readonly VITE_APP_URL?: string;
    };

    const deploymentDefaults = loadDeploymentDefaults({
      envPath: process.env['GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS'],
      stagedDir: deploymentDefaultsStagedDir({
        isPackaged: app.isPackaged,
        resourcesPath: process.resourcesPath,
        appPath: app.getAppPath(),
      }),
    });

    const bakedHosts = deploymentDefaults?.['deployment'];
    deploymentHosts =
      bakedHosts && typeof bakedHosts === 'object' && !Array.isArray(bakedHosts)
        ? (bakedHosts as Record<string, unknown>)
        : null;

    runtimeConfigResult = await loadRuntimeConfig({
      isDev: is.dev,
      deploymentDefaults,
      env: {
        apiUrl: viteEnv.VITE_API_URL,
        wsUrl: viteEnv.VITE_WS_URL,
        appUrl: viteEnv.VITE_APP_URL,
      },
    });

    presetOverlay = loadPresetOverlay({
      envPath: process.env['GOOSAR_PRESET_OVERLAY'],
      stagedDir: presetOverlayStagedDir({
        isPackaged: app.isPackaged,
        resourcesPath: process.resourcesPath,
        appPath: app.getAppPath(),
      }),
    });
    if (presetOverlay) {
      console.log('[preset-overlay] loaded MCP preset provisioning overlay');
    }

    electronApp.setAppUserModelId(
      is.dev ? 'ru.goosar.desktop.dev' : 'ru.goosar.desktop',
    );

    if (is.dev && process.platform === 'darwin' && app.dock) {
      const icon = nativeImage.createFromPath(BUNDLED_ICON_PATH);
      if (!icon.isEmpty()) app.dock.setIcon(icon);
    }

    app.on('browser-window-created', (_, window) => {
      optimizer.watchWindowShortcuts(window);
    });

    ipcMain.handle('shell:openExternal', (_event, url: string) => {
      return openExternalSafely(url);
    });

    ipcMain.on('window:close', (event) => {
      BrowserWindow.fromWebContents(event.sender)?.close();
    });

    ipcMain.handle('window:open-issue', (event, request: unknown) => {
      if (!BrowserWindow.fromWebContents(event.sender)) {
        return { ok: false, reason: 'invalid_request' } as const;
      }
      const context = parseIssueWindowRequest(request);
      if (!context) {
        return { ok: false, reason: 'invalid_request' } as const;
      }
      createIssueWindow(context);
      return { ok: true } as const;
    });

    ipcMain.handle('file:download-url', (event, url: string) => {
      const sourceWindow = BrowserWindow.fromWebContents(event.sender);
      if (!sourceWindow) {
        console.warn('[download] ignored file:download-url — source window torn down');
        return;
      }
      downloadURLSafely(sourceWindow, url);
    });

    ipcMain.on('app:get-info', (event) => {
      const p = process.platform;
      const os =
        p === 'darwin' ? 'macos' : p === 'win32' ? 'windows' : p === 'linux' ? 'linux' : 'unknown';
      event.returnValue = { version: getAppVersion(), os };
    });

    ipcMain.on('freeze:get-last', (event) => {
      event.returnValue = readFreezeBreadcrumb(freezeBreadcrumbPath());
    });

    ipcMain.on('freeze:ack', (event, ts: unknown) => {
      if (!BrowserWindow.fromWebContents(event.sender)) return;
      if (typeof ts !== 'number' || !Number.isFinite(ts)) return;
      ackFreezeBreadcrumb(freezeBreadcrumbPath(), ts);
    });

    ipcMain.on(DIAGNOSTICS_CONTROL_CHANNEL, (event, control: unknown) => {
      if (!BrowserWindow.fromWebContents(event.sender)) return;
      diagnosticsControl.apply(event.sender, control);
    });

    ipcMain.on(UI_LOCALE_CHANNEL, (event, value: unknown) => {
      if (!BrowserWindow.fromWebContents(event.sender)) return;
      const locale = parseUiLocale(value);
      if (!locale) return;
      recordRendererUiLocale(event.sender, locale);
    });

    ipcMain.on('runtime-config:get', (event) => {
      event.returnValue = runtimeConfigResult;
    });

    ipcMain.on('preset-overlay:get', (event) => {
      event.returnValue = presetOverlay;
    });

    ipcMain.on('deployment-hosts:get', (event) => {
      event.returnValue = deploymentHosts;
    });

    ipcMain.handle('runtime-config:probe', (_event, apiUrl: unknown) =>
      probeRuntimeServer({
        apiUrl: typeof apiUrl === 'string' ? apiUrl : '',
      }),
    );

    ipcMain.handle('runtime-config:set', async (_event, patch: unknown) => {
      const result = await saveRuntimeConfig({ patch });
      if (!result.ok) return result;

      runtimeConfigResult = { ok: true, config: result.config };
      setImmediate(() => {
        for (const win of BrowserWindow.getAllWindows()) {
          win.webContents.reload();
        }
      });
      return result;
    });

    ipcMain.handle('agent-runtime:set-config', (_event, patch: unknown) => {
      const sanitized = sanitizeAgentConfigPatch(patch);
      if (!sanitized) {
        return {
          ok: false,
          field: null,
          kind: 'bad_usage',
          message: 'There is nothing to save.',
        } satisfies AgentConfigSaveResult;
      }
      return saveAgentConfig({ home: homedir(), env: process.env }, sanitized);
    });

    ipcMain.handle('llm-runtime:get-config', (): LlmRuntimeConfigView => {
      const ctx = { home: homedir(), env: process.env };
      const configPath = resolveAgentConfigPath(ctx);
      let apiBase: string | null = null;
      let model: string | null = null;
      let hasKey = false;
      if (configPath) {
        try {
          const scalars = readLlmProfileScalars(readFileSync(configPath, 'utf-8'));
          apiBase = scalars.apiBase;
          model = scalars.model;
          hasKey = scalars.apiKey !== null;
        } catch {
          // Unreadable config → report it as "unset" rather than crashing the
          // read the settings form awaits; the form still lets the user write.
        }
      }
      return {
        apiBase,
        model,
        hasKey,
        configPath,
      };
    });

    ipcMain.handle('llm-runtime:save', (_event, input: unknown) =>
      saveLlmRuntimeSettings(
        {
          saveConfig: (patch) => saveAgentConfig({ home: homedir(), env: process.env }, patch),
          probeReachability: probePerimeterLlmReachability,
        },
        parseLlmRuntimeSaveInput(input),
      ),
    );

    ipcMain.handle('uninstall:plan', () => planUninstall(uninstallContext()));

    ipcMain.handle('uninstall:perform', (_event, options: unknown) =>
      performUninstall(uninstallContext(), parseUninstallOptions(options)),
    );

    ipcMain.on(RENDERER_ROUTE_CONTEXT_CHANNEL, (event, context: unknown) => {
      if (!BrowserWindow.fromWebContents(event.sender)) return;
      const sanitized = sanitizeRendererRouteContext(context);
      if (!sanitized) return;
      rendererRouteContexts.set(event.sender, sanitized);
    });

    ipcMain.on(MAIN_RENDERER_CHANNEL_STATE_CHANNEL, (event, state: unknown) => {
      if (!mainWindow || event.sender !== mainWindow.webContents) return;
      const parsed = parseMainRendererChannelState(state);
      if (!parsed) return;
      mainRendererMessages.setReady(parsed.channel, parsed.ready, sendMainRendererMessage);
    });

    ipcMain.on(AUTH_SESSION_STATE_CHANNEL, (event, value: unknown) => {
      const sourceWindow = BrowserWindow.fromWebContents(event.sender);
      const userId = parseAuthSessionUserId(value);
      if (!sourceWindow || userId === undefined) return;

      if (sourceWindow === mainWindow) {
        const accountInvalidated = authSessionCoordinator.reportMain(userId);
        if (accountInvalidated) {
          authSessionGeneration += 1;
          mainRendererMessages.clear('inbox:open');
        }
        return;
      }
      if (issueWindows.has(sourceWindow)) {
        authSessionCoordinator.reportIssue(sourceWindow, userId);
      }
    });

    ipcMain.handle('window:setImmersive', (event, immersive: boolean) => {
      if (process.platform !== 'darwin') return;
      BrowserWindow.fromWebContents(event.sender)?.setWindowButtonVisibility(!immersive);
    });

    ipcMain.on('notification:show', (event, value: unknown) => {
      const sourceWindow = BrowserWindow.fromWebContents(event.sender);
      if (!sourceWindow) return;
      if (sourceWindow === mainWindow) {
        if (!authSessionCoordinator.hasActiveMainSession()) return;
      } else if (
        !issueWindows.has(sourceWindow) ||
        !authSessionCoordinator.isCurrentIssueSession(sourceWindow)
      ) {
        return;
      }

      const payload = parseNativeNotificationPayload(value);
      if (!payload || !Notification.isSupported()) return;
      const anyWindowFocused = BrowserWindow.getAllWindows().some(
        (window) => !window.isDestroyed() && window.isFocused(),
      );
      if (!notificationGate.shouldShow(payload.itemId, anyWindowFocused)) {
        return;
      }

      const notification = new Notification({
        title: payload.title,
        body: payload.body,
      });
      const notificationSessionGeneration = authSessionGeneration;
      notification.on('click', () => {
        if (notificationSessionGeneration !== authSessionGeneration) return;
        dispatchToMainRenderer('inbox:open', {
          slug: payload.slug,
          itemId: payload.itemId,
          issueKey: payload.issueKey,
        });
      });
      notification.show();
    });

    ipcMain.on('badge:set', (_event, rawCount: number) => {
      const count = Math.max(0, Math.floor(rawCount));
      if (process.platform === 'darwin') {
        const label = count === 0 ? '' : count > 99 ? '99+' : String(count);
        app.dock?.setBadge(label);
      } else {
        app.setBadgeCount(count);
      }
    });

    const rebuildAppMenu = installAppMenu({
      isDebugLoggingEnabled: () => isDebugFileLogging(),
      setDebugLogging: async (enabled) => {
        const result = await saveDebugLoggingToggle({ enabled });
        if (result.ok) {
          setDebugFileLogging(result.settings.debug);
          console.log(
            `[logging] debug file logging ${result.settings.debug ? 'enabled' : 'disabled'}`,
          );
        } else {
          console.warn(`[logging] failed to persist the debug toggle: ${result.error.message}`);
        }
      },
      installCli: () => {
        void (async () => {
          const bin = await getCliBinaryPath();
          const lang = app.getPreferredSystemLanguages()[0]?.startsWith('ru') ? 'ru' : 'en';
          const message = bin
            ? describeCliPathResult(await installCliOnPath({ target: bin }), lang)
            : lang === 'ru'
              ? 'Бинарь goosar не найден в приложении — переустановите его.'
              : 'The goosar binary was not found in the app — reinstall it.';
          await dialog.showMessageBox({ type: 'info', message });
        })();
      },
      openLogsFolder: () => {
        const logPath = mainLogFilePath();
        if (existsSync(logPath)) {
          shell.showItemInFolder(logPath);
        } else {
          void shell.openPath(dirname(logPath));
        }
      },
    });

    watchLoggingConfig({
      configPath: desktopConfigPath(),
      readConfig: () => {
        try {
          return readFileSync(desktopConfigPath(), 'utf-8');
        } catch {
          return null;
        }
      },
      isDebugEnabled: () => isDebugFileLogging(),
      applyDebug: (enabled) => setDebugFileLogging(enabled),
      onApplied: (enabled) => {
        console.log(
          `[logging] debug file logging ${enabled ? 'enabled' : 'disabled'} via desktop.json edit`,
        );
        rebuildAppMenu();
      },
    });

    desktopInitialized = true;

    setResyncClientSecrets(resyncClientSecretsForRetry);

    await initPerimeter({
      restartDaemonIfAlive,
      getMainWindow: () => mainWindow,
      liveProbes: {
        server: probePerimeterServerReachability,
        llm: probePerimeterLlmReachability,
        daemon: probeDaemonReachability,
        provisioning: probeProvisioningReachability,
      },
    });

    initProvisioningLifecycle();
    setProvisioningRestartHook(notifyProvisioningPackagesChanged);
    setProvisioningDaemonBusyHook(isDaemonBusyForProvisioningSwap);
    setLocalProxyRuntimeResolver((name) =>
      resolveProvisionedRuntimeDir({ home: homedir(), env: process.env }, name),
    );

    createWindow();

    setupAutoUpdater(() => mainWindow);
    setupDaemonManager(() => mainWindow);
    setupLocalDirectory(() => mainWindow);

    app.on('activate', () => {
      const window = ensureMainWindow();
      if (window) focusMainWindow(window);
    });
  });
}

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});

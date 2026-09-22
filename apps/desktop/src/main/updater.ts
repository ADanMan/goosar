import { autoUpdater, type UpdateDownloadedEvent } from 'electron-updater';
import { app, type BrowserWindow, ipcMain } from 'electron';
import type { ManualUpdateCheckResult, UpdaterPreferences } from '../shared/updater-types';
import {
  DEFAULT_UPDATER_PREFERENCES,
  loadUpdaterPreferences,
  saveUpdaterPreferences,
  updaterPreferencesPath,
} from './updater-preferences';
import { updaterHardDisabled } from './update-gate';
import { UPDATE_CONTROL_CHANNEL, parseUpdateControl } from '../shared/update-control';

autoUpdater.autoDownload = true;
autoUpdater.autoInstallOnAppQuit = true;

if (process.platform === 'win32' && process.arch === 'arm64') {
  autoUpdater.channel = 'latest-arm64';
}

interface ChannelConfigurableUpdater {
  channel: string | null;
  allowDowngrade: boolean;
}

export function configureMacX64UpdateChannel(
  updater: ChannelConfigurableUpdater,
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch,
): void {
  if (platform !== 'darwin' || arch !== 'x64') return;

  updater.channel = 'latest-x64';
  updater.allowDowngrade = false;
}

configureMacX64UpdateChannel(autoUpdater);

const STARTUP_CHECK_DELAY_MS = 5_000;
const PERIODIC_CHECK_INTERVAL_MS = 60 * 60 * 1000; 

type RendererChannel =
  'updater:update-available' | 'updater:download-progress' | 'updater:update-downloaded';

function isDestroyedObjectError(err: unknown): boolean {
  return err instanceof Error && err.message.includes('Object has been destroyed');
}

function sendToLiveRenderer(
  win: BrowserWindow | null,
  channel: RendererChannel,
  payload: unknown,
): void {
  if (!win || win.isDestroyed()) return;

  try {
    const { webContents } = win;
    if (webContents.isDestroyed()) return;
    webContents.send(channel, payload);
  } catch (err) {
    if (isDestroyedObjectError(err)) return;
    throw err;
  }
}

let inFlightCheck: Promise<unknown> | null = null;
function checkForUpdatesOnce(): Promise<unknown> {
  if (inFlightCheck) return inFlightCheck;
  const p = autoUpdater
    .checkForUpdates()
    .then((result) => {
      void (result as { downloadPromise?: Promise<unknown> } | null)?.downloadPromise?.catch(
        (err) => {
          console.error('Failed to download update:', err);
        },
      );
      return result;
    })
    .finally(() => {
      if (inFlightCheck === p) inFlightCheck = null;
    });
  inFlightCheck = p;
  return p;
}

export function setupAutoUpdater(getMainWindow: () => BrowserWindow | null): void {
  const preferencesFilePath = updaterPreferencesPath(app.getPath('userData'));
  const hardDisabled = updaterHardDisabled();
  let perimeterProfileReported = false;
  const updatesBlocked = (): boolean => hardDisabled || perimeterProfileReported;
  let automaticUpdatesEnabled = DEFAULT_UPDATER_PREFERENCES.automaticUpdates;
  let startupCheckElapsed = false;
  let startupTimer: ReturnType<typeof setTimeout> | null = null;
  let periodicTimer: ReturnType<typeof setInterval> | null = null;
  const preferencesReady = loadUpdaterPreferences(preferencesFilePath).then((preferences) => {
    automaticUpdatesEnabled = preferences.automaticUpdates;
    return preferences;
  });

  const runAutomaticCheck = (errorMessage: string): void => {
    void preferencesReady
      .then(() => {
        if (!automaticUpdatesEnabled || updatesBlocked()) return;
        return checkForUpdatesOnce();
      })
      .catch((err) => {
        console.error(errorMessage, err);
      });
  };

  const scheduleBackgroundChecks = (): void => {
    if (updatesBlocked()) return;
    if (startupTimer === null && !startupCheckElapsed) {
      startupTimer = setTimeout(() => {
        startupTimer = null;
        startupCheckElapsed = true;
        runAutomaticCheck('Failed to check for updates:');
      }, STARTUP_CHECK_DELAY_MS);
    }
    if (periodicTimer === null) {
      periodicTimer = setInterval(() => {
        runAutomaticCheck('Periodic update check failed:');
      }, PERIODIC_CHECK_INTERVAL_MS);
    }
  };

  const cancelBackgroundChecks = (): void => {
    if (startupTimer !== null) {
      clearTimeout(startupTimer);
      startupTimer = null;
    }
    if (periodicTimer !== null) {
      clearInterval(periodicTimer);
      periodicTimer = null;
    }
  };

  autoUpdater.on('update-available', (info) => {
    sendToLiveRenderer(getMainWindow(), 'updater:update-available', {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on('download-progress', (progress) => {
    sendToLiveRenderer(getMainWindow(), 'updater:download-progress', {
      percent: progress.percent,
    });
  });

  autoUpdater.on('update-downloaded', (info: UpdateDownloadedEvent) => {
    sendToLiveRenderer(getMainWindow(), 'updater:update-downloaded', {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on('error', (err) => {
    console.error('Auto-updater error:', err);
  });

  ipcMain.on(UPDATE_CONTROL_CHANNEL, (_event, control: unknown) => {
    const { perimeterProfile } = parseUpdateControl(control);
    if (perimeterProfile === perimeterProfileReported) return;
    perimeterProfileReported = perimeterProfile;

    if (perimeterProfile) {
      cancelBackgroundChecks();
      return;
    }
    void preferencesReady
      .then(() => {
        if (!automaticUpdatesEnabled || updatesBlocked()) return;
        if (startupCheckElapsed) {
          runAutomaticCheck('Failed to check for updates:');
        }
        scheduleBackgroundChecks();
      })
      .catch((err) => {
        console.error('Failed to re-arm update checks:', err);
      });
  });

  ipcMain.handle('updater:download', () => {
    if (updatesBlocked()) {
      throw new Error('Updates are managed by your operator (perimeter delivery profile)');
    }
    return autoUpdater.downloadUpdate();
  });

  ipcMain.handle('updater:install', () => {
    if (updatesBlocked()) return;
    autoUpdater.quitAndInstall(false, true);
  });

  ipcMain.handle('updater:get-preferences', async (): Promise<UpdaterPreferences> => {
    await preferencesReady;
    return { automaticUpdates: automaticUpdatesEnabled };
  });

  ipcMain.handle(
    'updater:set-automatic-updates',
    async (_event, enabled: unknown): Promise<UpdaterPreferences> => {
      if (typeof enabled !== 'boolean') {
        throw new TypeError('automaticUpdates must be a boolean');
      }

      await preferencesReady;
      const wasEnabled = automaticUpdatesEnabled;
      const preferences = { automaticUpdates: enabled };
      await saveUpdaterPreferences(preferencesFilePath, preferences);
      automaticUpdatesEnabled = enabled;

      if (!enabled) {
        cancelBackgroundChecks();
      } else if (!wasEnabled) {
        if (startupCheckElapsed) {
          runAutomaticCheck('Failed to check for updates:');
        }
        scheduleBackgroundChecks();
      }

      return preferences;
    },
  );

  ipcMain.handle('updater:check', async (): Promise<ManualUpdateCheckResult> => {
    if (updatesBlocked()) {
      return {
        ok: false,
        error: 'Updates are managed by your operator (perimeter delivery profile)',
      };
    }
    try {
      const result = (await checkForUpdatesOnce()) as {
        updateInfo: { version: string };
        isUpdateAvailable?: boolean;
      } | null;
      const currentVersion = app.getVersion();
      return {
        ok: true,
        currentVersion,
        latestVersion: result?.updateInfo.version ?? currentVersion,
        available: result?.isUpdateAvailable ?? false,
      };
    } catch (err) {
      return {
        ok: false,
        error: err instanceof Error ? err.message : String(err),
      };
    }
  });

  scheduleBackgroundChecks();
}

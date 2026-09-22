import { watch, type FSWatcher } from 'fs';
import { basename, dirname } from 'path';
import { parseLoggingSettings } from '../shared/logging-config';

export const DEFAULT_WATCH_DEBOUNCE_MS = 250;
export const DEFAULT_WATCH_RETRY_MS = 30_000;

export interface LoggingConfigWatchOptions {
  configPath: string;
  readConfig: () => string | null;
  isDebugEnabled: () => boolean;
  applyDebug: (enabled: boolean) => void;
  onApplied?: (enabled: boolean) => void;
  watchImpl?: (
    dir: string,
    listener: (eventType: string, filename: string | Buffer | null) => void,
  ) => FSWatcher;
  debounceMs?: number;
  retryMs?: number;
}

export interface LoggingConfigWatch {
  dispose: () => void;
}

export function watchLoggingConfig(options: LoggingConfigWatchOptions): LoggingConfigWatch {
  const dir = dirname(options.configPath);
  const fileName = basename(options.configPath);
  const watchImpl = options.watchImpl ?? watch;
  const debounceMs = options.debounceMs ?? DEFAULT_WATCH_DEBOUNCE_MS;
  const retryMs = options.retryMs ?? DEFAULT_WATCH_RETRY_MS;

  let watcher: FSWatcher | null = null;
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let disposed = false;

  const applyFromFile = (): void => {
    debounceTimer = null;
    let raw: string | null;
    try {
      raw = options.readConfig();
    } catch {
      return;
    }
    const settings = parseLoggingSettings(raw);
    if (settings.debug === options.isDebugEnabled()) return;
    options.applyDebug(settings.debug);
    options.onApplied?.(settings.debug);
  };

  const scheduleApply = (): void => {
    if (disposed) return;
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(applyFromFile, debounceMs);
  };

  const scheduleRearm = (): void => {
    if (disposed || retryTimer) return;
    retryTimer = setTimeout(() => {
      retryTimer = null;
      arm();
    }, retryMs);
  };

  const dropWatcher = (): void => {
    if (!watcher) return;
    const current = watcher;
    watcher = null;
    try {
      current.close();
    } catch {
      // A watcher that already died on its own has nothing left to close.
    }
  };

  const arm = (): void => {
    if (disposed) return;
    try {
      watcher = watchImpl(dir, (_eventType, filename) => {
        if (filename && filename.toString() !== fileName) return;
        scheduleApply();
      });
    } catch {
      scheduleRearm();
      return;
    }
    watcher.on('error', () => {
      dropWatcher();
      scheduleRearm();
    });
    scheduleApply();
  };

  arm();

  return {
    dispose: () => {
      disposed = true;
      if (debounceTimer) clearTimeout(debounceTimer);
      if (retryTimer) clearTimeout(retryTimer);
      debounceTimer = null;
      retryTimer = null;
      dropWatcher();
    },
  };
}

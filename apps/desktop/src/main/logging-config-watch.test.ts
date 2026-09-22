import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { FSWatcher } from 'fs';
import {
  DEFAULT_WATCH_DEBOUNCE_MS,
  DEFAULT_WATCH_RETRY_MS,
  watchLoggingConfig,
  type LoggingConfigWatchOptions,
} from './logging-config-watch';

type DirListener = (eventType: string, filename: string | Buffer | null) => void;

function createFakeWatcher() {
  const errorListeners: Array<(err: Error) => void> = [];
  return {
    close: vi.fn(),
    on: vi.fn((event: string, listener: (err: Error) => void) => {
      if (event === 'error') errorListeners.push(listener);
    }),
    emitError(err: Error) {
      for (const listener of errorListeners) listener(err);
    },
  };
}

function createHarness(overrides: Partial<LoggingConfigWatchOptions> = {}): {
  options: LoggingConfigWatchOptions;
  state: { raw: string | null; debug: boolean };
  watchers: ReturnType<typeof createFakeWatcher>[];
  listeners: DirListener[];
  applyDebug: ReturnType<typeof vi.fn>;
  onApplied: ReturnType<typeof vi.fn>;
  watchImpl: ReturnType<typeof vi.fn>;
} {
  const state = { raw: null as string | null, debug: false };
  const watchers: ReturnType<typeof createFakeWatcher>[] = [];
  const listeners: DirListener[] = [];
  const applyDebug = vi.fn((enabled: boolean) => {
    state.debug = enabled;
  });
  const onApplied = vi.fn();
  const watchImpl = vi.fn((_dir: string, listener: DirListener) => {
    const watcher = createFakeWatcher();
    watchers.push(watcher);
    listeners.push(listener);
    return watcher as unknown as FSWatcher;
  });
  return {
    options: {
      configPath: '/home/user/.goosar/desktop.json',
      readConfig: () => state.raw,
      isDebugEnabled: () => state.debug,
      applyDebug,
      onApplied,
      watchImpl,
      ...overrides,
    },
    state,
    watchers,
    listeners,
    applyDebug,
    onApplied,
    watchImpl,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('watchLoggingConfig', () => {
  it('watches the config directory, not the file (atomic rename survives)', () => {
    const h = createHarness();
    const watch = watchLoggingConfig(h.options);
    expect(h.watchImpl).toHaveBeenCalledWith('/home/user/.goosar', expect.any(Function));
    watch.dispose();
  });

  it('applies a debug flip from a file change after the debounce', () => {
    const h = createHarness();
    const watch = watchLoggingConfig(h.options);
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS); 

    h.state.raw = '{"logging":{"debug":true}}';
    h.listeners[0]('rename', 'desktop.json');
    expect(h.applyDebug).not.toHaveBeenCalled(); 
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    expect(h.applyDebug).toHaveBeenCalledExactlyOnceWith(true);
    expect(h.onApplied).toHaveBeenCalledExactlyOnceWith(true);
    watch.dispose();
  });

  it('applies pre-existing file state right after arming', () => {
    const h = createHarness();
    h.state.raw = '{"logging":{"debug":true}}';
    const watch = watchLoggingConfig(h.options);
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);
    expect(h.applyDebug).toHaveBeenCalledExactlyOnceWith(true);
    watch.dispose();
  });

  it('collapses an event burst into a single re-read', () => {
    const h = createHarness();
    const watch = watchLoggingConfig(h.options);
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    h.state.raw = '{"logging":{"debug":true}}';
    h.listeners[0]('rename', 'desktop.json');
    h.listeners[0]('change', 'desktop.json');
    h.listeners[0]('rename', 'desktop.json');
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    expect(h.applyDebug).toHaveBeenCalledTimes(1);
    watch.dispose();
  });

  it('ignores events for sibling files', () => {
    const h = createHarness();
    const watch = watchLoggingConfig(h.options);
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    h.state.raw = '{"logging":{"debug":true}}';
    h.listeners[0]('change', 'agent-config.json');
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    expect(h.applyDebug).not.toHaveBeenCalled();
    watch.dispose();
  });

  it('treats a null filename as potentially the config file', () => {
    const h = createHarness();
    const watch = watchLoggingConfig(h.options);
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    h.state.raw = '{"logging":{"debug":true}}';
    h.listeners[0]('change', null);
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    expect(h.applyDebug).toHaveBeenCalledExactlyOnceWith(true);
    watch.dispose();
  });

  it('does nothing when the file already matches the live level (last write wins)', () => {
    const h = createHarness();
    h.state.debug = true;
    h.state.raw = '{"logging":{"debug":true}}';
    const watch = watchLoggingConfig(h.options);

    h.listeners[0]('change', 'desktop.json');
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    expect(h.applyDebug).not.toHaveBeenCalled();
    expect(h.onApplied).not.toHaveBeenCalled();
    watch.dispose();
  });

  it('degrades a deleted or malformed file to debug=false', () => {
    const h = createHarness();
    h.state.debug = true;
    h.state.raw = null;
    const watch = watchLoggingConfig(h.options);

    h.listeners[0]('rename', 'desktop.json');
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    expect(h.applyDebug).toHaveBeenCalledExactlyOnceWith(false);
    watch.dispose();
  });

  it('survives a readConfig throw without applying anything', () => {
    const h = createHarness({
      readConfig: () => {
        throw new Error('EBUSY');
      },
    });
    const watch = watchLoggingConfig(h.options);

    h.listeners[0]('change', 'desktop.json');
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    expect(h.applyDebug).not.toHaveBeenCalled();
    watch.dispose();
  });

  it('retries arming when the directory does not exist yet', () => {
    const h = createHarness();
    h.watchImpl
      .mockImplementationOnce(() => {
        throw new Error('ENOENT');
      })
      .mockImplementationOnce(() => {
        throw new Error('ENOENT');
      });
    const watch = watchLoggingConfig(h.options);
    expect(h.listeners).toHaveLength(0);

    vi.advanceTimersByTime(DEFAULT_WATCH_RETRY_MS); 
    vi.advanceTimersByTime(DEFAULT_WATCH_RETRY_MS); 
    expect(h.watchImpl).toHaveBeenCalledTimes(3);
    expect(h.listeners).toHaveLength(1);

    h.state.raw = '{"logging":{"debug":true}}';
    h.listeners[0]('change', 'desktop.json');
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);
    expect(h.applyDebug).toHaveBeenCalledExactlyOnceWith(true);
    watch.dispose();
  });

  it('re-arms after a watcher error and closes the dead watcher', () => {
    const h = createHarness();
    const watch = watchLoggingConfig(h.options);
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    h.watchers[0].emitError(new Error('EPERM'));
    expect(h.watchers[0].close).toHaveBeenCalled();
    expect(h.watchImpl).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(DEFAULT_WATCH_RETRY_MS);
    expect(h.watchImpl).toHaveBeenCalledTimes(2);

    h.state.raw = '{"logging":{"debug":true}}';
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);
    expect(h.applyDebug).toHaveBeenCalledExactlyOnceWith(true);
    watch.dispose();
  });

  it('stops reacting after dispose', () => {
    const h = createHarness();
    const watch = watchLoggingConfig(h.options);
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS);

    watch.dispose();
    expect(h.watchers[0].close).toHaveBeenCalled();

    h.state.raw = '{"logging":{"debug":true}}';
    h.listeners[0]('change', 'desktop.json');
    vi.advanceTimersByTime(DEFAULT_WATCH_DEBOUNCE_MS + DEFAULT_WATCH_RETRY_MS);
    expect(h.applyDebug).not.toHaveBeenCalled();
  });
});

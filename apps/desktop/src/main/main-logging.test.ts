import { describe, expect, it, vi } from 'vitest';
import type { LogMessage, MainLogger } from 'electron-log';

vi.mock('electron-log/main', () => ({ default: {} }));

import {
  DEFAULT_LOG_FILE_MAX_SIZE_BYTES,
  initMainLogging,
  isDebugFileLogging,
  mainLogFilePath,
  redactionHook,
  setDebugFileLogging,
} from './main-logging';
import { REDACTED_VALUE } from './log-redaction';

type FakeLogger = {
  transports: {
    file: {
      level: string | false;
      maxSize: number;
      getFile: () => { path: string };
    };
  };
  hooks: Array<(message: LogMessage) => LogMessage>;
  errorHandler: { startCatching: ReturnType<typeof vi.fn> };
  functions: Record<string, (...args: unknown[]) => void>;
};

function createFakeLogger(): FakeLogger {
  return {
    transports: {
      file: {
        level: false,
        maxSize: 0,
        getFile: () => ({ path: '/logs/Goosar/main.log' }),
      },
    },
    hooks: [],
    errorHandler: { startCatching: vi.fn() },
    functions: {
      log: vi.fn(),
      info: vi.fn(),
      warn: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
    },
  };
}

function asMainLogger(fake: FakeLogger): MainLogger {
  return fake as unknown as MainLogger;
}

function makeMessage(data: unknown[]): LogMessage {
  return { data, date: new Date(0), level: 'info' } as LogMessage;
}

describe('initMainLogging', () => {
  it('starts the file transport at info level by default', () => {
    const fake = createFakeLogger();
    initMainLogging({ debug: false, consoleTarget: {} }, asMainLogger(fake));
    expect(fake.transports.file.level).toBe('info');
    expect(fake.transports.file.maxSize).toBe(DEFAULT_LOG_FILE_MAX_SIZE_BYTES);
  });

  it('starts at debug level when the desktop.json toggle is on', () => {
    const fake = createFakeLogger();
    initMainLogging({ debug: true, consoleTarget: {} }, asMainLogger(fake));
    expect(fake.transports.file.level).toBe('debug');
  });

  it('honors a configurable rotation threshold', () => {
    const fake = createFakeLogger();
    initMainLogging(
      { debug: false, maxFileSizeBytes: 123_456, consoleTarget: {} },
      asMainLogger(fake),
    );
    expect(fake.transports.file.maxSize).toBe(123_456);
  });

  it('installs the redaction hook', () => {
    const fake = createFakeLogger();
    initMainLogging({ debug: false, consoleTarget: {} }, asMainLogger(fake));
    expect(fake.hooks).toHaveLength(1);
    const hook = fake.hooks[0];
    const result = hook(makeMessage(['boot', { api_key: 'sk-fake' }]));
    expect(result.data).toEqual(['boot', { api_key: REDACTED_VALUE }]);
  });

  it('catches process-level failures without a dialog', () => {
    const fake = createFakeLogger();
    initMainLogging({ debug: false, consoleTarget: {} }, asMainLogger(fake));
    expect(fake.errorHandler.startCatching).toHaveBeenCalledWith({
      showDialog: false,
    });
  });

  it('re-binds console.* to the logger functions without rewriting call sites', () => {
    const fake = createFakeLogger();
    const consoleTarget: Partial<Console> = {};
    initMainLogging({ debug: false, consoleTarget }, asMainLogger(fake));
    consoleTarget.warn?.('daemon', 'restarting');
    expect(fake.functions.warn).toHaveBeenCalledWith('daemon', 'restarting');
  });
});

describe('redactionHook', () => {
  it('returns a new message instead of mutating', () => {
    const message = makeMessage([{ token: 't' }]);
    const result = redactionHook(message);
    expect(result).not.toBe(message);
    expect(message.data).toEqual([{ token: 't' }]);
    expect(result.data).toEqual([{ token: REDACTED_VALUE }]);
  });
});

describe('debug toggle', () => {
  it('flips the file level at runtime and reports it back', () => {
    const fake = createFakeLogger();
    initMainLogging({ debug: false, consoleTarget: {} }, asMainLogger(fake));
    expect(isDebugFileLogging(asMainLogger(fake))).toBe(false);

    setDebugFileLogging(true, asMainLogger(fake));
    expect(fake.transports.file.level).toBe('debug');
    expect(isDebugFileLogging(asMainLogger(fake))).toBe(true);

    setDebugFileLogging(false, asMainLogger(fake));
    expect(fake.transports.file.level).toBe('info');
    expect(isDebugFileLogging(asMainLogger(fake))).toBe(false);
  });
});

describe('mainLogFilePath', () => {
  it('resolves through the file transport', () => {
    const fake = createFakeLogger();
    expect(mainLogFilePath(asMainLogger(fake))).toBe('/logs/Goosar/main.log');
  });
});

import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { WebContents } from 'electron';
import type { MainLogger } from 'electron-log';

const ctx = vi.hoisted(() => ({
  appOn: vi.fn(),
}));

vi.mock('electron', () => ({ app: { on: ctx.appOn } }));
vi.mock('electron-log/main', () => ({ default: {} }));

import {
  attachRendererLogCapture,
  installRendererLogCapture,
  MAX_CAPTURED_CONSOLE_ERRORS,
} from './renderer-log-capture';

type Listener = (...args: unknown[]) => void;

function createFakeWebContents() {
  const listeners = new Map<string, Listener[]>();
  return {
    on(event: string, listener: Listener) {
      listeners.set(event, [...(listeners.get(event) ?? []), listener]);
      return this;
    },
    emit(event: string, ...args: unknown[]) {
      for (const listener of listeners.get(event) ?? []) listener(...args);
    },
  };
}

function createFakeLogger() {
  const scopedError = vi.fn();
  const scope = vi.fn().mockReturnValue({ error: scopedError });
  return { logger: { scope } as unknown as MainLogger, scope, scopedError };
}

function consoleMessage(overrides: Record<string, unknown> = {}) {
  return {
    level: 'error',
    message: 'Uncaught TypeError: boom',
    lineNumber: 3,
    sourceId: 'file:///app/index.js',
    ...overrides,
  };
}

beforeEach(() => {
  ctx.appOn.mockClear();
});

describe('attachRendererLogCapture', () => {
  it('logs error-level console messages under the renderer scope with source', () => {
    const contents = createFakeWebContents();
    const { logger, scope, scopedError } = createFakeLogger();
    attachRendererLogCapture(contents as unknown as WebContents, logger);

    contents.emit('console-message', consoleMessage());

    expect(scope).toHaveBeenCalledWith('renderer');
    expect(scopedError).toHaveBeenCalledWith(
      '[console-error] Uncaught TypeError: boom (file:///app/index.js:3)',
    );
  });

  it('ignores non-error console levels', () => {
    const contents = createFakeWebContents();
    const { logger, scopedError } = createFakeLogger();
    attachRendererLogCapture(contents as unknown as WebContents, logger);

    for (const level of ['info', 'warning', 'debug']) {
      contents.emit('console-message', consoleMessage({ level }));
    }

    expect(scopedError).not.toHaveBeenCalled();
  });

  it('omits the source suffix when sourceId is empty', () => {
    const contents = createFakeWebContents();
    const { logger, scopedError } = createFakeLogger();
    attachRendererLogCapture(contents as unknown as WebContents, logger);

    contents.emit('console-message', consoleMessage({ sourceId: '' }));

    expect(scopedError).toHaveBeenCalledWith('[console-error] Uncaught TypeError: boom');
  });

  it('caps captured console errors per webContents and marks the last one', () => {
    const contents = createFakeWebContents();
    const { logger, scopedError } = createFakeLogger();
    attachRendererLogCapture(contents as unknown as WebContents, logger);

    for (let i = 0; i < MAX_CAPTURED_CONSOLE_ERRORS + 10; i += 1) {
      contents.emit('console-message', consoleMessage({ message: `err ${i}` }));
    }

    expect(scopedError).toHaveBeenCalledTimes(MAX_CAPTURED_CONSOLE_ERRORS);
    expect(scopedError).toHaveBeenLastCalledWith(
      `[console-error] err ${MAX_CAPTURED_CONSOLE_ERRORS - 1} (file:///app/index.js:3) — further console errors from this window are suppressed`,
    );
  });

  it('counts the cap per webContents, not globally', () => {
    const first = createFakeWebContents();
    const second = createFakeWebContents();
    const { logger, scopedError } = createFakeLogger();
    attachRendererLogCapture(first as unknown as WebContents, logger);
    attachRendererLogCapture(second as unknown as WebContents, logger);

    for (let i = 0; i < MAX_CAPTURED_CONSOLE_ERRORS; i += 1) {
      first.emit('console-message', consoleMessage());
    }
    second.emit('console-message', consoleMessage({ message: 'still logged' }));

    expect(scopedError).toHaveBeenCalledTimes(MAX_CAPTURED_CONSOLE_ERRORS + 1);
    expect(scopedError).toHaveBeenLastCalledWith(
      '[console-error] still logged (file:///app/index.js:3)',
    );
  });

  it('logs render-process-gone even after the console-error cap', () => {
    const contents = createFakeWebContents();
    const { logger, scopedError } = createFakeLogger();
    attachRendererLogCapture(contents as unknown as WebContents, logger);

    for (let i = 0; i < MAX_CAPTURED_CONSOLE_ERRORS; i += 1) {
      contents.emit('console-message', consoleMessage());
    }
    contents.emit('render-process-gone', {}, { reason: 'crashed', exitCode: 5 });

    expect(scopedError).toHaveBeenLastCalledWith('[render-process-gone] reason=crashed exitCode=5');
  });
});

describe('installRendererLogCapture', () => {
  it('attaches the capture to every created webContents', () => {
    const { logger, scopedError } = createFakeLogger();
    installRendererLogCapture(logger);

    expect(ctx.appOn).toHaveBeenCalledWith('web-contents-created', expect.any(Function));
    const handler = ctx.appOn.mock.calls[0][1] as Listener;

    const contents = createFakeWebContents();
    handler({}, contents);
    contents.emit('console-message', consoleMessage());

    expect(scopedError).toHaveBeenCalledWith(
      '[console-error] Uncaught TypeError: boom (file:///app/index.js:3)',
    );
  });
});

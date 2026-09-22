import { app } from 'electron';
import type { WebContents } from 'electron';
import log from 'electron-log/main';
import type { MainLogger } from 'electron-log';

export const MAX_CAPTURED_CONSOLE_ERRORS = 50;
const MAX_MESSAGE_LENGTH = 10_000;
const MAX_SOURCE_LENGTH = 512;

export function installRendererLogCapture(logger: MainLogger = log): void {
  app.on('web-contents-created', (_event, contents) => {
    attachRendererLogCapture(contents, logger);
  });
}

export function attachRendererLogCapture(contents: WebContents, logger: MainLogger = log): void {
  const scoped = logger.scope('renderer');
  let captured = 0;

  contents.on('console-message', (details) => {
    if (details.level !== 'error') return;
    if (captured >= MAX_CAPTURED_CONSOLE_ERRORS) return;
    captured += 1;

    const message = clampString(details.message, MAX_MESSAGE_LENGTH);
    const source = formatSource(details.sourceId, details.lineNumber);
    const suffix = source ? ` (${source})` : '';
    const capNote =
      captured === MAX_CAPTURED_CONSOLE_ERRORS
        ? ' — further console errors from this window are suppressed'
        : '';
    scoped.error(`[console-error] ${message}${suffix}${capNote}`);
  });

  contents.on('render-process-gone', (_event, details) => {
    scoped.error(`[render-process-gone] reason=${details.reason} exitCode=${details.exitCode}`);
  });
}

function formatSource(sourceId: unknown, lineNumber: unknown): string {
  if (typeof sourceId !== 'string' || !sourceId.trim()) return '';
  const line = typeof lineNumber === 'number' ? `:${lineNumber}` : '';
  return `${clampString(sourceId, MAX_SOURCE_LENGTH)}${line}`;
}

function clampString(value: unknown, maxLength: number): string {
  if (typeof value !== 'string') return '';
  return value.length > maxLength ? value.slice(0, maxLength) : value;
}

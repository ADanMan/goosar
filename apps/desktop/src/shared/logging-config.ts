import { backfillBootFields, isRecord, parseDesktopDocumentBase } from './perimeter-config';

export interface LoggingSettings {
  debug: boolean;
}

export const DEFAULT_LOGGING_SETTINGS: LoggingSettings = Object.freeze({
  debug: false,
});

export function parseLoggingSettings(raw: string | null): LoggingSettings {
  if (!raw) return { ...DEFAULT_LOGGING_SETTINGS };
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { ...DEFAULT_LOGGING_SETTINGS };
  }
  if (!isRecord(parsed) || !isRecord(parsed.logging)) {
    return { ...DEFAULT_LOGGING_SETTINGS };
  }
  return { debug: parsed.logging.debug === true };
}

export type LoggingToggleResult =
  { ok: true; json: string; settings: LoggingSettings } | { ok: false; message: string };

export function applyDebugLoggingToggle(
  existingRaw: string | null,
  enabled: boolean,
): LoggingToggleResult {
  const parsed = parseDesktopDocumentBase(existingRaw, 'changing the debug logging toggle');
  if (!parsed.ok) return { ok: false, message: parsed.message };

  const base = parsed.base;
  const existingLogging = isRecord(base.logging) ? base.logging : {};
  const next: Record<string, unknown> = {
    ...backfillBootFields(base),
    logging: { ...existingLogging, debug: enabled },
  };
  const json = `${JSON.stringify(next, null, 2)}\n`;
  return { ok: true, json, settings: parseLoggingSettings(json) };
}

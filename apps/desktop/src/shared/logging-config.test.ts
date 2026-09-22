import { describe, expect, it } from 'vitest';
import {
  applyDebugLoggingToggle,
  DEFAULT_LOGGING_SETTINGS,
  parseLoggingSettings,
} from './logging-config';

describe('parseLoggingSettings', () => {
  it('returns defaults for a missing document', () => {
    expect(parseLoggingSettings(null)).toEqual({ debug: false });
  });

  it('returns defaults for malformed JSON', () => {
    expect(parseLoggingSettings('{not json')).toEqual({ debug: false });
  });

  it('returns defaults when the logging block is absent', () => {
    const raw = JSON.stringify({ schemaVersion: 1, apiUrl: 'https://x.example' });
    expect(parseLoggingSettings(raw)).toEqual({ debug: false });
  });

  it('reads logging.debug === true', () => {
    const raw = JSON.stringify({ schemaVersion: 1, logging: { debug: true } });
    expect(parseLoggingSettings(raw)).toEqual({ debug: true });
  });

  it('treats anything but literal true as off', () => {
    for (const value of ['true', 1, {}, [], null]) {
      const raw = JSON.stringify({ logging: { debug: value } });
      expect(parseLoggingSettings(raw)).toEqual({ debug: false });
    }
  });

  it('exposes frozen defaults', () => {
    expect(DEFAULT_LOGGING_SETTINGS.debug).toBe(false);
    expect(Object.isFrozen(DEFAULT_LOGGING_SETTINGS)).toBe(true);
  });
});

describe('applyDebugLoggingToggle', () => {
  it('writes logging.debug into an empty document and backfills boot fields', () => {
    const result = applyDebugLoggingToggle(null, true);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    const parsed = JSON.parse(result.json) as Record<string, unknown>;
    expect(parsed.schemaVersion).toBe(1);
    expect(typeof parsed.apiUrl).toBe('string');
    expect(parsed.logging).toEqual({ debug: true });
    expect(result.settings).toEqual({ debug: true });
  });

  it('preserves every field it does not own', () => {
    const existing = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://goosar.example',
      wsUrl: 'wss://goosar.example/ws',
      proxy: { perimeter: true, host: '10.0.0.1' },
      customOperatorField: { keep: 'me' },
    });
    const result = applyDebugLoggingToggle(existing, true);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    const parsed = JSON.parse(result.json) as Record<string, unknown>;
    expect(parsed.apiUrl).toBe('https://goosar.example');
    expect(parsed.wsUrl).toBe('wss://goosar.example/ws');
    expect(parsed.proxy).toEqual({ perimeter: true, host: '10.0.0.1' });
    expect(parsed.customOperatorField).toEqual({ keep: 'me' });
    expect(parsed.logging).toEqual({ debug: true });
  });

  it('preserves unmodelled fields inside the logging block', () => {
    const existing = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://goosar.example',
      logging: { debug: true, futureField: 'keep' },
    });
    const result = applyDebugLoggingToggle(existing, false);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    const parsed = JSON.parse(result.json) as Record<string, unknown>;
    expect(parsed.logging).toEqual({ debug: false, futureField: 'keep' });
    expect(result.settings).toEqual({ debug: false });
  });

  it('refuses an unparsable document instead of replacing it', () => {
    const result = applyDebugLoggingToggle('{broken', true);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.message).toContain('desktop.json');
  });

  it('round-trips through parseLoggingSettings', () => {
    const on = applyDebugLoggingToggle(null, true);
    expect(on.ok).toBe(true);
    if (!on.ok) return;
    expect(parseLoggingSettings(on.json)).toEqual({ debug: true });

    const off = applyDebugLoggingToggle(on.json, false);
    expect(off.ok).toBe(true);
    if (!off.ok) return;
    expect(parseLoggingSettings(off.json)).toEqual({ debug: false });
  });
});

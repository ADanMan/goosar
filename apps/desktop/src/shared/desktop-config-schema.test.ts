import { describe, expect, it } from 'vitest';
import {
  CURRENT_DESKTOP_SCHEMA_VERSION,
  desktopSchemaVersionOf,
  futureDesktopSchemaRefusal,
  migrateDesktopConfigObject,
} from './desktop-config-schema';
import { applyRuntimeConfigPatch, parseRuntimeConfig } from './runtime-config';
import { parseDesktopDocumentBase } from './perimeter-config';

describe('desktop config schema (#294)', () => {
  it('treats a document without schemaVersion as version 0', () => {
    expect(desktopSchemaVersionOf({})).toBe(0);
    expect(desktopSchemaVersionOf({ apiUrl: 'https://x' })).toBe(0);
  });

  it('rejects a non-integer schemaVersion by name', () => {
    for (const bad of ['1', 1.5, -1, null]) {
      expect(() => desktopSchemaVersionOf({ schemaVersion: bad })).toThrow(/schemaVersion/);
    }
  });

  it('migrates every past version to the current shape', () => {
    const migrated = migrateDesktopConfigObject({ apiUrl: 'https://goosar.ru' });
    expect(migrated.schemaVersion).toBe(CURRENT_DESKTOP_SCHEMA_VERSION);
    expect(migrated.apiUrl).toBe('https://goosar.ru');
  });

  it('refuses a future version with a message naming both versions', () => {
    const future = { schemaVersion: CURRENT_DESKTOP_SCHEMA_VERSION + 1 };
    expect(() => migrateDesktopConfigObject(future)).toThrow(/newer version/);
    expect(futureDesktopSchemaRefusal(future)).toMatch(/newer version/);
    expect(futureDesktopSchemaRefusal({ schemaVersion: 1 })).toBeNull();
  });

  it('loads a v0 (pre-schema) desktop.json into the current runtime config', () => {
    const config = parseRuntimeConfig(JSON.stringify({ apiUrl: 'https://goosar.ru' }));
    expect(config.schemaVersion).toBe(1);
    expect(config.apiUrl).toBe('https://goosar.ru');
    expect(config.wsUrl).toBe('wss://goosar.ru/ws');
  });

  it('refuses to load a future-version desktop.json', () => {
    expect(() =>
      parseRuntimeConfig(JSON.stringify({ schemaVersion: 99, apiUrl: 'https://goosar.ru' })),
    ).toThrow(/newer version/);
  });

  it('refuses to patch endpoints over a future-version document', () => {
    const future = JSON.stringify({
      schemaVersion: 99,
      apiUrl: 'https://new.example',
      futureField: 'must survive',
    });
    expect(() => applyRuntimeConfigPatch(future, { apiUrl: 'https://other.example' })).toThrow(
      /newer version/,
    );
  });

  it('refuses perimeter/logging writes over a future-version document', () => {
    const result = parseDesktopDocumentBase(JSON.stringify({ schemaVersion: 99 }), 'testing');
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.message).toMatch(/newer version/);
  });

  it('still round-trips unknown fields on a same-version patch', () => {
    const existing = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://old.example',
      operatorField: { keep: true },
    });
    const doc = applyRuntimeConfigPatch(existing, {
      apiUrl: 'https://new.example',
    });
    expect(JSON.parse(doc.json).operatorField).toEqual({ keep: true });
    expect(JSON.parse(doc.json).schemaVersion).toBe(CURRENT_DESKTOP_SCHEMA_VERSION);
  });
});

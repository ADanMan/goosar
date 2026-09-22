// Версионированная схема ~/.goosar/desktop.json. Все читатели и писатели
// используют этот модуль: документ прошлой версии мигрируется по шагам,
// документ будущей версии отклоняется с понятным сообщением.

export const CURRENT_DESKTOP_SCHEMA_VERSION = 1;

export function desktopSchemaVersionOf(obj: Record<string, unknown>): number {
  const v = obj.schemaVersion;
  if (v === undefined) return 0;
  if (typeof v !== 'number' || !Number.isInteger(v) || v < 0) {
    throw new Error('Invalid desktop runtime config: schemaVersion must be a non-negative integer');
  }
  return v;
}

export function futureDesktopSchemaMessage(version: number): string {
  return (
    `desktop.json has schemaVersion ${version}, but this build supports up to ` +
    `${CURRENT_DESKTOP_SCHEMA_VERSION} — it was written by a newer version of the app. ` +
    `Update the app instead of editing the config with this build.`
  );
}

const MIGRATIONS: Array<(obj: Record<string, unknown>) => Record<string, unknown>> = [
  (obj) => ({ ...obj, schemaVersion: 1 }),
];

export function migrateDesktopConfigObject(obj: Record<string, unknown>): Record<string, unknown> {
  let version = desktopSchemaVersionOf(obj);
  if (version > CURRENT_DESKTOP_SCHEMA_VERSION) {
    throw new Error(futureDesktopSchemaMessage(version));
  }
  let out = obj;
  for (; version < CURRENT_DESKTOP_SCHEMA_VERSION; version++) {
    out = MIGRATIONS[version](out);
  }
  return out;
}

export function futureDesktopSchemaRefusal(obj: Record<string, unknown>): string | null {
  let version: number;
  try {
    version = desktopSchemaVersionOf(obj);
  } catch {
    return null; 
  }
  return version > CURRENT_DESKTOP_SCHEMA_VERSION ? futureDesktopSchemaMessage(version) : null;
}

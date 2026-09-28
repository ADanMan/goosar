// Human-readable file size for the /download "all platforms" list. Units are
// passed in (not hardcoded) so the label matches the active locale — see
// `download.allPlatforms.fileSizeUnits` in the i18n dictionaries.

const UNIT_KEYS = ['b', 'kb', 'mb', 'gb'] as const;

export type FileSizeUnitKey = (typeof UNIT_KEYS)[number];

export type FileSizeUnits = Record<FileSizeUnitKey, string>;

export function formatFileSize(bytes: number, units: FileSizeUnits): string | null {
  if (!Number.isFinite(bytes) || bytes <= 0) return null;

  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < UNIT_KEYS.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  const unitKey = UNIT_KEYS[unitIndex] ?? 'b';
  const formatted = unitIndex > 0 && value < 10 ? value.toFixed(1) : Math.round(value).toString();
  return `${formatted} ${units[unitKey]}`;
}

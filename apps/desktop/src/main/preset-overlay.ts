import { existsSync, readFileSync } from 'fs';
import { join } from 'path';

export const OVERLAY_FILENAME = 'preset-overlay.json';

export function parsePresetOverlay(raw: string): Record<string, unknown> | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return null;
  }
  return parsed as Record<string, unknown>;
}

export interface PresetOverlaySource {
  envPath?: string;
  stagedDir?: string | null;
}

export function loadPresetOverlay(source: PresetOverlaySource): Record<string, unknown> | null {
  const candidates: string[] = [];
  if (source.envPath?.trim()) candidates.push(source.envPath.trim());
  if (source.stagedDir) {
    candidates.push(join(source.stagedDir, OVERLAY_FILENAME));
  }
  for (const path of candidates) {
    if (!existsSync(path)) continue;
    try {
      const overlay = parsePresetOverlay(readFileSync(path, 'utf-8'));
      if (overlay) return overlay;
    } catch {
      // Unreadable — fall through to the next candidate.
    }
  }
  return null;
}

export function presetOverlayStagedDir(opts: {
  isPackaged: boolean;
  resourcesPath: string;
  appPath: string;
}): string | null {
  const dir = opts.isPackaged
    ? join(opts.resourcesPath, 'preset-overlay')
    : join(opts.appPath, 'resources-preset-overlay');
  return existsSync(dir) ? dir : null;
}

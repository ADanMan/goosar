import { existsSync, readFileSync } from 'fs';
import { join } from 'path';

export const DEPLOYMENT_DEFAULTS_FILENAME = 'deployment.json';

export function parseDeploymentDefaults(raw: string): Record<string, unknown> | null {
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

export interface DeploymentDefaultsSource {
  envPath?: string;
  stagedDir?: string | null;
}

export function loadDeploymentDefaults(
  source: DeploymentDefaultsSource,
): Record<string, unknown> | null {
  const candidates: string[] = [];
  if (source.envPath?.trim()) candidates.push(source.envPath.trim());
  if (source.stagedDir) {
    candidates.push(join(source.stagedDir, DEPLOYMENT_DEFAULTS_FILENAME));
  }
  for (const path of candidates) {
    if (!existsSync(path)) continue;
    try {
      const defaults = parseDeploymentDefaults(readFileSync(path, 'utf-8'));
      if (defaults) return defaults;
    } catch {
      // Unreadable — fall through to the next candidate.
    }
  }
  return null;
}

export function deploymentDefaultsStagedDir(opts: {
  isPackaged: boolean;
  resourcesPath: string;
  appPath: string;
}): string | null {
  const dir = opts.isPackaged
    ? join(opts.resourcesPath, 'deployment')
    : join(opts.appPath, 'resources-deployment');
  return existsSync(dir) ? dir : null;
}

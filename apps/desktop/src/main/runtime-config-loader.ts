import { app } from 'electron';
import { chmod, mkdir, readFile, rename, writeFile } from 'fs/promises';
import { dirname, join } from 'path';
import {
  applyRuntimeConfigPatch,
  DEFAULT_RUNTIME_CONFIG,
  mergeDeploymentDefaults,
  normalizeApiUrl,
  parseRuntimeConfig,
  parseRuntimeConfigPatch,
  runtimeConfigFromDevEnv,
  type RuntimeConfig,
  type RuntimeConfigEnv,
  type RuntimeConfigError,
  type RuntimeConfigResult,
  type RuntimeConfigSaveResult,
  type ServerProbeResult,
} from '../shared/runtime-config';
import {
  applyPerimeterExtras,
  type PerimeterExtras,
  type PerimeterExtrasPatch,
} from '../shared/perimeter-config';
import { applyDebugLoggingToggle, type LoggingSettings } from '../shared/logging-config';

export async function loadRuntimeConfig(options: {
  isDev: boolean;
  env: RuntimeConfigEnv;
  configPath?: string;
  deploymentDefaults?: Record<string, unknown> | null;
}): Promise<RuntimeConfigResult> {
  if (options.isDev) {
    try {
      return { ok: true, config: runtimeConfigFromDevEnv(options.env) };
    } catch (err) {
      return { ok: false, error: { message: errorMessage(err) } };
    }
  }

  const configPath = options.configPath ?? desktopConfigPath();
  const defaults = options.deploymentDefaults ?? null;
  try {
    const raw = await readFile(configPath, 'utf-8');
    return { ok: true, config: parseConfigWithDefaults(raw, defaults) };
  } catch (err) {
    if (isMissingFileError(err)) {
      if (defaults) {
        try {
          return { ok: true, config: parseRuntimeConfig(JSON.stringify(defaults)) };
        } catch {
          // fall through to the shipped default
        }
      }
      return { ok: true, config: { ...DEFAULT_RUNTIME_CONFIG } };
    }
    return {
      ok: false,
      error: {
        message: `Invalid ${configPath}: ${errorMessage(err)}`,
      },
    };
  }
}

function parseConfigWithDefaults(
  raw: string,
  defaults: Record<string, unknown> | null,
): RuntimeConfig {
  if (!defaults) return parseRuntimeConfig(raw);
  try {
    return parseRuntimeConfig(mergeDeploymentDefaults(defaults, raw));
  } catch {
    return parseRuntimeConfig(raw);
  }
}

export async function saveRuntimeConfig(options: {
  patch: unknown;
  configPath?: string;
}): Promise<RuntimeConfigSaveResult> {
  const configPath = options.configPath ?? desktopConfigPath();
  try {
    const patch = parseRuntimeConfigPatch(options.patch);
    const existingRaw = await readExistingConfig(configPath);
    const document = applyRuntimeConfigPatch(existingRaw, patch);
    await writeAtomically(configPath, document.json);
    return { ok: true, config: document.config };
  } catch (err) {
    return { ok: false, error: { message: errorMessage(err) } };
  }
}

export interface ProbeResponse {
  ok: boolean;
  status: number;
}

export type ProbeFetch = (
  url: string,
  init: { signal: AbortSignal; redirect: 'follow' },
) => Promise<ProbeResponse>;

const HEALTH_PATH = '/health';
const DEFAULT_PROBE_TIMEOUT_MS = 5_000;

export async function probeRuntimeServer(options: {
  apiUrl: string;
  fetchImpl?: ProbeFetch;
  timeoutMs?: number;
}): Promise<ServerProbeResult> {
  const raw = typeof options.apiUrl === 'string' ? options.apiUrl.trim() : '';
  let address: string;
  try {
    address = normalizeApiUrl(raw);
  } catch (err) {
    return { ok: false, address: raw, message: errorMessage(err) };
  }

  const timeoutMs = options.timeoutMs ?? DEFAULT_PROBE_TIMEOUT_MS;
  const doFetch: ProbeFetch = options.fetchImpl ?? ((url, init) => fetch(url, init));
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const res = await doFetch(`${address}${HEALTH_PATH}`, {
      signal: controller.signal,
      redirect: 'follow',
    });
    if (!res.ok) {
      return {
        ok: false,
        address,
        message: `server answered HTTP ${res.status}`,
      };
    }
    return { ok: true, address };
  } catch (err) {
    if (isAbortError(err)) {
      return { ok: false, address, message: `no response within ${timeoutMs}ms` };
    }
    return { ok: false, address, message: errorMessage(err) };
  } finally {
    clearTimeout(timer);
  }
}

export function desktopConfigPath(): string {
  return join(app.getPath('home'), '.goosar', 'desktop.json');
}

export type PerimeterExtrasSaveResult =
  { ok: true; extras: PerimeterExtras } | { ok: false; error: RuntimeConfigError };

export async function savePerimeterExtras(options: {
  patch: PerimeterExtrasPatch;
  configPath?: string;
}): Promise<PerimeterExtrasSaveResult> {
  const configPath = options.configPath ?? desktopConfigPath();
  try {
    const existingRaw = await readExistingConfig(configPath);
    const result = applyPerimeterExtras(existingRaw, options.patch);
    if (!result.ok) return { ok: false, error: { message: result.message } };
    await writeAtomically(configPath, result.json);
    return { ok: true, extras: result.extras };
  } catch (err) {
    return { ok: false, error: { message: errorMessage(err) } };
  }
}

export type LoggingToggleSaveResult =
  { ok: true; settings: LoggingSettings } | { ok: false; error: RuntimeConfigError };

export async function saveDebugLoggingToggle(options: {
  enabled: boolean;
  configPath?: string;
}): Promise<LoggingToggleSaveResult> {
  const configPath = options.configPath ?? desktopConfigPath();
  try {
    const existingRaw = await readExistingConfig(configPath);
    const result = applyDebugLoggingToggle(existingRaw, options.enabled);
    if (!result.ok) return { ok: false, error: { message: result.message } };
    await writeAtomically(configPath, result.json);
    return { ok: true, settings: result.settings };
  } catch (err) {
    return { ok: false, error: { message: errorMessage(err) } };
  }
}

async function readExistingConfig(configPath: string): Promise<string | null> {
  try {
    return await readFile(configPath, 'utf-8');
  } catch (err) {
    if (isMissingFileError(err)) return null;
    throw err;
  }
}

async function writeAtomically(configPath: string, contents: string): Promise<void> {
  await mkdir(dirname(configPath), { recursive: true });
  const tempPath = `${configPath}.${process.pid}.tmp`;
  await writeFile(tempPath, contents, { encoding: 'utf-8', mode: 0o600 });
  await chmod(tempPath, 0o600);
  await rename(tempPath, configPath);
}

function isAbortError(err: unknown): boolean {
  return Boolean(err && typeof err === 'object' && 'name' in err && err.name === 'AbortError');
}

function isMissingFileError(err: unknown): boolean {
  return Boolean(
    err &&
    typeof err === 'object' &&
    'code' in err &&
    (err as NodeJS.ErrnoException).code === 'ENOENT',
  );
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export type { RuntimeConfig, RuntimeConfigResult };

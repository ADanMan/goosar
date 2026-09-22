import {
  CURRENT_DESKTOP_SCHEMA_VERSION,
  futureDesktopSchemaRefusal,
  migrateDesktopConfigObject,
} from './desktop-config-schema';

export interface RuntimeConfig {
  schemaVersion: 1;
  apiUrl: string;
  wsUrl: string;
  appUrl: string;
}

export interface RuntimeConfigError {
  message: string;
}

export type RuntimeConfigResult =
  { ok: true; config: RuntimeConfig } | { ok: false; error: RuntimeConfigError };

export interface RuntimeConfigPatch {
  apiUrl: string;
  wsUrl?: string;
  appUrl?: string;
}

export interface RuntimeConfigDocument {
  json: string;
  config: RuntimeConfig;
}

export type RuntimeConfigSaveResult =
  { ok: true; config: RuntimeConfig } | { ok: false; error: RuntimeConfigError };

export type ServerProbeResult =
  { ok: true; address: string } | { ok: false; address: string; message: string };

export const DEFAULT_RUNTIME_CONFIG: RuntimeConfig = Object.freeze({
  schemaVersion: 1,
  apiUrl: 'https://goosar.ru',
  wsUrl: 'wss://goosar.ru/ws',
  appUrl: 'https://goosar.ru',
});

const LOCAL_DEV_RUNTIME_CONFIG: RuntimeConfig = Object.freeze({
  schemaVersion: 1,
  apiUrl: 'http://localhost:8080',
  wsUrl: 'ws://localhost:8080/ws',
  appUrl: 'http://localhost:3000',
});

export interface RuntimeConfigEnv {
  apiUrl?: string;
  wsUrl?: string;
  appUrl?: string;
}

export function runtimeConfigFromDevEnv(env: RuntimeConfigEnv): RuntimeConfig {
  const apiUrl = normalizeHttpUrl(env.apiUrl || LOCAL_DEV_RUNTIME_CONFIG.apiUrl, 'VITE_API_URL');
  return {
    schemaVersion: 1,
    apiUrl,
    wsUrl: env.wsUrl ? normalizeWsUrl(env.wsUrl, 'VITE_WS_URL') : deriveWsUrl(apiUrl),
    appUrl: env.appUrl ? normalizeHttpUrl(env.appUrl, 'VITE_APP_URL') : deriveDevAppUrl(apiUrl),
  };
}

export function parseRuntimeConfig(raw: string): RuntimeConfig {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(
      `Invalid desktop runtime config JSON: ${err instanceof Error ? err.message : 'parse failed'}`,
    );
  }

  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Invalid desktop runtime config: expected a JSON object');
  }

  const obj = migrateDesktopConfigObject(parsed as Record<string, unknown>);

  const apiUrl = requiredString(obj.apiUrl, 'apiUrl');
  const appUrl = optionalString(obj.appUrl, 'appUrl');
  const wsUrl = optionalString(obj.wsUrl, 'wsUrl');

  const normalizedApiUrl = normalizeHttpUrl(apiUrl, 'apiUrl', { secureByDefault: true });
  return {
    schemaVersion: 1,
    apiUrl: normalizedApiUrl,
    wsUrl: wsUrl ? normalizeWsUrl(wsUrl, 'wsUrl') : deriveWsUrl(normalizedApiUrl),
    appUrl: appUrl
      ? normalizeHttpUrl(appUrl, 'appUrl', { secureByDefault: true })
      : deriveAppUrl(normalizedApiUrl),
  };
}

export function parseRuntimeConfigPatch(input: unknown): RuntimeConfigPatch {
  if (!input || typeof input !== 'object' || Array.isArray(input)) {
    throw new Error('Invalid desktop runtime config patch: expected an object');
  }
  const obj = input as Record<string, unknown>;
  return {
    apiUrl: requiredString(obj.apiUrl, 'apiUrl'),
    wsUrl: optionalString(obj.wsUrl, 'wsUrl'),
    appUrl: optionalString(obj.appUrl, 'appUrl'),
  };
}

export function applyRuntimeConfigPatch(
  existingRaw: string | null,
  patch: RuntimeConfigPatch,
): RuntimeConfigDocument {
  assertWritableDesktopDocument(existingRaw);
  const preserved = unmodelledFields(existingRaw);
  const next: Record<string, unknown> = {
    schemaVersion: CURRENT_DESKTOP_SCHEMA_VERSION,
    apiUrl: patch.apiUrl,
    ...(patch.wsUrl ? { wsUrl: patch.wsUrl } : {}),
    ...(patch.appUrl ? { appUrl: patch.appUrl } : {}),
    ...preserved,
  };

  const json = `${JSON.stringify(next, null, 2)}\n`;
  return { json, config: parseRuntimeConfig(json) };
}

export function mergeDeploymentDefaults(
  defaults: Record<string, unknown> | null,
  userRaw: string,
): string {
  if (!defaults) return userRaw;
  let parsed: unknown;
  try {
    parsed = JSON.parse(userRaw);
  } catch {
    return userRaw;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return userRaw;
  }
  const user = parsed as Record<string, unknown>;
  const base = typeof user.apiUrl === 'string' ? withoutEndpoints(defaults) : defaults;
  return JSON.stringify(deepMergeObjects(base, user));
}

function withoutEndpoints(defaults: Record<string, unknown>): Record<string, unknown> {
  const { apiUrl: _apiUrl, wsUrl: _wsUrl, appUrl: _appUrl, ...rest } = defaults;
  return rest;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function deepMergeObjects(
  base: Record<string, unknown>,
  override: Record<string, unknown>,
): Record<string, unknown> {
  const merged: Record<string, unknown> = { ...base };
  for (const [key, value] of Object.entries(override)) {
    const existing = merged[key];
    merged[key] =
      isPlainObject(existing) && isPlainObject(value) ? deepMergeObjects(existing, value) : value;
  }
  return merged;
}

function assertWritableDesktopDocument(existingRaw: string | null): void {
  if (!existingRaw) return;
  let parsed: unknown;
  try {
    parsed = JSON.parse(existingRaw);
  } catch {
    return;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return;
  const refusal = futureDesktopSchemaRefusal(parsed as Record<string, unknown>);
  if (refusal) throw new Error(refusal);
}

function unmodelledFields(existingRaw: string | null): Record<string, unknown> {
  if (!existingRaw) return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(existingRaw);
  } catch {
    return {};
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
  const {
    schemaVersion: _schemaVersion,
    apiUrl: _apiUrl,
    wsUrl: _wsUrl,
    appUrl: _appUrl,
    ...rest
  } = parsed as Record<string, unknown>;
  return rest;
}

export interface ReplacedEndpointPin {
  field: 'wsUrl' | 'appUrl';
  previous: string;
  next: string;
}

export function endpointPinsReplacedBy(
  current: RuntimeConfig,
  nextApiUrl: string,
): ReplacedEndpointPin[] {
  const replaced: ReplacedEndpointPin[] = [];

  const nextWsUrl = deriveWsUrl(nextApiUrl);
  if (current.wsUrl !== nextWsUrl && current.wsUrl !== deriveWsUrl(current.apiUrl)) {
    replaced.push({ field: 'wsUrl', previous: current.wsUrl, next: nextWsUrl });
  }

  const nextAppUrl = deriveAppUrl(nextApiUrl);
  if (
    current.appUrl !== nextAppUrl &&
    current.appUrl !== deriveAppUrl(current.apiUrl) &&
    current.appUrl !== deriveDevAppUrl(current.apiUrl)
  ) {
    replaced.push({ field: 'appUrl', previous: current.appUrl, next: nextAppUrl });
  }

  return replaced;
}

export function normalizeApiUrl(value: string): string {
  return normalizeHttpUrl(value, 'apiUrl', { secureByDefault: true });
}

export function deriveWsUrl(apiUrl: string): string {
  const url = new URL(apiUrl);
  if (url.protocol === 'https:') url.protocol = 'wss:';
  else if (url.protocol === 'http:') url.protocol = 'ws:';
  else throw new Error('apiUrl must use http or https');
  url.pathname = joinPath(url.pathname, '/ws');
  url.search = '';
  url.hash = '';
  return trimTrailingSlash(url.toString());
}

export function deriveAppUrl(apiUrl: string): string {
  const url = new URL(apiUrl);
  url.pathname = '';
  url.search = '';
  url.hash = '';
  if (url.hostname.startsWith('api.') && url.hostname.split('.').length >= 3) {
    url.hostname = url.hostname.slice('api.'.length);
  }
  return trimTrailingSlash(url.toString());
}

export function deriveDevAppUrl(apiUrl: string): string {
  const url = new URL(apiUrl);
  if (url.hostname === 'localhost' || url.hostname === '127.0.0.1') {
    return LOCAL_DEV_RUNTIME_CONFIG.appUrl;
  }
  return deriveAppUrl(apiUrl);
}

function requiredString(value: unknown, field: string): string {
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`Invalid desktop runtime config: ${field} must be a non-empty string`);
  }
  return value;
}

function optionalString(value: unknown, field: string): string | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== 'string' || value.trim().length === 0) {
    throw new Error(`Invalid desktop runtime config: ${field} must be a non-empty string when set`);
  }
  return value;
}

function hasUrlScheme(value: string): boolean {
  const match = /^[a-zA-Z][a-zA-Z\d+.-]*:(.*)$/.exec(value);
  if (!match) return false;
  const rest = match[1] ?? '';
  return !/^\d+([/?#].*)?$/.test(rest);
}

function isLoopbackOrPrivateHost(hostname: string): boolean {
  const host = hostname.toLowerCase();
  if (host === 'localhost' || host === '[::1]') return true;
  if (/^127\./.test(host)) return true; 
  if (/^10\./.test(host)) return true; 
  if (/^192\.168\./.test(host)) return true; 
  if (/^172\.(1[6-9]|2\d|3[01])\./.test(host)) return true; 
  return false;
}

interface NormalizeHttpUrlOptions {
  secureByDefault?: boolean;
}

function normalizeHttpUrl(
  value: string,
  field: string,
  options: NormalizeHttpUrlOptions = {},
): string {
  const trimmed = value.trim();
  const candidate =
    options.secureByDefault && !hasUrlScheme(trimmed) ? `https://${trimmed}` : trimmed;

  let url: URL;
  try {
    url = new URL(candidate);
  } catch {
    throw new Error(`Invalid desktop runtime config: ${field} must be a valid URL`);
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error(`Invalid desktop runtime config: ${field} must use http or https`);
  }
  if (
    options.secureByDefault &&
    url.protocol === 'http:' &&
    !isLoopbackOrPrivateHost(url.hostname)
  ) {
    url.protocol = 'https:';
  }
  url.search = '';
  url.hash = '';
  return trimTrailingSlash(url.toString());
}

function normalizeWsUrl(value: string, field: string): string {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    throw new Error(`Invalid desktop runtime config: ${field} must be a valid URL`);
  }
  if (url.protocol !== 'ws:' && url.protocol !== 'wss:') {
    throw new Error(`Invalid desktop runtime config: ${field} must use ws or wss`);
  }
  url.search = '';
  url.hash = '';
  return trimTrailingSlash(url.toString());
}

function joinPath(base: string, suffix: string): string {
  const normalizedBase = base.endsWith('/') ? base.slice(0, -1) : base;
  return `${normalizedBase}${suffix}`;
}

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, '');
}

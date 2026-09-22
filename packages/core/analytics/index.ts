// Клей фронтенд-аналитики. Тонкая обёртка над posthog-js.

import posthog from 'posthog-js';
import { redactExceptionProperties } from './redact-exception';
import { shouldDropException } from './exception-dedupe';
import { isBenignException } from './benign-exceptions';

export const EVENT_SCHEMA_VERSION = 2;

const SIGNUP_SOURCE_COOKIE = 'goosar_signup_source';
const SIGNUP_SOURCE_VALUE_MAX_LEN = 96;
const SIGNUP_SOURCE_MAX_LEN = 512;
const UTM_KEYS = ['utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term'] as const;

let initialized = false;
let pendingIdentify: { userId: string; props?: Record<string, unknown> } | null = null;
let currentUserId: string | null = null;
let analyticsEnvironment = 'dev';
type PendingOp =
  | {
      kind: 'event';
      name: string;
      props?: Record<string, unknown>;
      options?: CaptureEventOptions;
    }
  | { kind: 'set'; props: Record<string, unknown> }
  | { kind: 'exception'; error: unknown; props?: Record<string, unknown> };
const pendingOps: PendingOp[] = [];
let superProperties: Record<string, unknown> = {};

export interface AnalyticsConfig {
  key: string;
  host: string;
  appVersion?: string;
  environment?: string;
}

export type ClientType = 'desktop' | 'web';

export function detectClientType(): ClientType {
  if (typeof window === 'undefined') return 'web';
  const w = window as unknown as { electron?: unknown; desktopAPI?: unknown };
  if (w.electron || w.desktopAPI) return 'desktop';
  if (typeof navigator !== 'undefined' && /Electron/i.test(navigator.userAgent)) {
    return 'desktop';
  }
  return 'web';
}

export function initAnalytics(config: AnalyticsConfig | null | undefined): boolean {
  if (typeof window === 'undefined') return false;
  if (!config?.key) return false;
  if (initialized) return true;

  posthog.init(config.key, {
    api_host: config.host || 'https://us.i.posthog.com',
    person_profiles: 'identified_only',
    capture_pageview: false,
    autocapture: false,
    capture_heatmaps: false,
    capture_dead_clicks: false,
    capture_exceptions: true,
    before_send: (event) => {
      if (event && event.event === '$exception') {
        if (isBenignException(event.properties)) return null;
        redactExceptionProperties(event.properties);
        if (shouldDropException(event.properties)) return null;
      }
      return event;
    },
    disable_session_recording: true,
    disable_surveys: true,
  });
  analyticsEnvironment = normalizeEnvironment(config.environment);
  superProperties = {
    client_type: detectClientType(),
    event_schema_version: EVENT_SCHEMA_VERSION,
    environment: analyticsEnvironment,
    is_demo: false,
  };
  if (config.appVersion) superProperties.app_version = config.appVersion;
  posthog.register(superProperties);
  initialized = true;

  if (pendingIdentify) {
    currentUserId = pendingIdentify.userId;
    posthog.identify(pendingIdentify.userId, pendingIdentify.props);
    pendingIdentify = null;
  }
  while (pendingOps.length > 0) {
    const op = pendingOps.shift()!;
    if (op.kind === 'event') {
      captureNow(op.name, op.props, op.options);
    } else if (op.kind === 'exception') {
      posthog.captureException(op.error, withClientEventProperties(op.props));
    } else {
      capturePersonSet(op.props);
    }
  }
  return true;
}

export function identify(userId: string, userProperties?: Record<string, unknown>): void {
  currentUserId = userId;
  if (!initialized) {
    pendingIdentify = { userId, props: userProperties };
    return;
  }
  posthog.identify(userId, userProperties);
}

export function resetAnalytics(): void {
  currentUserId = null;
  pendingIdentify = null;
  pendingOps.length = 0;
  if (!initialized) return;
  posthog.reset();
  if (Object.keys(superProperties).length > 0) {
    posthog.register(superProperties);
  }
}

export interface CaptureEventOptions {
  sendInstantly?: boolean;
  onCaptured?: () => void;
}

export function captureEvent(
  name: string,
  props?: Record<string, unknown>,
  options?: CaptureEventOptions,
): void {
  if (!initialized) {
    pendingOps.push({ kind: 'event', name, props, options });
    return;
  }
  captureNow(name, props, options);
}

function captureNow(
  name: string,
  props?: Record<string, unknown>,
  options?: CaptureEventOptions,
): void {
  posthog.capture(
    name,
    withClientEventProperties(props),
    options?.sendInstantly ? { send_instantly: true } : undefined,
  );
  options?.onCaptured?.();
}

export function captureException(error: unknown, props?: Record<string, unknown>): void {
  if (!initialized) {
    pendingOps.push({ kind: 'exception', error, props });
    return;
  }
  posthog.captureException(error, withClientEventProperties(props));
}

export function setPersonProperties(props: Record<string, unknown>): void {
  if (!initialized) {
    pendingOps.push({ kind: 'set', props });
    return;
  }
  capturePersonSet(props);
}

function capturePersonSet(props: Record<string, unknown>): void {
  posthog.capture('$set', { $set: props });
}

function withClientEventProperties(props?: Record<string, unknown>): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(props ?? {}) };
  if (currentUserId && next.user_id === undefined) {
    next.user_id = currentUserId;
  }
  if (next.event_schema_version === undefined) {
    next.event_schema_version = EVENT_SCHEMA_VERSION;
  }
  if (next.environment === undefined) {
    next.environment = analyticsEnvironment;
  }
  if (next.is_demo === undefined) {
    next.is_demo = false;
  }
  return next;
}

function normalizeEnvironment(value: string | undefined): string {
  switch ((value || '').trim().toLowerCase()) {
    case 'production':
    case 'prod':
      return 'production';
    case 'staging':
    case 'stage':
      return 'staging';
    case 'development':
    case 'dev':
    case 'test':
    case 'local':
      return 'dev';
    default:
      return 'dev';
  }
}

export function captureSignupSource(): void {
  if (typeof window === 'undefined' || typeof document === 'undefined') return;
  if (readCookie(SIGNUP_SOURCE_COOKIE)) return;

  const source: Record<string, string> = {};
  const cap = (v: string) =>
    v.length > SIGNUP_SOURCE_VALUE_MAX_LEN ? v.slice(0, SIGNUP_SOURCE_VALUE_MAX_LEN) : v;

  try {
    const params = new URLSearchParams(window.location.search);
    for (const key of UTM_KEYS) {
      const v = params.get(key);
      if (v) source[key] = cap(v);
    }
  } catch {
    // URL APIs unavailable — skip silently.
  }

  const refOrigin = safeReferrerOrigin(document.referrer);
  if (refOrigin) source.referrer_origin = cap(refOrigin);

  if (Object.keys(source).length === 0) return;

  const payload = JSON.stringify(source);
  if (payload.length > SIGNUP_SOURCE_MAX_LEN) return;

  const maxAge = 60 * 60 * 24 * 30;
  document.cookie = `${SIGNUP_SOURCE_COOKIE}=${encodeURIComponent(payload)}; path=/; max-age=${maxAge}; samesite=lax`;
}

function safeReferrerOrigin(referrer: string): string {
  if (!referrer) return '';
  try {
    const url = new URL(referrer);
    if (url.origin === window.location.origin) return '';
    return url.origin;
  } catch {
    return '';
  }
}

function readCookie(name: string): string {
  if (typeof document === 'undefined') return '';
  const prefix = `${name}=`;
  const parts = document.cookie ? document.cookie.split('; ') : [];
  for (const part of parts) {
    if (part.startsWith(prefix)) return decodeURIComponent(part.slice(prefix.length));
  }
  return '';
}

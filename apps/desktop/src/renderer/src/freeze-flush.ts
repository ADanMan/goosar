import type { CaptureEventOptions } from '@goosar/core/analytics';
import type { FreezeBreadcrumb } from '../../shared/freeze-breadcrumb';
import { sanitizeHangStackFrames } from '../../shared/hang-stack';

export const FREEZE_ACK_GRACE_MS = 10_000;

type CaptureFn = (
  name: string,
  props: Record<string, unknown>,
  options?: CaptureEventOptions,
) => void;

export interface FlushFreezeBreadcrumbDeps {
  getLastFreeze: () => FreezeBreadcrumb | null;
  ackFreeze: (ts: number) => void;
  capture: CaptureFn;
  graceMs?: number;
}

export function flushFreezeBreadcrumb({
  getLastFreeze,
  ackFreeze,
  capture,
  graceMs = FREEZE_ACK_GRACE_MS,
}: FlushFreezeBreadcrumbDeps): () => void {
  const last = getLastFreeze();
  if (!last) return () => undefined;

  let ackTimer: ReturnType<typeof setTimeout> | null = null;
  const crashed = last.kind === 'render-process-gone';

  capture(crashed ? 'client_crash' : 'client_unresponsive', buildFreezeEventProps(last), {
    sendInstantly: true,
    onCaptured: () => {
      ackTimer = setTimeout(() => ackFreeze(last.ts), graceMs);
    },
  });

  return () => {
    if (ackTimer) clearTimeout(ackTimer);
  };
}

export function buildFreezeEventProps(breadcrumb: FreezeBreadcrumb): Record<string, unknown> {
  const crashed = breadcrumb.kind === 'render-process-gone';
  const context = (breadcrumb.context ?? {}) as Record<string, unknown>;

  return {
    source: crashed ? 'render-process-gone' : 'main-unresponsive',
    recovered: false,
    breadcrumb_ts: breadcrumb.ts,
    crashed_version: breadcrumb.version,
    ...routeProps(context.desktopRoute),
    ...stackProps(context.stack),
    ...crashProps(context.details),
  };
}

function routeProps(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object') return {};
  const route = value as Record<string, unknown>;
  return {
    ...(typeof route.path === 'string' ? { path: route.path } : {}),
    ...(typeof route.surface === 'string' ? { surface: route.surface } : {}),
  };
}

function stackProps(value: unknown): Record<string, unknown> {
  const frames = sanitizeHangStackFrames(value);
  if (!frames) return {};
  const top = frames[0]!;
  return {
    stack: frames,
    stack_depth: frames.length,
    stack_function: top.functionName,
    ...(top.url ? { stack_url: top.url } : {}),
    stack_line: top.lineNumber,
  };
}

function crashProps(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object') return {};
  const details = value as Record<string, unknown>;
  return {
    ...(typeof details.reason === 'string' ? { crash_reason: details.reason } : {}),
    ...(typeof details.exitCode === 'number' ? { crash_exit_code: details.exitCode } : {}),
  };
}

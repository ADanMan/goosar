// Сторож зависаний клиента — общий для web и desktop.

import { captureEvent } from '../analytics';
import { getDiagnosticRoute } from './diagnostic-context';

const FREEZE_THRESHOLD_MS = 2000;

const COOLDOWN_MS = 60_000;
let lastEmitMs = 0;

let installed = false;

export function installFreezeWatchdog(): void {
  if (installed) return;
  if (typeof window === 'undefined') return;
  if (typeof PerformanceObserver === 'undefined') return;
  installed = true;

  try {
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        if (entry.duration < FREEZE_THRESHOLD_MS) continue;
        const now = Date.now();
        if (now - lastEmitMs < COOLDOWN_MS) continue;
        lastEmitMs = now;
        captureEvent('client_unresponsive', {
          source: 'longtask',
          duration_ms: Math.round(entry.duration),
          path: resolveDiagnosticPath(),
        });
      }
    });
    observer.observe({ type: 'longtask' });
  } catch {
    // longtask entry type unsupported on this engine — nothing else to do.
  }
}

function resolveDiagnosticPath(): string | undefined {
  const route = getDiagnosticRoute();
  if (route) return route;
  return typeof location !== 'undefined' ? location.pathname : undefined;
}

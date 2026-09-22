export type ShortcutPlatform = 'macos' | 'windows' | 'linux' | 'unknown';

export type ShortcutRuntime = 'web' | 'desktop';

let configuredPlatform: ShortcutPlatform | null = null;
let configuredRuntime: ShortcutRuntime | null = null;

function normalizePlatform(value: string): ShortcutPlatform {
  const platform = value.toLowerCase();
  if (platform.includes('mac') || platform.includes('darwin')) return 'macos';
  if (platform.includes('win')) return 'windows';
  if (platform.includes('linux') || platform.includes('x11')) return 'linux';
  return 'unknown';
}

export function detectShortcutPlatform(): ShortcutPlatform {
  if (typeof navigator === 'undefined') return 'unknown';
  const nav = navigator as Navigator & {
    userAgentData?: { platform?: string };
  };
  const signals = [nav.userAgentData?.platform, nav.platform, nav.userAgent];
  for (const signal of signals) {
    if (!signal?.trim()) continue;
    const platform = normalizePlatform(signal);
    if (platform !== 'unknown') return platform;
  }
  return 'unknown';
}

export function configureShortcutPlatform(platform: ShortcutPlatform | null | undefined): void {
  configuredPlatform = platform ?? null;
}

export function getShortcutPlatform(): ShortcutPlatform {
  return configuredPlatform ?? detectShortcutPlatform();
}

export function detectShortcutRuntime(): ShortcutRuntime {
  if (typeof window === 'undefined') return 'web';
  const w = window as unknown as { electron?: unknown; desktopAPI?: unknown };
  if (w.electron || w.desktopAPI) return 'desktop';
  if (typeof navigator !== 'undefined' && /Electron/i.test(navigator.userAgent)) {
    return 'desktop';
  }
  return 'web';
}

export function configureShortcutRuntime(runtime: ShortcutRuntime | null | undefined): void {
  configuredRuntime = runtime ?? null;
}

export function getShortcutRuntime(): ShortcutRuntime {
  return configuredRuntime ?? detectShortcutRuntime();
}

// Общий словарь деинсталлятора и его UI.

export type UninstallItemKind = 'software' | 'user_data';

export interface UninstallItem {
  id: string;
  path: string;
  label: string;
  kind: UninstallItemKind;
  bytes: number | null;
}

export interface KeptItem {
  path: string;
  reason: string;
}

export interface ManualStep {
  path: string;
  instruction: string;
}

export interface UninstallPlan {
  software: UninstallItem[];
  userData: UninstallItem[];
  kept: KeptItem[];
  manualSteps: ManualStep[];
}

export interface UninstallFailure {
  item: UninstallItem;
  message: string;
}

export type UninstallStatus = 'removed' | 'partial' | 'blocked' | 'nothing_to_remove';

export interface DaemonStopReport {
  stopped: boolean;
  detail?: string;
}

export interface UninstallOutcome {
  status: UninstallStatus;
  daemon: DaemonStopReport;
  removed: UninstallItem[];
  failed: UninstallFailure[];
  kept: KeptItem[];
  manualSteps: ManualStep[];
}

export interface UninstallOptions {
  includeUserData: boolean;
}

export function parseUninstallOptions(value: unknown): UninstallOptions {
  if (typeof value !== 'object' || value === null) {
    return { includeUserData: false };
  }
  const includeUserData = (value as { includeUserData?: unknown }).includeUserData;
  return { includeUserData: includeUserData === true };
}

const BYTES_PER_UNIT = 1024;
const SIZE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const;

export function formatBytes(bytes: number | null): string {
  if (bytes === null || !Number.isFinite(bytes) || bytes < 0) {
    return 'unknown size';
  }
  let value = bytes;
  let unit = 0;
  while (value >= BYTES_PER_UNIT && unit < SIZE_UNITS.length - 1) {
    value /= BYTES_PER_UNIT;
    unit += 1;
  }
  const rounded = unit === 0 ? String(Math.round(value)) : value.toFixed(1);
  return `${rounded} ${SIZE_UNITS[unit]}`;
}

export function totalBytes(items: readonly UninstallItem[]): number | null {
  let total = 0;
  for (const item of items) {
    if (item.bytes === null) return null;
    total += item.bytes;
  }
  return total;
}

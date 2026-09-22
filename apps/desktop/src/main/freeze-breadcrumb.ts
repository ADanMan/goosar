import { writeFileSync, readFileSync, rmSync } from 'node:fs';
import type { FreezeBreadcrumb } from '../shared/freeze-breadcrumb';

export type { FreezeBreadcrumb };

export const FREEZE_BREADCRUMB_TTL_MS = 7 * 24 * 60 * 60 * 1000;

export function writeFreezeBreadcrumb(filePath: string, breadcrumb: FreezeBreadcrumb): void {
  try {
    writeFileSync(filePath, JSON.stringify(breadcrumb), 'utf8');
  } catch {
    // Disk full / permissions — drop silently.
  }
}

export function clearFreezeBreadcrumb(filePath: string, ownerId?: string): void {
  try {
    if (ownerId !== undefined) {
      const parsed = JSON.parse(readFileSync(filePath, 'utf8')) as unknown;
      if (
        !parsed ||
        typeof parsed !== 'object' ||
        (parsed as FreezeBreadcrumb).ownerId !== ownerId
      ) {
        return;
      }
    }
    rmSync(filePath, { force: true });
  } catch {
    // Nothing to clear / permissions — ignore.
  }
}

export function readFreezeBreadcrumb(filePath: string, now = Date.now()): FreezeBreadcrumb | null {
  let raw: string;
  try {
    raw = readFileSync(filePath, 'utf8');
  } catch {
    return null;
  }

  let breadcrumb: FreezeBreadcrumb | null = null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (
      parsed &&
      typeof parsed === 'object' &&
      typeof (parsed as FreezeBreadcrumb).kind === 'string'
    ) {
      breadcrumb = parsed as FreezeBreadcrumb;
    }
  } catch {
    // Corrupt JSON — fall through to the discard below.
  }

  if (!breadcrumb) {
    discard(filePath);
    return null;
  }
  if (isExpired(breadcrumb, now)) {
    discard(filePath);
    return null;
  }
  return breadcrumb;
}

export function ackFreezeBreadcrumb(filePath: string, ts: number): void {
  try {
    const parsed = JSON.parse(readFileSync(filePath, 'utf8')) as unknown;
    if (!parsed || typeof parsed !== 'object' || (parsed as FreezeBreadcrumb).ts !== ts) {
      return;
    }
    rmSync(filePath, { force: true });
  } catch {
    // Already gone / unreadable — nothing to acknowledge.
  }
}

function isExpired(breadcrumb: FreezeBreadcrumb, now: number): boolean {
  if (typeof breadcrumb.ts !== 'number' || !Number.isFinite(breadcrumb.ts)) {
    return true;
  }
  return now - breadcrumb.ts > FREEZE_BREADCRUMB_TTL_MS;
}

function discard(filePath: string): void {
  try {
    rmSync(filePath, { force: true });
  } catch {
    // If we can't delete it we'd re-read it next launch; acceptable over throwing.
  }
}

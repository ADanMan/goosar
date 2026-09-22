import type { StorageAdapter } from '../types/storage';
import { defaultStorage } from '../platform/storage';

const COMPLETION_MARK_PREFIX = 'goosar_onboarding_completion_pending';

const MARK_VALUE = '1';

function markKey(userId: string): string {
  return `${COMPLETION_MARK_PREFIX}:${userId}`;
}

function tryStorage<T>(op: () => T, fallback: T): T {
  try {
    return op();
  } catch {
    return fallback;
  }
}

export function hasPendingOnboardingCompletion(
  userId: string | null | undefined,
  storage: StorageAdapter = defaultStorage,
): boolean {
  if (!userId) return false;
  return tryStorage(() => storage.getItem(markKey(userId)) === MARK_VALUE, false);
}

export function markOnboardingCompletionPending(
  userId: string | null | undefined,
  storage: StorageAdapter = defaultStorage,
): void {
  if (!userId) return;
  tryStorage(() => storage.setItem(markKey(userId), MARK_VALUE), undefined);
}

export function clearOnboardingCompletionMark(
  userId: string | null | undefined,
  storage: StorageAdapter = defaultStorage,
): void {
  if (!userId) return;
  tryStorage(() => storage.removeItem(markKey(userId)), undefined);
}

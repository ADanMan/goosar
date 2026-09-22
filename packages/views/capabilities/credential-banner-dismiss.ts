'use client';

import { useCallback, useSyncExternalStore } from 'react';

const STORAGE_PREFIX = 'goosar.capability_credentials.dismiss.';

export const MAX_CREDENTIAL_BANNER_APPEARANCES = 3;

export interface CredentialBannerDismissState {
  count: number;
  signature: string;
}

const EMPTY_STATE: CredentialBannerDismissState = { count: 0, signature: '' };

export function shouldShowCredentialBanner(
  state: CredentialBannerDismissState,
  signature: string,
): boolean {
  if (signature === '') return false;
  if (state.signature !== signature) return true;
  return state.count < MAX_CREDENTIAL_BANNER_APPEARANCES;
}

export function nextAppearanceState(
  state: CredentialBannerDismissState,
  signature: string,
): CredentialBannerDismissState {
  if (state.signature !== signature) return { count: 1, signature };
  return { count: state.count + 1, signature };
}

export function nextDismissState(
  _state: CredentialBannerDismissState,
  signature: string,
): CredentialBannerDismissState {
  return { count: MAX_CREDENTIAL_BANNER_APPEARANCES, signature };
}

function storageKey(userId: string, workspaceId: string): string {
  return `${STORAGE_PREFIX}${userId}.${workspaceId}`;
}

function readState(
  userId: string | null | undefined,
  workspaceId: string | null | undefined,
): CredentialBannerDismissState {
  if (!userId || !workspaceId) return EMPTY_STATE;
  if (typeof window === 'undefined') return EMPTY_STATE;
  try {
    const raw = window.localStorage.getItem(storageKey(userId, workspaceId));
    if (!raw) return EMPTY_STATE;
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return EMPTY_STATE;
    const record = parsed as Record<string, unknown>;
    const count =
      typeof record.count === 'number' && Number.isFinite(record.count)
        ? Math.max(0, Math.trunc(record.count))
        : 0;
    const signature = typeof record.signature === 'string' ? record.signature : '';
    return { count, signature };
  } catch {
    return EMPTY_STATE;
  }
}

const listeners = new Set<() => void>();

function notify(): void {
  for (const fn of listeners) fn();
}

function subscribeToStorage(callback: () => void): () => void {
  if (typeof window === 'undefined') return () => {};
  const handler = (event: StorageEvent) => {
    if (event.key && event.key.startsWith(STORAGE_PREFIX)) callback();
  };
  window.addEventListener('storage', handler);
  return () => window.removeEventListener('storage', handler);
}

let cachedIdentity: string | null = null;
let cachedVersion = -1;
let cachedState: CredentialBannerDismissState = EMPTY_STATE;
let version = 0;

function snapshot(
  userId: string | null | undefined,
  workspaceId: string | null | undefined,
): CredentialBannerDismissState {
  const identity = `${userId ?? ''}.${workspaceId ?? ''}`;
  if (cachedIdentity !== identity || cachedVersion !== version) {
    cachedIdentity = identity;
    cachedVersion = version;
    cachedState = readState(userId, workspaceId);
  }
  return cachedState;
}

function writeState(userId: string, workspaceId: string, next: CredentialBannerDismissState): void {
  try {
    window.localStorage.setItem(storageKey(userId, workspaceId), JSON.stringify(next));
  } catch {
    // Private-mode Safari and friends: treat as a no-op. Worst case the
    // banner reappears next session without backing off.
  }
  version += 1;
  notify();
}

export interface CredentialBannerReminder {
  state: CredentialBannerDismissState;
  recordAppearance: (signature: string) => void;
  dismiss: (signature: string) => void;
}

export function useCredentialBannerDismiss(
  userId: string | null | undefined,
  workspaceId: string | null | undefined,
): CredentialBannerReminder {
  const state = useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      const off = subscribeToStorage(cb);
      return () => {
        listeners.delete(cb);
        off();
      };
    },
    () => snapshot(userId, workspaceId),
    () => EMPTY_STATE,
  );

  const recordAppearance = useCallback(
    (signature: string) => {
      if (!userId || !workspaceId) return;
      if (typeof window === 'undefined') return;
      writeState(
        userId,
        workspaceId,
        nextAppearanceState(readState(userId, workspaceId), signature),
      );
    },
    [userId, workspaceId],
  );

  const dismiss = useCallback(
    (signature: string) => {
      if (!userId || !workspaceId) return;
      if (typeof window === 'undefined') return;
      writeState(userId, workspaceId, nextDismissState(readState(userId, workspaceId), signature));
    },
    [userId, workspaceId],
  );

  return { state, recordAppearance, dismiss };
}

export function _resetCredentialBannerDismissForTests(): void {
  if (typeof window === 'undefined') return;
  for (let i = window.localStorage.length - 1; i >= 0; i--) {
    const key = window.localStorage.key(i);
    if (key && key.startsWith(STORAGE_PREFIX)) {
      window.localStorage.removeItem(key);
    }
  }
  version += 1;
  notify();
}

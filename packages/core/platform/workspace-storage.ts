import type { StateStorage } from 'zustand/middleware';
import type { StorageAdapter } from '../types/storage';

let _currentSlug: string | null = null;
let _currentWsId: string | null = null;

const _rehydrateFns: Array<() => void> = [];
const _slugSubscribers = new Set<(slug: string | null) => void>();
let _pendingNotify = false;
let _pendingRehydrate = false;

export function setCurrentWorkspace(slug: string | null, wsId: string | null) {
  if (_currentSlug === slug) {
    _currentWsId = wsId;
    return;
  }
  _currentSlug = slug;
  _currentWsId = wsId;

  if (!_pendingNotify) {
    _pendingNotify = true;
    queueMicrotask(() => {
      _pendingNotify = false;
      const current = _currentSlug;
      for (const fn of _slugSubscribers) {
        fn(current);
      }
    });
  }

  if (!_pendingRehydrate) {
    _pendingRehydrate = true;
    queueMicrotask(() => {
      _pendingRehydrate = false;
      for (const fn of _rehydrateFns) {
        fn();
      }
    });
  }
}

export function getCurrentSlug(): string | null {
  return _currentSlug;
}

export function getCurrentWsId(): string | null {
  return _currentWsId;
}

export function subscribeToCurrentSlug(fn: (slug: string | null) => void): () => void {
  _slugSubscribers.add(fn);
  return () => {
    _slugSubscribers.delete(fn);
  };
}

export function registerForWorkspaceRehydration(fn: () => void) {
  _rehydrateFns.push(fn);
}

export function createWorkspaceAwareStorage(adapter: StorageAdapter): StateStorage {
  return {
    getItem: (key) => (_currentSlug ? adapter.getItem(`${key}:${_currentSlug}`) : null),
    setItem: (key, value) => {
      if (_currentSlug) adapter.setItem(`${key}:${_currentSlug}`, value);
    },
    removeItem: (key) => {
      if (_currentSlug) adapter.removeItem(`${key}:${_currentSlug}`);
    },
  };
}

import type { StateStorage } from 'zustand/middleware';
import type { StorageAdapter } from '../types/storage';

export function createPersistStorage(adapter: StorageAdapter): StateStorage {
  return {
    getItem: (key) => adapter.getItem(key),
    setItem: (key, value) => adapter.setItem(key, value),
    removeItem: (key) => adapter.removeItem(key),
  };
}

import type { StorageAdapter } from '../types/storage';
import { abortAll as abortAllUploads } from './upload-coordinator';

export interface DraftCleanupEntry {
  storageKey: string;
  workspaceScoped: boolean;
  resetInMemory: () => void;
}

const entries = new Map<string, DraftCleanupEntry>();

export function registerDraftCleanup(entry: DraftCleanupEntry): void {
  entries.set(entry.storageKey, entry);
}

export function clearRegisteredWorkspaceDrafts(adapter: StorageAdapter, slug: string): void {
  for (const entry of entries.values()) {
    if (entry.workspaceScoped) {
      adapter.removeItem(`${entry.storageKey}:${slug}`);
    } else {
      adapter.removeItem(entry.storageKey);
    }
  }
}

export function resetAllRegisteredDrafts(): void {
  abortAllUploads();
  for (const entry of entries.values()) {
    entry.resetInMemory();
  }
}

export function __clearDraftCleanupRegistryForTest(): void {
  entries.clear();
}

export function __getRegisteredDraftKeysForTest(): string[] {
  return [...entries.keys()];
}

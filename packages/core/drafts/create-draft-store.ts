import { create, type StoreApi, type UseBoundStore } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../platform/workspace-storage';
import { defaultStorage } from '../platform/storage';
import { registerDraftCleanup } from './cleanup-registry';

export interface DraftStoreConfig<TData> {
  storageKey: string;
  emptyData: TData;
  hasMeaningful: (data: TData) => boolean;
  workspaceScoped?: boolean;
  migrateData?: (persistedData: unknown) => Partial<TData>;
}

export interface DraftStore<TData> {
  draft: TData;
  setDraft: (patch: Partial<TData>) => void;
  clearDraft: () => void;
  hasDraft: () => boolean;
}

export function createDraftStore<TData extends object>(
  config: DraftStoreConfig<TData>,
): UseBoundStore<StoreApi<DraftStore<TData>>> {
  const workspaceScoped = config.workspaceScoped ?? true;
  const empty = () => structuredCloneData(config.emptyData);

  const useStore = create<DraftStore<TData>>()(
    persist(
      (set, get) => ({
        draft: empty(),
        setDraft: (patch) => set((s) => ({ draft: { ...s.draft, ...patch } })),
        clearDraft: () => set({ draft: empty() }),
        hasDraft: () => config.hasMeaningful(get().draft),
      }),
      {
        name: config.storageKey,
        storage: createJSONStorage(() =>
          workspaceScoped ? createWorkspaceAwareStorage(defaultStorage) : defaultStorage,
        ),
        merge: (persistedState, currentState) => {
          const persisted = (persistedState ?? {}) as Partial<DraftStore<TData>>;
          const persistedData = persisted.draft as unknown;
          const overlay = config.migrateData
            ? config.migrateData(persistedData)
            : (persistedData as Partial<TData> | undefined);
          return {
            ...currentState,
            ...persisted,
            draft: { ...structuredCloneData(config.emptyData), ...overlay },
          };
        },
      },
    ),
  );

  if (workspaceScoped) {
    registerForWorkspaceRehydration(() => useStore.persist.rehydrate());
  }

  registerDraftCleanup({
    storageKey: config.storageKey,
    workspaceScoped,
    resetInMemory: () => useStore.getState().clearDraft(),
  });

  return useStore;
}

function structuredCloneData<T>(value: T): T {
  return typeof structuredClone === 'function'
    ? structuredClone(value)
    : (JSON.parse(JSON.stringify(value)) as T);
}

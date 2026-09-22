'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';
import { registerDraftCleanup } from '../../drafts/cleanup-registry';

export type QuickCreateActorType = 'agent' | 'squad';

interface QuickCreateState {
  lastActorType: QuickCreateActorType | null;
  lastActorId: string | null;
  setLastActor: (type: QuickCreateActorType | null, id: string | null) => void;
  lastProjectId: string | null;
  setLastProjectId: (id: string | null) => void;
  keepOpen: boolean;
  setKeepOpen: (v: boolean) => void;
}

export const useQuickCreateStore = create<QuickCreateState>()(
  persist(
    (set) => ({
      lastActorType: null,
      lastActorId: null,
      setLastActor: (type, id) => set({ lastActorType: type, lastActorId: id }),
      lastProjectId: null,
      setLastProjectId: (id) => set({ lastProjectId: id }),
      keepOpen: false,
      setKeepOpen: (v) => set({ keepOpen: v }),
    }),
    {
      name: 'goosar_quick_create',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useQuickCreateStore.persist.rehydrate());

registerDraftCleanup({
  storageKey: 'goosar_quick_create',
  workspaceScoped: true,
  resetInMemory: () =>
    useQuickCreateStore.setState({
      lastActorType: null,
      lastActorId: null,
      lastProjectId: null,
      keepOpen: false,
    }),
});

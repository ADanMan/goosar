'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { defaultStorage } from '../../platform/storage';
import { useModalStore } from '../../modals';

export type CreateMode = 'agent' | 'manual';

interface CreateModeState {
  lastMode: CreateMode;
  setLastMode: (mode: CreateMode) => void;
}

export const useCreateModeStore = create<CreateModeState>()(
  persist(
    (set) => ({
      lastMode: 'agent',
      setLastMode: (mode) => set({ lastMode: mode }),
    }),
    {
      name: 'goosar_create_mode',
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);

export function openCreateIssueWithPreference(data?: Record<string, unknown> | null) {
  const lastMode = useCreateModeStore.getState().lastMode;
  const modal = lastMode === 'manual' ? 'create-issue' : 'quick-create-issue';
  useModalStore.getState().open(modal, data ?? null);
}

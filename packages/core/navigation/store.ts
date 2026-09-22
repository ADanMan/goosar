'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../platform/workspace-storage';
import { defaultStorage } from '../platform/storage';

const EXCLUDED_PREFIXES = [
  '/login',
  '/signup',
  '/logout',
  '/workspaces/',
  '/auth/',
  '/invite/',
  '/pair/',
];

interface NavigationState {
  lastPath: string | null;
  onPathChange: (path: string) => void;
}

export const useNavigationStore = create<NavigationState>()(
  persist(
    (set) => ({
      lastPath: null,
      onPathChange: (path: string) => {
        if (!EXCLUDED_PREFIXES.some((prefix) => path.startsWith(prefix))) {
          set({ lastPath: path });
        }
      },
    }),
    {
      name: 'goosar_navigation',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ lastPath: state.lastPath }),
    },
  ),
);

registerForWorkspaceRehydration(() => useNavigationStore.persist.rehydrate());

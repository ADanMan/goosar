import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { defaultStorage } from '../../platform/storage';

interface IssueLinkStore {
  openInNewTab: boolean;
  setOpenInNewTab: (open: boolean) => void;
}

export const useIssueLinkStore = create<IssueLinkStore>()(
  persist(
    (set) => ({
      openInNewTab: true,
      setOpenInNewTab: (open) => set({ openInNewTab: open }),
    }),
    {
      name: 'goosar_issue_link',
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);

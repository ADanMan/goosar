import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { defaultStorage } from '../../platform/storage';

interface CommentComposerStore {
  sticky: boolean;
  toggleSticky: () => void;
}

export const useCommentComposerStore = create<CommentComposerStore>()(
  persist(
    (set) => ({
      sticky: true,
      toggleSticky: () => set((s) => ({ sticky: !s.sticky })),
    }),
    {
      name: 'goosar_comment_composer',
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);

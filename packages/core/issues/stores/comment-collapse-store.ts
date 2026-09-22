import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';

interface CommentCollapseStore {
  collapsedByIssue: Record<string, string[]>;
  isCollapsed: (issueId: string, commentId: string) => boolean;
  toggle: (issueId: string, commentId: string) => void;
  collapseAll: (issueId: string, commentIds: readonly string[]) => void;
  expandAll: (issueId: string) => void;
}

export const useCommentCollapseStore = create<CommentCollapseStore>()(
  persist(
    (set, get) => ({
      collapsedByIssue: {},
      isCollapsed: (issueId, commentId) => {
        const ids = get().collapsedByIssue[issueId];
        return ids ? ids.includes(commentId) : false;
      },
      toggle: (issueId, commentId) =>
        set((s) => {
          const current = s.collapsedByIssue[issueId] ?? [];
          const isCurrentlyCollapsed = current.includes(commentId);
          if (isCurrentlyCollapsed) {
            const next = current.filter((id) => id !== commentId);
            if (next.length === 0) {
              const { [issueId]: _, ...rest } = s.collapsedByIssue;
              return { collapsedByIssue: rest };
            }
            return { collapsedByIssue: { ...s.collapsedByIssue, [issueId]: next } };
          }
          return {
            collapsedByIssue: { ...s.collapsedByIssue, [issueId]: [...current, commentId] },
          };
        }),
      collapseAll: (issueId, commentIds) =>
        set((s) => {
          if (commentIds.length === 0) {
            if (!(issueId in s.collapsedByIssue)) return s;
            const { [issueId]: _, ...rest } = s.collapsedByIssue;
            return { collapsedByIssue: rest };
          }
          return {
            collapsedByIssue: { ...s.collapsedByIssue, [issueId]: [...new Set(commentIds)] },
          };
        }),
      expandAll: (issueId) =>
        set((s) => {
          if (!(issueId in s.collapsedByIssue)) return s;
          const { [issueId]: _, ...rest } = s.collapsedByIssue;
          return { collapsedByIssue: rest };
        }),
    }),
    {
      name: 'goosar_comment_collapse',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useCommentCollapseStore.persist.rehydrate());

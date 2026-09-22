// Учёт неудавшихся оптимистичных комментариев, чтобы лента показала
// «Ошибка · Повторить · Удалить» на нужной строке.
import { create } from "zustand";

export interface FailedCommentPayload {
  content: string;
  parentId?: string;
  attachmentIds?: string[];
  error: string;
}

interface FailedCommentsState {
  failed: Record<string, FailedCommentPayload>;
  markFailed: (optimisticId: string, payload: FailedCommentPayload) => void;
  clear: (optimisticId: string) => void;
}

export const useFailedCommentsStore = create<FailedCommentsState>(
  (set, get) => ({
    failed: {},
    markFailed: (optimisticId, payload) => {
      set({ failed: { ...get().failed, [optimisticId]: payload } });
    },
    clear: (optimisticId) => {
      const current = get().failed;
      if (!(optimisticId in current)) return;
      const next = { ...current };
      delete next[optimisticId];
      set({ failed: next });
    },
  }),
);

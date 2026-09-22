// Метка «когда пользователь последний раз смотрел ленту задачи» — для разделителя
// «Новое с последнего просмотра». Только в памяти.
import { create } from "zustand";

interface LastViewedState {
  lastViewed: Record<string, string>;
  getLastViewed: (issueId: string) => string | undefined;
  markViewed: (issueId: string, when?: string) => void;
}

export const useLastViewedStore = create<LastViewedState>((set, get) => ({
  lastViewed: {},
  getLastViewed: (issueId) => get().lastViewed[issueId],
  markViewed: (issueId, when) => {
    const ts = when ?? new Date().toISOString();
    set({ lastViewed: { ...get().lastViewed, [issueId]: ts } });
  },
}));

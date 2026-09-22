// Zustand-стор представления «Моих задач» (область и фильтры). Зеркалит форму
// полей веб-стора, чтобы одинаковый фильтр давал одинаковый набор задач.
import { create } from "zustand";
import type { IssuePriority, IssueStatus } from "@goosar/core/types";
import type { MyIssuesScope } from "@/data/queries/issue-keys";

interface MyIssuesViewState {
  scope: MyIssuesScope;
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  setScope: (scope: MyIssuesScope) => void;
  toggleStatusFilter: (status: IssueStatus) => void;
  togglePriorityFilter: (priority: IssuePriority) => void;
  clearFilters: () => void;
}

export const useMyIssuesViewStore = create<MyIssuesViewState>((set) => ({
  scope: "assigned",
  statusFilters: [],
  priorityFilters: [],
  setScope: (scope) => set({ scope }),
  toggleStatusFilter: (status) =>
    set((state) => ({
      statusFilters: state.statusFilters.includes(status)
        ? state.statusFilters.filter((s) => s !== status)
        : [...state.statusFilters, status],
    })),
  togglePriorityFilter: (priority) =>
    set((state) => ({
      priorityFilters: state.priorityFilters.includes(priority)
        ? state.priorityFilters.filter((p) => p !== priority)
        : [...state.priorityFilters, priority],
    })),
  clearFilters: () => set({ statusFilters: [], priorityFilters: [] }),
}));

// Стор представления страницы задач воркспейса: как у «Моих задач», плюс поле
// scope (all / members / agents).
import { create } from "zustand";
import type { IssuePriority, IssueStatus } from "@goosar/core/types";

export type IssuesScope = "all" | "members" | "agents";

interface IssuesViewState {
  scope: IssuesScope;
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  setScope: (scope: IssuesScope) => void;
  toggleStatusFilter: (status: IssueStatus) => void;
  togglePriorityFilter: (priority: IssuePriority) => void;
  clearFilters: () => void;
}

export const useIssuesViewStore = create<IssuesViewState>((set) => ({
  scope: "all",
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

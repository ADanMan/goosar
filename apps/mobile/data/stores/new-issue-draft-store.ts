// Черновик модалки новой задачи. Стор вместо useState, потому что пикеры-formSheet
// живут в отдельных экранах Stack без React-родителя.
import { useEffect, useRef } from "react";
import { create } from "zustand";
import type {
  IssuePriority,
  IssueStatus,
  Project,
} from "@goosar/core/types";
import type { AssigneeValue } from "@/components/issue/pickers/assignee-picker-body";

interface NewIssueDraftState {
  status: IssueStatus;
  priority: IssuePriority;
  assignee: AssigneeValue;
  dueDate: string | null;
  project: Project | null;
  setStatus: (next: IssueStatus) => void;
  setPriority: (next: IssuePriority) => void;
  setAssignee: (next: AssigneeValue) => void;
  setDueDate: (next: string | null) => void;
  setProject: (next: Project | null) => void;
  reset: () => void;
}

const INITIAL: Pick<
  NewIssueDraftState,
  "status" | "priority" | "assignee" | "dueDate" | "project"
> = {
  status: "todo",
  priority: "none",
  assignee: null,
  dueDate: null,
  project: null,
};

export const useNewIssueDraftStore = create<NewIssueDraftState>((set) => ({
  ...INITIAL,
  setStatus: (next) => set({ status: next }),
  setPriority: (next) => set({ priority: next }),
  setAssignee: (next) => set({ assignee: next }),
  setDueDate: (next) => set({ dueDate: next }),
  setProject: (next) => set({ project: next }),
  reset: () => set({ ...INITIAL }),
}));

export function useNewIssueDraftResetOnWorkspaceChange(wsId: string | null) {
  const prevRef = useRef(wsId);
  useEffect(() => {
    if (prevRef.current !== wsId) {
      useNewIssueDraftStore.getState().reset();
      prevRef.current = wsId;
    }
  }, [wsId]);
}

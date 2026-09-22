// Черновик модалки нового проекта. Зеркалит new-issue-draft-store.ts.
import { useEffect, useRef } from "react";
import { create } from "zustand";
import type { ProjectPriority, ProjectStatus } from "@goosar/core/types";

interface NewProjectDraftState {
  status: ProjectStatus;
  priority: ProjectPriority;
  setStatus: (next: ProjectStatus) => void;
  setPriority: (next: ProjectPriority) => void;
  reset: () => void;
}

const INITIAL: Pick<NewProjectDraftState, "status" | "priority"> = {
  status: "planned",
  priority: "none",
};

export const useNewProjectDraftStore = create<NewProjectDraftState>((set) => ({
  ...INITIAL,
  setStatus: (next) => set({ status: next }),
  setPriority: (next) => set({ priority: next }),
  reset: () => set({ ...INITIAL }),
}));

export function useNewProjectDraftResetOnWorkspaceChange(wsId: string | null) {
  const prevRef = useRef(wsId);
  useEffect(() => {
    if (prevRef.current !== wsId) {
      useNewProjectDraftStore.getState().reset();
      prevRef.current = wsId;
    }
  }, [wsId]);
}

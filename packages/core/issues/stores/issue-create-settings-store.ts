'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';

export type QuickCreateField = 'project' | 'priority' | 'due_date';
export type ManualCreateField =
  'status' | 'priority' | 'assignee' | 'labels' | 'project' | 'due_date' | 'start_date';

export const QUICK_CREATE_FIELDS: QuickCreateField[] = ['project', 'priority', 'due_date'];
export const MANUAL_CREATE_FIELDS: ManualCreateField[] = [
  'status',
  'priority',
  'assignee',
  'labels',
  'project',
  'due_date',
  'start_date',
];

export const DEFAULT_QUICK_CREATE_FIELDS: QuickCreateField[] = ['project'];
export const DEFAULT_MANUAL_CREATE_FIELDS: ManualCreateField[] = [
  'status',
  'priority',
  'assignee',
  'labels',
  'project',
];

interface IssueCreateSettingsState {
  quickCreateFields: QuickCreateField[];
  setQuickCreateFieldVisible: (field: QuickCreateField, visible: boolean) => void;
  manualCreateFields: ManualCreateField[];
  setManualCreateFieldVisible: (field: ManualCreateField, visible: boolean) => void;
}

function toggle<F extends string>(all: F[], current: F[], field: F, visible: boolean): F[] {
  return all.filter((f) => (f === field ? visible : current.includes(f)));
}

export const useIssueCreateSettingsStore = create<IssueCreateSettingsState>()(
  persist(
    (set) => ({
      quickCreateFields: DEFAULT_QUICK_CREATE_FIELDS,
      setQuickCreateFieldVisible: (field, visible) =>
        set((s) => ({
          quickCreateFields: toggle(QUICK_CREATE_FIELDS, s.quickCreateFields, field, visible),
        })),
      manualCreateFields: DEFAULT_MANUAL_CREATE_FIELDS,
      setManualCreateFieldVisible: (field, visible) =>
        set((s) => ({
          manualCreateFields: toggle(MANUAL_CREATE_FIELDS, s.manualCreateFields, field, visible),
        })),
    }),
    {
      name: 'goosar_issue_create_settings',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      merge: (persistedState, currentState) => {
        const persisted = (persistedState ?? {}) as Partial<IssueCreateSettingsState>;
        return {
          ...currentState,
          ...persisted,
          quickCreateFields: persisted.quickCreateFields ?? DEFAULT_QUICK_CREATE_FIELDS,
          manualCreateFields: persisted.manualCreateFields ?? DEFAULT_MANUAL_CREATE_FIELDS,
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueCreateSettingsStore.persist.rehydrate());

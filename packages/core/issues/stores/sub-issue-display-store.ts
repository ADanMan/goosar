import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { defaultStorage } from '../../platform/storage';

export const SUB_ISSUE_ROW_PROPERTY_KEYS = [
  'priority',
  'labels',
  'childProgress',
  'dueDate',
  'assignee',
] as const;
export type SubIssueRowPropertyKey = (typeof SUB_ISSUE_ROW_PROPERTY_KEYS)[number];
export type SubIssueRowProperties = Record<SubIssueRowPropertyKey, boolean>;

export const DEFAULT_SUB_ISSUE_ROW_PROPERTIES: SubIssueRowProperties = {
  priority: true,
  labels: true,
  childProgress: true,
  dueDate: true,
  assignee: true,
};

interface SubIssueDisplayStore {
  rowProperties: SubIssueRowProperties;
  rowPropertyIds: string[];
  toggleRowProperty: (key: SubIssueRowPropertyKey) => void;
  toggleRowPropertyId: (propertyId: string) => void;
}

export const useSubIssueDisplayStore = create<SubIssueDisplayStore>()(
  persist(
    (set) => ({
      rowProperties: { ...DEFAULT_SUB_ISSUE_ROW_PROPERTIES },
      rowPropertyIds: [],
      toggleRowProperty: (key) =>
        set((s) => ({
          rowProperties: { ...s.rowProperties, [key]: !s.rowProperties[key] },
        })),
      toggleRowPropertyId: (propertyId) =>
        set((s) => ({
          rowPropertyIds: s.rowPropertyIds.includes(propertyId)
            ? s.rowPropertyIds.filter((id) => id !== propertyId)
            : [...s.rowPropertyIds, propertyId],
        })),
    }),
    {
      name: 'goosar_sub_issue_display',
      storage: createJSONStorage(() => defaultStorage),
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<SubIssueDisplayStore>;
        return {
          ...current,
          ...p,
          rowProperties: {
            ...current.rowProperties,
            ...(p.rowProperties ?? {}),
          },
        };
      },
    },
  ),
);

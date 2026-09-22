'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';

export type AutopilotScope = 'all' | 'active' | 'paused';

export const AUTOPILOT_SCOPES: AutopilotScope[] = ['all', 'active', 'paused'];

export type AutopilotSortField = 'name' | 'lastRun' | 'nextRun' | 'created';

export type AutopilotSortDirection = 'asc' | 'desc';

export const AUTOPILOT_SORT_DEFAULT_DIRECTION: Record<AutopilotSortField, AutopilotSortDirection> =
  {
    name: 'asc',
    lastRun: 'desc',
    nextRun: 'asc',
    created: 'desc',
  };

export interface AutopilotListFilters {
  assignees: string[];
  modes: string[];
  triggerKinds: string[];
  creators: string[];
}

export const EMPTY_AUTOPILOT_FILTERS: AutopilotListFilters = {
  assignees: [],
  modes: [],
  triggerKinds: [],
  creators: [],
};

export type AutopilotColumnKey =
  'assignee' | 'trigger' | 'lastRun' | 'nextRun' | 'mode' | 'creator' | 'created';

export const AUTOPILOT_DEFAULT_HIDDEN_COLUMNS: AutopilotColumnKey[] = [
  'mode',
  'creator',
  'created',
];

export interface AutopilotsViewState {
  scope: AutopilotScope;
  sortField: AutopilotSortField;
  sortDirection: AutopilotSortDirection;
  hiddenColumns: AutopilotColumnKey[];
  filters: AutopilotListFilters;
  setScope: (scope: AutopilotScope) => void;
  toggleSort: (field: AutopilotSortField) => void;
  setSortField: (field: AutopilotSortField) => void;
  setSortDirection: (direction: AutopilotSortDirection) => void;
  toggleColumn: (key: AutopilotColumnKey) => void;
  toggleFilter: (key: keyof AutopilotListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  scope: 'all' as AutopilotScope,
  sortField: 'lastRun' as AutopilotSortField,
  sortDirection: AUTOPILOT_SORT_DEFAULT_DIRECTION.lastRun,
  hiddenColumns: AUTOPILOT_DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_AUTOPILOT_FILTERS,
};

export const useAutopilotsViewStore = create<AutopilotsViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      setScope: (scope) => set({ scope }),
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? {
                sortDirection: state.sortDirection === 'asc' ? 'desc' : 'asc',
              }
            : {
                sortField: field,
                sortDirection: AUTOPILOT_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: AUTOPILOT_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortDirection: (direction) => set({ sortDirection: direction }),
      toggleColumn: (key) =>
        set((state) => ({
          hiddenColumns: state.hiddenColumns.includes(key)
            ? state.hiddenColumns.filter((k) => k !== key)
            : [...state.hiddenColumns, key],
        })),
      toggleFilter: (key, value) =>
        set((state) => {
          const list = state.filters[key] as string[];
          const next = list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
          return { filters: { ...state.filters, [key]: next } };
        }),
      clearFilters: () => set({ filters: EMPTY_AUTOPILOT_FILTERS }),
    }),
    {
      name: 'goosar_autopilots_view',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({
        scope: state.scope,
        sortField: state.sortField,
        sortDirection: state.sortDirection,
        hiddenColumns: state.hiddenColumns,
        filters: state.filters,
      }),
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<AutopilotsViewState>;
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_AUTOPILOT_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useAutopilotsViewStore.persist.rehydrate());

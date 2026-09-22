'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';

export type ProjectViewMode = 'compact' | 'comfortable';

export type ProjectSortField = 'name' | 'priority' | 'status' | 'progress' | 'created';

export type ProjectSortDirection = 'asc' | 'desc';

export const PROJECT_SORT_DEFAULT_DIRECTION: Record<ProjectSortField, ProjectSortDirection> = {
  name: 'asc',
  priority: 'desc',
  status: 'asc',
  progress: 'desc',
  created: 'desc',
};

export interface ProjectListFilters {
  statuses: string[];
  priorities: string[];
  leads: string[];
}

export const EMPTY_PROJECT_FILTERS: ProjectListFilters = {
  statuses: [],
  priorities: [],
  leads: [],
};

export type ProjectColumnKey = 'priority' | 'progress' | 'lead' | 'issues' | 'created';

export const PROJECT_DEFAULT_HIDDEN_COLUMNS: ProjectColumnKey[] = ['issues'];

export interface ProjectViewState {
  viewMode: ProjectViewMode;
  sortField: ProjectSortField;
  sortDirection: ProjectSortDirection;
  hiddenColumns: ProjectColumnKey[];
  filters: ProjectListFilters;
  setViewMode: (mode: ProjectViewMode) => void;
  toggleSort: (field: ProjectSortField) => void;
  setSortField: (field: ProjectSortField) => void;
  setSortDirection: (direction: ProjectSortDirection) => void;
  toggleColumn: (key: ProjectColumnKey) => void;
  toggleFilter: (key: keyof ProjectListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  viewMode: 'compact' as ProjectViewMode,
  sortField: 'created' as ProjectSortField,
  sortDirection: PROJECT_SORT_DEFAULT_DIRECTION.created,
  hiddenColumns: PROJECT_DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_PROJECT_FILTERS,
};

export const useProjectViewStore = create<ProjectViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      setViewMode: (mode) => set({ viewMode: mode }),
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? { sortDirection: state.sortDirection === 'asc' ? 'desc' : 'asc' }
            : {
                sortField: field,
                sortDirection: PROJECT_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: PROJECT_SORT_DEFAULT_DIRECTION[field],
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
      clearFilters: () => set({ filters: EMPTY_PROJECT_FILTERS }),
    }),
    {
      name: 'goosar_projects_view',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({
        viewMode: state.viewMode,
        sortField: state.sortField,
        sortDirection: state.sortDirection,
        hiddenColumns: state.hiddenColumns,
        filters: state.filters,
      }),
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<ProjectViewState>;
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_PROJECT_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useProjectViewStore.persist.rehydrate());

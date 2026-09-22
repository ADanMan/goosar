'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';

export type SkillSortField = 'name' | 'usedBy' | 'updated' | 'created';

export type SkillSortDirection = 'asc' | 'desc';

export const SKILL_SORT_DEFAULT_DIRECTION: Record<SkillSortField, SkillSortDirection> = {
  name: 'asc',
  usedBy: 'desc',
  updated: 'desc',
  created: 'desc',
};

export type SkillOriginType = 'manual' | 'runtime_local' | 'clawhub' | 'skills_sh' | 'github';

export interface SkillListFilters {
  usage: ('used' | 'unused')[];
  origins: SkillOriginType[];
  agents: string[];
  creators: string[];
}

export const EMPTY_SKILL_FILTERS: SkillListFilters = {
  usage: [],
  origins: [],
  agents: [],
  creators: [],
};

export type SkillColumnKey = 'usedBy' | 'source' | 'creator' | 'updated' | 'created';

export const DEFAULT_HIDDEN_COLUMNS: SkillColumnKey[] = ['source', 'created'];

export interface SkillsViewState {
  sortField: SkillSortField;
  sortDirection: SkillSortDirection;
  hiddenColumns: SkillColumnKey[];
  filters: SkillListFilters;
  toggleSort: (field: SkillSortField) => void;
  setSortField: (field: SkillSortField) => void;
  setSortDirection: (direction: SkillSortDirection) => void;
  toggleColumn: (key: SkillColumnKey) => void;
  toggleFilter: (key: keyof SkillListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  sortField: 'updated' as SkillSortField,
  sortDirection: SKILL_SORT_DEFAULT_DIRECTION.updated,
  hiddenColumns: DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_SKILL_FILTERS,
};

export const useSkillsViewStore = create<SkillsViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? {
                sortDirection: state.sortDirection === 'asc' ? 'desc' : 'asc',
              }
            : {
                sortField: field,
                sortDirection: SKILL_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: SKILL_SORT_DEFAULT_DIRECTION[field],
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
      clearFilters: () => set({ filters: EMPTY_SKILL_FILTERS }),
    }),
    {
      name: 'goosar_skills_view',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({
        sortField: state.sortField,
        sortDirection: state.sortDirection,
        hiddenColumns: state.hiddenColumns,
        filters: state.filters,
      }),
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<SkillsViewState>;
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_SKILL_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useSkillsViewStore.persist.rehydrate());

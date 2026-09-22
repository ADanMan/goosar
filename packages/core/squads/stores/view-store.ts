'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';

export type SquadsScope = 'mine' | 'all';

export const SQUAD_SCOPES: SquadsScope[] = ['mine', 'all'];

export type SquadSortField = 'name' | 'members' | 'created';

export type SquadSortDirection = 'asc' | 'desc';

export const SQUAD_SORT_DEFAULT_DIRECTION: Record<SquadSortField, SquadSortDirection> = {
  name: 'asc',
  members: 'desc',
  created: 'desc',
};

export type SquadColumnKey = 'members' | 'creator' | 'created';

export const SQUAD_DEFAULT_HIDDEN_COLUMNS: SquadColumnKey[] = ['created'];

export interface SquadListFilters {
  leaders: string[];
  creators: string[];
}

export const EMPTY_SQUAD_FILTERS: SquadListFilters = {
  leaders: [],
  creators: [],
};

export interface SquadsViewState {
  scope: SquadsScope;
  sortField: SquadSortField;
  sortDirection: SquadSortDirection;
  hiddenColumns: SquadColumnKey[];
  filters: SquadListFilters;
  setScope: (scope: SquadsScope) => void;
  toggleSort: (field: SquadSortField) => void;
  setSortField: (field: SquadSortField) => void;
  setSortDirection: (direction: SquadSortDirection) => void;
  toggleColumn: (key: SquadColumnKey) => void;
  toggleFilter: (key: keyof SquadListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  scope: 'mine' as SquadsScope,
  sortField: 'name' as SquadSortField,
  sortDirection: SQUAD_SORT_DEFAULT_DIRECTION.name,
  hiddenColumns: SQUAD_DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_SQUAD_FILTERS,
};

export const useSquadsViewStore = create<SquadsViewState>()(
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
                sortDirection: SQUAD_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: SQUAD_SORT_DEFAULT_DIRECTION[field],
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
      clearFilters: () => set({ filters: EMPTY_SQUAD_FILTERS }),
    }),
    {
      name: 'goosar_squads_view',
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
        const p = persisted as Partial<SquadsViewState>;
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_SQUAD_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useSquadsViewStore.persist.rehydrate());

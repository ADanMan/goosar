'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';
import type { AccessScope } from '../effective-access';

export type AgentsScope = 'mine' | 'all' | 'archived';

export const AGENT_SCOPES: AgentsScope[] = ['mine', 'all', 'archived'];

export type AgentSortField = 'lastActive' | 'name' | 'runs' | 'created';

export type AgentSortDirection = 'asc' | 'desc';

export const AGENT_SORT_DEFAULT_DIRECTION: Record<AgentSortField, AgentSortDirection> = {
  lastActive: 'desc',
  name: 'asc',
  runs: 'desc',
  created: 'desc',
};

export interface AgentListFilters {
  availability: string[];
  runtimes: string[];
  owners: string[];
  models: string[];
  access: AccessScope[];
}

export const EMPTY_AGENT_FILTERS: AgentListFilters = {
  availability: [],
  runtimes: [],
  owners: [],
  models: [],
  access: [],
};

export type AgentColumnKey =
  'status' | 'owner' | 'access' | 'runtime' | 'lastActive' | 'runs' | 'model' | 'created';

export const AGENT_DEFAULT_HIDDEN_COLUMNS: AgentColumnKey[] = ['model', 'created'];

export interface AgentsViewState {
  scope: AgentsScope;
  sortField: AgentSortField;
  sortDirection: AgentSortDirection;
  hiddenColumns: AgentColumnKey[];
  filters: AgentListFilters;
  setScope: (scope: AgentsScope) => void;
  toggleSort: (field: AgentSortField) => void;
  setSortField: (field: AgentSortField) => void;
  setSortDirection: (direction: AgentSortDirection) => void;
  toggleColumn: (key: AgentColumnKey) => void;
  toggleFilter: (key: keyof AgentListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  scope: 'mine' as AgentsScope,
  sortField: 'lastActive' as AgentSortField,
  sortDirection: AGENT_SORT_DEFAULT_DIRECTION.lastActive,
  hiddenColumns: AGENT_DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_AGENT_FILTERS,
};

export const useAgentsViewStore = create<AgentsViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      setScope: (scope) =>
        set(scope === 'mine' ? { scope, filters: EMPTY_AGENT_FILTERS } : { scope }),
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? {
                sortDirection: state.sortDirection === 'asc' ? 'desc' : 'asc',
              }
            : {
                sortField: field,
                sortDirection: AGENT_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: AGENT_SORT_DEFAULT_DIRECTION[field],
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
          const scope = state.scope === 'mine' ? 'all' : state.scope;
          return { scope, filters: { ...state.filters, [key]: next } };
        }),
      clearFilters: () => set({ filters: EMPTY_AGENT_FILTERS }),
    }),
    {
      name: 'goosar_agents_view',
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
        const p = persisted as Partial<AgentsViewState>;
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_AGENT_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useAgentsViewStore.persist.rehydrate());

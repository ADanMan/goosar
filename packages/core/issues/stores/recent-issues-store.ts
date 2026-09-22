'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { defaultStorage } from '../../platform/storage';

const MAX_RECENT_ISSUES = 20;
const MAX_WORKSPACES = 50;
const EMPTY: RecentIssueEntry[] = [];

export interface RecentIssueEntry {
  id: string;
  visitedAt: number;
}

interface RecentIssuesState {
  byWorkspace: Record<string, RecentIssueEntry[]>;
  recordVisit: (wsId: string, id: string) => void;
  forgetIssue: (wsId: string, id: string) => void;
  pruneWorkspaces: (activeWsIds: string[]) => void;
}

export const useRecentIssuesStore = create<RecentIssuesState>()(
  persist(
    (set) => ({
      byWorkspace: {},
      recordVisit: (wsId, id) =>
        set((state) => {
          const bucket = state.byWorkspace[wsId] ?? EMPTY;
          const filtered = bucket.filter((i) => i.id !== id);
          const updated: RecentIssueEntry = { id, visitedAt: Date.now() };
          const nextBucket = [updated, ...filtered].slice(0, MAX_RECENT_ISSUES);

          let nextByWorkspace = {
            ...state.byWorkspace,
            [wsId]: nextBucket,
          };

          const ids = Object.keys(nextByWorkspace);
          if (ids.length > MAX_WORKSPACES) {
            const oldest = ids.reduce((oldestId, candidateId) => {
              const a = nextByWorkspace[oldestId]?.[0]?.visitedAt ?? 0;
              const b = nextByWorkspace[candidateId]?.[0]?.visitedAt ?? 0;
              return b < a ? candidateId : oldestId;
            });
            const { [oldest]: _, ...rest } = nextByWorkspace;
            nextByWorkspace = rest;
          }

          return { byWorkspace: nextByWorkspace };
        }),
      forgetIssue: (wsId, id) =>
        set((state) => {
          const bucket = state.byWorkspace[wsId];
          if (!bucket) return state;
          const nextBucket = bucket.filter((entry) => entry.id !== id);
          if (nextBucket.length === bucket.length) return state;
          if (nextBucket.length === 0) {
            const { [wsId]: _, ...rest } = state.byWorkspace;
            return { byWorkspace: rest };
          }
          return {
            byWorkspace: { ...state.byWorkspace, [wsId]: nextBucket },
          };
        }),
      pruneWorkspaces: (activeWsIds) =>
        set((state) => {
          const allow = new Set(activeWsIds);
          let changed = false;
          const next: Record<string, RecentIssueEntry[]> = {};
          for (const [wsId, items] of Object.entries(state.byWorkspace)) {
            if (allow.has(wsId)) next[wsId] = items;
            else changed = true;
          }
          return changed ? { byWorkspace: next } : state;
        }),
    }),
    {
      name: 'goosar_recent_issues',
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ byWorkspace: state.byWorkspace }),
      version: 1,
      migrate: () => ({ byWorkspace: {} }),
    },
  ),
);

export function selectRecentIssues(wsId: string | null) {
  return (state: RecentIssuesState) => (wsId ? (state.byWorkspace[wsId] ?? EMPTY) : EMPTY);
}

/**
 * In-memory LRU недавно просмотренных issue (per workspace).
 * Используется для секции "Recent" в подсказках чат-композера при вводе `@`.
 * Хранится только в памяти (без SecureStore): свежесть — сигнал сессии,
 * очистка при холодном старте не критична.
 */
import { create } from "zustand";

const CAPACITY = 10;

interface State {
  byWorkspace: Record<string, string[]>;
  push: (wsId: string, id: string) => void;
  remove: (wsId: string, id: string) => void;
}

export const useViewedIssuesStore = create<State>((set) => ({
  byWorkspace: {},
  push: (wsId, id) =>
    set((s) => {
      const prev = s.byWorkspace[wsId] ?? [];
      const next = [id, ...prev.filter((x) => x !== id)].slice(0, CAPACITY);
      return { byWorkspace: { ...s.byWorkspace, [wsId]: next } };
    }),
  remove: (wsId, id) =>
    set((s) => {
      const prev = s.byWorkspace[wsId];
      if (!prev) return s;
      return {
        byWorkspace: {
          ...s.byWorkspace,
          [wsId]: prev.filter((x) => x !== id),
        },
      };
    }),
}));

const EMPTY_IDS: readonly string[] = Object.freeze([]);

export function selectViewedIssueIds(wsId: string | null) {
  return (s: State): readonly string[] =>
    wsId ? s.byWorkspace[wsId] ?? EMPTY_IDS : EMPTY_IDS;
}

import { create } from 'zustand';

interface ResolvedExpandStore {
  expandedByIssue: Record<string, ReadonlySet<string>>;
  setExpanded: (issueId: string, commentId: string, expand: boolean) => void;
  expandAll: (issueId: string, commentIds: readonly string[]) => void;
  collapseAll: (issueId: string) => void;
}

const EMPTY_EXPANDED: ReadonlySet<string> = new Set();

function withoutIssue(expandedByIssue: Record<string, ReadonlySet<string>>, issueId: string) {
  const { [issueId]: _, ...rest } = expandedByIssue;
  return rest;
}

export const useResolvedExpandStore = create<ResolvedExpandStore>()((set) => ({
  expandedByIssue: {},
  setExpanded: (issueId, commentId, expand) =>
    set((s) => {
      const current = s.expandedByIssue[issueId] ?? EMPTY_EXPANDED;
      if (current.has(commentId) === expand) return s;
      const next = new Set(current);
      if (expand) next.add(commentId);
      else next.delete(commentId);
      if (next.size === 0) {
        return { expandedByIssue: withoutIssue(s.expandedByIssue, issueId) };
      }
      return { expandedByIssue: { ...s.expandedByIssue, [issueId]: next } };
    }),
  expandAll: (issueId, commentIds) =>
    set((s) => {
      if (commentIds.length === 0) return s;
      const next = new Set(s.expandedByIssue[issueId] ?? EMPTY_EXPANDED);
      for (const id of commentIds) next.add(id);
      return { expandedByIssue: { ...s.expandedByIssue, [issueId]: next } };
    }),
  collapseAll: (issueId) =>
    set((s) => {
      if (!(issueId in s.expandedByIssue)) return s;
      return { expandedByIssue: withoutIssue(s.expandedByIssue, issueId) };
    }),
}));

export function selectExpandedResolved(issueId: string) {
  return (s: ResolvedExpandStore): ReadonlySet<string> =>
    s.expandedByIssue[issueId] ?? EMPTY_EXPANDED;
}

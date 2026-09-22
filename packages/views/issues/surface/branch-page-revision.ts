import type { IssueTableRowsResponse } from '@goosar/core/types';

export function pageRevision(
  page: IssueTableRowsResponse | undefined,
  isPlaceholderData: boolean,
): string {
  if (!page || isPlaceholderData) return '';
  return `${page.next_cursor ?? ''}|${page.rows.map((row) => row.issue.id).join(',')}`;
}

export type BranchPageRevisions = Record<string, string>;

export function revisionKey(cursor: string | null): string {
  return cursor ?? '';
}

export function branchChainMoved(
  previous: BranchPageRevisions | undefined,
  next: BranchPageRevisions,
): boolean {
  if (!previous) return false;
  for (const [cursor, revision] of Object.entries(next)) {
    if (revision === '') continue;
    const seen = previous[cursor];
    if (seen === undefined || seen === '') continue;
    if (seen !== revision) return true;
  }
  return false;
}

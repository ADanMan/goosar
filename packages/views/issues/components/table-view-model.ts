import { propertyIdFromViewKey } from '@goosar/core/issues/stores/view-store';
import type { TableCalculation, TableColumnKey } from '@goosar/core/issues/stores/view-store';
import type { Issue, IssuePropertyValue } from '@goosar/core/types';

export class IssueTableExportIntegrityError extends Error {
  constructor() {
    super('Table export response was incomplete');
    this.name = 'IssueTableExportIntegrityError';
  }
}

export type IssueTableDisplayRow =
  | {
      kind: 'group';
      key: string;
      label: string;
      count: number;
      collapsed: boolean;
    }
  | {
      kind: 'issue';
      key: string;
      issue: Issue;
      depth: number;
      hasChildren: boolean;
      collapsed: boolean;
    }
  | {
      kind: 'load_more';
      key: string;
      label: string;
      loading: boolean;
      autoLoad?: boolean;
      onLoad?: () => void;
    };

export function getIssueTableSelectionRange(
  issueIds: string[],
  anchorId: string | null,
  targetId: string,
): string[] | null {
  if (!anchorId) return null;
  const anchorIndex = issueIds.indexOf(anchorId);
  const targetIndex = issueIds.indexOf(targetId);
  if (anchorIndex === -1 || targetIndex === -1) return null;

  const start = Math.min(anchorIndex, targetIndex);
  const end = Math.max(anchorIndex, targetIndex);
  return issueIds.slice(start, end + 1);
}

export function refreshFrozenTableRows(
  snapshot: IssueTableDisplayRow[],
  issueById: ReadonlyMap<string, Issue>,
): IssueTableDisplayRow[] {
  return snapshot.map((row) => {
    if (row.kind !== 'issue') return row;
    const live = issueById.get(row.issue.id);
    return live && live !== row.issue ? { ...row, issue: live } : row;
  });
}
function columnValue(
  issue: Issue,
  columnKey: TableColumnKey,
): IssuePropertyValue | string | number | null | undefined {
  const propertyId = propertyIdFromViewKey(columnKey);
  if (propertyId) return issue.properties[propertyId];
  switch (columnKey) {
    case 'identifier':
      return issue.identifier;
    case 'title':
      return issue.title;
    case 'status':
      return issue.status;
    case 'priority':
      return issue.priority;
    case 'assignee':
      return issue.assignee_id;
    case 'labels':
      return issue.labels?.map((label) => label.name).join(', ');
    case 'project':
      return issue.project_id;
    case 'start_date':
      return issue.start_date;
    case 'due_date':
      return issue.due_date;
    case 'created_at':
      return issue.created_at;
    case 'updated_at':
      return issue.updated_at;
    case 'child_progress':
      return undefined;
    case 'creator':
      return issue.creator_id;
  }
  return undefined;
}

export function calculateIssueTableColumn(
  issues: Issue[],
  columnKey: TableColumnKey,
  calculation: TableCalculation,
) {
  if (calculation === 'none') return null;
  const values = issues
    .map((issue) => columnValue(issue, columnKey))
    .filter((value) => value !== undefined && value !== null && value !== '');
  if (calculation === 'count') return values.length;
  const numbers = values.filter((value): value is number => typeof value === 'number');
  if (numbers.length === 0) return null;
  const sum = numbers.reduce((total, value) => total + value, 0);
  return calculation === 'sum' ? sum : sum / numbers.length;
}

function escapeCsvCell(value: unknown) {
  const raw = value == null ? '' : String(value);
  const text = typeof value === 'string' && /^[=+\-@\t\r]/.test(raw) ? `'${raw}` : raw;
  return /[",\n\r]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

export function buildIssueTableCsv(headers: string[], rows: unknown[][]) {
  return [headers, ...rows].map((row) => row.map(escapeCsvCell).join(',')).join('\r\n');
}

import type { Issue, IssueStatus, IssuePriority, IssueAssigneeType } from '../types';

export interface CommonAssignee {
  type: IssueAssigneeType | null;
  id: string | null;
}

export interface CommonIssueFields {
  status: IssueStatus | null;
  priority: IssuePriority | null;
  assignee: CommonAssignee | null;
}

function sharedValue<T>(values: readonly T[]): T | null {
  if (values.length === 0) return null;
  const first = values[0]!;
  return values.every((v) => v === first) ? first : null;
}

const ASSIGNEE_KEY_SEP = '\u0000';

function assigneeKey(type: IssueAssigneeType | null, id: string | null): string {
  return `${type ?? ''}${ASSIGNEE_KEY_SEP}${id ?? ''}`;
}

export function commonIssueFields(issues: readonly Issue[]): CommonIssueFields {
  const status = sharedValue(issues.map((i) => i.status));
  const priority = sharedValue(issues.map((i) => i.priority));

  const sharedAssigneeKey = sharedValue(
    issues.map((i) => assigneeKey(i.assignee_type, i.assignee_id)),
  );
  const assignee =
    sharedAssigneeKey !== null && issues.length > 0
      ? { type: issues[0]!.assignee_type, id: issues[0]!.assignee_id }
      : null;

  return { status, priority, assignee };
}

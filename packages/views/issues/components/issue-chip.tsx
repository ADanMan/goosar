'use client';

import { useQuery } from '@tanstack/react-query';
import { issueListOptions, issueDetailOptions } from '@goosar/core/issues/queries';
import { useWorkspaceId } from '@goosar/core/hooks';
import { StatusIcon } from './status-icon';

export interface IssueChipProps {
  issueId: string;
  fallbackLabel?: string;
  className?: string;
}

const BASE_CLASS =
  'issue-mention inline-flex min-w-0 max-w-full items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-xs';

export function IssueChip({ issueId, fallbackLabel, className }: IssueChipProps) {
  const wsId = useWorkspaceId();
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const listIssue = issues.find((i) => i.id === issueId);

  const { data: detailIssue } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !listIssue,
  });

  const issue = listIssue ?? detailIssue;
  const cls = className ? `${BASE_CLASS} ${className}` : BASE_CLASS;

  if (!issue) {
    return (
      <span className={cls}>
        <span className="min-w-0 truncate font-medium text-muted-foreground">
          {fallbackLabel ?? issueId.slice(0, 8)}
        </span>
      </span>
    );
  }

  return (
    <span className={cls}>
      <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
      <span className="font-medium text-muted-foreground shrink-0">{issue.identifier}</span>
      <span className="min-w-0 truncate text-foreground">{issue.title}</span>
    </span>
  );
}

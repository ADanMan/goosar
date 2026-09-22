'use client';

import { AppLink } from '../../navigation';
import { useWorkspacePaths } from '@goosar/core/paths';
import { useIssueLinkStore } from '@goosar/core/issues/stores';
import { IssueChip } from './issue-chip';

interface IssueMentionCardProps {
  issueId: string;
  fallbackLabel?: string;
}

export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const p = useWorkspacePaths();
  const openInNewTab = useIssueLinkStore((s) => s.openInNewTab);
  return (
    <AppLink
      href={p.issueDetail(issueId)}
      target={openInNewTab ? '_blank' : undefined}
      newTabTitle={fallbackLabel}
      className="issue-mention not-prose align-middle"
    >
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </AppLink>
  );
}

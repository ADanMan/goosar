'use client';

import { NodeViewWrapper } from '@tiptap/react';
import type { NodeViewProps } from '@tiptap/react';
import { useWorkspacePaths } from '@goosar/core/paths';
import { useIssueLinkStore } from '@goosar/core/issues/stores';
import { useNavigation } from '../../navigation';
import { IssueChip } from '../../issues/components/issue-chip';
import { ProjectChip } from '../../projects/components/project-chip';

export function MentionView({ node }: NodeViewProps) {
  const { type, id, label } = node.attrs;

  if (type === 'issue') {
    return (
      <NodeViewWrapper as="span" className="inline">
        <IssueMention issueId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  if (type === 'project') {
    return (
      <NodeViewWrapper as="span" className="inline">
        <ProjectMention projectId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  return (
    <NodeViewWrapper as="span" className="inline">
      <span className="mention">@{label ?? id}</span>
    </NodeViewWrapper>
  );
}

function ProjectMention({
  projectId,
  fallbackLabel,
}: {
  projectId: string;
  fallbackLabel?: string;
}) {
  const p = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();
  const projectPath = p.projectDetail(projectId);

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) openInNewTab(projectPath, fallbackLabel);
      return;
    }
    push(projectPath);
  };

  return (
    <a href={projectPath} onClick={handleClick} className="project-mention">
      <ProjectChip
        projectId={projectId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}

function IssueMention({ issueId, fallbackLabel }: { issueId: string; fallbackLabel?: string }) {
  const p = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();
  const newTabPreferred = useIssueLinkStore((s) => s.openInNewTab);
  const issuePath = p.issueDetail(issueId);

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(issuePath, fallbackLabel);
      }
      return;
    }
    if (newTabPreferred) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(issuePath, fallbackLabel, { activate: true });
      }
      return;
    }
    e.preventDefault();
    push(issuePath);
  };

  return (
    <a
      href={issuePath}
      target={newTabPreferred ? '_blank' : undefined}
      rel={newTabPreferred ? 'noopener noreferrer' : undefined}
      onClick={handleClick}
      className="issue-mention"
    >
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}

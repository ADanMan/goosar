'use client';

import { useState, type ReactElement } from 'react';
import type { Issue } from '@goosar/core/types';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from '@goosar/ui/components/ui/dropdown-menu';
import { useIssueActions } from './use-issue-actions';
import { IssueActionsMenuItems, dropdownPrimitives } from './issue-actions-menu-items';
import { AssigneePicker } from '../components/pickers';

interface IssueActionsDropdownProps {
  issue: Issue;
  trigger: ReactElement;
  align?: 'start' | 'end' | 'center';
  onDeletedFallbackPath?: string;
}

export function IssueActionsDropdown({
  issue,
  trigger,
  align = 'end',
  onDeletedFallbackPath,
}: IssueActionsDropdownProps) {
  const actions = useIssueActions(issue);
  const [assigneeOpen, setAssigneeOpen] = useState(false);

  return (
    <span className="relative inline-flex">
      <DropdownMenu>
        <DropdownMenuTrigger render={trigger} />
        <DropdownMenuContent align={align} className="w-auto">
          <IssueActionsMenuItems
            issue={issue}
            actions={actions}
            primitives={dropdownPrimitives}
            onOpenAssignee={() => setAssigneeOpen(true)}
            onDeletedFallbackPath={onDeletedFallbackPath}
          />
        </DropdownMenuContent>
      </DropdownMenu>
      {/* Mount the picker only once the user actually opens it. Otherwise
          every row in a list/board would subscribe to members/agents/squads
          /frequency queries on mount, multiplying memory + render cost. */}
      {assigneeOpen && (
        <AssigneePicker
          assigneeType={issue.assignee_type}
          assigneeId={issue.assignee_id}
          onUpdate={actions.updateField}
          open={assigneeOpen}
          onOpenChange={setAssigneeOpen}
          triggerRender={<span aria-hidden className="pointer-events-none absolute inset-0" />}
          trigger={<span />}
          align={align}
        />
      )}
    </span>
  );
}

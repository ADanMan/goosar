'use client';

import { useCallback } from 'react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  ArrowDown,
  ArrowUp,
  Calendar,
  CalendarClock,
  FolderOpen,
  Link2,
  Network,
  Pin,
  PinOff,
  Plus,
  Trash2,
  Unlink,
  UserMinus,
} from 'lucide-react';
import type { AgentTask, Issue } from '@goosar/core/types';
import { todayDateOnly, addDaysDateOnly } from '@goosar/core/issues/date';
import { api } from '@goosar/core/api';
import { ALL_STATUSES, PRIORITY_ORDER, PRIORITY_CONFIG } from '@goosar/core/issues/config';
import { issueKeys } from '@goosar/core/issues/queries';
import { StatusIcon } from '../components/status-icon';
import { PriorityIcon } from '../components/priority-icon';
import {
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuSeparator,
} from '@goosar/ui/components/ui/dropdown-menu';
import {
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent,
  ContextMenuSeparator,
} from '@goosar/ui/components/ui/context-menu';
import { copyText } from '@goosar/ui/lib/clipboard';
import type { UseIssueActionsResult } from './use-issue-actions';
import { useT } from '../../i18n';

export interface MenuPrimitives {
  Item: typeof DropdownMenuItem;
  Sub: typeof DropdownMenuSub;
  SubTrigger: typeof DropdownMenuSubTrigger;
  SubContent: typeof DropdownMenuSubContent;
  Separator: typeof DropdownMenuSeparator;
}

export const dropdownPrimitives: MenuPrimitives = {
  Item: DropdownMenuItem,
  Sub: DropdownMenuSub,
  SubTrigger: DropdownMenuSubTrigger,
  SubContent: DropdownMenuSubContent,
  Separator: DropdownMenuSeparator,
};

export const contextPrimitives: MenuPrimitives = {
  Item: ContextMenuItem as unknown as typeof DropdownMenuItem,
  Sub: ContextMenuSub as unknown as typeof DropdownMenuSub,
  SubTrigger: ContextMenuSubTrigger as unknown as typeof DropdownMenuSubTrigger,
  SubContent: ContextMenuSubContent as unknown as typeof DropdownMenuSubContent,
  Separator: ContextMenuSeparator as unknown as typeof DropdownMenuSeparator,
};

interface IssueActionsMenuItemsProps {
  issue: Issue;
  actions: UseIssueActionsResult;
  primitives: MenuPrimitives;
  onOpenAssignee: () => void;
  onDeletedFallbackPath?: string;
}

export function IssueActionsMenuItems({
  issue,
  actions,
  primitives: P,
  onOpenAssignee,
  onDeletedFallbackPath,
}: IssueActionsMenuItemsProps) {
  const { t } = useT('issues');
  const {
    isPinned,
    updateField,
    togglePin,
    copyLink,
    openCreateSubIssue,
    openSetParent,
    removeParent,
    openAddChild,
    openDeleteConfirm,
  } = actions;

  const { data: tasks } = useQuery({
    queryKey: issueKeys.tasks(issue.id),
    queryFn: () => api.listTasksByIssue(issue.id),
    staleTime: 30_000,
  });

  const handleCopyWorkdirPath = useCallback(() => {
    const latestWorkDir = pickLatestWorkDir(tasks);
    if (!latestWorkDir) {
      toast.error(t(($) => $.detail.workdir_path_unavailable));
      return;
    }
    void copyText(latestWorkDir).then((ok) => {
      if (ok) toast.success(t(($) => $.detail.workdir_path_copied));
      else toast.error(t(($) => $.detail.workdir_path_copy_failed));
    });
  }, [tasks, t]);

  return (
    <>
      {/* Status */}
      <P.Sub>
        <P.SubTrigger>
          <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
          {t(($) => $.actions.status)}
        </P.SubTrigger>
        <P.SubContent>
          {ALL_STATUSES.map((s) => (
            <P.Item key={s} onClick={() => updateField({ status: s })}>
              <StatusIcon status={s} className="h-3.5 w-3.5" />
              {t(($) => $.status[s])}
              {issue.status === s && (
                <span className="ml-auto text-xs text-muted-foreground">{'✓'}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Priority */}
      <P.Sub>
        <P.SubTrigger>
          <PriorityIcon priority={issue.priority} />
          {t(($) => $.actions.priority)}
        </P.SubTrigger>
        <P.SubContent>
          {PRIORITY_ORDER.map((p) => (
            <P.Item key={p} onClick={() => updateField({ priority: p })}>
              <span
                className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
              >
                <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                {t(($) => $.priority[p])}
              </span>
              {issue.priority === p && (
                <span className="ml-auto text-xs text-muted-foreground">{'✓'}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Assignee — closes this menu and hands off to the shared
          AssigneePicker (members + agents + squads, with search and
          permission checks). Keeps a single source of truth for the
          assignee UX across detail sidebar, board cards, and right-click /
          3-dot menus. */}
      <P.Item onClick={onOpenAssignee}>
        <UserMinus className="h-3.5 w-3.5" />
        {t(($) => $.actions.assignee)}
      </P.Item>

      {/* Start date */}
      <P.Sub>
        <P.SubTrigger>
          <CalendarClock className="h-3.5 w-3.5" />
          {t(($) => $.actions.start_date)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={() => updateField({ start_date: todayDateOnly() })}>
            {t(($) => $.actions.start_today)}
          </P.Item>
          <P.Item onClick={() => updateField({ start_date: addDaysDateOnly(1) })}>
            {t(($) => $.actions.start_tomorrow)}
          </P.Item>
          <P.Item onClick={() => updateField({ start_date: addDaysDateOnly(7) })}>
            {t(($) => $.actions.start_next_week)}
          </P.Item>
          {issue.start_date && (
            <>
              <P.Separator />
              <P.Item onClick={() => updateField({ start_date: null })}>
                {t(($) => $.actions.start_clear)}
              </P.Item>
            </>
          )}
        </P.SubContent>
      </P.Sub>

      {/* Due date */}
      <P.Sub>
        <P.SubTrigger>
          <Calendar className="h-3.5 w-3.5" />
          {t(($) => $.actions.due_date)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={() => updateField({ due_date: todayDateOnly() })}>
            {t(($) => $.actions.due_today)}
          </P.Item>
          <P.Item onClick={() => updateField({ due_date: addDaysDateOnly(1) })}>
            {t(($) => $.actions.due_tomorrow)}
          </P.Item>
          <P.Item onClick={() => updateField({ due_date: addDaysDateOnly(7) })}>
            {t(($) => $.actions.due_next_week)}
          </P.Item>
          {issue.due_date && (
            <>
              <P.Separator />
              <P.Item onClick={() => updateField({ due_date: null })}>
                {t(($) => $.actions.due_clear)}
              </P.Item>
            </>
          )}
        </P.SubContent>
      </P.Sub>

      <P.Separator />

      <P.Item onClick={togglePin}>
        {isPinned ? <PinOff className="h-3.5 w-3.5" /> : <Pin className="h-3.5 w-3.5" />}
        {isPinned ? t(($) => $.actions.unpin_from_sidebar) : t(($) => $.actions.pin_to_sidebar)}
      </P.Item>
      <P.Item onClick={copyLink}>
        <Link2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.copy_link)}
      </P.Item>
      <P.Item onClick={handleCopyWorkdirPath}>
        <FolderOpen className="h-3.5 w-3.5" />
        {t(($) => $.actions.copy_workdir_path)}
      </P.Item>

      <P.Separator />

      {/* Relationship actions live under "Relations" — a semantically explicit
          label (unlike the old "More") so the first level tells you what the
          submenu does. Holds parent/sub-issue links today, and will grow
          (blocks, duplicates, related) as we add more relation types. */}
      <P.Sub>
        <P.SubTrigger>
          <Network className="h-3.5 w-3.5" />
          {t(($) => $.actions.relations)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={openCreateSubIssue}>
            <Plus className="h-3.5 w-3.5" />
            {t(($) => $.actions.create_sub_issue)}
          </P.Item>
          <P.Item onClick={openSetParent}>
            <ArrowUp className="h-3.5 w-3.5" />
            {t(($) => $.actions.set_parent_issue)}
          </P.Item>
          {issue.parent_issue_id && (
            <P.Item onClick={removeParent}>
              <Unlink className="h-3.5 w-3.5" />
              {t(($) => $.actions.remove_parent_issue)}
            </P.Item>
          )}
          <P.Item onClick={openAddChild}>
            <ArrowDown className="h-3.5 w-3.5" />
            {t(($) => $.actions.add_sub_issue)}
          </P.Item>
        </P.SubContent>
      </P.Sub>

      <P.Separator />

      <P.Item variant="destructive" onClick={() => openDeleteConfirm({ onDeletedFallbackPath })}>
        <Trash2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.delete_issue)}
      </P.Item>
    </>
  );
}

function pickLatestWorkDir(tasks: AgentTask[] | undefined): string | undefined {
  if (!tasks?.length) return undefined;
  let latest: AgentTask | undefined;
  for (const task of tasks) {
    if (!task.work_dir) continue;
    if (!latest || task.created_at > latest.created_at) {
      latest = task;
    }
  }
  return latest?.work_dir;
}

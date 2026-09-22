import type { IssueStatus } from './issue';

export type InboxSeverity = 'action_required' | 'attention' | 'info';

export type InboxItemType =
  | 'issue_assigned'
  | 'issue_subscribed'
  | 'unassigned'
  | 'assignee_changed'
  | 'status_changed'
  | 'priority_changed'
  | 'start_date_changed'
  | 'due_date_changed'
  | 'new_comment'
  | 'mentioned'
  | 'review_requested'
  | 'task_completed'
  | 'task_failed'
  | 'agent_blocked'
  | 'agent_completed'
  | 'reaction_added'
  | 'quick_create_done'
  | 'quick_create_failed'
  // Quick create whose outcome could not be verified. Distinct from
  // quick_create_failed because it must NOT be rendered with failure framing:
  // the issue may actually have been created.
  | 'quick_create_unconfirmed';

export interface InboxWorkspaceUnread {
  workspace_id: string;
  count: number;
}

export interface InboxItem {
  id: string;
  workspace_id: string;
  recipient_type: 'member' | 'agent';
  recipient_id: string;
  actor_type: 'member' | 'agent' | 'system' | null;
  actor_id: string | null;
  type: InboxItemType;
  severity: InboxSeverity;
  issue_id: string | null;
  title: string;
  body: string | null;
  issue_status: IssueStatus | null;
  read: boolean;
  archived: boolean;
  created_at: string;
  details: Record<string, string> | null;
}

import type { Label } from './label';
import type { IssuePropertyValues } from './property';

export type IssueStatus =
  'backlog' | 'todo' | 'in_progress' | 'in_review' | 'done' | 'blocked' | 'cancelled';

export type IssuePriority = 'urgent' | 'high' | 'medium' | 'low' | 'none';

export type IssueAssigneeType = 'member' | 'agent' | 'squad';

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

export type IssueMetadataValue = string | number | boolean;
export type IssueMetadata = Record<string, IssueMetadataValue>;

export interface Issue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  description: string | null;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  creator_type: IssueAssigneeType;
  creator_id: string;
  parent_issue_id: string | null;
  project_id: string | null;
  position: number;
  stage: number | null;
  start_date: string | null;
  due_date: string | null;
  metadata: IssueMetadata;
  properties: IssuePropertyValues;
  reactions?: IssueReaction[];
  labels?: Label[];
  created_at: string;
  updated_at: string;
}

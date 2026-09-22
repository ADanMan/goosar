import type { IssueScope } from '@goosar/core/issues/surface/scope';
import type { CreateIssueRequest } from '@goosar/core/types';
import type { ViewMode } from '@goosar/core/issues/stores/view-store';

export type IssueCreateDefaults = Partial<
  Omit<CreateIssueRequest, 'assignee_type' | 'assignee_id' | 'parent_issue_id' | 'project_id'>
> & {
  assignee_type?: CreateIssueRequest['assignee_type'] | null;
  assignee_id?: string | null;
  parent_issue_id?: string | null;
  parent_issue_identifier?: string;
  project_id?: string | null;
};

export type IssueSurfaceMode = Extract<ViewMode, 'board' | 'list' | 'table' | 'swimlane' | 'gantt'>;

export interface IssueSurfaceProps {
  scope: IssueScope;
  modes: IssueSurfaceMode[];
  surfaceKey?: string;
  createDefaults?: IssueCreateDefaults;
  search?: string;
}

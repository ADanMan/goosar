import type { Issue, IssueMetadata, IssueStatus, IssuePriority, IssueAssigneeType } from './issue';
import type { MemberRole } from './workspace';
import type { Project } from './project';

export interface CreateIssueRequest {
  title: string;
  description?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_type?: IssueAssigneeType;
  assignee_id?: string;
  parent_issue_id?: string;
  project_id?: string;
  stage?: number;
  start_date?: string;
  due_date?: string;
  attachment_ids?: string[];
  label_ids?: string[];
}

export interface UpdateIssueRequest {
  title?: string;
  description?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_type?: IssueAssigneeType | null;
  assignee_id?: string | null;
  position?: number;
  start_date?: string | null;
  due_date?: string | null;
  parent_issue_id?: string | null;
  project_id?: string | null;
  stage?: number | null;
  attachment_ids?: string[];
  suppress_run?: boolean;
  handoff_note?: string;
}

export interface MoveIssueRequest extends Pick<
  UpdateIssueRequest,
  'status' | 'assignee_type' | 'assignee_id' | 'parent_issue_id' | 'project_id'
> {
  before_id: string | null;
  after_id: string | null;
}

export interface IssueTriggerPreviewParams {
  issueIds?: string[];
  isCreate?: boolean;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
  status?: IssueStatus;
}

export interface IssueTriggerPreviewItem {
  issue_id: string;
  agent_id: string;
  source: string;
  handoff_supported: boolean;
}

export interface IssueTriggerPreview {
  triggers: IssueTriggerPreviewItem[];
  total_count: number;
}

export interface ListIssuesParams {
  limit?: number;
  offset?: number;
  workspace_id?: string;
  q?: string;
  status?: IssueStatus;
  statuses?: IssueStatus[];
  priority?: IssuePriority;
  priorities?: IssuePriority[];
  assignee_id?: string;
  assignee_ids?: string[];
  assignee_types?: IssueAssigneeType[];
  creator_id?: string;
  project_id?: string;
  assignee_filters?: IssueActorRef[];
  include_no_assignee?: boolean;
  creator_filters?: IssueActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  top_level_only?: boolean;
  ids?: string[];
  involves_user_id?: string;
  metadata?: IssueMetadata;
  properties?: Record<string, string[]>;
  open_only?: boolean;
  scheduled?: boolean;
  date_field?: 'created_at' | 'updated_at';
  date_start?: string;
  date_end?: string;
  sort_by?:
    | 'position'
    | 'status'
    | 'priority'
    | 'title'
    | 'created_at'
    | 'updated_at'
    | 'start_date'
    | 'due_date'
    | `property:${string}`;
  sort_direction?: 'asc' | 'desc';
}

export interface IssueActorRef {
  type: IssueAssigneeType;
  id: string;
}

export interface ListGroupedIssuesParams {
  group_by: 'assignee';
  limit?: number;
  offset?: number;
  workspace_id?: string;
  statuses?: IssueStatus[];
  priorities?: IssuePriority[];
  assignee_types?: IssueAssigneeType[];
  assignee_id?: string;
  assignee_ids?: string[];
  creator_id?: string;
  project_id?: string;
  involves_user_id?: string;
  metadata?: IssueMetadata;
  properties?: Record<string, string[]>;
  assignee_filters?: IssueActorRef[];
  include_no_assignee?: boolean;
  creator_filters?: IssueActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  group_assignee_type?: IssueAssigneeType | 'none';
  group_assignee_id?: string;
  date_field?: 'created_at' | 'updated_at';
  date_start?: string;
  date_end?: string;
  sort_by?:
    | 'position'
    | 'status'
    | 'priority'
    | 'title'
    | 'created_at'
    | 'updated_at'
    | 'start_date'
    | 'due_date'
    | `property:${string}`;
  sort_direction?: 'asc' | 'desc';
}

export interface ListIssuesResponse {
  issues: Issue[];
  total: number;
}

export interface IssueMetadataResponse {
  metadata: IssueMetadata;
}

export interface IssueAssigneeGroup {
  id: string;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  issues: Issue[];
  total: number;
}

export interface GroupedIssuesResponse {
  groups: IssueAssigneeGroup[];
}

export type IssueTableScope =
  | { kind: 'workspace'; assignee_types?: IssueAssigneeType[] }
  | { kind: 'project'; project_id: string }
  | { kind: 'assignee'; actor: IssueActorRef }
  | { kind: 'creator'; actor: IssueActorRef }
  | { kind: 'my'; relation: 'assigned' | 'created' | 'involved' | 'any' };

export interface IssueTableFilters {
  statuses?: IssueStatus[];
  priorities?: IssuePriority[];
  assignees?: IssueActorRef[];
  include_no_assignee?: boolean;
  creators?: IssueActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  properties?: Record<string, string[]>;
  date?: {
    field: 'created_at' | 'updated_at';
    start: string;
    end: string;
  };
  working_only?: boolean;
  working_issue_ids?: string[];
  include_sub_issues?: boolean;
}

export type IssueTableSortField =
  | 'position'
  | 'status'
  | 'priority'
  | 'title'
  | 'created_at'
  | 'updated_at'
  | 'start_date'
  | 'due_date'
  | `property:${string}`;

export interface IssueTableQuerySpec {
  scope: IssueTableScope;
  filters: IssueTableFilters;
  search?: string;
  sort: {
    field: IssueTableSortField;
    direction: 'asc' | 'desc';
  };
}

export type IssueTableGroupSpec =
  | { kind: 'none' }
  | { kind: 'status' }
  | { kind: 'assignee' }
  | { kind: 'project' }
  | { kind: 'parent' }
  | {
      kind: 'compound';
      primary: 'assignee' | 'project' | 'parent';
      secondary: 'status';
      secondary_values?: IssueStatus[];
    }
  | { kind: 'property'; property_id: string; include_empty?: boolean };

export interface IssueTableActorRef {
  type: string;
  id: string;
}

export interface IssueTableParentRef {
  id: string;
  number: number;
  identifier: string;
  title: string;
  status: string;
}

export type IssueTableGroupValue =
  | { kind: 'status'; status: string }
  | { kind: 'assignee'; actor: IssueTableActorRef | null }
  | { kind: 'project'; project_id: string | null }
  | {
      kind: 'parent';
      parent_id: string | null;
      parent: IssueTableParentRef | null;
      value_state: 'value' | 'unavailable' | 'unset';
    }
  | {
      kind: 'property';
      property_id: string;
      value?: string | boolean | null;
      value_state: 'value' | 'unavailable' | 'unset';
    };

export interface IssueTableGroupDescriptor {
  key: string;
  value: IssueTableGroupValue;
  count: number;
  secondary_groups?: IssueTableGroupDescriptor[];
}

export interface IssueTablePageRequest {
  limit?: number;
  cursor?: string | null;
}

export interface IssueTableGroupsRequest {
  query: IssueTableQuerySpec;
  group: Exclude<IssueTableGroupSpec, { kind: 'none' }>;
  page?: IssueTablePageRequest;
}

export interface IssueTableGroupsResponse {
  query_fingerprint: string;
  total: number;
  groups: IssueTableGroupDescriptor[];
  next_cursor: string | null;
}

export interface IssueTableRowsRequest {
  query: IssueTableQuerySpec;
  group: IssueTableGroupSpec;
  group_key: string | null;
  hierarchy: { enabled: boolean };
  parent_id: string | null;
  page?: IssueTablePageRequest;
}

export interface IssueTableRow {
  issue: Issue;
  direct_child_count: number;
}

export interface IssueTableRowsResponse {
  query_fingerprint: string;
  group_key: string | null;
  parent_id: string | null;
  total: number;
  rows: IssueTableRow[];
  branch_total: number;
  next_cursor: string | null;
}

export type IssueTableFacetSpec =
  | { kind: 'status' }
  | { kind: 'priority' }
  | { kind: 'assignee' }
  | { kind: 'creator' }
  | { kind: 'project' }
  | { kind: 'label' }
  | { kind: 'property'; property_id: string };

export interface IssueTableFacetsRequest {
  query: IssueTableQuerySpec;
  facets: IssueTableFacetSpec[];
  include_total?: boolean;
}

export interface IssueTableFacetValue {
  key: string;
  count: number;
}

export interface IssueTableFacet {
  kind: IssueTableFacetSpec['kind'];
  property_id?: string;
  values: IssueTableFacetValue[];
}

export interface IssueTableFacetsResponse {
  query_fingerprint: string;
  total: number;
  facets: IssueTableFacet[];
}

export interface IssueStatusBucket {
  issues: Issue[];
  total: number;
}

export interface ListIssuesCache {
  byStatus: Partial<Record<IssueStatus, IssueStatusBucket>>;
}

export interface SearchIssueResult extends Issue {
  match_source: 'title' | 'description' | 'comment';
  matched_snippet?: string;
  matched_description_snippet?: string;
  matched_comment_snippet?: string;
}

export interface SearchIssuesResponse {
  issues: SearchIssueResult[];
  total: number;
}

export interface SearchProjectResult extends Project {
  match_source: 'title' | 'description';
  matched_snippet?: string;
}

export interface SearchProjectsResponse {
  projects: SearchProjectResult[];
  total: number;
}

export interface UpdateMeRequest {
  name?: string;
  avatar_url?: string;
  language?: string;
  profile_description?: string;
  timezone?: string;
}

export interface CreateMemberRequest {
  email: string;
  role?: MemberRole;
}

export interface UpdateMemberRequest {
  role?: MemberRole;
  perimeter_access?: boolean;
}

export interface PersonalAccessToken {
  id: string;
  name: string;
  token_prefix: string;
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
}

export interface CreatePersonalAccessTokenRequest {
  name: string;
  expires_in_days?: number;
}

export interface CreatePersonalAccessTokenResponse extends PersonalAccessToken {
  token: string;
}

export interface PaginationParams {
  limit?: number;
  offset?: number;
}

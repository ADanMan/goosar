import type { Issue, UpdateIssueRequest } from '../../types';
import type { MyIssuesFilter } from '../queries';

export type IssueMembership = true | false | 'unknown';

export interface IssueChangedDims {
  assignee: boolean;
  project: boolean;
  status: boolean;
}

export function issueChangedDims(
  patch: Partial<Issue> | UpdateIssueRequest,
  base?: Issue,
): IssueChangedDims {
  const has = (field: string) => Object.prototype.hasOwnProperty.call(patch, field);
  const p = patch as Partial<Issue>;
  return {
    assignee:
      (has('assignee_id') && (!base || base.assignee_id !== p.assignee_id)) ||
      (has('assignee_type') && (!base || base.assignee_type !== p.assignee_type)),
    project: has('project_id') && (!base || base.project_id !== p.project_id),
    status: has('status') && p.status !== undefined && (!base || base.status !== p.status),
  };
}

export function listFilterDependsOn(
  scope: string | undefined,
  filter: MyIssuesFilter,
  changed: IssueChangedDims,
): boolean {
  if (scope === 'all') return changed.assignee;
  if (
    changed.assignee &&
    (filter.assignee_id !== undefined ||
      filter.assignee_ids !== undefined ||
      filter.assignee_types !== undefined ||
      filter.involves_user_id !== undefined)
  ) {
    return true;
  }
  if (changed.project && filter.project_id !== undefined) return true;
  return false;
}

export function issueMatchesListFilter(
  issue: Partial<Issue>,
  scope: string | undefined,
  filter: MyIssuesFilter,
): IssueMembership {
  if (scope === 'all') return 'unknown';

  let unknown = false;

  if (filter.assignee_id !== undefined) {
    if (issue.assignee_id === undefined) unknown = true;
    else if (issue.assignee_id !== filter.assignee_id) return false;
  }
  if (filter.assignee_ids !== undefined) {
    if (issue.assignee_id === undefined) unknown = true;
    else if (issue.assignee_id === null || !filter.assignee_ids.includes(issue.assignee_id)) {
      return false;
    }
  }
  if (filter.assignee_types !== undefined) {
    if (issue.assignee_type === undefined) unknown = true;
    else if (issue.assignee_type === null || !filter.assignee_types.includes(issue.assignee_type)) {
      return false;
    }
  }
  if (filter.creator_id !== undefined) {
    if (issue.creator_id === undefined) unknown = true;
    else if (issue.creator_id !== filter.creator_id) return false;
  }
  if (filter.project_id !== undefined) {
    if (issue.project_id === undefined) unknown = true;
    else if (issue.project_id !== filter.project_id) return false;
  }
  if (filter.involves_user_id !== undefined) {
    unknown = true;
  }

  return unknown ? 'unknown' : true;
}

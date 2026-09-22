import type { UseQueryOptions } from '@tanstack/react-query';
import {
  issueAssigneeGroupsOptions,
  issueFlatExportOptions,
  issueFlatListOptions,
  issueListOptions,
  myIssueAssigneeGroupsOptions,
  myIssueListOptions,
  projectGanttIssuesOptions,
  type AssigneeGroupedIssuesFilter,
  type IssueFlatFilter,
  type IssueSortParam,
} from '../queries';
import type { GroupedIssuesResponse, Issue, ListIssuesCache } from '../../types';
import type { IssueSurfaceQueryPlan } from './query-plan';

export function issueSurfaceListOptions(
  wsId: string,
  plan: IssueSurfaceQueryPlan,
  sort?: IssueSortParam,
): UseQueryOptions<ListIssuesCache, Error, Issue[]> {
  return (
    plan.kind === 'workspace'
      ? issueListOptions(wsId, sort)
      : myIssueListOptions(wsId, plan.queryScope, plan.queryFilter, plan.userId, sort)
  ) as UseQueryOptions<ListIssuesCache, Error, Issue[]>;
}

export function issueSurfaceFlatOptions(
  wsId: string,
  plan: IssueSurfaceQueryPlan,
  sort?: IssueSortParam,
  facets: IssueFlatFilter = {},
) {
  return issueFlatListOptions(
    wsId,
    plan.queryScope ?? plan.scopeKey,
    { ...plan.queryFilter, ...facets },
    plan.userId,
    sort,
  );
}

export function issueSurfaceFlatExportOptions(
  wsId: string,
  plan: IssueSurfaceQueryPlan,
  sort?: IssueSortParam,
  facets: IssueFlatFilter = {},
) {
  return issueFlatExportOptions(
    wsId,
    plan.queryScope ?? plan.scopeKey,
    { ...plan.queryFilter, ...facets },
    plan.userId,
    sort,
  );
}

export function issueSurfaceAssigneeGroupsOptions(
  wsId: string,
  plan: IssueSurfaceQueryPlan,
  filter: AssigneeGroupedIssuesFilter,
  sort?: IssueSortParam,
): UseQueryOptions<GroupedIssuesResponse> {
  return (
    plan.kind === 'workspace'
      ? issueAssigneeGroupsOptions(wsId, filter, sort)
      : myIssueAssigneeGroupsOptions(wsId, plan.queryScope, filter, plan.userId, sort)
  ) as UseQueryOptions<GroupedIssuesResponse>;
}

export function issueSurfaceGanttOptions(wsId: string, projectId: string) {
  return projectGanttIssuesOptions(wsId, projectId);
}

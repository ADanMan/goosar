import {
  infiniteQueryOptions,
  keepPreviousData,
  queryOptions,
  type QueryClient,
} from '@tanstack/react-query';
import { api } from '../api';
import type {
  GroupedIssuesResponse,
  Issue,
  IssueStatus,
  IssueTableFacetsRequest,
  IssueTableGroupSpec,
  IssueTableGroupsRequest,
  IssueTableQuerySpec,
  IssueTableRowsRequest,
  ListGroupedIssuesParams,
  ListIssuesParams,
  ListIssuesCache,
} from '../types';
import { ALL_STATUSES } from './config';

export interface IssueSortParam {
  sort_by?: ListIssuesParams['sort_by'];
  sort_direction?: ListIssuesParams['sort_direction'];
  date_field?: ListIssuesParams['date_field'];
  date_start?: ListIssuesParams['date_start'];
  date_end?: ListIssuesParams['date_end'];
  properties?: ListIssuesParams['properties'];
}

export const issueKeys = {
  all: (wsId: string) => ['issues', wsId] as const,
  list: (wsId: string) => [...issueKeys.all(wsId), 'list'] as const,
  listSorted: (wsId: string, sort?: IssueSortParam) =>
    [...issueKeys.list(wsId), sort ?? {}] as const,
  flatAll: (wsId: string) => [...issueKeys.all(wsId), 'flat'] as const,
  flat: (wsId: string, scope: string, filter: IssueFlatFilter, sort?: IssueSortParam) =>
    [...issueKeys.flatAll(wsId), scope, filter, sort ?? {}] as const,
  flatExport: (wsId: string, scope: string, filter: IssueFlatFilter, sort?: IssueSortParam) =>
    [...issueKeys.flatAll(wsId), 'export', scope, filter, sort ?? {}] as const,
  tableAll: (wsId: string) => [...issueKeys.all(wsId), 'table-query'] as const,
  tableGroups: (
    wsId: string,
    query: IssueTableQuerySpec,
    group: IssueTableGroupsRequest['group'],
  ) => [...issueKeys.tableAll(wsId), 'groups', query, group] as const,
  tableFacets: (wsId: string, request: IssueTableFacetsRequest) =>
    [...issueKeys.tableAll(wsId), 'facets', request] as const,
  tableRows: (
    wsId: string,
    query: IssueTableQuerySpec,
    group: IssueTableGroupSpec,
    groupKey: string | null,
    hierarchy: boolean,
    parentId: string | null,
  ) =>
    [...issueKeys.tableAll(wsId), 'rows', query, group, groupKey, { hierarchy, parentId }] as const,
  assigneeGroupsAll: (wsId: string) => [...issueKeys.all(wsId), 'assignee-groups'] as const,
  assigneeGroups: (wsId: string, filter: AssigneeGroupedIssuesFilter) =>
    [...issueKeys.assigneeGroupsAll(wsId), filter] as const,
  myAll: (wsId: string) => [...issueKeys.all(wsId), 'my'] as const,
  myList: (wsId: string, scope: string, filter: MyIssuesFilter) =>
    [...issueKeys.myAll(wsId), scope, filter] as const,
  myListSorted: (wsId: string, scope: string, filter: MyIssuesFilter, sort?: IssueSortParam) =>
    [...issueKeys.myList(wsId, scope, filter), sort ?? {}] as const,
  myAssigneeGroupsAll: (wsId: string) => [...issueKeys.myAll(wsId), 'assignee-groups'] as const,
  myAssigneeGroups: (wsId: string, scope: string, filter: AssigneeGroupedIssuesFilter) =>
    [...issueKeys.myAssigneeGroupsAll(wsId), scope, filter] as const,
  projectGanttAll: (wsId: string) => [...issueKeys.all(wsId), 'project-gantt'] as const,
  projectGantt: (wsId: string, projectId: string) =>
    [...issueKeys.projectGanttAll(wsId), projectId] as const,
  detail: (wsId: string, id: string) => [...issueKeys.all(wsId), 'detail', id] as const,
  identifier: (wsId: string, identifier: string) =>
    [...issueKeys.all(wsId), 'identifier', identifier] as const,
  childrenAll: (wsId: string) => [...issueKeys.all(wsId), 'children'] as const,
  children: (wsId: string, id: string) => [...issueKeys.childrenAll(wsId), id] as const,
  childrenByParentsAll: (wsId: string) => [...issueKeys.all(wsId), 'children-by-parents'] as const,
  childrenByParents: (wsId: string, parentIds: readonly string[]) =>
    [...issueKeys.childrenByParentsAll(wsId), parentIds] as const,
  childProgress: (wsId: string) => [...issueKeys.all(wsId), 'child-progress'] as const,
  timelineAll: () => ['issues', 'timeline'] as const,
  timeline: (issueId: string) => [...issueKeys.timelineAll(), issueId] as const,
  commentTriggerPreviewAll: () => ['issues', 'comment-trigger-preview'] as const,
  commentTriggerPreview: (issueId: string) =>
    [...issueKeys.commentTriggerPreviewAll(), issueId] as const,
  issueTriggerPreviewAll: () => ['issues', 'issue-trigger-preview'] as const,
  issueTriggerPreview: (signature: string) =>
    [...issueKeys.issueTriggerPreviewAll(), signature] as const,
  reactionsAll: () => ['issues', 'reactions'] as const,
  reactions: (issueId: string) => [...issueKeys.reactionsAll(), issueId] as const,
  subscribersAll: () => ['issues', 'subscribers'] as const,
  subscribers: (issueId: string) => [...issueKeys.subscribersAll(), issueId] as const,
  usageAll: () => ['issues', 'usage'] as const,
  usage: (issueId: string) => [...issueKeys.usageAll(), issueId] as const,
  attachmentsAll: () => ['issues', 'attachments'] as const,
  attachments: (issueId: string) => [...issueKeys.attachmentsAll(), issueId] as const,
  tasksAll: () => ['issues', 'tasks'] as const,
  tasks: (issueId: string) => [...issueKeys.tasksAll(), issueId] as const,
};

export type MyIssuesFilter = Pick<
  ListIssuesParams,
  | 'assignee_id'
  | 'assignee_ids'
  | 'assignee_types'
  | 'creator_id'
  | 'project_id'
  | 'involves_user_id'
>;

export type IssueFlatFilter = MyIssuesFilter &
  Pick<
    ListIssuesParams,
    | 'q'
    | 'statuses'
    | 'priorities'
    | 'assignee_filters'
    | 'include_no_assignee'
    | 'creator_filters'
    | 'project_ids'
    | 'include_no_project'
    | 'label_ids'
    | 'top_level_only'
    | 'ids'
  >;

export type AssigneeGroupedIssuesFilter = Omit<
  ListGroupedIssuesParams,
  'group_by' | 'limit' | 'offset' | 'group_assignee_type' | 'group_assignee_id'
>;

export const ISSUE_PAGE_SIZE = 50;
export const ISSUE_FLAT_PAGE_SIZE = 100;

export const PAGINATED_STATUSES: readonly IssueStatus[] = ALL_STATUSES;

export function flattenIssueBuckets(data: ListIssuesCache) {
  const out = [];
  for (const status of PAGINATED_STATUSES) {
    const bucket = data.byStatus[status];
    if (bucket) out.push(...bucket.issues);
  }
  return out;
}

async function fetchFirstPages(
  filter: MyIssuesFilter = {},
  sort?: IssueSortParam,
): Promise<ListIssuesCache> {
  const responses = await Promise.all(
    PAGINATED_STATUSES.map((status) =>
      api.listIssues({ status, limit: ISSUE_PAGE_SIZE, offset: 0, ...sort, ...filter }),
    ),
  );
  const byStatus: ListIssuesCache['byStatus'] = {};
  PAGINATED_STATUSES.forEach((status, i) => {
    const res = responses[i]!;
    byStatus[status] = { issues: res.issues, total: res.total };
  });
  return { byStatus };
}

const MERGE_PRIORITY_RANK: Record<string, number> = {
  urgent: 0,
  high: 1,
  medium: 2,
  low: 3,
  none: 4,
};
const MERGE_STATUS_RANK: Record<string, number> = {
  backlog: 0,
  todo: 1,
  in_progress: 2,
  in_review: 3,
  done: 4,
  blocked: 5,
  cancelled: 6,
};

export function compareIssuesForSort(a: Issue, b: Issue, sort?: IssueSortParam): number {
  const by = sort?.sort_by ?? 'position';
  const dir = by !== 'position' && sort?.sort_direction === 'desc' ? -1 : 1;
  const tieBreak = () =>
    new Date(b.created_at).getTime() - new Date(a.created_at).getTime() ||
    (a.id < b.id ? 1 : a.id > b.id ? -1 : 0);

  const missingAware = (av: string | null, bv: string | null): number => {
    if (!av && !bv) return tieBreak();
    if (!av) return 1;
    if (!bv) return -1;
    return dir * av.localeCompare(bv) || tieBreak();
  };

  if (by.startsWith('property:')) {
    const propertyId = by.slice('property:'.length);
    const av = a.properties?.[propertyId];
    const bv = b.properties?.[propertyId];
    const aMissing = av === undefined || Array.isArray(av) || typeof av === 'boolean';
    const bMissing = bv === undefined || Array.isArray(bv) || typeof bv === 'boolean';
    if (aMissing && bMissing) return tieBreak();
    if (aMissing) return 1;
    if (bMissing) return -1;
    if (typeof av === 'number' && typeof bv === 'number') return dir * (av - bv) || tieBreak();
    return dir * String(av).localeCompare(String(bv)) || tieBreak();
  }
  switch (by) {
    case 'status':
      return (
        dir * ((MERGE_STATUS_RANK[a.status] ?? 9) - (MERGE_STATUS_RANK[b.status] ?? 9)) ||
        tieBreak()
      );
    case 'priority':
      return (
        dir * ((MERGE_PRIORITY_RANK[a.priority] ?? 9) - (MERGE_PRIORITY_RANK[b.priority] ?? 9)) ||
        tieBreak()
      );
    case 'title':
      return dir * a.title.localeCompare(b.title) || tieBreak();
    case 'created_at':
      return (
        dir * (new Date(a.created_at).getTime() - new Date(b.created_at).getTime()) || tieBreak()
      );
    case 'updated_at':
      return (
        dir * (new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime()) || tieBreak()
      );
    case 'start_date':
      return missingAware(a.start_date, b.start_date);
    case 'due_date':
      return missingAware(a.due_date, b.due_date);
    case 'position':
    default:
      return a.position - b.position || tieBreak();
  }
}

async function fetchAllFlatPages(filter: IssueFlatFilter, sort?: IssueSortParam): Promise<Issue[]> {
  const issues: Issue[] = [];
  const seenIds = new Set<string>();
  let offset = 0;
  while (true) {
    const response = await api.listIssues({
      ...filter,
      ...sort,
      limit: ISSUE_FLAT_PAGE_SIZE,
      offset,
    });
    let added = 0;
    for (const issue of response.issues) {
      if (seenIds.has(issue.id)) continue;
      seenIds.add(issue.id);
      issues.push(issue);
      added += 1;
    }
    if (issues.length >= response.total) break;
    if (response.issues.length === 0 || added === 0) {
      throw new Error('Issue export pagination did not advance');
    }
    offset += response.issues.length;
  }
  return issues;
}

async function fetchAllMyFlatIssues(
  userId: string,
  filter: IssueFlatFilter,
  sort?: IssueSortParam,
): Promise<Issue[]> {
  const relations = await Promise.all([
    fetchAllFlatPages({ ...filter, assignee_id: userId }, sort),
    fetchAllFlatPages({ ...filter, creator_id: userId }, sort),
    fetchAllFlatPages({ ...filter, involves_user_id: userId }, sort),
  ]);
  const byId = new Map<string, Issue>();
  for (const issues of relations) {
    for (const issue of issues) byId.set(issue.id, issue);
  }
  return [...byId.values()].sort((a, b) => compareIssuesForSort(a, b, sort));
}

export function issueFlatListOptions(
  wsId: string,
  scope: string,
  filter: IssueFlatFilter,
  userId?: string,
  sort?: IssueSortParam,
) {
  const allMyIssues = scope === 'all' && !!userId;
  return infiniteQueryOptions({
    queryKey: issueKeys.flat(wsId, scope, filter, sort),
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      if (allMyIssues) {
        const issues = await fetchAllMyFlatIssues(userId, filter, sort);
        return { issues, total: issues.length };
      }
      return api.listIssues({
        ...filter,
        ...sort,
        limit: ISSUE_FLAT_PAGE_SIZE,
        offset: pageParam,
      });
    },
    getNextPageParam: (lastPage, allPages) => {
      if (allMyIssues) return undefined;
      const loaded = allPages.reduce((count, page) => count + page.issues.length, 0);
      return loaded < lastPage.total ? loaded : undefined;
    },
    placeholderData: keepPreviousData,
  });
}

export function issueTableGroupsOptions(
  wsId: string,
  query: IssueTableQuerySpec,
  group: IssueTableGroupsRequest['group'],
) {
  return infiniteQueryOptions({
    queryKey: issueKeys.tableGroups(wsId, query, group),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) =>
      api.listIssueTableGroups({
        query,
        group,
        page: { limit: 100, cursor: pageParam },
      }),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
    retry: false,
  });
}

export function issueTableRowPageOptions(wsId: string, request: IssueTableRowsRequest) {
  const cursor = request.page?.cursor ?? null;
  return queryOptions({
    queryKey: [
      ...issueKeys.tableRows(
        wsId,
        request.query,
        request.group,
        request.group_key,
        request.hierarchy.enabled,
        request.parent_id,
      ),
      'page',
      cursor,
    ] as const,
    queryFn: () => api.listIssueTableRows(request),
    placeholderData: keepPreviousData,
    retry: false,
    retryOnMount: false,
    refetchOnMount: (query) => query.state.status !== 'error',
  });
}

export function issueTableFacetsOptions(wsId: string, request: IssueTableFacetsRequest) {
  return queryOptions({
    queryKey: issueKeys.tableFacets(wsId, request),
    queryFn: () => api.listIssueTableFacets(request),
  });
}

export function issueFlatExportOptions(
  wsId: string,
  scope: string,
  filter: IssueFlatFilter,
  userId?: string,
  sort?: IssueSortParam,
) {
  return queryOptions({
    queryKey: issueKeys.flatExport(wsId, scope, filter, sort),
    queryFn: () =>
      scope === 'all' && userId
        ? fetchAllMyFlatIssues(userId, filter, sort)
        : fetchAllFlatPages(filter, sort),
    staleTime: 0,
  });
}

async function fetchAllMyFirstPages(
  userId: string,
  sort?: IssueSortParam,
): Promise<ListIssuesCache> {
  const [byAssignee, byCreator, byInvolves] = await Promise.all([
    fetchFirstPages({ assignee_id: userId }, sort),
    fetchFirstPages({ creator_id: userId }, sort),
    fetchFirstPages({ involves_user_id: userId }, sort),
  ]);
  const byStatus: ListIssuesCache['byStatus'] = {};
  for (const status of PAGINATED_STATUSES) {
    const seen = new Set<string>();
    const merged: Issue[] = [];
    for (const cache of [byAssignee, byCreator, byInvolves]) {
      const bucket = cache.byStatus[status];
      if (!bucket) continue;
      for (const issue of bucket.issues) {
        if (seen.has(issue.id)) continue;
        seen.add(issue.id);
        merged.push(issue);
      }
    }
    merged.sort((a, b) => compareIssuesForSort(a, b, sort));
    byStatus[status] = { issues: merged, total: merged.length };
  }
  return { byStatus };
}

async function fetchAllMyAssigneeGroups(
  userId: string,
  filter: AssigneeGroupedIssuesFilter,
  sort?: IssueSortParam,
): Promise<GroupedIssuesResponse> {
  const variants: AssigneeGroupedIssuesFilter[] = [
    { ...filter, assignee_id: userId },
    { ...filter, creator_id: userId },
    { ...filter, involves_user_id: userId },
  ];
  const responses = await Promise.all(
    variants.map((f) =>
      api.listGroupedIssues({
        group_by: 'assignee',
        limit: ISSUE_PAGE_SIZE,
        offset: 0,
        ...sort,
        ...f,
      }),
    ),
  );
  const groupKey = (g: GroupedIssuesResponse['groups'][number]) =>
    `${g.assignee_type ?? '_'}::${g.assignee_id ?? '_'}`;
  const merged = new Map<string, GroupedIssuesResponse['groups'][number]>();
  for (const res of responses) {
    for (const group of res.groups) {
      const key = groupKey(group);
      const existing = merged.get(key);
      if (!existing) {
        merged.set(key, {
          ...group,
          issues: [...group.issues],
          total: group.issues.length,
        });
        continue;
      }
      const seen = new Set(existing.issues.map((i) => i.id));
      for (const issue of group.issues) {
        if (seen.has(issue.id)) continue;
        seen.add(issue.id);
        existing.issues.push(issue);
      }
      existing.total = existing.issues.length;
    }
  }
  const groups = [...merged.values()];
  for (const group of groups) {
    group.issues.sort((a, b) => compareIssuesForSort(a, b, sort));
  }
  return { groups };
}

export function issueListOptions(wsId: string, sort?: IssueSortParam) {
  return queryOptions({
    queryKey: issueKeys.listSorted(wsId, sort),
    queryFn: () => fetchFirstPages({}, sort),
    select: flattenIssueBuckets,
    placeholderData: keepPreviousData,
  });
}

export function issueAssigneeGroupsOptions(
  wsId: string,
  filter: AssigneeGroupedIssuesFilter,
  sort?: IssueSortParam,
) {
  return queryOptions<GroupedIssuesResponse>({
    queryKey: issueKeys.assigneeGroups(wsId, { ...filter, ...sort }),
    queryFn: () =>
      api.listGroupedIssues({
        group_by: 'assignee',
        limit: ISSUE_PAGE_SIZE,
        offset: 0,
        ...sort,
        ...filter,
      }),
    placeholderData: keepPreviousData,
  });
}

export function myIssueListOptions(
  wsId: string,
  scope: string,
  filter: MyIssuesFilter,
  userId?: string,
  sort?: IssueSortParam,
) {
  return queryOptions({
    queryKey: issueKeys.myListSorted(wsId, scope, filter, sort),
    queryFn: () =>
      scope === 'all' && userId
        ? fetchAllMyFirstPages(userId, sort)
        : fetchFirstPages(filter, sort),
    select: flattenIssueBuckets,
    placeholderData: keepPreviousData,
  });
}

export const PROJECT_GANTT_PAGE_LIMIT = 500;

export const PROJECT_GANTT_MAX_ISSUES = 10_000;

async function fetchProjectGanttIssues(projectId: string) {
  const issues = [];
  let offset = 0;
  while (offset < PROJECT_GANTT_MAX_ISSUES) {
    const res = await api.listIssues({
      project_id: projectId,
      scheduled: true,
      limit: PROJECT_GANTT_PAGE_LIMIT,
      offset,
    });
    issues.push(...res.issues);
    if (res.issues.length < PROJECT_GANTT_PAGE_LIMIT) break;
    if (issues.length >= res.total) break;
    offset += PROJECT_GANTT_PAGE_LIMIT;
  }
  return issues;
}

export function projectGanttIssuesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: issueKeys.projectGantt(wsId, projectId),
    queryFn: () => fetchProjectGanttIssues(projectId),
  });
}

export function myIssueAssigneeGroupsOptions(
  wsId: string,
  scope: string,
  filter: AssigneeGroupedIssuesFilter,
  userId?: string,
  sort?: IssueSortParam,
) {
  return queryOptions<GroupedIssuesResponse>({
    queryKey: issueKeys.myAssigneeGroups(wsId, scope, { ...filter, ...sort }),
    queryFn: () =>
      scope === 'all' && userId
        ? fetchAllMyAssigneeGroups(userId, filter, sort)
        : api.listGroupedIssues({
            group_by: 'assignee',
            limit: ISSUE_PAGE_SIZE,
            offset: 0,
            ...sort,
            ...filter,
          }),
    placeholderData: keepPreviousData,
  });
}

export function issueDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueKeys.detail(wsId, id),
    queryFn: () => api.getIssue(id),
  });
}

export function issueIdentifierOptions(wsId: string, identifier: string) {
  return queryOptions({
    queryKey: issueKeys.identifier(wsId, identifier),
    queryFn: async ({ signal }) => {
      const res = await api.searchIssues({
        q: identifier,
        limit: 10,
        include_closed: true,
        signal,
      });
      return res.issues.find((i) => i.identifier === identifier) ?? null;
    },
    staleTime: 5 * 60_000,
  });
}

export function childIssueProgressOptions(wsId: string) {
  return queryOptions({
    queryKey: issueKeys.childProgress(wsId),
    queryFn: () => api.getChildIssueProgress(),
    select: (data) => {
      const map = new Map<string, { done: number; total: number }>();
      for (const entry of data.progress) {
        map.set(entry.parent_issue_id, { done: entry.done, total: entry.total });
      }
      return map;
    },
  });
}

export function childIssuesOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueKeys.children(wsId, id),
    queryFn: () => api.listChildIssues(id).then((r) => r.issues),
    refetchOnMount: 'always',
  });
}

export const CHILDREN_BY_PARENTS_CHUNK_SIZE = 200;

async function fetchAndHydrateChildrenByParents(
  qc: QueryClient,
  wsId: string,
  parentIds: readonly string[],
) {
  const chunks: string[][] = [];
  for (let i = 0; i < parentIds.length; i += CHILDREN_BY_PARENTS_CHUNK_SIZE) {
    chunks.push([...parentIds.slice(i, i + CHILDREN_BY_PARENTS_CHUNK_SIZE)]);
  }
  const responses = await Promise.all(chunks.map((c) => api.listChildrenByParents(c)));
  const grouped = new Map<string, Issue[]>();
  for (const response of responses) {
    for (const issue of response.issues) {
      if (!issue.parent_issue_id) continue;
      const bucket = grouped.get(issue.parent_issue_id);
      if (bucket) {
        bucket.push(issue);
      } else {
        grouped.set(issue.parent_issue_id, [issue]);
      }
    }
  }
  for (const [parentId, children] of grouped) {
    const existing = qc.getQueryData<Issue[]>(issueKeys.children(wsId, parentId));
    if (!existing || existing.length === 0) {
      qc.setQueryData(issueKeys.children(wsId, parentId), children);
    }
  }
  return grouped;
}

export function childrenByParentsOptions(
  wsId: string,
  parentIds: readonly string[],
  qc: QueryClient,
) {
  return queryOptions({
    queryKey: issueKeys.childrenByParents(wsId, parentIds),
    queryFn: () => fetchAndHydrateChildrenByParents(qc, wsId, parentIds),
    enabled: parentIds.length > 0,
  });
}

export function issueTimelineOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.timeline(issueId),
    queryFn: () => api.listTimeline(issueId),
  });
}

export function issueReactionsOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.reactions(issueId),
    queryFn: async () => {
      const issue = await api.getIssue(issueId);
      return issue.reactions ?? [];
    },
  });
}

export function issueSubscribersOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.subscribers(issueId),
    queryFn: () => api.listIssueSubscribers(issueId),
  });
}

export function issueUsageOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.usage(issueId),
    queryFn: () => api.getIssueUsage(issueId),
  });
}

export function issueAttachmentsOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.attachments(issueId),
    queryFn: () => api.listAttachments(issueId),
  });
}

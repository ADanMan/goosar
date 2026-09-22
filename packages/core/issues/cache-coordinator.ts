import { hashKey, type InfiniteData, type QueryClient, type QueryKey } from '@tanstack/react-query';
import {
  issueKeys,
  type IssueFlatFilter,
  type IssueSortParam,
  type MyIssuesFilter,
} from './queries';
import { inboxKeys } from '../inbox/queries';
import { patchInboxIssueStatus } from '../inbox/ws-updaters';
import { projectKeys } from '../projects/queries';
import {
  decrementBucketTotal,
  findIssueLocation,
  moveBucketTotal,
  patchIssueInBuckets,
  removeIssueFromBuckets,
} from './cache-helpers';
import {
  issueMatchesListFilter,
  listFilterDependsOn,
  type IssueChangedDims,
} from './surface/membership';
import type {
  InboxItem,
  Issue,
  IssueTableRowsResponse,
  ListIssuesCache,
  ListIssuesResponse,
} from '../types';

export type IssueFlatCache = InfiniteData<ListIssuesResponse, number>;
export type IssueTableRowCache = IssueTableRowsResponse;

export interface IssueCacheChangeResult {
  prevLists: [QueryKey, ListIssuesCache][];
  prevFlatLists: [QueryKey, IssueFlatCache][];
  prevTableRows: [QueryKey, IssueTableRowCache][];
  prevDetail: Issue | undefined;
  prevInboxList: InboxItem[] | undefined;
  staleKeys: QueryKey[];
  prevIssue: Issue | undefined;
}

function listContractFromKey(key: QueryKey): {
  scope: string | undefined;
  filter: MyIssuesFilter;
  sort: IssueSortParam;
} {
  if (key[2] === 'my') {
    return {
      scope: typeof key[3] === 'string' ? key[3] : undefined,
      filter: (key[4] ?? {}) as MyIssuesFilter,
      sort: (key[5] ?? {}) as IssueSortParam,
    };
  }
  return {
    scope: undefined,
    filter: {},
    sort: (key[3] ?? {}) as IssueSortParam,
  };
}

function bucketedListEntries(qc: QueryClient, wsId: string): [QueryKey, ListIssuesCache][] {
  return [
    ...qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) }),
    ...qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.myAll(wsId) }),
  ].filter((entry): entry is [QueryKey, ListIssuesCache] => !!entry[1]?.byStatus);
}

function flatListEntries(qc: QueryClient, wsId: string): [QueryKey, IssueFlatCache][] {
  return qc
    .getQueriesData<IssueFlatCache>({ queryKey: issueKeys.flatAll(wsId) })
    .filter(
      (entry): entry is [QueryKey, IssueFlatCache] => !!entry[1] && Array.isArray(entry[1].pages),
    );
}

function tableRowEntries(qc: QueryClient, wsId: string): [QueryKey, IssueTableRowCache][] {
  return qc
    .getQueriesData<unknown>({ queryKey: issueKeys.tableAll(wsId) })
    .filter(
      (entry): entry is [QueryKey, IssueTableRowCache] =>
        !!entry[1] &&
        typeof entry[1] === 'object' &&
        Array.isArray((entry[1] as IssueTableRowCache).rows),
    );
}

function flatContractFromKey(key: QueryKey): {
  scope: string | undefined;
  filter: IssueFlatFilter;
  sort: IssueSortParam;
} {
  return {
    scope: typeof key[3] === 'string' ? key[3] : undefined,
    filter: (key[4] ?? {}) as IssueFlatFilter,
    sort: (key[5] ?? {}) as IssueSortParam,
  };
}

function patchFieldChanged<K extends keyof Issue>(
  patch: Partial<Issue>,
  base: Issue | undefined,
  field: K,
) {
  if (!Object.prototype.hasOwnProperty.call(patch, field)) return false;
  return !base || !Object.is(patch[field], base[field]);
}

function patchChangesAnyIssueField(patch: Partial<Issue>, base: Issue | undefined): boolean {
  return Object.keys(patch).some((field) => patchFieldChanged(patch, base, field as keyof Issue));
}

function flatWindowNeedsReconcile(
  key: QueryKey,
  patch: Partial<Issue>,
  base: Issue | undefined,
  changed: IssueChangedDims,
) {
  const { scope, filter, sort } = flatContractFromKey(key);
  const anyIssueFieldChanged = patchChangesAnyIssueField(patch, base);

  if (listFilterDependsOn(scope, filter, changed)) return true;
  if (filter.q && patchFieldChanged(patch, base, 'title')) return true;
  if (changed.status && (filter.statuses?.length ?? 0) > 0) return true;
  if (patchFieldChanged(patch, base, 'priority') && (filter.priorities?.length ?? 0) > 0) {
    return true;
  }
  if (
    changed.assignee &&
    ((filter.assignee_filters?.length ?? 0) > 0 || filter.include_no_assignee)
  ) {
    return true;
  }
  if (changed.project && ((filter.project_ids?.length ?? 0) > 0 || filter.include_no_project)) {
    return true;
  }
  if (patchFieldChanged(patch, base, 'parent_issue_id') && filter.top_level_only) {
    return true;
  }
  if (sort.date_field === 'updated_at' && anyIssueFieldChanged) return true;
  if (sort.date_field === 'created_at' && patchFieldChanged(patch, base, 'created_at')) {
    return true;
  }

  switch (sort.sort_by ?? 'position') {
    case 'title':
      return patchFieldChanged(patch, base, 'title');
    case 'status':
      return changed.status;
    case 'priority':
      return patchFieldChanged(patch, base, 'priority');
    case 'created_at':
      return patchFieldChanged(patch, base, 'created_at');
    case 'updated_at':
      return anyIssueFieldChanged;
    case 'start_date':
      return patchFieldChanged(patch, base, 'start_date');
    case 'due_date':
      return patchFieldChanged(patch, base, 'due_date');
    case 'position':
      return patchFieldChanged(patch, base, 'position');
    default:
      return false;
  }
}

export function applyIssueChange(
  qc: QueryClient,
  wsId: string,
  id: string,
  patch: Partial<Issue>,
  opts: {
    changed: IssueChangedDims;
    baseIssue?: Issue;
  },
): IssueCacheChangeResult {
  const { changed, baseIssue } = opts;
  const prevLists: [QueryKey, ListIssuesCache][] = [];
  const prevFlatLists: [QueryKey, IssueFlatCache][] = [];
  const prevTableRows: [QueryKey, IssueTableRowCache][] = [];
  const staleKeys: QueryKey[] = [];
  let prevIssue: Issue | undefined = baseIssue;

  for (const [key, data] of bucketedListEntries(qc, wsId)) {
    const { scope, filter, sort } = listContractFromKey(key);
    const loc = findIssueLocation(data, id);
    const filterTouched = listFilterDependsOn(scope, filter, changed);

    if (
      sort.sort_by === 'updated_at' &&
      patchChangesAnyIssueField(patch, loc?.issue ?? baseIssue)
    ) {
      staleKeys.push(key);
    }

    if (loc) {
      if (!prevIssue) prevIssue = loc.issue;
      let next: ListIssuesCache;
      if (filterTouched) {
        const membership = issueMatchesListFilter({ ...loc.issue, ...patch }, scope, filter);
        if (membership === false) {
          next = removeIssueFromBuckets(data, id);
        } else {
          next = patchIssueInBuckets(data, id, patch);
          if (membership === 'unknown') staleKeys.push(key);
        }
      } else {
        next = patchIssueInBuckets(data, id, patch);
      }
      if (next !== data) {
        prevLists.push([key, data]);
        qc.setQueryData<ListIssuesCache>(key, next);
      }
      continue;
    }

    if (!filterTouched && !changed.status) continue;
    const wasMember = baseIssue ? issueMatchesListFilter(baseIssue, scope, filter) : 'unknown';
    const isMember = issueMatchesListFilter({ ...baseIssue, ...patch }, scope, filter);
    if (wasMember === false && isMember === false) continue;

    if (wasMember === true && baseIssue) {
      if (isMember === true) {
        if (!changed.status || patch.status === undefined) continue;
        const next = moveBucketTotal(data, baseIssue.status, patch.status);
        if (next !== data) {
          prevLists.push([key, data]);
          qc.setQueryData<ListIssuesCache>(key, next);
          staleKeys.push(key);
        }
        continue;
      }
      if (isMember === false) {
        const next = decrementBucketTotal(data, baseIssue.status);
        if (next !== data) {
          prevLists.push([key, data]);
          qc.setQueryData<ListIssuesCache>(key, next);
        }
        continue;
      }
    }
    staleKeys.push(key);
  }

  for (const [key, data] of flatListEntries(qc, wsId)) {
    let found: Issue | undefined;
    const pages = data.pages.map((page) => ({
      ...page,
      issues: page.issues.map((issue) => {
        if (issue.id !== id) return issue;
        found = issue;
        return { ...issue, ...patch };
      }),
    }));
    if (found) {
      if (!prevIssue) prevIssue = found;
      prevFlatLists.push([key, data]);
      qc.setQueryData<IssueFlatCache>(key, { ...data, pages });
    }
    if (flatWindowNeedsReconcile(key, patch, found ?? baseIssue, changed)) {
      staleKeys.push(key);
    }
  }

  for (const [key, data] of tableRowEntries(qc, wsId)) {
    let found: Issue | undefined;
    const rows = data.rows.map((row) => {
      if (row.issue.id !== id) return row;
      found = row.issue;
      return { ...row, issue: { ...row.issue, ...patch } };
    });
    if (!found) continue;
    if (!prevIssue) prevIssue = found;
    prevTableRows.push([key, data]);
    qc.setQueryData<IssueTableRowCache>(key, { ...data, rows });
  }

  const prevDetail = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));
  if (prevDetail) {
    qc.setQueryData<Issue>(issueKeys.detail(wsId, id), {
      ...prevDetail,
      ...patch,
    });
    if (!prevIssue) prevIssue = prevDetail;
  }

  let prevInboxList: InboxItem[] | undefined;
  if (patch.status !== undefined) {
    prevInboxList = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    if (prevInboxList) patchInboxIssueStatus(qc, wsId, id, patch.status);
  }

  return {
    prevLists,
    prevFlatLists,
    prevTableRows,
    prevDetail,
    prevInboxList,
    staleKeys,
    prevIssue,
  };
}

export function rollbackIssueChange(
  qc: QueryClient,
  wsId: string,
  id: string,
  result: Pick<
    IssueCacheChangeResult,
    'prevLists' | 'prevFlatLists' | 'prevTableRows' | 'prevDetail' | 'prevInboxList'
  >,
) {
  for (const [key, snapshot] of result.prevLists) {
    qc.setQueryData(key, snapshot);
  }
  for (const [key, snapshot] of result.prevFlatLists) {
    qc.setQueryData(key, snapshot);
  }
  for (const [key, snapshot] of result.prevTableRows) {
    qc.setQueryData(key, snapshot);
  }
  if (result.prevDetail !== undefined) {
    qc.setQueryData(issueKeys.detail(wsId, id), result.prevDetail);
  }
  if (result.prevInboxList !== undefined) {
    qc.setQueryData(inboxKeys.list(wsId), result.prevInboxList);
  }
}

export function invalidateIssueDerivatives(
  qc: QueryClient,
  wsId: string,
  opts: { statusOrProjectChanged: boolean },
) {
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
  if (opts.statusOrProjectChanged) {
    qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
  }
}

function queryKeyHasUpdatedAtSort(key: QueryKey): boolean {
  return key.some(
    (part) =>
      !!part &&
      typeof part === 'object' &&
      !Array.isArray(part) &&
      (part as Record<string, unknown>).sort_by === 'updated_at',
  );
}

export function invalidateUpdatedAtSortedIssueLists(qc: QueryClient, wsId: string): void {
  qc.invalidateQueries({
    queryKey: issueKeys.all(wsId),
    predicate: (query) => queryKeyHasUpdatedAtSort(query.queryKey),
  });
}

export function invalidateStaleListKeys(qc: QueryClient, staleKeys: QueryKey[]) {
  const seen = new Set<string>();
  for (const key of staleKeys) {
    const hash = hashKey(key);
    if (seen.has(hash)) continue;
    seen.add(hash);
    qc.invalidateQueries({ queryKey: key, exact: true });
  }
}

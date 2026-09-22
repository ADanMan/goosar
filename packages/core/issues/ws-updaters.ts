import type { QueryClient } from '@tanstack/react-query';
import { issueKeys } from './queries';
import { labelKeys } from '../labels/queries';
import { projectKeys } from '../projects/queries';
import {
  applyIssueChange,
  invalidateIssueDerivatives,
  invalidateStaleListKeys,
  invalidateUpdatedAtSortedIssueLists,
  type IssueFlatCache,
} from './cache-coordinator';
import { addIssueToBuckets, findIssueLocation, patchIssueInBuckets } from './cache-helpers';
import { cleanupDeletedIssueCaches } from './delete-cache';
import type {
  Issue,
  IssueLabelsResponse,
  IssueMetadata,
  IssuePropertyValues,
  IssueTableRowsResponse,
  Label,
} from '../types';
import type { ListIssuesCache } from '../types';

function patchIssueInFlatCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
) {
  for (const [key, data] of qc.getQueriesData<IssueFlatCache>({
    queryKey: issueKeys.flatAll(wsId),
  })) {
    if (!data?.pages) continue;
    qc.setQueryData<IssueFlatCache>(key, {
      ...data,
      pages: data.pages.map((page) => ({
        ...page,
        issues: page.issues.map((issue) => (issue.id === issueId ? { ...issue, ...patch } : issue)),
      })),
    });
  }
}

function patchIssueInChildrenCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
) {
  for (const [key, data] of qc.getQueriesData<Issue[]>({
    queryKey: issueKeys.childrenAll(wsId),
  })) {
    if (!data || !data.some((child) => child.id === issueId)) continue;
    qc.setQueryData<Issue[]>(
      key,
      data.map((child) => (child.id === issueId ? { ...child, ...patch } : child)),
    );
  }
}

function patchIssueInTableCaches(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  patch: Partial<Issue>,
) {
  for (const [key, data] of qc.getQueriesData<unknown>({
    queryKey: issueKeys.tableAll(wsId),
  })) {
    if (
      !data ||
      typeof data !== 'object' ||
      !Array.isArray((data as IssueTableRowsResponse).rows)
    ) {
      continue;
    }
    const page = data as IssueTableRowsResponse;
    if (!page.rows.some((row) => row.issue.id === issueId)) continue;
    qc.setQueryData<IssueTableRowsResponse>(key, {
      ...page,
      rows: page.rows.map((row) =>
        row.issue.id === issueId ? { ...row, issue: { ...row.issue, ...patch } } : row,
      ),
    });
  }
}

function findIssueInFlatCaches(qc: QueryClient, wsId: string, issueId: string) {
  for (const [, data] of qc.getQueriesData<IssueFlatCache>({
    queryKey: issueKeys.flatAll(wsId),
  })) {
    for (const page of data?.pages ?? []) {
      const issue = page.issues.find((candidate) => candidate.id === issueId);
      if (issue) return issue;
    }
  }
  return undefined;
}

export function onIssueCreated(qc: QueryClient, wsId: string, issue: Issue) {
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.list(wsId),
  })) {
    if (data) qc.setQueryData<ListIssuesCache>(key, addIssueToBuckets(data, issue));
  }
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  if (issue.project_id) {
    qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
  }
  qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
  if (issue.parent_issue_id) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, issue.parent_issue_id) });
    qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
  }
}

export function onIssueUpdated(
  qc: QueryClient,
  wsId: string,
  issue: Partial<Issue> & { id: string },
  meta: {
    assigneeChanged?: boolean;
    statusChanged?: boolean;
    projectChanged?: boolean;
  } = {},
) {
  const listQueries = qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) });
  const firstListData = listQueries[0]?.[1];
  const detailData = qc.getQueryData<Issue>(issueKeys.detail(wsId, issue.id));
  const cachedIssue =
    detailData ??
    (firstListData ? findIssueLocation(firstListData, issue.id)?.issue : undefined) ??
    findIssueInFlatCaches(qc, wsId, issue.id);
  const oldParentId = detailData?.parent_issue_id ?? cachedIssue?.parent_issue_id ?? null;
  const newParentId = issue.parent_issue_id ?? null;
  const parentChanged = issue.parent_issue_id !== undefined && newParentId !== oldParentId;

  const oldProjectId = detailData?.project_id ?? cachedIssue?.project_id ?? null;
  const changed = {
    assignee:
      meta.assigneeChanged ??
      (cachedIssue !== undefined &&
        ((issue.assignee_id !== undefined && issue.assignee_id !== cachedIssue.assignee_id) ||
          (issue.assignee_type !== undefined &&
            issue.assignee_type !== cachedIssue.assignee_type))),
    project:
      meta.projectChanged ??
      (issue.project_id !== undefined && (issue.project_id ?? null) !== oldProjectId),
    status:
      meta.statusChanged ??
      (cachedIssue !== undefined &&
        issue.status !== undefined &&
        issue.status !== cachedIssue.status),
  };

  const change = applyIssueChange(qc, wsId, issue.id, issue, {
    changed,
    baseIssue: cachedIssue,
  });
  invalidateStaleListKeys(qc, change.staleKeys);
  invalidateIssueDerivatives(qc, wsId, {
    statusOrProjectChanged: issue.status !== undefined || issue.project_id !== undefined,
  });
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });

  if (oldParentId) {
    if (parentChanged) {
      qc.invalidateQueries({ queryKey: issueKeys.children(wsId, oldParentId) });
    } else {
      qc.setQueryData<Issue[]>(issueKeys.children(wsId, oldParentId), (old) =>
        old?.map((c) => (c.id === issue.id ? { ...c, ...issue } : c)),
      );
    }
  }
  if (newParentId && parentChanged) {
    qc.invalidateQueries({ queryKey: issueKeys.children(wsId, newParentId) });
  }
  if (oldParentId || newParentId) {
    if (issue.status !== undefined || issue.parent_issue_id !== undefined) {
      qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
    }
    qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
  }
}

export function onIssueLabelsChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  labels: Label[],
) {
  patchIssueLabels(qc, wsId, issueId, labels);
  invalidateIssueLabelDerivatives(qc, wsId);
}

export function patchIssueLabels(qc: QueryClient, wsId: string, issueId: string, labels: Label[]) {
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.list(wsId),
  })) {
    if (data) qc.setQueryData<ListIssuesCache>(key, patchIssueInBuckets(data, issueId, { labels }));
  }
  patchIssueInFlatCaches(qc, wsId, issueId, { labels });
  patchIssueInTableCaches(qc, wsId, issueId, { labels });
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, labels } : old,
  );
  qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(wsId, issueId), (old) =>
    old ? { ...old, labels } : old,
  );
  patchIssueInChildrenCaches(qc, wsId, issueId, { labels });
  for (const [key, data] of qc.getQueriesData<Issue[]>({
    queryKey: issueKeys.projectGanttAll(wsId),
  })) {
    if (!data) continue;
    const next = data.map((issue) => (issue.id === issueId ? { ...issue, labels } : issue));
    qc.setQueryData<Issue[]>(key, next);
  }
}

export function invalidateIssueLabelDerivatives(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: issueKeys.childrenAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
  qc.invalidateQueries({
    queryKey: issueKeys.flatAll(wsId),
    predicate: (query) =>
      query.queryKey.some((part) => {
        if (!part || typeof part !== 'object' || Array.isArray(part)) return false;
        const labelIds = (part as { label_ids?: unknown }).label_ids;
        return Array.isArray(labelIds) && labelIds.length > 0;
      }),
  });
}

export function onIssueMetadataChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  metadata: IssueMetadata,
) {
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.list(wsId),
  })) {
    if (data)
      qc.setQueryData<ListIssuesCache>(key, patchIssueInBuckets(data, issueId, { metadata }));
  }
  patchIssueInFlatCaches(qc, wsId, issueId, { metadata });
  patchIssueInTableCaches(qc, wsId, issueId, { metadata });
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, metadata } : old,
  );
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  invalidateUpdatedAtSortedIssueLists(qc, wsId);
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
}

export function onIssuePropertiesChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  properties: IssuePropertyValues,
) {
  patchIssueProperties(qc, wsId, issueId, properties);
  qc.invalidateQueries({ queryKey: issueKeys.childrenAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
  invalidatePropertyWindowQueries(qc, wsId);
  invalidateUpdatedAtSortedIssueLists(qc, wsId);
}

export function patchIssueProperties(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  properties: IssuePropertyValues,
) {
  for (const [key, data] of qc.getQueriesData<ListIssuesCache>({
    queryKey: issueKeys.list(wsId),
  })) {
    if (data)
      qc.setQueryData<ListIssuesCache>(key, patchIssueInBuckets(data, issueId, { properties }));
  }
  patchIssueInFlatCaches(qc, wsId, issueId, { properties });
  patchIssueInTableCaches(qc, wsId, issueId, { properties });
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, properties } : old,
  );
  patchIssueInChildrenCaches(qc, wsId, issueId, { properties });
}

export function invalidatePropertyWindowQueries(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({
    queryKey: issueKeys.all(wsId),
    predicate: (query) =>
      query.queryKey.some((part) => {
        if (!part || typeof part !== 'object' || Array.isArray(part)) return false;
        const rec = part as Record<string, unknown>;
        if (
          rec.properties &&
          typeof rec.properties === 'object' &&
          Object.keys(rec.properties as Record<string, unknown>).length > 0
        ) {
          return true;
        }
        return typeof rec.sort_by === 'string' && rec.sort_by.startsWith('property:');
      }),
  });
}

export function onIssueDeleted(qc: QueryClient, wsId: string, issueId: string) {
  cleanupDeletedIssueCaches(qc, wsId, issueId);
  qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
}

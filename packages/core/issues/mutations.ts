import { useState, useCallback } from 'react';
import { hashKey, useMutation, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { api } from '../api';
import {
  issueKeys,
  ISSUE_PAGE_SIZE,
  type AssigneeGroupedIssuesFilter,
  type IssueSortParam,
  type MyIssuesFilter,
} from './queries';
import { projectKeys } from '../projects/queries';
import { inboxKeys } from '../inbox/queries';
import {
  applyIssueChange,
  invalidateIssueDerivatives,
  invalidateStaleListKeys,
  rollbackIssueChange,
  type IssueFlatCache,
  type IssueTableRowCache,
} from './cache-coordinator';
import { issueChangedDims } from './surface/membership';
import { addIssueToBuckets, getBucket, setBucket } from './cache-helpers';
import {
  cleanupDeletedIssueCaches,
  collectDeletedIssueCacheMetadata,
  invalidateDeletedIssueDependentCaches,
  invalidateDeletedIssueParentCaches,
  invalidateIssueScopedCaches,
  pruneDeletedIssueFromListCaches,
  pruneDeletedIssueFromParentChildrenCaches,
} from './delete-cache';
import { useWorkspaceId } from '../hooks';
import { useRecentContextStore } from '../chat/recent-context-store';
import { useRecentIssuesStore } from './stores';
import type {
  GroupedIssuesResponse,
  InboxItem,
  Issue,
  IssueAssigneeGroup,
  IssueReaction,
  IssueStatus,
} from '../types';
import type {
  CreateIssueRequest,
  ListIssuesCache,
  MoveIssueRequest,
  UpdateIssueRequest,
} from '../types';
import type { TimelineEntry, IssueSubscriber, Reaction } from '../types';
import { sortTimelineEntriesAsc } from './timeline-sort';

export type ToggleCommentReactionVars = {
  commentId: string;
  emoji: string;
  existing: Reaction | undefined;
};

export type ToggleIssueReactionVars = {
  emoji: string;
  existing: IssueReaction | undefined;
};

export type UpdateIssueMutationInput = {
  id: string;
  move_intent?: Pick<MoveIssueRequest, 'before_id' | 'after_id'>;
} & UpdateIssueRequest;

export function useLoadMoreByStatus(
  status: IssueStatus,
  myIssues?: { scope: string; filter: MyIssuesFilter },
  sort?: IssueSortParam,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [isLoading, setIsLoading] = useState(false);

  const activeKey = myIssues
    ? issueKeys.myListSorted(wsId, myIssues.scope, myIssues.filter, sort)
    : issueKeys.listSorted(wsId, sort);
  const cache = qc.getQueryData<ListIssuesCache>(activeKey);
  const bucket = cache?.byStatus[status];
  const loaded = bucket?.issues.length ?? 0;
  const total = bucket?.total ?? 0;
  const hasMore = loaded < total;

  const loadMore = useCallback(async () => {
    if (isLoading || !hasMore) return;
    setIsLoading(true);
    try {
      const res = await api.listIssues({
        status,
        limit: ISSUE_PAGE_SIZE,
        offset: loaded,
        ...sort,
        ...myIssues?.filter,
      });
      qc.setQueryData<ListIssuesCache>(activeKey, (old) => {
        if (!old) return old;
        const prev = getBucket(old, status);
        const existingIds = new Set(prev.issues.map((i) => i.id));
        const appended = res.issues.filter((i) => !existingIds.has(i.id));
        return setBucket(old, status, {
          issues: [...prev.issues, ...appended],
          total: res.total,
        });
      });
    } finally {
      setIsLoading(false);
    }
  }, [qc, activeKey, status, loaded, hasMore, isLoading, myIssues?.filter, sort]);

  return { loadMore, hasMore, isLoading, total };
}

export function useLoadMoreByAssigneeGroup(
  group: Pick<IssueAssigneeGroup, 'id' | 'assignee_type' | 'assignee_id'>,
  queryKey: QueryKey,
  filter: AssigneeGroupedIssuesFilter,
  sort?: IssueSortParam,
) {
  const qc = useQueryClient();
  const [isLoading, setIsLoading] = useState(false);

  const cache = qc.getQueryData<GroupedIssuesResponse>(queryKey);
  const cachedGroup = cache?.groups.find((g) => g.id === group.id);
  const loaded = cachedGroup?.issues.length ?? 0;
  const total = cachedGroup?.total ?? 0;
  const hasMore = loaded < total;

  const loadMore = useCallback(async () => {
    if (isLoading || !hasMore) return;
    setIsLoading(true);
    try {
      const res = await api.listGroupedIssues({
        group_by: 'assignee',
        limit: ISSUE_PAGE_SIZE,
        offset: loaded,
        ...sort,
        ...filter,
        group_assignee_type: group.assignee_type ?? 'none',
        group_assignee_id: group.assignee_id ?? undefined,
      });
      const nextGroup = res.groups[0];
      if (!nextGroup) return;

      qc.setQueryData<GroupedIssuesResponse>(queryKey, (old) => {
        if (!old) return old;
        return {
          groups: old.groups.map((existing) => {
            if (existing.id !== nextGroup.id) return existing;
            const existingIds = new Set(existing.issues.map((issue) => issue.id));
            const appended = nextGroup.issues.filter((issue) => !existingIds.has(issue.id));
            return {
              ...existing,
              issues: [...existing.issues, ...appended],
              total: nextGroup.total,
            };
          }),
        };
      });
    } finally {
      setIsLoading(false);
    }
  }, [
    filter,
    group.assignee_id,
    group.assignee_type,
    hasMore,
    isLoading,
    loaded,
    qc,
    queryKey,
    sort,
  ]);

  return { loadMore, hasMore, isLoading, total };
}

export function useCreateIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateIssueRequest) => api.createIssue(data),
    onSuccess: (newIssue) => {
      for (const [key, data] of qc.getQueriesData<ListIssuesCache>({
        queryKey: issueKeys.list(wsId),
      })) {
        if (data) qc.setQueryData<ListIssuesCache>(key, addIssueToBuckets(data, newIssue));
      }
      useRecentIssuesStore.getState().recordVisit(wsId, newIssue.id);
      if (newIssue.parent_issue_id) {
        qc.invalidateQueries({ queryKey: issueKeys.children(wsId, newIssue.parent_issue_id) });
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}

export function useUpdateIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, move_intent: moveIntent, ...data }: UpdateIssueMutationInput) => {
      if (!moveIntent) return api.updateIssue(id, data);
      const { position: _optimisticPosition, ...target } = data;
      return api.moveIssue(id, { ...target, ...moveIntent });
    },
    onMutate: ({ id, move_intent: _moveIntent, ...data }) => {
      const { suppress_run: _suppressRun, handoff_note: _handoffNote, ...patch } = data;
      qc.cancelQueries({ queryKey: issueKeys.list(wsId) });
      qc.cancelQueries({ queryKey: issueKeys.myAll(wsId) });
      qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.cancelQueries({ queryKey: issueKeys.tableAll(wsId) });
      if (patch.status !== undefined) {
        qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      }
      const prevDetail = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));
      const change = applyIssueChange(qc, wsId, id, patch as Partial<Issue>, {
        changed: issueChangedDims(patch, prevDetail),
        baseIssue: prevDetail,
      });

      let parentId: string | null =
        prevDetail?.parent_issue_id ?? change.prevIssue?.parent_issue_id ?? null;
      if (!parentId) {
        const childrenCaches = qc.getQueriesData<Issue[]>({
          queryKey: [...issueKeys.all(wsId), 'children'],
        });
        for (const [key, data] of childrenCaches) {
          if (!data?.some((c) => c.id === id)) continue;
          const candidate = key[key.length - 1];
          if (typeof candidate === 'string') {
            parentId = candidate;
            break;
          }
        }
      }
      const prevChildren = parentId
        ? qc.getQueryData<Issue[]>(issueKeys.children(wsId, parentId))
        : undefined;

      if (parentId) {
        const detachedFromParent =
          Object.prototype.hasOwnProperty.call(patch, 'parent_issue_id') &&
          patch.parent_issue_id !== parentId;
        qc.setQueryData<Issue[]>(issueKeys.children(wsId, parentId), (old) =>
          detachedFromParent
            ? old?.filter((c) => c.id !== id)
            : old?.map((c) => (c.id === id ? { ...c, ...patch } : c)),
        );
      }
      return { change, prevChildren, parentId, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx) {
        rollbackIssueChange(qc, wsId, ctx.id, ctx.change);
      }
      if (ctx?.parentId && ctx.prevChildren !== undefined) {
        qc.setQueryData(issueKeys.children(wsId, ctx.parentId), ctx.prevChildren);
      }
    },
    onSuccess: (serverIssue, vars) => {
      const {
        suppress_run: _suppressRun,
        handoff_note: _handoffNote,
        move_intent: _moveIntent,
        id: _id,
        ...intent
      } = vars;
      const { properties: _staleBag, ...reconcilable } = serverIssue;
      const reconcile = applyIssueChange(
        qc,
        wsId,
        serverIssue.id,
        reconcilable as typeof serverIssue,
        {
          changed: issueChangedDims(intent, serverIssue),
          baseIssue: serverIssue,
        },
      );
      invalidateStaleListKeys(qc, reconcile.staleKeys);
    },
    onSettled: (_data, _err, vars, ctx) => {
      invalidateIssueDerivatives(qc, wsId, {
        statusOrProjectChanged:
          vars.status !== undefined || Object.prototype.hasOwnProperty.call(vars, 'project_id'),
      });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
      if (ctx) {
        invalidateStaleListKeys(qc, ctx.change.staleKeys);
      }
      if (vars.attachment_ids?.length) {
        qc.invalidateQueries({ queryKey: issueKeys.attachments(vars.id) });
      }
      if (ctx?.parentId) {
        qc.invalidateQueries({
          queryKey: issueKeys.children(wsId, ctx.parentId),
        });
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
      const newParentId = vars.parent_issue_id;
      if (newParentId && newParentId !== ctx?.parentId) {
        qc.invalidateQueries({
          queryKey: issueKeys.children(wsId, newParentId),
        });
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
      if (ctx?.parentId || newParentId) {
        qc.invalidateQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) });
      }
    },
  });
}

export function useDeleteIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteIssue(id),
    onMutate: async (id) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: issueKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: issueKeys.myAll(wsId) }),
        qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) }),
      ]);
      const metadata = collectDeletedIssueCacheMetadata(qc, wsId, id);
      await Promise.all(
        metadata.parentIssueIds.map((parentId) =>
          qc.cancelQueries({ queryKey: issueKeys.children(wsId, parentId) }),
        ),
      );
      const prevLists = qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) });
      const prevMyLists = qc.getQueriesData<ListIssuesCache>({
        queryKey: issueKeys.myAll(wsId),
      });
      const prevFlatLists = qc.getQueriesData<IssueFlatCache>({
        queryKey: issueKeys.flatAll(wsId),
      });
      const prevDetail = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));
      const prevChildren = new Map<string, Issue[] | undefined>();
      for (const parentId of metadata.parentIssueIds) {
        prevChildren.set(parentId, qc.getQueryData<Issue[]>(issueKeys.children(wsId, parentId)));
      }

      pruneDeletedIssueFromListCaches(qc, wsId, id);
      pruneDeletedIssueFromParentChildrenCaches(qc, wsId, id, metadata);
      qc.removeQueries({ queryKey: issueKeys.detail(wsId, id) });
      return {
        id,
        metadata,
        prevLists,
        prevMyLists,
        prevFlatLists,
        prevDetail,
        prevChildren,
      };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevDetail) {
        qc.setQueryData(issueKeys.detail(wsId, ctx.id), ctx.prevDetail);
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSuccess: (_data, id, ctx) => {
      useRecentContextStore.getState().forgetContext(wsId, { type: 'issue', id });
      cleanupDeletedIssueCaches(qc, wsId, id, ctx?.metadata);
    },
    onSettled: (_data, _err, _id, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      if (ctx?.metadata) invalidateDeletedIssueParentCaches(qc, wsId, ctx.metadata);
    },
  });
}

export function useBatchUpdateIssues() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ ids, updates }: { ids: string[]; updates: UpdateIssueRequest }) =>
      api.batchUpdateIssues(ids, updates),
    onMutate: async ({ ids, updates }) => {
      const { suppress_run: _suppressRun, handoff_note: _handoffNote, ...patch } = updates;
      await qc.cancelQueries({ queryKey: issueKeys.list(wsId) });
      await qc.cancelQueries({ queryKey: issueKeys.myAll(wsId) });
      await qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) });
      await qc.cancelQueries({ queryKey: issueKeys.tableAll(wsId) });
      if (patch.status !== undefined) {
        await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      }

      const prevListByHash = new Map<string, [QueryKey, ListIssuesCache]>();
      const prevFlatListByHash = new Map<string, [QueryKey, IssueFlatCache]>();
      const prevTableRowByHash = new Map<string, [QueryKey, IssueTableRowCache]>();
      const prevDetailById = new Map<string, Issue>();
      let prevInboxList: InboxItem[] | undefined;
      const staleKeys: QueryKey[] = [];
      for (const id of ids) {
        const base = qc.getQueryData<Issue>(issueKeys.detail(wsId, id));
        const change = applyIssueChange(qc, wsId, id, patch as Partial<Issue>, {
          changed: issueChangedDims(patch, base),
          baseIssue: base,
        });
        for (const [key, snapshot] of change.prevLists) {
          const hash = hashKey(key);
          if (!prevListByHash.has(hash)) prevListByHash.set(hash, [key, snapshot]);
        }
        for (const [key, snapshot] of change.prevFlatLists) {
          const hash = hashKey(key);
          if (!prevFlatListByHash.has(hash)) {
            prevFlatListByHash.set(hash, [key, snapshot]);
          }
        }
        for (const [key, snapshot] of change.prevTableRows) {
          const hash = hashKey(key);
          if (!prevTableRowByHash.has(hash)) {
            prevTableRowByHash.set(hash, [key, snapshot]);
          }
        }
        if (change.prevDetail) prevDetailById.set(id, change.prevDetail);
        if (prevInboxList === undefined && change.prevInboxList !== undefined) {
          prevInboxList = change.prevInboxList;
        }
        staleKeys.push(...change.staleKeys);
      }

      const idSet = new Set(ids);
      const childrenCaches = qc.getQueriesData<Issue[]>({
        queryKey: [...issueKeys.all(wsId), 'children'],
      });
      const prevChildren = new Map<string, Issue[] | undefined>();
      const affectedParentIds = new Set<string>();
      for (const [key, data] of childrenCaches) {
        if (!data?.some((c) => idSet.has(c.id))) continue;
        const parentId = key[key.length - 1];
        if (typeof parentId !== 'string') continue;
        affectedParentIds.add(parentId);
        prevChildren.set(parentId, data);
        qc.setQueryData<Issue[]>(issueKeys.children(wsId, parentId), (old) =>
          old?.map((c) => (idSet.has(c.id) ? { ...c, ...patch } : c)),
        );
      }

      return {
        prevLists: [...prevListByHash.values()],
        prevFlatLists: [...prevFlatListByHash.values()],
        prevTableRows: [...prevTableRowByHash.values()],
        prevDetailById,
        prevInboxList,
        staleKeys,
        prevChildren,
        affectedParentIds,
      };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevTableRows) {
        for (const [key, snapshot] of ctx.prevTableRows) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevDetailById) {
        for (const [id, snapshot] of ctx.prevDetailById) {
          qc.setQueryData(issueKeys.detail(wsId, id), snapshot);
        }
      }
      if (ctx?.prevInboxList !== undefined) {
        qc.setQueryData(inboxKeys.list(wsId), ctx.prevInboxList);
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSettled: (_data, _err, _vars, ctx) => {
      invalidateIssueDerivatives(qc, wsId, {
        statusOrProjectChanged:
          _vars.updates.status !== undefined ||
          Object.prototype.hasOwnProperty.call(_vars.updates, 'project_id'),
      });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
      if (ctx) {
        invalidateStaleListKeys(qc, ctx.staleKeys);
      }
      if (ctx?.affectedParentIds && ctx.affectedParentIds.size > 0) {
        for (const parentId of ctx.affectedParentIds) {
          qc.invalidateQueries({
            queryKey: issueKeys.children(wsId, parentId),
          });
        }
        qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
      }
    },
  });
}

export function useBatchDeleteIssues() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (ids: string[]) => api.batchDeleteIssues(ids),
    onMutate: async (ids) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: issueKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: issueKeys.myAll(wsId) }),
        qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) }),
      ]);
      const metadataById = new Map(
        ids.map((id) => [id, collectDeletedIssueCacheMetadata(qc, wsId, id)]),
      );
      const parentIssueIds = new Set<string>();
      for (const metadata of metadataById.values()) {
        for (const parentId of metadata.parentIssueIds) {
          parentIssueIds.add(parentId);
        }
      }
      await Promise.all(
        Array.from(parentIssueIds).map((parentId) =>
          qc.cancelQueries({ queryKey: issueKeys.children(wsId, parentId) }),
        ),
      );
      const prevLists = qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) });
      const prevMyLists = qc.getQueriesData<ListIssuesCache>({
        queryKey: issueKeys.myAll(wsId),
      });
      const prevFlatLists = qc.getQueriesData<IssueFlatCache>({
        queryKey: issueKeys.flatAll(wsId),
      });
      const prevChildren = new Map<string, Issue[] | undefined>();
      for (const parentId of parentIssueIds) {
        prevChildren.set(parentId, qc.getQueryData<Issue[]>(issueKeys.children(wsId, parentId)));
      }

      for (const id of ids) {
        const metadata = metadataById.get(id);
        pruneDeletedIssueFromListCaches(qc, wsId, id);
        if (metadata) {
          pruneDeletedIssueFromParentChildrenCaches(qc, wsId, id, metadata);
        }
      }
      return {
        prevLists,
        prevMyLists,
        prevFlatLists,
        prevChildren,
        parentIssueIds,
        metadataById,
      };
    },
    onError: (_err, _ids, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSuccess: (data, ids, ctx) => {
      if (data.deleted === ids.length) {
        const { forgetContext } = useRecentContextStore.getState();
        for (const id of ids) {
          forgetContext(wsId, { type: 'issue', id });
          cleanupDeletedIssueCaches(qc, wsId, id, ctx?.metadataById.get(id));
        }
        return;
      }

      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(issueKeys.children(wsId, parentId), snapshot);
        }
      }
      for (const id of ids) {
        invalidateIssueScopedCaches(qc, wsId, id);
      }
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      invalidateDeletedIssueDependentCaches(qc, wsId);
    },
    onSettled: (_data, _err, _ids, ctx) => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      if (ctx?.parentIssueIds && ctx.parentIssueIds.size > 0) {
        invalidateDeletedIssueParentCaches(qc, wsId, {
          parentIssueIds: Array.from(ctx.parentIssueIds),
        });
      }
    },
  });
}

type TimelineCache = TimelineEntry[];

export function useCreateComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      content,
      type,
      parentId,
      attachmentIds,
      suppressAgentIds,
    }: {
      content: string;
      type?: string;
      parentId?: string;
      attachmentIds?: string[];
      suppressAgentIds?: string[];
    }) => api.createComment(issueId, content, type, parentId, attachmentIds, suppressAgentIds),
    onSuccess: (comment) => {
      const entry: TimelineEntry = {
        type: 'comment',
        id: comment.id,
        actor_type: comment.author_type,
        actor_id: comment.author_id,
        content: comment.content,
        parent_id: comment.parent_id,
        comment_type: comment.type,
        reactions: comment.reactions ?? [],
        attachments: comment.attachments ?? [],
        created_at: comment.created_at,
        updated_at: comment.updated_at,
      };
      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) => {
        if (!old) return [entry];
        if (old.some((e) => e.id === entry.id)) return old;
        return sortTimelineEntriesAsc([...old, entry]);
      });
      qc.invalidateQueries({ queryKey: issueKeys.commentTriggerPreview(issueId) });
    },
    // No onSettled invalidate. The `comment:created` WS broadcast keeps
    // the timeline cache fresh after a successful create, and reconnect
    // recovery in useIssueTimeline already invalidates if the connection
    // dropped. Re-fetching on every submit replaces every entry's
    // reference, which forces every memoized CommentCard subtree to
    // re-render (visible as a flash across sibling threads during AI
    // streaming).
  });
}

export function useUpdateComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      commentId,
      content,
      attachmentIds,
      suppressAgentIds,
    }: {
      commentId: string;
      content: string;
      attachmentIds: string[];
      suppressAgentIds?: string[];
    }) => api.updateComment(commentId, content, attachmentIds, suppressAgentIds),
    onMutate: async ({ commentId, content, attachmentIds }) => {
      await qc.cancelQueries({ queryKey: issueKeys.timeline(issueId) });
      const prev = qc.getQueryData<TimelineCache>(issueKeys.timeline(issueId));
      const kept = new Set(attachmentIds);
      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) =>
        old?.map((e) =>
          e.id === commentId
            ? { ...e, content, attachments: e.attachments?.filter((a) => kept.has(a.id)) }
            : e,
        ),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(issueKeys.timeline(issueId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

export function useDeleteComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (commentId: string) => api.deleteComment(commentId),
    onMutate: async (commentId) => {
      await qc.cancelQueries({ queryKey: issueKeys.timeline(issueId) });
      const prev = qc.getQueryData<TimelineCache>(issueKeys.timeline(issueId));

      const toRemove = new Set<string>([commentId]);
      if (prev) {
        let changed = true;
        while (changed) {
          changed = false;
          for (const e of prev) {
            if (e.parent_id && toRemove.has(e.parent_id) && !toRemove.has(e.id)) {
              toRemove.add(e.id);
              changed = true;
            }
          }
        }
      }

      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) =>
        old?.filter((e) => !toRemove.has(e.id)),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(issueKeys.timeline(issueId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

function collectThreadCommentIds(entries: TimelineCache, commentId: string): Set<string> {
  const byId = new Map<string, TimelineEntry>();
  for (const e of entries) {
    if (e.type === 'comment') byId.set(e.id, e);
  }
  let rootId = commentId;
  const guard = new Set<string>();
  let cur = byId.get(commentId);
  while (cur?.parent_id && byId.has(cur.parent_id) && !guard.has(cur.id)) {
    guard.add(cur.id);
    rootId = cur.parent_id;
    cur = byId.get(cur.parent_id);
  }
  const childrenByParent = new Map<string, string[]>();
  for (const e of byId.values()) {
    if (e.parent_id) {
      const list = childrenByParent.get(e.parent_id) ?? [];
      list.push(e.id);
      childrenByParent.set(e.parent_id, list);
    }
  }
  const ids = new Set<string>([rootId]);
  const stack = [rootId];
  while (stack.length > 0) {
    const id = stack.pop()!;
    for (const child of childrenByParent.get(id) ?? []) {
      if (!ids.has(child)) {
        ids.add(child);
        stack.push(child);
      }
    }
  }
  return ids;
}

export function useResolveComment(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ commentId, resolved }: { commentId: string; resolved: boolean }) =>
      resolved ? api.resolveComment(commentId) : api.unresolveComment(commentId),
    onMutate: async ({ commentId, resolved }) => {
      await qc.cancelQueries({ queryKey: issueKeys.timeline(issueId) });
      const prev = qc.getQueryData<TimelineCache>(issueKeys.timeline(issueId));
      qc.setQueryData<TimelineCache>(issueKeys.timeline(issueId), (old) => {
        if (!old) return old;
        const threadIds = resolved ? collectThreadCommentIds(old, commentId) : null;
        return old.map((e) => {
          if (e.id === commentId) {
            return {
              ...e,
              resolved_at: resolved ? new Date().toISOString() : null,
              resolved_by_type: resolved ? (e.resolved_by_type ?? null) : null,
              resolved_by_id: resolved ? (e.resolved_by_id ?? null) : null,
            };
          }
          if (resolved && e.resolved_at && threadIds?.has(e.id)) {
            return { ...e, resolved_at: null, resolved_by_type: null, resolved_by_id: null };
          }
          return e;
        });
      });
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(issueKeys.timeline(issueId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

export function useToggleCommentReaction(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ['toggleCommentReaction', issueId] as const,
    mutationFn: async ({ commentId, emoji, existing }: ToggleCommentReactionVars) => {
      if (existing) {
        await api.removeReaction(commentId, emoji);
        return null;
      }
      return api.addReaction(commentId, emoji);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    },
  });
}

export function useToggleIssueReaction(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ['toggleIssueReaction', issueId] as const,
    mutationFn: async ({ emoji, existing }: ToggleIssueReactionVars) => {
      if (existing) {
        await api.removeIssueReaction(issueId, emoji);
        return null;
      }
      return api.addIssueReaction(issueId, emoji);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.reactions(issueId) });
    },
  });
}

export function useToggleIssueSubscriber(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      userId,
      userType,
      subscribed,
    }: {
      userId: string;
      userType: 'member' | 'agent';
      subscribed: boolean;
    }) => {
      if (subscribed) {
        await api.unsubscribeFromIssue(issueId, userId, userType);
      } else {
        await api.subscribeToIssue(issueId, userId, userType);
      }
    },
    onMutate: async ({ userId, userType, subscribed }) => {
      await qc.cancelQueries({ queryKey: issueKeys.subscribers(issueId) });
      const prev = qc.getQueryData<IssueSubscriber[]>(issueKeys.subscribers(issueId));

      if (subscribed) {
        qc.setQueryData<IssueSubscriber[]>(issueKeys.subscribers(issueId), (old) =>
          old?.filter((s) => !(s.user_id === userId && s.user_type === userType)),
        );
      } else {
        const temp: IssueSubscriber = {
          issue_id: issueId,
          user_type: userType,
          user_id: userId,
          reason: 'manual',
          created_at: new Date().toISOString(),
        };
        qc.setQueryData<IssueSubscriber[]>(issueKeys.subscribers(issueId), (old) => {
          if (old?.some((s) => s.user_id === userId && s.user_type === userType)) return old;
          return [...(old ?? []), temp];
        });
      }
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(issueKeys.subscribers(issueId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.subscribers(issueId) });
    },
  });
}

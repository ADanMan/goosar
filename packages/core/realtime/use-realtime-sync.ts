'use client';

import { useEffect, useRef } from 'react';
import { useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query';
import type { WSClient } from '../api/ws-client';
import type { StoreApi, UseBoundStore } from 'zustand';
import type { AuthState } from '../auth/store';
import { createLogger } from '../logger';
import { clearWorkspaceStorage } from '../platform/storage-cleanup';
import { defaultStorage } from '../platform/storage';
import { notifyWorkspaceAccessRevoked } from '../platform/workspace-access-revoked';
import { getCurrentWsId, getCurrentSlug } from '../platform/workspace-storage';
import { issueKeys } from '../issues/queries';
import { projectKeys } from '../projects/queries';
import { pinKeys } from '../pins/queries';
import { autopilotKeys } from '../autopilots/queries';
import { runtimeKeys } from '../runtimes/queries';
import { labelKeys } from '../labels/queries';
import { propertyKeys } from '../properties/queries';
import {
  agentTaskSnapshotKeys,
  workspaceWorkingAgentsKeys,
  agentActivityKeys,
  agentRunCountsKeys,
  agentTasksKeys,
} from '../agents/queries';
import { githubKeys } from '../github/queries';
import { slackKeys } from '../slack/queries';
import {
  onIssueCreated,
  onIssueUpdated,
  onIssueDeleted,
  onIssueLabelsChanged,
  onIssuePropertiesChanged,
  onIssueMetadataChanged,
} from '../issues/ws-updaters';
import { invalidateUpdatedAtSortedIssueLists } from '../issues/cache-coordinator';
import {
  onInboxNew,
  onInboxInvalidate,
  onInboxIssueStatusChanged,
  onInboxIssueDeleted,
  onInboxSummaryInvalidate,
} from '../inbox/ws-updaters';
import { inboxKeys } from '../inbox/queries';
import {
  notificationPreferenceOptions,
  notificationPreferenceKeys,
} from '../notification-preferences/queries';
import { workspaceKeys, workspaceListOptions } from '../workspace/queries';
import { isWorkspaceDeletePending } from '../workspace/pending-delete';
import {
  showWebNotification,
  type SystemNotificationPayload,
} from '../platform/system-notification';
import type { Workspace } from '../types/workspace';
import { chatKeys, mergeTaskMessagesBySeq, sortChatSessions } from '../chat/queries';
import { useChatStore } from '../chat';
import { resolvePostAuthDestination, useHasOnboarded } from '../paths';
import type {
  MemberAddedPayload,
  WorkspaceDeletedPayload,
  WorkspaceUpdatedPayload,
  MemberRemovedPayload,
  IssueUpdatedPayload,
  IssueCreatedPayload,
  IssueDeletedPayload,
  IssueLabelsChangedPayload,
  IssueMetadataChangedPayload,
  IssuePropertiesChangedPayload,
  InboxNewPayload,
  InboxItem,
  NotificationPreferenceResponse,
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  CommentResolvedPayload,
  CommentUnresolvedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
  IssueReactionAddedPayload,
  IssueReactionRemovedPayload,
  SubscriberAddedPayload,
  SubscriberRemovedPayload,
  TaskMessagePayload,
  TaskQueuedPayload,
  TaskDispatchPayload,
  TaskRunningPayload,
  TaskWaitingLocalDirectoryPayload,
  TaskCompletedPayload,
  TaskFailedPayload,
  TaskCancelledPayload,
  ChatDonePayload,
  ChatCancelFinalizedPayload,
  ChatMessage,
  ChatPendingTask,
  ChatMessagesPage,
  ChatSession,
  InvitationCreatedPayload,
} from '../types';

const chatWsLogger = createLogger('chat.ws');

const logger = createLogger('realtime-sync');

export function invalidateChatMessageQueries(qc: QueryClient, sessionId: string) {
  qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
  qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
}

export function refetchPendingChatAggregate(qc: QueryClient, wsId: string | null | undefined) {
  if (!wsId) return;
  qc.invalidateQueries({ queryKey: chatKeys.pendingTasks(wsId) });
}

export function applyChatDoneToCache(qc: QueryClient, payload: ChatDonePayload) {
  const sessionId = payload.chat_session_id;
  const taskId = payload.task_id;
  const messageId = payload.message_id;
  const content = payload.content;
  if (messageId && content !== undefined) {
    const assistant: ChatMessage = {
      id: messageId,
      chat_session_id: sessionId,
      role: 'assistant',
      content,
      task_id: taskId,
      created_at: payload.created_at ?? new Date().toISOString(),
      elapsed_ms: payload.elapsed_ms ?? null,
      message_kind: payload.message_kind ?? 'message',
    };
    qc.setQueryData<ChatMessage[] | undefined>(chatKeys.messages(sessionId), (old) => {
      if (!old) return old; 
      if (old.some((m) => m.id === messageId)) return old;
      return [...old, assistant];
    });
    qc.setQueryData<InfiniteData<ChatMessagesPage> | undefined>(
      chatKeys.messagesPage(sessionId),
      (old) => patchLatestChatMessagePage(old, assistant),
    );
  }
  qc.setQueryData(chatKeys.pendingTask(sessionId), {});
  invalidateChatMessageQueries(qc, sessionId);
  qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
}

function patchLatestChatMessagePage(
  old: InfiniteData<ChatMessagesPage> | undefined,
  message: ChatMessage,
): InfiniteData<ChatMessagesPage> | undefined {
  if (!old?.pages.length) return old;
  const seen = old.pages.some((page) => page.messages.some((m) => m.id === message.id));
  if (seen) return old;
  return {
    ...old,
    pages: old.pages.map((page, index) => {
      if (index !== 0) return page;
      return {
        ...page,
        messages: [...page.messages, message],
      };
    }),
  };
}

type ChatSessionUpdatedPayload = {
  chat_session_id: string;
  title?: string;
  project_id?: string | null;
  pinned?: boolean;
  status?: 'active' | 'archived';
  updated_at?: string;
};

export function applyChatSessionUpdatedToCache(
  qc: QueryClient,
  wsId: string,
  payload: ChatSessionUpdatedPayload,
): void {
  qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) => {
    if (!old) return old;
    const next = old.map((s) =>
      s.id === payload.chat_session_id
        ? {
            ...s,
            title: payload.title ?? s.title,
            ...('project_id' in payload ? { project_id: payload.project_id } : {}),
            pinned: payload.pinned ?? s.pinned,
            status: payload.status ?? s.status,
            updated_at: payload.updated_at ?? s.updated_at,
            ...(payload.status === 'archived' ? { unread_count: 0, has_unread: false } : {}),
          }
        : s,
    );
    return payload.pinned === undefined && payload.status === undefined
      ? next
      : sortChatSessions(next);
  });
}

function removeChatMessageFromPageCache(qc: QueryClient, sessionId: string, messageId: string) {
  qc.setQueryData<InfiniteData<ChatMessagesPage> | undefined>(
    chatKeys.messagesPage(sessionId),
    (old) => {
      if (!old) return old;
      return {
        ...old,
        pages: old.pages.map((page) => ({
          ...page,
          messages: page.messages.filter((m) => m.id !== messageId),
        })),
      };
    },
  );
}

export function removeChatMessageFromCaches(qc: QueryClient, sessionId: string, messageId: string) {
  qc.setQueryData<ChatMessage[]>(
    chatKeys.messages(sessionId),
    (old) => old?.filter((m) => m.id !== messageId) ?? old,
  );
  removeChatMessageFromPageCache(qc, sessionId, messageId);
}

export function applyChatCancelFinalizedToCache(
  qc: QueryClient,
  payload: ChatCancelFinalizedPayload,
  currentUserId?: string,
) {
  const sessionId = payload.chat_session_id;
  if (!sessionId) return;
  if (payload.outcome === 'stopped') {
    applyChatDoneToCache(qc, {
      chat_session_id: sessionId,
      task_id: payload.task_id,
      message_id: payload.message_id,
      content: payload.content,
      elapsed_ms: payload.elapsed_ms,
      created_at: payload.created_at,
      message_kind: payload.message_kind,
    });
    return;
  }
  if (payload.outcome === 'restored') {
    if (payload.message_id) {
      removeChatMessageFromCaches(qc, sessionId, payload.message_id);
    }
    qc.setQueryData(chatKeys.pendingTask(sessionId), {});
    invalidateChatMessageQueries(qc, sessionId);
    const isInitiator =
      !!payload.initiator_user_id && !!currentUserId && payload.initiator_user_id === currentUserId;
    if (isInitiator) {
      void qc.invalidateQueries({ queryKey: chatKeys.draftRestores(sessionId) });
    }
  }
}

export function handleSelfMemberRemoved(
  slug: string | null,
  wsId: string | null,
  deps: {
    relocate: (lostWsId: string) => void;
    onToast?: (message: string, type?: 'info' | 'error') => void;
  },
): void {
  if (!slug || !wsId) return;
  notifyWorkspaceAccessRevoked(wsId);
  clearWorkspaceStorage(defaultStorage, slug);
  logger.warn('removed from workspace, switching');
  deps.onToast?.('You were removed from this workspace', 'info');
  deps.relocate(wsId);
}

export function applyWorkspaceUpdatedToCache(
  qc: QueryClient,
  payload: WorkspaceUpdatedPayload,
): void {
  const next = payload.workspace;
  if (next?.id) {
    const list = qc.getQueryData<Workspace[]>(workspaceKeys.list());
    const cached = list?.find((w) => w.id === next.id) ?? null;
    if (cached && cached.issue_prefix !== next.issue_prefix) {
      qc.invalidateQueries({ queryKey: issueKeys.all(next.id) });
    }
    if (cached && list) {
      qc.setQueryData<Workspace[]>(
        workspaceKeys.list(),
        list.map((workspace) => (workspace.id === next.id ? next : workspace)),
      );
      return;
    }
    qc.invalidateQueries({ queryKey: issueKeys.all(next.id) });
  }
  qc.invalidateQueries({ queryKey: workspaceKeys.list() });
}

export async function resolveInboxSourceSlug(
  qc: QueryClient,
  workspaceId: string,
): Promise<string | null> {
  if (!workspaceId) return null;
  try {
    const workspaces = await qc.ensureQueryData(workspaceListOptions());
    return workspaces?.find((w) => w.id === workspaceId)?.slug ?? null;
  } catch {
    return null;
  }
}

export async function handleInboxNew(qc: QueryClient, item: InboxItem): Promise<void> {
  const sourceWsId = item.workspace_id;
  if (sourceWsId) onInboxNew(qc, sourceWsId, item);
  onInboxSummaryInvalidate(qc);
  if (typeof document !== 'undefined' && document.hasFocus()) return;
  const slug = await resolveInboxSourceSlug(qc, sourceWsId);
  if (sourceWsId) {
    try {
      const prefData = slug
        ? await qc.ensureQueryData(notificationPreferenceOptions(sourceWsId, slug))
        : qc.getQueryData<NotificationPreferenceResponse>(
            notificationPreferenceKeys.all(sourceWsId),
          );
      if (prefData?.preferences?.system_notifications === 'muted') return;
    } catch {
      // Fall through with default behavior.
    }
  }
  const payload: SystemNotificationPayload = {
    slug: slug ?? '',
    itemId: item.id,
    issueKey: item.issue_id ?? item.id,
    title: item.title,
    body: item.body ?? '',
  };
  const desktopAPI = (
    globalThis as unknown as {
      desktopAPI?: {
        showNotification?: (payload: SystemNotificationPayload) => void;
      };
    }
  ).desktopAPI;
  if (desktopAPI?.showNotification) {
    desktopAPI.showNotification(payload);
    return;
  }
  showWebNotification(payload);
}

function invalidateWorkspaceScopedQueries(qc: QueryClient): void {
  const wsId = getCurrentWsId();
  if (wsId) {
    qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
    qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: workspaceWorkingAgentsKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: agentActivityKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: agentRunCountsKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: chatKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
  }
  onInboxSummaryInvalidate(qc);
  qc.invalidateQueries({ queryKey: issueKeys.timelineAll() });
  qc.invalidateQueries({ queryKey: issueKeys.reactionsAll() });
  qc.invalidateQueries({ queryKey: issueKeys.subscribersAll() });
  qc.invalidateQueries({ queryKey: issueKeys.usageAll() });
  qc.invalidateQueries({ queryKey: issueKeys.attachmentsAll() });
  qc.invalidateQueries({ queryKey: issueKeys.tasksAll() });
  qc.invalidateQueries({ queryKey: chatKeys.messagesAll() });
  qc.invalidateQueries({ queryKey: chatKeys.messagesPageAll() });
  qc.invalidateQueries({ queryKey: chatKeys.pendingTaskAll() });
  qc.invalidateQueries({ queryKey: chatKeys.taskMessagesAll() });
  qc.invalidateQueries({ queryKey: chatKeys.draftRestoresAll() });
  qc.invalidateQueries({ queryKey: workspaceKeys.list() });
}

function invalidateSquadMemberStatusQueries(qc: QueryClient, wsId: string): void {
  qc.invalidateQueries({
    predicate: (query) => {
      const key = query.queryKey;
      return (
        key[0] === 'workspaces' &&
        key[1] === wsId &&
        key[2] === 'squads' &&
        key[4] === 'members-status'
      );
    },
  });
}

export interface RealtimeSyncStores {
  authStore: UseBoundStore<StoreApi<AuthState>>;
}

export function useRealtimeSync(
  ws: WSClient | null,
  stores: RealtimeSyncStores,
  onToast?: (message: string, type?: 'info' | 'error') => void,
) {
  const { authStore } = stores;
  const qc = useQueryClient();

  const hasOnboarded = useHasOnboarded();
  const hasOnboardedRef = useRef(hasOnboarded);
  hasOnboardedRef.current = hasOnboarded;

  useEffect(() => {
    if (!ws) return;

    const refreshMap: Record<string, () => void> = {
      inbox: () => {
        const wsId = getCurrentWsId();
        if (wsId) onInboxInvalidate(qc, wsId);
        onInboxSummaryInvalidate(qc);
      },
      agent: () => {
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
          qc.invalidateQueries({ queryKey: workspaceWorkingAgentsKeys.all(wsId) });
          invalidateSquadMemberStatusQueries(qc, wsId);
        }
      },
      member: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      },
      workspace: () => {
        qc.invalidateQueries({ queryKey: workspaceKeys.list() });
      },
      skill: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      },
      project: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      },
      squad: () => {
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
          qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
        }
      },
      label: () => {
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: ['labels', wsId] });
          qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
          qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
          qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
        }
      },
      pin: () => {
        const wsId = getCurrentWsId();
        const userId = authStore.getState().user?.id;
        if (wsId && userId) qc.invalidateQueries({ queryKey: pinKeys.all(wsId, userId) });
      },
      daemon: () => {
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
          invalidateSquadMemberStatusQueries(qc, wsId);
        }
      },
      autopilot: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
      },
      github_installation: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: githubKeys.installations(wsId) });
      },
      slack_installation: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: slackKeys.installations(wsId) });
      },
      vcs_connection: () => {
        const wsId = getCurrentWsId();
        if (wsId) qc.invalidateQueries({ queryKey: ['vcs', wsId] });
      },
      pull_request: () => {
        qc.invalidateQueries({ queryKey: ['github', 'pull-requests'] });
      },
      task: () => {
        const wsId = getCurrentWsId();
        if (!wsId) return;
        qc.invalidateQueries({ queryKey: agentTaskSnapshotKeys.list(wsId) });
        qc.invalidateQueries({ queryKey: workspaceWorkingAgentsKeys.all(wsId) });
        qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
        qc.invalidateQueries({ queryKey: agentActivityKeys.last30d(wsId) });
        qc.invalidateQueries({ queryKey: agentRunCountsKeys.last30d(wsId) });
        qc.invalidateQueries({ queryKey: agentTasksKeys.all(wsId) });
        qc.invalidateQueries({ queryKey: ['issues', 'tasks'] });
        qc.invalidateQueries({ queryKey: ['issues', 'usage'] });
        invalidateSquadMemberStatusQueries(qc, wsId);
        qc.invalidateQueries({ queryKey: issueKeys.commentTriggerPreviewAll() });
        // Issue-trigger previews (assign/status/create/batch) are deliberately
        // NOT invalidated here. Unlike comment triggers, the assign source
        // (create / assignee change) cancels existing tasks before enqueuing, so
        // a task event can never change its verdict; only the status source's
        // pending dedup could, and that preview is advisory — the write path
        // re-evaluates authoritatively, so a rare stale label is harmless.
        // Refetching every mounted preview on every workspace task event caused
        // visible flicker, so the preview now refetches only on input change
        // (signature), mirroring its query design (MUL-3375).
      },
    };

    const timers = new Map<string, ReturnType<typeof setTimeout>>();
    const debouncedRefresh = (prefix: string, fn: () => void) => {
      const existing = timers.get(prefix);
      if (existing) clearTimeout(existing);
      timers.set(
        prefix,
        setTimeout(() => {
          timers.delete(prefix);
          fn();
        }, 100),
      );
    };

    const specificEvents = new Set([
      'workspace:updated',
      'issue:updated',
      'issue:created',
      'issue:deleted',
      'issue_labels:changed',
      'issue_metadata:changed',
      'issue_properties:changed',
      'property:created',
      'property:updated',
      'inbox:new',
      'comment:created',
      'comment:updated',
      'comment:deleted',
      'comment:resolved',
      'comment:unresolved',
      'activity:created',
      'reaction:added',
      'reaction:removed',
      'issue_reaction:added',
      'issue_reaction:removed',
      'subscriber:added',
      'subscriber:removed',
      'daemon:heartbeat',
      'chat:message',
      'chat:done',
      'chat:cancel_finalized',
      'chat:session_read',
      'chat:session_deleted',
      'chat:session_updated',
      'task:message',
      // task:completed / task:failed deliberately NOT here. They go through
      // both the task-prefix invalidate (refreshes the agent-task-snapshot
      // cache) AND the chat-specific ws.on() handlers below. The two
      // channels are independent — onAny dispatch and ws.on are separate
      // subscriptions.
    ]);

    const unsubAny = ws.onAny((msg) => {
      if (specificEvents.has(msg.type)) return;
      const prefix = msg.type.split(':')[0] ?? '';
      const refresh = refreshMap[prefix];
      if (refresh) debouncedRefresh(prefix, refresh);
    });

    const unsubIssueUpdated = ws.on('issue:updated', (p) => {
      const payload = p as IssueUpdatedPayload;
      const { issue } = payload;
      if (!issue?.id) return;
      const wsId = getCurrentWsId();
      if (wsId) {
        onIssueUpdated(qc, wsId, issue, {
          assigneeChanged: payload.assignee_changed,
          statusChanged: payload.status_changed,
          projectChanged: payload.project_changed,
        });
        if (issue.status) {
          onInboxIssueStatusChanged(qc, wsId, issue.id, issue.status);
        }
      }
    });

    const unsubIssueCreated = ws.on('issue:created', (p) => {
      const { issue } = p as IssueCreatedPayload;
      if (!issue) return;
      const wsId = getCurrentWsId();
      if (wsId) onIssueCreated(qc, wsId, issue);
    });

    const unsubIssueDeleted = ws.on('issue:deleted', (p) => {
      const { issue_id } = p as IssueDeletedPayload;
      if (!issue_id) return;
      const wsId = getCurrentWsId();
      if (wsId) {
        onIssueDeleted(qc, wsId, issue_id);
        onInboxIssueDeleted(qc, wsId, issue_id);
      }
    });

    const unsubIssueLabelsChanged = ws.on('issue_labels:changed', (p) => {
      const { issue_id, labels } = p as IssueLabelsChangedPayload;
      if (!issue_id) return;
      const wsId = getCurrentWsId();
      if (wsId) onIssueLabelsChanged(qc, wsId, issue_id, labels ?? []);
    });

    const unsubIssueMetadataChanged = ws.on('issue_metadata:changed', (p) => {
      const { issue_id, metadata } = p as IssueMetadataChangedPayload;
      if (!issue_id) return;
      const wsId = getCurrentWsId();
      if (wsId) onIssueMetadataChanged(qc, wsId, issue_id, metadata ?? {});
    });

    const unsubIssuePropertiesChanged = ws.on('issue_properties:changed', (p) => {
      const { issue_id, properties } = p as IssuePropertiesChangedPayload;
      if (!issue_id) return;
      const wsId = getCurrentWsId();
      if (wsId) {
        onIssuePropertiesChanged(qc, wsId, issue_id, properties ?? {});
        qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
      }
    });

    const unsubPropertyChanged = ['property:created', 'property:updated'].map((event) =>
      ws.on(event as 'property:created' | 'property:updated', () => {
        const wsId = getCurrentWsId();
        if (wsId) {
          qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
          qc.invalidateQueries({ queryKey: issueKeys.tableAll(wsId) });
        }
      }),
    );

    const unsubInboxNew = ws.on('inbox:new', async (p) => {
      const { item } = p as InboxNewPayload;
      if (!item) return;
      await handleInboxNew(qc, item);
    });

    const invalidateTimeline = (issueId: string) => {
      qc.invalidateQueries({
        queryKey: issueKeys.timeline(issueId),
        refetchType: 'none',
      });
    };

    const unsubCommentCreated = ws.on('comment:created', (p) => {
      const { comment } = p as CommentCreatedPayload;
      if (!comment?.issue_id) return;
      invalidateTimeline(comment.issue_id);
      const wsId = getCurrentWsId();
      if (wsId) invalidateUpdatedAtSortedIssueLists(qc, wsId);
    });

    const unsubCommentUpdated = ws.on('comment:updated', (p) => {
      const { comment } = p as CommentUpdatedPayload;
      if (comment?.issue_id) invalidateTimeline(comment.issue_id);
    });

    const unsubCommentDeleted = ws.on('comment:deleted', (p) => {
      const { issue_id } = p as CommentDeletedPayload;
      if (issue_id) invalidateTimeline(issue_id);
    });

    const unsubCommentResolved = ws.on('comment:resolved', (p) => {
      const { comment } = p as CommentResolvedPayload;
      if (comment?.issue_id) invalidateTimeline(comment.issue_id);
    });

    const unsubCommentUnresolved = ws.on('comment:unresolved', (p) => {
      const { comment } = p as CommentUnresolvedPayload;
      if (comment?.issue_id) invalidateTimeline(comment.issue_id);
    });

    const unsubActivityCreated = ws.on('activity:created', (p) => {
      const { issue_id } = p as ActivityCreatedPayload;
      if (issue_id) invalidateTimeline(issue_id);
    });

    const unsubReactionAdded = ws.on('reaction:added', (p) => {
      const { issue_id } = p as ReactionAddedPayload;
      if (issue_id) invalidateTimeline(issue_id);
    });

    const unsubReactionRemoved = ws.on('reaction:removed', (p) => {
      const { issue_id } = p as ReactionRemovedPayload;
      if (issue_id) invalidateTimeline(issue_id);
    });

    const unsubIssueReactionAdded = ws.on('issue_reaction:added', (p) => {
      const { issue_id } = p as IssueReactionAddedPayload;
      if (issue_id) qc.invalidateQueries({ queryKey: issueKeys.reactions(issue_id) });
    });

    const unsubIssueReactionRemoved = ws.on('issue_reaction:removed', (p) => {
      const { issue_id } = p as IssueReactionRemovedPayload;
      if (issue_id) qc.invalidateQueries({ queryKey: issueKeys.reactions(issue_id) });
    });

    const unsubSubscriberAdded = ws.on('subscriber:added', (p) => {
      const { issue_id } = p as SubscriberAddedPayload;
      if (issue_id) qc.invalidateQueries({ queryKey: issueKeys.subscribers(issue_id) });
    });

    const unsubSubscriberRemoved = ws.on('subscriber:removed', (p) => {
      const { issue_id } = p as SubscriberRemovedPayload;
      if (issue_id) qc.invalidateQueries({ queryKey: issueKeys.subscribers(issue_id) });
    });

    const relocateAfterWorkspaceLoss = async (lostWsId: string) => {
      const wsList = await qc.fetchQuery({
        ...workspaceListOptions(),
        staleTime: 0,
      });
      const remaining = wsList.filter((w) => w.id !== lostWsId);
      const target = resolvePostAuthDestination(remaining, hasOnboardedRef.current);
      if (typeof window !== 'undefined') {
        window.location.assign(target);
      }
    };

    const unsubWsUpdated = ws.on('workspace:updated', (p) => {
      applyWorkspaceUpdatedToCache(qc, p as WorkspaceUpdatedPayload);
    });

    const unsubWsDeleted = ws.on('workspace:deleted', (p) => {
      const { workspace_id } = p as WorkspaceDeletedPayload;
      if (isWorkspaceDeletePending(workspace_id)) return;
      const wsList = qc.getQueryData<{ id: string; slug: string }[]>(workspaceKeys.list()) ?? [];
      const deletedSlug = wsList.find((w) => w.id === workspace_id)?.slug;
      if (deletedSlug) clearWorkspaceStorage(defaultStorage, deletedSlug);
      if (getCurrentWsId() === workspace_id) {
        logger.warn('current workspace deleted, switching');
        onToast?.('This workspace was deleted', 'info');
        relocateAfterWorkspaceLoss(workspace_id);
      }
    });

    const unsubMemberRemoved = ws.on('member:removed', (p) => {
      const { user_id } = p as MemberRemovedPayload;
      const myUserId = authStore.getState().user?.id;
      if (user_id === myUserId) {
        handleSelfMemberRemoved(getCurrentSlug(), getCurrentWsId(), {
          relocate: relocateAfterWorkspaceLoss,
          onToast,
        });
      }
    });

    const unsubMemberAdded = ws.on('member:added', (p) => {
      const { member, workspace_name } = p as MemberAddedPayload;
      const myUserId = authStore.getState().user?.id;
      if (member.user_id === myUserId) {
        qc.invalidateQueries({ queryKey: workspaceKeys.list() });
        qc.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
        onToast?.(`You joined ${workspace_name ?? 'a workspace'}`, 'info');
      }
    });

    const unsubInvitationCreated = ws.on('invitation:created', (p) => {
      const { workspace_name } = p as InvitationCreatedPayload;
      qc.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
      onToast?.(`You were invited to ${workspace_name ?? 'a workspace'}`, 'info');
    });

    const unsubInvitationAccepted = ws.on('invitation:accepted', () => {
      const currentWsId = getCurrentWsId();
      if (currentWsId) {
        qc.invalidateQueries({ queryKey: workspaceKeys.invitations(currentWsId) });
        qc.invalidateQueries({ queryKey: workspaceKeys.members(currentWsId) });
      }
    });
    const unsubInvitationDeclined = ws.on('invitation:declined', () => {
      const currentWsId = getCurrentWsId();
      if (currentWsId) {
        qc.invalidateQueries({ queryKey: workspaceKeys.invitations(currentWsId) });
      }
    });
    const unsubInvitationRevoked = ws.on('invitation:revoked', () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
    });

    const unsubTaskMessage = ws.on('task:message', (p) => {
      const payload = p as TaskMessagePayload;
      qc.setQueryData<TaskMessagePayload[]>(chatKeys.taskMessages(payload.task_id), (old = []) =>
        mergeTaskMessagesBySeq(old, [payload]),
      );
      chatWsLogger.debug('task:message (global)', {
        task_id: payload.task_id,
        seq: payload.seq,
        type: payload.type,
      });
    });

    let aggregateRefreshTimer: ReturnType<typeof setTimeout> | null = null;
    const invalidatePendingAggregate = () => {
      if (aggregateRefreshTimer) clearTimeout(aggregateRefreshTimer);
      aggregateRefreshTimer = setTimeout(() => {
        aggregateRefreshTimer = null;
        refetchPendingChatAggregate(qc, getCurrentWsId());
      }, 750);
    };
    const invalidateSessionLists = () => {
      const id = getCurrentWsId();
      if (id) qc.invalidateQueries({ queryKey: chatKeys.sessions(id) });
    };

    const unsubChatMessage = ws.on('chat:message', (p) => {
      const payload = p as { chat_session_id: string };
      chatWsLogger.info('chat:message (global)', { chat_session_id: payload.chat_session_id });
      invalidateChatMessageQueries(qc, payload.chat_session_id);
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      // NOTE: intentionally does NOT touch the pending aggregate. chat:message
      // fires per streamed message with no status; the aggregate is maintained
      // by the task lifecycle handlers below (MUL-4159).
    });

    const unsubChatDone = ws.on('chat:done', (p) => {
      const payload = p as ChatDonePayload;
      chatWsLogger.info('chat:done (global)', {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
        has_message: !!payload.message_id,
      });
      applyChatDoneToCache(qc, payload);
      invalidateSessionLists();
    });

    const unsubChatCancelFinalized = ws.on('chat:cancel_finalized', (p) => {
      const payload = p as ChatCancelFinalizedPayload;
      chatWsLogger.info('chat:cancel_finalized (global)', {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
        outcome: payload.outcome,
      });
      applyChatCancelFinalizedToCache(qc, payload, authStore.getState().user?.id);
      if (payload.outcome === 'stopped') {
        invalidateSessionLists();
      }
    });

    const unsubTaskQueued = ws.on('task:queued', (p) => {
      const payload = p as TaskQueuedPayload;
      if (!payload.chat_session_id) return;
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(payload.chat_session_id), (old) => ({
        ...(old ?? {}),
        task_id: payload.task_id,
        status: 'queued',
      }));
      invalidatePendingAggregate();
    });

    const unsubTaskDispatch = ws.on('task:dispatch', (p) => {
      const payload = p as TaskDispatchPayload;
      if (!payload.chat_session_id) return;
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(payload.chat_session_id), (old) => {
        if (!old || old.task_id !== payload.task_id) return old;
        return { ...old, status: 'running' };
      });
      invalidatePendingAggregate();
    });

    const unsubTaskRunning = ws.on('task:running', (p) => {
      const payload = p as TaskRunningPayload;
      if (!payload.chat_session_id) return;
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(payload.chat_session_id), (old) => {
        if (!old || old.task_id !== payload.task_id) return old;
        return { ...old, status: 'running' };
      });
      invalidatePendingAggregate();
    });

    const unsubTaskWaitingLocalDir = ws.on('task:waiting_local_directory', (p) => {
      const payload = p as TaskWaitingLocalDirectoryPayload;
      if (!payload.chat_session_id) return;
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(payload.chat_session_id), (old) => {
        if (!old || old.task_id !== payload.task_id) return old;
        return { ...old, status: 'waiting_local_directory' };
      });
      invalidatePendingAggregate();
    });

    const unsubTaskCancelled = ws.on('task:cancelled', (p) => {
      const payload = p as TaskCancelledPayload;
      if (!payload.chat_session_id) return;
      chatWsLogger.info('task:cancelled (global, chat)', {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
      invalidateChatMessageQueries(qc, payload.chat_session_id);
      invalidatePendingAggregate();
    });

    const unsubTaskCompleted = ws.on('task:completed', (p) => {
      const payload = p as TaskCompletedPayload;
      if (!payload.chat_session_id) return; 
      chatWsLogger.info('task:completed (global, chat)', {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      invalidatePendingAggregate();
    });

    const unsubTaskFailed = ws.on('task:failed', (p) => {
      const payload = p as TaskFailedPayload;
      if (!payload.chat_session_id) return;
      chatWsLogger.warn('task:failed (global, chat)', {
        task_id: payload.task_id,
        chat_session_id: payload.chat_session_id,
      });
      qc.setQueryData(chatKeys.pendingTask(payload.chat_session_id), {});
      invalidateChatMessageQueries(qc, payload.chat_session_id);
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();
      invalidateSessionLists();
    });

    const unsubChatSessionRead = ws.on('chat:session_read', (p) => {
      const payload = p as { chat_session_id: string };
      chatWsLogger.info('chat:session_read (global)', payload);
      invalidateSessionLists();
    });

    const unsubChatSessionUpdated = ws.on('chat:session_updated', (p) => {
      const payload = p as ChatSessionUpdatedPayload;
      chatWsLogger.info('chat:session_updated (global)', payload);
      const id = getCurrentWsId();
      if (!id) return;
      applyChatSessionUpdatedToCache(qc, id, payload);
    });

    const unsubChatSessionDeleted = ws.on('chat:session_deleted', (p) => {
      const payload = p as { chat_session_id: string };
      chatWsLogger.info('chat:session_deleted (global)', payload);
      const id = getCurrentWsId();
      if (id) {
        const drop = (old?: { id: string }[]) =>
          old?.filter((s) => s.id !== payload.chat_session_id);
        qc.setQueryData(chatKeys.sessions(id), drop);
      }
      qc.removeQueries({ queryKey: chatKeys.messages(payload.chat_session_id) });
      qc.removeQueries({ queryKey: chatKeys.pendingTask(payload.chat_session_id) });
      invalidatePendingAggregate();

      const chatState = useChatStore.getState?.();
      if (chatState && chatState.activeSessionId === payload.chat_session_id) {
        chatState.setActiveSession(null);
      }
    });

    return () => {
      unsubAny();
      unsubIssueUpdated();
      unsubIssueCreated();
      unsubIssueDeleted();
      unsubIssueLabelsChanged();
      unsubIssueMetadataChanged();
      unsubIssuePropertiesChanged();
      unsubPropertyChanged.forEach((unsub) => unsub());
      unsubInboxNew();
      unsubCommentCreated();
      unsubCommentUpdated();
      unsubCommentDeleted();
      unsubCommentResolved();
      unsubCommentUnresolved();
      unsubActivityCreated();
      unsubReactionAdded();
      unsubReactionRemoved();
      unsubIssueReactionAdded();
      unsubIssueReactionRemoved();
      unsubSubscriberAdded();
      unsubSubscriberRemoved();
      unsubWsUpdated();
      unsubWsDeleted();
      unsubMemberRemoved();
      unsubMemberAdded();
      unsubInvitationCreated();
      unsubInvitationAccepted();
      unsubInvitationDeclined();
      unsubInvitationRevoked();
      unsubTaskMessage();
      unsubChatMessage();
      unsubChatDone();
      unsubChatCancelFinalized();
      unsubTaskQueued();
      unsubTaskDispatch();
      unsubTaskRunning();
      unsubTaskWaitingLocalDir();
      unsubTaskCancelled();
      unsubTaskCompleted();
      unsubTaskFailed();
      unsubChatSessionRead();
      unsubChatSessionDeleted();
      unsubChatSessionUpdated();
      if (aggregateRefreshTimer) clearTimeout(aggregateRefreshTimer);
      timers.forEach(clearTimeout);
      timers.clear();
    };
  }, [ws, qc, authStore, onToast]);

  useEffect(() => {
    if (!ws) return;

    const unsub = ws.onReconnect(async () => {
      logger.info('reconnected, refetching all data');
      try {
        invalidateWorkspaceScopedQueries(qc);
      } catch (e) {
        logger.error('reconnect refetch failed', e);
      }
    });

    return unsub;
  }, [ws, qc]);

  const wsInstanceRef = useRef<WSClient | null>(null);
  useEffect(() => {
    if (!ws) return;
    if (wsInstanceRef.current === null) {
      wsInstanceRef.current = ws;
      return;
    }
    if (wsInstanceRef.current === ws) return;
    wsInstanceRef.current = ws;

    logger.info('new WSClient instance detected, invalidating workspace queries');
    invalidateWorkspaceScopedQueries(qc);
  }, [ws, qc]);
}

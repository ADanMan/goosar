'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { toast } from 'sonner';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useAuthStore } from '@goosar/core/auth';
import { agentListOptions, memberListOptions } from '@goosar/core/workspace/queries';
import { projectListOptions } from '@goosar/core/projects/queries';
import { canAssignAgent } from '@goosar/views/issues/components';
import { api, dispatchReasonCode } from '@goosar/core/api';
import { useAgentPresenceDetail, useWorkspaceAgentAvailability } from '@goosar/core/agents';
import {
  chatSessionsOptions,
  chatMessagesPageOptions,
  pendingChatTaskOptions,
  chatKeys,
  isTaskMessageTaskId,
  sortChatSessions,
} from '@goosar/core/chat/queries';
import { useRealtimePollingInterval } from '@goosar/core/realtime';
import {
  useCreateChatSession,
  useMarkChatSessionRead,
  useSetChatSessionProject,
  useSetChatSessionArchived,
} from '@goosar/core/chat/mutations';
import { useChatStore } from '@goosar/core/chat';
import { removeChatMessageFromCaches } from '@goosar/core/realtime';
import { useChatDraftRestore } from './use-chat-draft-restore';
import { useChatProjectContextSupport } from './use-chat-project-context-support';
import { createLogger } from '@goosar/core/logger';
import type {
  Agent,
  Attachment,
  ChatMessage,
  ChatMessagesPage,
  ChatPendingTask,
} from '@goosar/core/types';
import { useT } from '../../i18n';
import { useAppForeground } from '../../common/use-app-foreground';

const uiLogger = createLogger('chat.ui');
const apiLogger = createLogger('chat.api');

const CHAT_TITLE_MAX = 30;
export function deriveChatTitle(content: string): string {
  const firstLine = (content.split('\n').find((l) => l.trim()) ?? content).trim();
  const cleaned = firstLine
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#*`>~_]/g, '')
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1') 
    .replace(/\s+/g, ' ')
    .trim();
  if (cleaned.length <= CHAT_TITLE_MAX) return cleaned;
  return cleaned.slice(0, CHAT_TITLE_MAX - 1).trimEnd() + '…';
}

export function isStillOnComposeTarget(
  liveActiveSessionId: string | null,
  sentFromSessionId: string | null,
): boolean {
  return liveActiveSessionId === sentFromSessionId;
}

export type ProjectContextChange =
  | { kind: 'awaitSession' }
  | { kind: 'detachCurrent'; sessionId: string }
  | { kind: 'startFreshChat'; agentId: string; projectId: string }
  | { kind: 'setDraftProject'; projectId: string | null };

export function planProjectContextChange(input: {
  targetProjectId: string | null;
  activeSessionId: string | null;
  currentSession: { id: string; agent_id: string } | null;
}): ProjectContextChange {
  if (input.activeSessionId) {
    if (!input.currentSession) return { kind: 'awaitSession' };
    if (input.targetProjectId === null) {
      return { kind: 'detachCurrent', sessionId: input.currentSession.id };
    }
    return {
      kind: 'startFreshChat',
      agentId: input.currentSession.agent_id,
      projectId: input.targetProjectId,
    };
  }
  return { kind: 'setDraftProject', projectId: input.targetProjectId };
}

export function hasInFlightPendingTask(
  qc: ReturnType<typeof useQueryClient>,
  sessionId: string,
): boolean {
  const pending = qc.getQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId));
  return Boolean(pending?.task_id);
}
const CHAT_VIRTUOSO_INITIAL_FIRST_ITEM_INDEX = 1_000_000;

function appendChatMessageToLatestPageCache(
  qc: ReturnType<typeof useQueryClient>,
  sessionId: string,
  message: ChatMessage,
) {
  qc.setQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sessionId), (old) => {
    if (!old) {
      return {
        pages: [
          {
            messages: [message],
            limit: 50,
            has_more: false,
            next_cursor: null,
          },
        ],
        pageParams: [null],
      };
    }
    if (old.pages.some((page) => page.messages.some((m) => m.id === message.id))) {
      return old;
    }
    return {
      ...old,
      pages: old.pages.map((page, index) =>
        index === 0 ? { ...page, messages: [...page.messages, message] } : page,
      ),
    };
  });
}

export function useChatController(opts?: { isActive?: boolean }) {
  const isActive = opts?.isActive ?? true;
  const { t } = useT('chat');
  const wsId = useWorkspaceId();
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const selectedProjectId = useChatStore((s) => s.selectedProjectId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const setSelectedProjectId = useChatStore((s) => s.setSelectedProjectId);
  const user = useAuthStore((s) => s.user);
  const { data: agents = [], isSuccess: agentsLoaded } = useQuery(agentListOptions(wsId));
  const { data: members = [], isSuccess: membersLoaded } = useQuery(memberListOptions(wsId));
  const pollingInterval = useRealtimePollingInterval();
  const { data: sessions = [], isSuccess: sessionsLoaded } = useQuery({
    ...chatSessionsOptions(wsId),
    refetchInterval: pollingInterval,
  });
  const { data: projects = [], isSuccess: projectsLoaded } = useQuery(projectListOptions(wsId));
  const {
    data: rawMessagePages,
    isLoading: messagesLoading,
    fetchNextPage: fetchOlderMessages,
    hasNextPage: hasOlderMessages,
    isFetchingNextPage: isFetchingOlderMessages,
  } = useInfiniteQuery({
    ...chatMessagesPageOptions(activeSessionId ?? ''),
    refetchInterval: pollingInterval,
  });

  const messagePages = activeSessionId ? (rawMessagePages?.pages ?? []) : [];
  const messages = [...messagePages].reverse().flatMap((page) => page.messages);
  const olderMessageCount = messagePages
    .slice(1)
    .reduce((sum, page) => sum + page.messages.length, 0);
  const firstItemIndex =
    messages.length > 0 ? CHAT_VIRTUOSO_INITIAL_FIRST_ITEM_INDEX - olderMessageCount : 0;
  const showSkeleton = !!activeSessionId && messagesLoading;

  const { data: pendingTask } = useQuery({
    ...pendingChatTaskOptions(activeSessionId ?? ''),
    refetchInterval: pollingInterval,
  });
  const pendingTaskId = pendingTask?.task_id ?? null;
  const stopRequestedBeforeTaskRef = useRef(false);
  const appForeground = useAppForeground();
  const { restoreDraftRequest, enqueueLocalRestore, handleRestoreDraftApplied } =
    useChatDraftRestore(activeSessionId, isActive && appForeground);
  const [focusInputRequest, setFocusInputRequest] = useState(0);
  const requestInputFocus = useCallback(() => setFocusInputRequest((n) => n + 1), []);

  const currentSession = activeSessionId ? sessions.find((s) => s.id === activeSessionId) : null;
  const isSessionArchived = currentSession?.status === 'archived';
  const candidateProjectId = currentSession
    ? (currentSession.project_id ?? null)
    : selectedProjectId;
  const activeProjectId =
    candidateProjectId &&
    (!projectsLoaded || projects.some((project) => project.id === candidateProjectId))
      ? candidateProjectId
      : null;

  useEffect(() => {
    if (!projectsLoaded || !selectedProjectId) return;
    if (projects.some((project) => project.id === selectedProjectId)) return;
    setSelectedProjectId(null);
  }, [projectsLoaded, projects, selectedProjectId, setSelectedProjectId]);

  const qc = useQueryClient();
  const createSession = useCreateChatSession();
  const markRead = useMarkChatSessionRead();
  const setSessionProject = useSetChatSessionProject();
  const setArchived = useSetChatSessionArchived();

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;
  const availableAgents = agents.filter(
    (a) => !a.archived_at && canAssignAgent(a, user?.id, memberRole),
  );
  const agentsSettled = agentsLoaded && membersLoaded;

  const sessionAgent = currentSession
    ? (agents.find((a) => a.id === currentSession.agent_id) ?? null)
    : null;
  const isAgentArchived = !!sessionAgent?.archived_at;

  const activeAgent =
    sessionAgent ??
    availableAgents.find((a) => a.id === selectedAgentId) ??
    availableAgents[0] ??
    null;

  const agentAvailability = useWorkspaceAgentAvailability();
  const noAgent = agentAvailability === 'none';

  const projectContextSupport = useChatProjectContextSupport(wsId, activeAgent);

  const presenceDetail = useAgentPresenceDetail(wsId, activeAgent?.id);
  const availability = presenceDetail === 'loading' ? undefined : presenceDetail.availability;

  const currentHasUnread = sessions.find((s) => s.id === activeSessionId)?.has_unread ?? false;
  useEffect(() => {
    if (!isActive || !appForeground || !activeSessionId) return;
    if (!currentHasUnread) return;
    const sessionId = activeSessionId;
    const timer = setTimeout(() => {
      if (useChatStore.getState().activeSessionId !== sessionId) return;
      uiLogger.info('auto markRead', { sessionId });
      markRead.mutate(sessionId);
    }, 0);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- markRead ref stable
  }, [isActive, appForeground, activeSessionId, currentHasUnread]);

  const sessionPromiseRef = useRef<Promise<string | null> | null>(null);
  const ensureSession = useCallback(
    async (titleSeed: string): Promise<string | null> => {
      if (
        activeSessionId &&
        (!sessionsLoaded ||
          sessions.some((s) => s.id === activeSessionId) ||
          hasInFlightPendingTask(qc, activeSessionId))
      ) {
        return activeSessionId;
      }
      if (!activeAgent) return null;
      if (sessionPromiseRef.current) return sessionPromiseRef.current;

      const promise = (async () => {
        try {
          const session = await createSession.mutateAsync({
            agent_id: activeAgent.id,
            title: deriveChatTitle(titleSeed),
            project_id: activeProjectId,
          });
          return session.id;
        } finally {
          sessionPromiseRef.current = null;
        }
      })();
      sessionPromiseRef.current = promise;
      return promise;
    },
    [activeSessionId, activeAgent, activeProjectId, createSession, sessions, sessionsLoaded, qc],
  );

  useEffect(() => {
    if (!activeSessionId || !sessionsLoaded) return;
    if (sessions.some((s) => s.id === activeSessionId)) return;
    if (hasInFlightPendingTask(qc, activeSessionId)) return;
    uiLogger.info('clearing dangling activeSessionId', { sessionId: activeSessionId });
    setActiveSession(null);
  }, [activeSessionId, sessionsLoaded, sessions, qc, setActiveSession]);

  const uploadEnabled = !!activeAgent;

  const cancelChatTask = useCallback(
    async (
      taskId: string,
      sessionId: string,
      options: { restoreDraftToInput: boolean; source: string },
    ) => {
      apiLogger.info('cancelTask.start', {
        taskId,
        sessionId,
        source: options.source,
      });
      qc.setQueryData(chatKeys.pendingTask(sessionId), {});

      try {
        const result = await api.cancelTaskById(taskId);
        const restored = result.cancelled_chat_message;
        if (restored?.restore_to_input) {
          removeChatMessageFromCaches(qc, restored.chat_session_id, restored.message_id);
          if (options.restoreDraftToInput && restored.chat_session_id === sessionId) {
            enqueueLocalRestore({
              id: restored.message_id,
              content: restored.content,
              attachments: restored.attachments,
              sessionId: restored.chat_session_id,
            });
          }
        }
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
        apiLogger.info('cancelTask.success', {
          taskId,
          sessionId,
          restoredToInput: !!restored?.restore_to_input && options.restoreDraftToInput,
        });
        return result;
      } catch (err) {
        apiLogger.warn('cancelTask.error (task may have already finished)', {
          taskId,
          sessionId,
          err,
        });
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
        return null;
      }
    },
    [qc, enqueueLocalRestore],
  );

  const handleSend = useCallback(
    async (
      content: string,
      attachmentIds?: string[],
      commitInput?: (options?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => void,
      draftAttachments: Attachment[] = [],
    ): Promise<boolean> => {
      if (!activeAgent) {
        apiLogger.warn('sendChatMessage skipped: no active agent');
        return false;
      }
      if (isAgentArchived) {
        apiLogger.warn('sendChatMessage skipped: agent is archived', {
          sessionId: activeSessionId,
          agentId: activeAgent.id,
        });
        return false;
      }

      const finalContent = content;
      const isNewSession = !activeSessionId;

      apiLogger.info('sendChatMessage.start', {
        sessionId: activeSessionId,
        isNewSession,
        agentId: activeAgent.id,
        contentLength: finalContent.length,
        attachmentCount: attachmentIds?.length ?? 0,
      });

      let sessionId: string | null = null;
      try {
        sessionId = await ensureSession(finalContent);
      } catch (err) {
        apiLogger.error('sendChatMessage.ensureSession.error', err);
        toast.error(
          dispatchReasonCode(err) === 'invocation_not_allowed'
            ? t(($) => $.input.send_blocked_toast)
            : t(($) => $.input.send_failed_toast),
        );
        return false;
      }
      if (!sessionId) {
        apiLogger.warn('sendChatMessage aborted: ensureSession returned null');
        return false;
      }

      let result;
      try {
        result = await api.sendChatMessage(sessionId, finalContent, attachmentIds);
      } catch (err) {
        apiLogger.error('sendChatMessage.error', { sessionId, err });
        toast.error(
          dispatchReasonCode(err) === 'invocation_not_allowed'
            ? t(($) => $.input.send_blocked_toast)
            : t(($) => $.input.send_failed_toast),
        );
        return false;
      }
      apiLogger.info('sendChatMessage.success', {
        sessionId,
        messageId: result.message_id,
        taskId: result.task_id,
      });

      const sent: ChatMessage = {
        id: result.message_id,
        chat_session_id: sessionId,
        role: 'user',
        content: finalContent,
        task_id: result.task_id,
        created_at: result.created_at,
        attachments: draftAttachments,
      };
      appendChatMessageToLatestPageCache(qc, sessionId, sent);
      qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
        old ? [...old, sent] : [sent],
      );
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: result.task_id,
        status: 'queued',
        created_at: result.created_at,
      });
      const live = useChatStore.getState();
      const stillOnSourceSession = isStillOnComposeTarget(live.activeSessionId, activeSessionId);
      if (stillOnSourceSession) {
        setActiveSession(sessionId);
      }
      commitInput?.({ extraDraftKeys: [sessionId], clearEditor: stillOnSourceSession });

      if (stopRequestedBeforeTaskRef.current) {
        stopRequestedBeforeTaskRef.current = false;
        await cancelChatTask(result.task_id, sessionId, {
          restoreDraftToInput: true,
          source: 'deferred-send',
        });
        return false;
      }
      if (attachmentIds && attachmentIds.length > 0 && result.attachment_ids) {
        const boundIds = new Set(result.attachment_ids);
        const missing = attachmentIds.filter((id) => !boundIds.has(id));
        if (missing.length > 0) {
          apiLogger.warn('sendChatMessage.attachments missing after send', {
            sessionId,
            messageId: result.message_id,
            missing,
          });
          toast.error(t(($) => $.input.attachment_bind_failed_toast));
        }
      }
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      return true;
    },
    [
      activeSessionId,
      activeAgent,
      isAgentArchived,
      ensureSession,
      cancelChatTask,
      qc,
      setActiveSession,
      t,
    ],
  );

  const handleStop = useCallback(() => {
    if (!pendingTaskId || !activeSessionId) {
      apiLogger.debug('cancelTask skipped: no pending task');
      return;
    }
    if (!isTaskMessageTaskId(pendingTaskId)) {
      stopRequestedBeforeTaskRef.current = true;
      apiLogger.info('cancelTask.deferred until server task id', {
        taskId: pendingTaskId,
        sessionId: activeSessionId,
      });
      return;
    }
    void cancelChatTask(pendingTaskId, activeSessionId, {
      restoreDraftToInput: true,
      source: 'active-input',
    });
  }, [pendingTaskId, activeSessionId, cancelChatTask]);

  const handleNewChat = useCallback(() => {
    uiLogger.info('newChat', {
      previousSessionId: activeSessionId,
      previousPendingTask: pendingTaskId,
    });
    setSelectedProjectId(null);
    setActiveSession(null);
    requestInputFocus();
  }, [activeSessionId, pendingTaskId, setSelectedProjectId, setActiveSession, requestInputFocus]);

  const handleStartNewChat = useCallback(
    (agent: Agent) => {
      uiLogger.info('startNewChat', {
        agentId: agent.id,
        previousSessionId: activeSessionId,
      });
      setSelectedAgentId(agent.id);
      setSelectedProjectId(null);
      setActiveSession(null);
      requestInputFocus();
    },
    [
      activeSessionId,
      setSelectedAgentId,
      setSelectedProjectId,
      setActiveSession,
      requestInputFocus,
    ],
  );

  const handleSelectSession = useCallback(
    (session: { id: string; agent_id: string; project_id?: string | null }) => {
      if (activeAgent && session.agent_id !== activeAgent.id) {
        uiLogger.info('selectSession (cross-agent)', {
          from: activeAgent.id,
          toAgent: session.agent_id,
          toSession: session.id,
        });
        setSelectedAgentId(session.agent_id);
      }
      setActiveSession(session.id);
    },
    [activeAgent, setSelectedAgentId, setActiveSession],
  );

  const handleProjectChange = useCallback(
    (projectId: string | null) => {
      if (projectId === activeProjectId) return;
      uiLogger.info('selectProjectContext', {
        from: activeProjectId,
        to: projectId,
        previousSessionId: activeSessionId,
      });
      const plan = planProjectContextChange({
        targetProjectId: projectId,
        activeSessionId,
        currentSession: currentSession ?? null,
      });
      switch (plan.kind) {
        case 'awaitSession':
          return;
        case 'detachCurrent':
          setSessionProject.mutate({ sessionId: plan.sessionId, projectId: null });
          break;
        case 'startFreshChat':
          setSelectedAgentId(plan.agentId);
          setSelectedProjectId(plan.projectId);
          setActiveSession(null);
          break;
        case 'setDraftProject':
          setSelectedProjectId(plan.projectId);
          break;
      }
      requestInputFocus();
    },
    [
      activeProjectId,
      activeSessionId,
      currentSession,
      setSessionProject,
      setSelectedAgentId,
      setSelectedProjectId,
      setActiveSession,
      requestInputFocus,
    ],
  );

  const advanceSelectionAfterArchive = useCallback(
    (session: { id: string; agent_id: string }) => {
      if (activeSessionId !== session.id) return;
      const history = sortChatSessions(sessions.filter((s) => s.status !== 'archived'));
      const idx = history.findIndex((s) => s.id === session.id);
      const next = history[idx + 1] ?? history[idx - 1] ?? null;
      if (next) handleSelectSession(next);
      else setActiveSession(null);
    },
    [activeSessionId, sessions, handleSelectSession, setActiveSession],
  );

  const archiveSession = useCallback(
    (sessionId: string) => setArchived.mutate({ sessionId, archived: true }),
    [setArchived],
  );

  const hasMessages = messages.length > 0 || !!pendingTaskId;

  return {
    wsId,
    user,
    agents,
    availableAgents,
    agentsSettled,
    sessions,
    projects,
    activeSessionId,
    selectedAgentId,
    activeProjectId,
    projectContextUnsupported: projectContextSupport === false,
    isProjectUpdating: setSessionProject.isPending || (!!activeSessionId && !currentSession),
    currentSession,
    isSessionArchived,
    isAgentArchived,
    activeAgent,
    noAgent,
    availability,
    messages,
    pendingTask,
    pendingTaskId,
    showSkeleton,
    hasMessages,
    firstItemIndex,
    hasOlderMessages: !!hasOlderMessages,
    isFetchingOlderMessages,
    fetchOlderMessages,
    restoreDraftRequest,
    handleRestoreDraftApplied,
    focusInputRequest,
    handleSend,
    handleStop,
    uploadEnabled,
    handleNewChat,
    handleStartNewChat,
    handleSelectSession,
    handleProjectChange,
    advanceSelectionAfterArchive,
    archiveSession,
    setActiveSession,
    setSelectedAgentId,
  };
}

export type ChatController = ReturnType<typeof useChatController>;

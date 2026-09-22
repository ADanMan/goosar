'use client';

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { motion } from 'motion/react';
import {
  Minus,
  Maximize2,
  Minimize2,
  ChevronDown,
  Plus,
  Check,
  Archive,
  Pencil,
  Loader2,
  Square,
} from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';
import { Tooltip, TooltipTrigger, TooltipContent } from '@goosar/ui/components/ui/tooltip';
import { Popover, PopoverContent, PopoverTrigger } from '@goosar/ui/components/ui/popover';
import { toast } from 'sonner';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useAuthStore } from '@goosar/core/auth';
import { agentListOptions, memberListOptions } from '@goosar/core/workspace/queries';
import { projectListOptions } from '@goosar/core/projects/queries';
import { canAssignAgent } from '@goosar/views/issues/components';
import { api } from '@goosar/core/api';
import { runTaskPreflight } from '@goosar/core/platform';
import { useAgentPresenceDetail, useWorkspaceAgentAvailability } from '@goosar/core/agents';
import { ActorAvatar } from '../../common/actor-avatar';
import { useAppForeground } from '../../common/use-app-foreground';
import {
  PickerEmpty,
  PickerItem,
  PickerSection,
  PropertyPicker,
} from '../../issues/components/pickers/property-picker';
import { matchesPinyin } from '../../editor/extensions/pinyin-match';
import { OfflineBanner } from './offline-banner';
import { NoAgentBanner } from './no-agent-banner';
import { ArchivedAgentBanner } from './archived-agent-banner';
import {
  chatSessionsOptions,
  chatMessagesPageOptions,
  pendingChatTaskOptions,
  pendingChatTasksOptions,
  chatKeys,
  isTaskMessageTaskId,
} from '@goosar/core/chat/queries';
import { useRealtimePollingInterval } from '@goosar/core/realtime';
import {
  useCreateChatSession,
  useMarkChatSessionRead,
  useSetChatSessionArchived,
  useSetChatSessionProject,
  useUpdateChatSession,
} from '@goosar/core/chat/mutations';
import { useChatStore } from '@goosar/core/chat';
import { removeChatMessageFromCaches } from '@goosar/core/realtime';
import { useChatDraftRestore } from './use-chat-draft-restore';
import { ChatMessageList, ChatMessageSkeleton } from './chat-message-list';
import { ChatInput } from './chat-input';
import { ChatResizeHandles } from './chat-resize-handles';
import { useChatContextItems } from './use-chat-context-items';
import { useChatResize } from './use-chat-resize';
import {
  hasInFlightPendingTask,
  isStillOnComposeTarget,
  planProjectContextChange,
} from './use-chat-controller';
import { useChatProjectContextSupport } from './use-chat-project-context-support';
import { ChatStarterCards, useChatStarterCards } from './chat-starter-cards';
import { createLogger } from '@goosar/core/logger';
import type {
  Agent,
  Attachment,
  ChatMessage,
  ChatMessagesPage,
  ChatPendingTask,
  ChatSession,
  PendingChatTasksResponse,
} from '@goosar/core/types';
import { useT, useUiLocale } from '../../i18n';

const uiLogger = createLogger('chat.ui');
const apiLogger = createLogger('chat.api');
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

export function ChatWindow() {
  const { t } = useT('chat');
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const selectedProjectId = useChatStore((s) => s.selectedProjectId);
  const setOpen = useChatStore((s) => s.setOpen);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const setSelectedProjectId = useChatStore((s) => s.setSelectedProjectId);
  const user = useAuthStore((s) => s.user);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const pollingInterval = useRealtimePollingInterval();
  const visiblePollingInterval = isOpen ? pollingInterval : false;
  const { data: sessions = [], isSuccess: sessionsLoaded } = useQuery({
    ...chatSessionsOptions(wsId),
    refetchInterval: visiblePollingInterval,
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
    refetchInterval: visiblePollingInterval,
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
    refetchInterval: visiblePollingInterval,
  });
  const pendingTaskId = pendingTask?.task_id ?? null;
  const stopRequestedBeforeTaskRef = useRef(false);
  const appForeground = useAppForeground();
  const { restoreDraftRequest, enqueueLocalRestore, handleRestoreDraftApplied } =
    useChatDraftRestore(activeSessionId, isOpen && appForeground);
  const [focusRequest, setFocusRequest] = useState(0);
  const requestInputFocus = useCallback(() => setFocusRequest((n) => n + 1), []);

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

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;
  const availableAgents = agents.filter(
    (a) => !a.archived_at && canAssignAgent(a, user?.id, memberRole),
  );

  const sessionAgent = currentSession
    ? (agents.find((a) => a.id === currentSession.agent_id) ?? null)
    : null;
  const isAgentArchived = !!sessionAgent?.archived_at;

  const activeAgent =
    sessionAgent ??
    availableAgents.find((a) => a.id === selectedAgentId) ??
    availableAgents[0] ??
    null;

  const projectContextSupport = useChatProjectContextSupport(wsId, activeAgent);

  const agentAvailability = useWorkspaceAgentAvailability();
  const noAgent = agentAvailability === 'none';

  const presenceDetail = useAgentPresenceDetail(wsId, activeAgent?.id);
  const availability = presenceDetail === 'loading' ? undefined : presenceDetail.availability;

  useEffect(() => {
    uiLogger.info('ChatWindow mount', {
      isOpen,
      activeSessionId,
      pendingTaskId,
      selectedAgentId,
      wsId,
    });
    return () => {
      uiLogger.info('ChatWindow unmount', {
        activeSessionId,
        pendingTaskId,
      });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per mount
  }, []);

  useEffect(() => {
    if (!activeSessionId || !sessionsLoaded) return;
    if (sessions.some((s) => s.id === activeSessionId)) return;
    if (hasInFlightPendingTask(qc, activeSessionId)) return;
    uiLogger.info('clearing dangling activeSessionId (floating)', { sessionId: activeSessionId });
    setActiveSession(null);
  }, [activeSessionId, sessionsLoaded, sessions, qc, setActiveSession]);

  const currentHasUnread = sessions.find((s) => s.id === activeSessionId)?.has_unread ?? false;
  useEffect(() => {
    if (!isOpen || !appForeground || !activeSessionId) return;
    if (!currentHasUnread) return;
    uiLogger.info('auto markRead', { sessionId: activeSessionId });
    markRead.mutate(activeSessionId);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- markRead ref stable
  }, [isOpen, appForeground, activeSessionId, currentHasUnread]);

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
            title: titleSeed.slice(0, 50),
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

      if (!(await runTaskPreflight())) {
        apiLogger.info('sendChatMessage stopped by platform preflight');
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
        toast.error(t(($) => $.input.send_failed_toast));
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
        toast.error(t(($) => $.input.send_failed_toast));
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

  const handleSelectAgent = useCallback(
    (agent: Agent) => {
      if (activeAgent && agent.id === activeAgent.id) return;
      uiLogger.info('selectAgent', {
        from: selectedAgentId,
        to: agent.id,
        previousSessionId: activeSessionId,
      });
      setSelectedAgentId(agent.id);
      setSelectedProjectId(currentSession ? null : activeProjectId);
      setActiveSession(null);
      requestInputFocus();
    },
    [
      activeAgent,
      selectedAgentId,
      activeSessionId,
      activeProjectId,
      currentSession,
      setSelectedAgentId,
      setSelectedProjectId,
      setActiveSession,
      requestInputFocus,
    ],
  );

  const handleNewChat = useCallback(() => {
    uiLogger.info('newChat', {
      previousSessionId: activeSessionId,
      previousPendingTask: pendingTaskId,
    });
    setSelectedProjectId(null);
    setActiveSession(null);
    requestInputFocus();
  }, [activeSessionId, pendingTaskId, setSelectedProjectId, setActiveSession, requestInputFocus]);

  const handleSelectSession = useCallback(
    (session: ChatSession) => {
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

  const handleMinimize = useCallback(() => {
    uiLogger.info('minimize (close)', {
      activeSessionId,
      pendingTaskId,
    });
    setOpen(false);
  }, [activeSessionId, pendingTaskId, setOpen]);

  const isExpanded = useChatStore((s) => s.isExpanded);

  const windowRef = useRef<HTMLDivElement>(null);
  const { renderWidth, renderHeight, isAtMax, boundsReady, isDragging, toggleExpand, startDrag } =
    useChatResize(windowRef);

  const hasMessages = messages.length > 0 || !!pendingTaskId;

  const isVisible = isOpen && (isExpanded || boundsReady);

  const containerClass =
    'absolute bottom-2 right-2 z-50 flex flex-col overflow-hidden rounded-xl bg-surface-raised shadow-[var(--floating-shadow)] ring-1 ring-surface-border';
  const containerStyle: React.CSSProperties = {
    transformOrigin: 'bottom right',
    pointerEvents: isOpen ? 'auto' : 'none',
  };

  const contextItems = useChatContextItems(wsId);

  return (
    <motion.div
      ref={windowRef}
      className={containerClass}
      style={containerStyle}
      initial={{ opacity: 0, scale: 0.95, width: renderWidth, height: renderHeight }}
      animate={{
        opacity: isVisible ? 1 : 0,
        scale: isVisible ? 1 : 0.95,
        width: renderWidth,
        height: renderHeight,
      }}
      transition={{
        width: isDragging ? { duration: 0 } : { type: 'spring', duration: 0.3, bounce: 0 },
        height: isDragging ? { duration: 0 } : { type: 'spring', duration: 0.3, bounce: 0 },
        opacity: { duration: 0.15 },
        scale: { type: 'spring', duration: 0.2, bounce: 0 },
      }}
    >
      <ChatResizeHandles onDragStart={startDrag} />
      {/* Header — ⊕ new + session dropdown | window tools */}
      <div className="flex items-center justify-between border-b px-4 py-2.5 gap-2">
        <div className="flex items-center gap-1 min-w-0">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground"
                  onClick={handleNewChat}
                />
              }
            >
              <Plus />
            </TooltipTrigger>
            <TooltipContent side="top">{t(($) => $.window.new_chat_tooltip)}</TooltipContent>
          </Tooltip>
          <SessionDropdown
            sessions={sessions}
            agents={agents}
            activeSessionId={activeSessionId}
            onSelectSession={handleSelectSession}
          />
        </div>
        <div className="flex items-center gap-0.5 shrink-0">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  onClick={toggleExpand}
                />
              }
            >
              {isExpanded || isAtMax ? <Minimize2 /> : <Maximize2 />}
            </TooltipTrigger>
            <TooltipContent side="top">
              {isExpanded || isAtMax
                ? t(($) => $.window.restore_tooltip)
                : t(($) => $.window.expand_tooltip)}
            </TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  onClick={handleMinimize}
                />
              }
            >
              <Minus />
            </TooltipTrigger>
            <TooltipContent side="top">{t(($) => $.window.minimize_tooltip)}</TooltipContent>
          </Tooltip>
        </div>
      </div>

      {/* Messages / skeleton / empty state */}
      {showSkeleton ? (
        <ChatMessageSkeleton />
      ) : hasMessages ? (
        <ChatMessageList
          key={activeSessionId}
          messages={messages}
          pendingTask={pendingTask}
          availability={availability}
          firstItemIndex={firstItemIndex}
          hasOlderMessages={!!hasOlderMessages}
          isFetchingOlderMessages={isFetchingOlderMessages}
          onLoadOlderMessages={() => void fetchOlderMessages()}
          isVisible={isOpen}
        />
      ) : (
        <EmptyState
          hasSessions={sessions.length > 0}
          agent={activeAgent ?? null}
          onPickPrompt={(text) => handleSend(text)}
        />
      )}

      {/* Status banner above the input — single mutually-exclusive slot.
       *  Priority: no-agent > offline / unstable. Agent presence is the
       *  hard prerequisite (you can't send anything without one), so it
       *  always wins over a presence hint. Recent issue/project navigation
       *  lives in the input action row; it is not message/session state.
       *
       *  We key off `noAgent` (the resolved-empty state) rather than
       *  `!activeAgent`, so the loading window between mount and the
       *  first agent-list response stays banner-free. */}
      {noAgent ? (
        <NoAgentBanner />
      ) : isAgentArchived ? (
        <ArchivedAgentBanner agentName={activeAgent?.name} />
      ) : (
        <OfflineBanner agentName={activeAgent?.name} availability={availability} />
      )}

      {/* Input — disabled for legacy archived sessions and for sessions whose
       *  agent has been archived (read-only); locked out entirely when there's
       *  no agent (the EmptyState above carries the CTA). */}
      <ChatInput
        onSend={handleSend}
        restoreDraftRequest={restoreDraftRequest}
        onRestoreDraftApplied={handleRestoreDraftApplied}
        uploadEnabled={!!activeAgent}
        onStop={handleStop}
        isRunning={!!pendingTaskId}
        disabled={isSessionArchived || isAgentArchived}
        noAgent={noAgent}
        agentArchived={isAgentArchived}
        agentName={activeAgent?.name}
        projects={projects}
        projectId={activeProjectId}
        onProjectChange={handleProjectChange}
        projectContextUnsupported={projectContextSupport === false}
        isProjectUpdating={setSessionProject.isPending || (!!activeSessionId && !currentSession)}
        leftAdornment={
          <AgentDropdown
            agents={availableAgents}
            activeAgent={activeAgent}
            userId={user?.id}
            onSelect={handleSelectAgent}
          />
        }
        contextItems={contextItems}
        focusRequest={focusRequest}
      />
    </motion.div>
  );
}

export function AgentDropdown({
  agents,
  activeAgent,
  userId,
  onSelect,
}: {
  agents: Agent[];
  activeAgent: Agent | null;
  userId: string | undefined;
  onSelect: (agent: Agent) => void;
}) {
  const { t } = useT('chat');
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState('');
  const { mine, others } = useMemo(() => {
    const mine: Agent[] = [];
    const others: Agent[] = [];
    for (const a of agents) {
      if (a.owner_id === userId) mine.push(a);
      else others.push(a);
    }
    return { mine, others };
  }, [agents, userId]);

  const query = filter.trim().toLowerCase();
  const matches = (name: string) =>
    !query || name.toLowerCase().includes(query) || matchesPinyin(name, query);
  const filteredMine = mine.filter((agent) => matches(agent.name));
  const filteredOthers = others.filter((agent) => matches(agent.name));

  const handlePick = (agent: Agent) => {
    onSelect(agent);
    setOpen(false);
  };

  if (!activeAgent) {
    return <span className="text-xs text-muted-foreground">{t(($) => $.window.no_agents)}</span>;
  }

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-64"
      align="start"
      side="top"
      searchable
      searchPlaceholder={t(($) => $.window.agent_filter_placeholder)}
      onSearchChange={setFilter}
      triggerRender={
        <button
          type="button"
          className="flex items-center gap-1.5 rounded-md px-1.5 py-1 -ml-1 cursor-pointer outline-none transition-colors hover:bg-accent aria-expanded:bg-accent"
        />
      }
      trigger={
        <>
          <ActorAvatar
            actorType="agent"
            actorId={activeAgent.id}
            size="md"
            enableHoverCard
            showStatusDot
          />
          <span className="text-xs font-medium max-w-28 truncate">{activeAgent.name}</span>
          <ChevronDown className="size-3 text-muted-foreground shrink-0" />
        </>
      }
    >
      {filteredMine.length === 0 && filteredOthers.length === 0 ? (
        <PickerEmpty />
      ) : (
        <>
          {filteredMine.length > 0 && (
            <PickerSection label={t(($) => $.window.my_agents)}>
              {filteredMine.map((agent) => (
                <AgentPickerItem
                  key={agent.id}
                  agent={agent}
                  isCurrent={agent.id === activeAgent.id}
                  onSelect={handlePick}
                />
              ))}
            </PickerSection>
          )}
          {filteredOthers.length > 0 && (
            <PickerSection label={t(($) => $.window.others)}>
              {filteredOthers.map((agent) => (
                <AgentPickerItem
                  key={agent.id}
                  agent={agent}
                  isCurrent={agent.id === activeAgent.id}
                  onSelect={handlePick}
                />
              ))}
            </PickerSection>
          )}
        </>
      )}
    </PropertyPicker>
  );
}

function AgentPickerItem({
  agent,
  isCurrent,
  onSelect,
}: {
  agent: Agent;
  isCurrent: boolean;
  onSelect: (agent: Agent) => void;
}) {
  return (
    <PickerItem selected={isCurrent} onClick={() => onSelect(agent)}>
      <ActorAvatar actorType="agent" actorId={agent.id} size="md" enableHoverCard showStatusDot />
      <span className="truncate flex-1">{agent.name}</span>
    </PickerItem>
  );
}

function SessionDropdown({
  sessions,
  agents,
  activeSessionId,
  onSelectSession,
}: {
  sessions: ChatSession[];
  agents: Agent[];
  activeSessionId: string | null;
  onSelectSession: (session: ChatSession) => void;
}) {
  const { t } = useT('chat');
  const wsId = useWorkspaceId();
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const title = activeSession?.title?.trim() || t(($) => $.window.untitled);
  const triggerAgent = activeSession ? (agentById.get(activeSession.agent_id) ?? null) : null;

  const historySessions = useMemo(
    () => sessions.filter((s) => s.status !== 'archived'),
    [sessions],
  );

  const [isHistoryOpen, setIsHistoryOpen] = useState(false);
  const [confirmingStopId, setConfirmingStopId] = useState<string | null>(null);
  const [stoppingTaskId, setStoppingTaskId] = useState<string | null>(null);
  const [completedFlashIds, setCompletedFlashIds] = useState<Set<string>>(() => new Set());
  const previousInFlightRef = useRef<Set<string>>(new Set());
  const completedFlashTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const setArchived = useSetChatSessionArchived();
  const updateSession = useUpdateChatSession();
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const queryClient = useQueryClient();
  const formatTimeAgo = useFormatTimeAgo();

  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));
  const pendingTaskBySessionId = useMemo(
    () => new Map((pending?.tasks ?? []).map((task) => [task.chat_session_id, task])),
    [pending],
  );
  const inFlightSessionIds = useMemo(
    () => new Set(pendingTaskBySessionId.keys()),
    [pendingTaskBySessionId],
  );

  useEffect(() => {
    const previous = previousInFlightRef.current;
    const unreadSessionIds = new Set(sessions.filter((s) => s.has_unread).map((s) => s.id));

    for (const sessionId of previous) {
      if (inFlightSessionIds.has(sessionId) || !unreadSessionIds.has(sessionId)) continue;

      setCompletedFlashIds((current) => {
        if (current.has(sessionId)) return current;
        return new Set(current).add(sessionId);
      });

      const existingTimer = completedFlashTimersRef.current.get(sessionId);
      if (existingTimer) clearTimeout(existingTimer);

      const timer = setTimeout(() => {
        setCompletedFlashIds((current) => {
          if (!current.has(sessionId)) return current;
          const next = new Set(current);
          next.delete(sessionId);
          return next;
        });
        completedFlashTimersRef.current.delete(sessionId);
      }, 1600);
      completedFlashTimersRef.current.set(sessionId, timer);
    }

    previousInFlightRef.current = inFlightSessionIds;
  }, [inFlightSessionIds, sessions]);

  useEffect(() => {
    const timers = completedFlashTimersRef.current;
    return () => {
      for (const timer of timers.values()) clearTimeout(timer);
      timers.clear();
    };
  }, []);

  useEffect(() => {
    if (!confirmingStopId || pendingTaskBySessionId.has(confirmingStopId)) return;
    setConfirmingStopId(null);
  }, [confirmingStopId, pendingTaskBySessionId]);

  const currentSessionRunning = activeSessionId ? inFlightSessionIds.has(activeSessionId) : false;
  const otherRunningCount = sessions.filter(
    (s) => s.id !== activeSessionId && inFlightSessionIds.has(s.id),
  ).length;
  const otherUnreadCount = sessions.filter((s) => s.id !== activeSessionId && s.has_unread).length;

  const handleArchive = (session: ChatSession) => {
    if (activeSessionId === session.id) {
      const idx = historySessions.findIndex((s) => s.id === session.id);
      const next = historySessions[idx + 1] ?? historySessions[idx - 1] ?? null;
      if (next) onSelectSession(next);
      else setActiveSession(null);
    }
    setArchived.mutate({ sessionId: session.id, archived: true });
  };

  const handleSubmitRename = (sessionId: string, raw: string) => {
    const trimmed = raw.trim();
    const current = sessions.find((s) => s.id === sessionId);
    setRenamingId(null);
    if (!trimmed || trimmed === current?.title) return;
    updateSession.mutate({ sessionId, title: trimmed });
  };

  const handleSelectSession = (session: ChatSession) => {
    onSelectSession(session);
    setIsHistoryOpen(false);
  };

  const handleConfirmStop = (
    session: ChatSession,
    task: PendingChatTasksResponse['tasks'][number],
  ) => {
    setStoppingTaskId(task.task_id);
    previousInFlightRef.current = new Set(
      [...previousInFlightRef.current].filter((sessionId) => sessionId !== session.id),
    );

    queryClient.setQueryData<PendingChatTasksResponse>(chatKeys.pendingTasks(wsId), (current) => {
      if (!current) return current;
      return {
        ...current,
        tasks: current.tasks.filter((item) => item.task_id !== task.task_id),
      };
    });
    queryClient.setQueryData(chatKeys.pendingTask(session.id), {});
    queryClient.invalidateQueries({ queryKey: chatKeys.messages(session.id) });
    queryClient.invalidateQueries({ queryKey: chatKeys.messagesPage(session.id) });

    api
      .cancelTaskById(task.task_id)
      .then(
        (result) => {
          const restored = result.cancelled_chat_message;
          if (restored?.restore_to_input) {
            removeChatMessageFromCaches(queryClient, restored.chat_session_id, restored.message_id);
          }
          apiLogger.info('cancelTask.success (history row)', {
            taskId: task.task_id,
            sessionId: session.id,
          });
        },
        (err) =>
          apiLogger.warn('cancelTask.error (history row; task may have already finished)', {
            taskId: task.task_id,
            sessionId: session.id,
            err,
          }),
      )
      .finally(() => {
        queryClient.invalidateQueries({ queryKey: chatKeys.pendingTasks(wsId) });
        queryClient.invalidateQueries({ queryKey: chatKeys.pendingTask(session.id) });
        setStoppingTaskId(null);
        setConfirmingStopId(null);
      });
  };

  const renderRow = (session: ChatSession) => {
    const isCurrent = session.id === activeSessionId;
    const agent = agentById.get(session.agent_id) ?? null;
    const pendingTask = pendingTaskBySessionId.get(session.id);
    const isRunning = !!pendingTask;
    const showCompleted = completedFlashIds.has(session.id) && !isCurrent;
    const showUnread = session.has_unread && !isCurrent;
    const isRenaming = renamingId === session.id;
    const isConfirmingStop = confirmingStopId === session.id && !!pendingTask;
    const isConfirmingAction = isConfirmingStop;
    const titleText = session.title?.trim() || t(($) => $.window.untitled);
    const trailingStatus = isRunning
      ? t(($) => $.session_history.row_subtitle.working)
      : showCompleted
        ? t(($) => $.session_history.row_subtitle.completed)
        : showUnread
          ? t(($) => $.session_history.row_subtitle.new_reply)
          : formatTimeAgo(session.updated_at);

    return (
      <div
        key={session.id}
        aria-current={isCurrent ? 'true' : undefined}
        tabIndex={0}
        onClick={() => {
          if (isRenaming || isConfirmingAction) return;
          handleSelectSession(session);
        }}
        onKeyDown={(e) => {
          if (isRenaming || isConfirmingAction) return;
          if (e.key !== 'Enter' && e.key !== ' ') return;
          e.preventDefault();
          handleSelectSession(session);
        }}
        className={cn(
          'group/history-row relative flex min-h-11 min-w-0 cursor-default items-center gap-2 overflow-hidden rounded-md py-1.5 pl-2 pr-2 outline-none transition-colors hover:bg-accent/60 focus-visible:bg-accent/60 focus-visible:ring-1 focus-visible:ring-ring',
          isCurrent && 'bg-accent/70',
          isConfirmingAction && 'bg-destructive/5 hover:bg-destructive/5',
        )}
      >
        {isCurrent && (
          <span className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-full bg-brand" />
        )}
        {agent ? (
          <ActorAvatar
            actorType="agent"
            actorId={agent.id}
            size="md"
            enableHoverCard
            showStatusDot
          />
        ) : (
          <span className="size-6 shrink-0" />
        )}
        <div className="min-w-0 flex-1">
          {isRenaming ? (
            <SessionRenameInput
              initialValue={session.title ?? ''}
              onSubmit={(value) => handleSubmitRename(session.id, value)}
              onCancel={() => setRenamingId(null)}
            />
          ) : isConfirmingStop ? (
            <div className="truncate text-sm font-medium text-destructive">
              {t(($) => $.session_history.stop_dialog.title)}
            </div>
          ) : (
            <div
              className={cn(
                'truncate text-sm',
                (showUnread || showCompleted) && !isRunning && 'font-medium',
              )}
              style={{
                maskImage: 'linear-gradient(to right, black calc(100% - 18px), transparent)',
                WebkitMaskImage: 'linear-gradient(to right, black calc(100% - 18px), transparent)',
              }}
            >
              {titleText}
            </div>
          )}
        </div>
        {!isRenaming &&
          (isConfirmingStop && pendingTask ? (
            <div className="flex shrink-0 items-center gap-1">
              <button
                type="button"
                onPointerDown={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                }}
                onClick={(e) => {
                  e.stopPropagation();
                  e.preventDefault();
                  setConfirmingStopId(null);
                }}
                disabled={stoppingTaskId === pendingTask.task_id}
                className="inline-flex h-7 items-center rounded px-2 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-50"
              >
                {t(($) => $.session_history.stop_dialog.cancel)}
              </button>
              <button
                type="button"
                onPointerDown={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                }}
                onClick={(e) => {
                  e.stopPropagation();
                  e.preventDefault();
                  handleConfirmStop(session, pendingTask);
                }}
                disabled={stoppingTaskId === pendingTask.task_id}
                className="inline-flex h-7 items-center rounded px-2 text-[11px] font-medium text-destructive transition-colors hover:bg-destructive/10 disabled:opacity-50"
              >
                {stoppingTaskId === pendingTask.task_id
                  ? t(($) => $.session_history.stop_dialog.confirming)
                  : t(($) => $.session_history.stop_dialog.confirm)}
              </button>
            </div>
          ) : (
            <div className="flex shrink-0 items-center">
              <div className="flex h-7 items-center justify-end gap-1.5 text-xs text-muted-foreground group-hover/history-row:hidden">
                {isRunning && <Loader2 className="size-3 animate-spin" />}
                {showCompleted && !isRunning && <Check className="size-3 text-emerald-500" />}
                {showUnread && !isRunning && !showCompleted && (
                  <span
                    aria-label={t(($) => $.window.unread)}
                    title={t(($) => $.window.unread)}
                    className="size-1.5 rounded-full bg-brand"
                  />
                )}
                <span
                  className={cn(
                    'truncate',
                    (showUnread || showCompleted || isRunning) && 'font-medium text-foreground',
                  )}
                >
                  {trailingStatus}
                </span>
              </div>
              <div className="hidden h-7 items-center gap-0.5 group-hover/history-row:flex">
                {isRunning && pendingTask && (
                  <button
                    type="button"
                    onPointerDown={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                    }}
                    onClick={(e) => {
                      e.stopPropagation();
                      e.preventDefault();
                      setConfirmingStopId(session.id);
                    }}
                    className="inline-flex h-7 items-center gap-1 rounded px-1.5 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive focus-visible:bg-destructive/10 focus-visible:text-destructive focus-visible:outline-none"
                    aria-label={t(($) => $.session_history.row_stop_aria)}
                    title={t(($) => $.session_history.row_stop_aria)}
                  >
                    <Square className="size-2.5 fill-current" />
                    {t(($) => $.session_history.stop_action)}
                  </button>
                )}
                {!isRunning && (
                  <>
                    <button
                      type="button"
                      onPointerDown={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                      }}
                      onClick={(e) => {
                        e.stopPropagation();
                        e.preventDefault();
                        setRenamingId(session.id);
                      }}
                      className="inline-flex size-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:bg-accent focus-visible:text-foreground focus-visible:outline-none"
                      aria-label={t(($) => $.session_history.row_rename_aria)}
                      title={t(($) => $.session_history.row_rename_aria)}
                    >
                      <Pencil className="size-3.5" />
                    </button>
                    <button
                      type="button"
                      onPointerDown={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                      }}
                      onClick={(e) => {
                        e.stopPropagation();
                        e.preventDefault();
                        handleArchive(session);
                      }}
                      className="inline-flex size-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:bg-accent focus-visible:text-foreground focus-visible:outline-none"
                      aria-label={t(($) => $.list.archive)}
                      title={t(($) => $.list.archive)}
                    >
                      <Archive className="size-3.5" />
                    </button>
                  </>
                )}
              </div>
            </div>
          ))}
      </div>
    );
  };

  return (
    <>
      <Popover open={isHistoryOpen} onOpenChange={setIsHistoryOpen}>
        <div className="flex min-w-0 items-center gap-1">
          <PopoverTrigger className="flex max-w-96 min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 transition-colors hover:bg-accent data-[popup-open]:bg-accent data-open:bg-accent">
            {triggerAgent && (
              <ActorAvatar
                actorType="agent"
                actorId={triggerAgent.id}
                size="md"
                enableHoverCard
                showStatusDot
              />
            )}
            <span className="min-w-0 truncate text-sm font-medium">{title}</span>
            {currentSessionRunning && (
              <Loader2
                aria-label={t(($) => $.session_history.row_subtitle.working)}
                className="size-3 shrink-0 animate-spin text-muted-foreground"
              />
            )}
            <ChevronDown className="size-3 text-muted-foreground shrink-0" />
          </PopoverTrigger>
          {otherRunningCount > 0 ? (
            <span
              aria-label={t(($) => $.window.another_running)}
              title={t(($) => $.window.another_running)}
              className="inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-1.5 text-xs font-medium text-muted-foreground"
            >
              <Loader2 className="size-3 animate-spin" />
              {otherRunningCount > 1 && <span>{otherRunningCount}</span>}
            </span>
          ) : otherUnreadCount > 0 ? (
            <span
              aria-label={t(($) => $.window.another_unread)}
              title={t(($) => $.window.another_unread)}
              className="inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-1.5 text-xs font-medium text-muted-foreground"
            >
              <span className="size-1.5 rounded-full bg-brand" />
              {otherUnreadCount > 1 && <span>{otherUnreadCount}</span>}
            </span>
          ) : null}
        </div>
        <PopoverContent
          align="start"
          className="max-h-96 w-auto min-w-[max(16rem,var(--anchor-width,16rem))] max-w-96 gap-0 overflow-y-auto p-1"
          onClick={(e) => e.stopPropagation()}
        >
          {historySessions.length === 0 ? (
            <div className="px-2 py-1.5 text-xs text-muted-foreground">
              {t(($) => $.window.no_previous)}
            </div>
          ) : (
            <div role="group" aria-label={t(($) => $.window.history_group)}>
              <div className="px-1.5 py-1 text-xs font-medium text-muted-foreground">
                {t(($) => $.window.history_group)}
              </div>
              {historySessions.map(renderRow)}
            </div>
          )}
        </PopoverContent>
      </Popover>
    </>
  );
}

function SessionRenameInput({
  initialValue,
  onSubmit,
  onCancel,
}: {
  initialValue: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT('chat');
  const [value, setValue] = useState(initialValue);
  const inputRef = useRef<HTMLInputElement>(null);
  const valueRef = useRef(value);
  valueRef.current = value;
  const onSubmitRef = useRef(onSubmit);
  onSubmitRef.current = onSubmit;

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();

    const handlePointerDown = (e: PointerEvent) => {
      const input = inputRef.current;
      if (!input) return;
      if (input.contains(e.target as Node)) return;
      onSubmitRef.current(valueRef.current);
    };
    document.addEventListener('pointerdown', handlePointerDown, true);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
    };
  }, []);

  return (
    <input
      ref={inputRef}
      type="text"
      value={value}
      maxLength={200}
      aria-label={t(($) => $.session_history.row_rename_aria)}
      onChange={(e) => setValue(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === 'Enter') {
          e.preventDefault();
          onSubmit(value);
        } else if (e.key === 'Escape') {
          e.preventDefault();
          onCancel();
        }
      }}
      className="w-full rounded-sm bg-background px-1 py-0.5 text-sm outline-none ring-1 ring-border focus-visible:ring-brand"
    />
  );
}

function useFormatTimeAgo(): (dateStr: string) => string {
  const { t } = useT('chat');
  const uiLocale = useUiLocale();
  return (dateStr: string) => {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 1) return t(($) => $.session_history.time.just_now);
    if (diffMins < 60) return t(($) => $.session_history.time.minutes, { count: diffMins });
    if (diffHours < 24) return t(($) => $.session_history.time.hours, { count: diffHours });
    if (diffDays < 7) return t(($) => $.session_history.time.days, { count: diffDays });
    return date.toLocaleDateString(uiLocale);
  };
}

const STARTER_KEYS: ('list_open' | 'summarize_today' | 'plan_next')[] = [
  'list_open',
  'summarize_today',
  'plan_next',
];
const STARTER_ICONS: Record<(typeof STARTER_KEYS)[number], string> = {
  list_open: '📋',
  summarize_today: '📝',
  plan_next: '💡',
};

function EmptyState({
  hasSessions,
  agent,
  onPickPrompt,
}: {
  hasSessions: boolean;
  agent: Agent | null;
  onPickPrompt: (text: string) => void;
}) {
  const { t } = useT('chat');
  const starterCards = useChatStarterCards(agent);
  const agentName = agent?.name;
  if (!hasSessions) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 py-8">
        <div className="text-center space-y-3">
          <h3 className="text-base font-semibold">{t(($) => $.empty_state.first_time_title)}</h3>
          <p className="text-sm text-muted-foreground">
            {t(($) => $.empty_state.first_time_intro)}{' '}
            <span className="font-medium text-foreground">
              {t(($) => $.empty_state.first_time_pillars)}
            </span>
            {t(($) => $.empty_state.first_time_pillars_suffix)}
          </p>
          <p className="text-sm text-muted-foreground">
            {t(($) => $.empty_state.first_time_actions)}
          </p>
        </div>
        <ChatStarterCards cards={starterCards} onPick={onPickPrompt} />
      </div>
    );
  }

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-5 px-6 py-8">
      <div className="text-center space-y-1">
        <h3 className="text-base font-semibold">
          {agentName
            ? t(($) => $.empty_state.returning_title_named, { name: agentName })
            : t(($) => $.empty_state.returning_title_default)}
        </h3>
        <p className="text-sm text-muted-foreground">
          {t(($) => $.empty_state.returning_subtitle)}
        </p>
      </div>
      {starterCards.length > 0 ? (
        <ChatStarterCards cards={starterCards} onPick={onPickPrompt} />
      ) : (
        <div className="w-full max-w-xs space-y-2">
          {STARTER_KEYS.map((key) => {
            const text = t(($) => $.starter_prompts[key]);
            return (
              <button
                key={key}
                type="button"
                onClick={() => onPickPrompt(text)}
                className="w-full rounded-lg border border-border bg-card px-3 py-2 text-left text-sm text-foreground transition-colors hover:bg-accent hover:border-brand/40"
              >
                <span className="mr-2">{STARTER_ICONS[key]}</span>
                {text}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

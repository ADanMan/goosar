// Вкладка чата, одноэкранная. Заголовок с переключателем сессии, список сообщений
// с живым статусом, баннер офлайна и композер.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, KeyboardAvoidingView, Platform, View } from 'react-native';
import { router } from 'expo-router';
import { useFocusEffect, useIsFocused } from '@react-navigation/native';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { Agent, ChatMessage, ChatPendingTask } from '@goosar/core/types';
import { api } from '@/data/api';
import { useAuthStore } from '@/data/auth-store';
import { useWorkspaceStore } from '@/data/workspace-store';
import { agentListOptions } from '@/data/queries/agents';
import { memberListOptions } from '@/data/queries/members';
import {
  chatKeys,
  chatMessagesOptions,
  chatSessionsOptions,
  pendingChatTaskOptions,
  taskMessagesOptions,
} from '@/data/queries/chat';
import {
  useCreateChatSession,
  useDeleteChatSession,
  useMarkChatSessionRead,
} from '@/data/mutations/chat';
import { DRAFT_NEW_SESSION, useChatDraftsStore } from '@/data/stores/chat-drafts-store';
import { useChatSessionPickerStore } from '@/data/stores/chat-session-picker-store';
import { useChatSessionRealtime } from '@/data/realtime/use-chat-session-realtime';
import { canAssignAgent } from '@/lib/can-assign-agent';
import { useWorkspaceAgentAvailability } from '@/lib/workspace-agent-availability';
import { useAgentPresence } from '@/lib/use-agent-presence';
import { Header } from '@/components/ui/header';
import { ChatTitleButton } from '@/components/chat/chat-title-button';
import { ChatSessionActions } from '@/components/chat/chat-session-actions';
import { ChatMessageList } from '@/components/chat/chat-message-list';
import { ChatComposer } from '@/components/chat/chat-composer';
import { AgentPickerSheet } from '@/components/chat/agent-picker-sheet';
import { NoAgentBanner } from '@/components/chat/no-agent-banner';
import { OfflineBanner } from '@/components/chat/offline-banner';
import { useChatSelectStore } from '@/data/chat-select-store';

export default function ChatTab() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id);

  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [agentPickerOpen, setAgentPickerOpen] = useState(false);

  const setStoreActiveSessionId = useChatSessionPickerStore((s) => s.setActiveSessionId);
  const selectRequest = useChatSessionPickerStore((s) => s.selectRequest);
  const consumeSelect = useChatSessionPickerStore((s) => s.consumeSelect);
  useEffect(() => {
    setStoreActiveSessionId(activeSessionId);
  }, [activeSessionId, setStoreActiveSessionId]);

  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const hydratedWsRef = useRef<string | null>(null);
  useEffect(() => {
    if (!wsId) return;
    if (hydratedWsRef.current === wsId) return;
    if (sessions.length === 0) {
      hydratedWsRef.current = wsId;
      return;
    }
    hydratedWsRef.current = wsId;
    setActiveSessionId(sessions[0].id);
  }, [wsId, sessions]);
  const { data: messages = [], isLoading: messagesLoading } = useQuery(
    chatMessagesOptions(activeSessionId),
  );
  const { data: pendingTask } = useQuery(pendingChatTaskOptions(activeSessionId));
  const { data: liveTaskMessages = [] } = useQuery(taskMessagesOptions(pendingTask?.task_id));

  const memberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );

  const availableAgents = useMemo(
    () => agents.filter((a) => !a.archived_at && canAssignAgent(a, userId, memberRole)),
    [agents, userId, memberRole],
  );

  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeSessionId) ?? null,
    [sessions, activeSessionId],
  );

  const currentAgent: Agent | null = useMemo(() => {
    if (selectedAgentId) {
      return availableAgents.find((a) => a.id === selectedAgentId) ?? null;
    }
    if (activeSession) {
      return agents.find((a) => a.id === activeSession.agent_id) ?? null;
    }
    return availableAgents[0] ?? null;
  }, [selectedAgentId, availableAgents, activeSession, agents]);

  const availability = useWorkspaceAgentAvailability();
  const presenceDetail = useAgentPresence(wsId, currentAgent?.id);
  const presenceAvailability =
    presenceDetail === 'loading' ? undefined : presenceDetail.availability;
  const isArchived = activeSession?.status === 'archived';
  const sending = !!pendingTask?.task_id;

  const draftKey = activeSessionId ?? DRAFT_NEW_SESSION;
  const draft = useChatDraftsStore((s) => s.drafts[draftKey] ?? '');
  const setDraft = useChatDraftsStore((s) => s.setDraft);
  const clearDraft = useChatDraftsStore((s) => s.clearDraft);
  const promoteNewDraft = useChatDraftsStore((s) => s.promoteNewDraft);

  useChatSessionRealtime(activeSessionId, () => {
    setActiveSessionId(null);
  });

  useFocusEffect(useCallback(() => () => useChatSelectStore.getState().clear(), []));

  const isFocused = useIsFocused();
  const markRead = useMarkChatSessionRead();
  useEffect(() => {
    if (!isFocused) return;
    if (!activeSessionId) return;
    if (!activeSession?.has_unread) return;
    markRead.mutate(activeSessionId);
  }, [isFocused, activeSessionId, activeSession?.has_unread, markRead]);

  const createSession = useCreateChatSession();
  const deleteSession = useDeleteChatSession();

  const sessionPromiseRef = useRef<Promise<string | null> | null>(null);

  const ensureSession = useCallback(
    async (titleSeed: string): Promise<string | null> => {
      if (activeSessionId) return activeSessionId;
      if (!currentAgent) return null;
      if (sessionPromiseRef.current) return sessionPromiseRef.current;

      const promise = (async () => {
        try {
          const session = await createSession.mutateAsync({
            agent_id: currentAgent.id,
            title: titleSeed.slice(0, 50),
          });
          return session.id;
        } finally {
          sessionPromiseRef.current = null;
        }
      })();
      sessionPromiseRef.current = promise;
      return promise;
    },
    [activeSessionId, currentAgent, createSession],
  );

  const handleSend = useCallback(
    async (content: string, attachmentIds: string[] = []) => {
      if (!currentAgent) return;

      const isNewSession = !activeSessionId;
      const sessionId = await ensureSession(content);
      if (!sessionId) return;

      const sentAt = new Date().toISOString();
      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: sessionId,
        role: 'user',
        content,
        task_id: null,
        created_at: sentAt,
      };
      qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
        old ? [...old, optimistic] : [optimistic],
      );
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: `optimistic-${optimistic.id}`,
        status: 'queued',
        created_at: sentAt,
      });
      if (isNewSession) {
        promoteNewDraft(sessionId);
        setActiveSessionId(sessionId);
      }

      try {
        const result = await api.sendChatMessage(sessionId, content, {
          attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
        });
        qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
          task_id: result.task_id,
          status: 'queued',
          created_at: result.created_at,
        });
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        clearDraft(sessionId);
      } catch (err) {
        qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
          old ? old.filter((m) => m.id !== optimistic.id) : old,
        );
        qc.setQueryData(chatKeys.pendingTask(sessionId), {});
        throw err;
      }
    },
    [activeSessionId, currentAgent, ensureSession, qc, promoteNewDraft, clearDraft],
  );

  const handleStop = useCallback(() => {
    if (!pendingTask?.task_id || !activeSessionId) return;
    qc.setQueryData(chatKeys.pendingTask(activeSessionId), {});
    void api.cancelTaskById(pendingTask.task_id).catch(() => {
      // Silent — task may have already terminated server-side.
    });
  }, [pendingTask?.task_id, activeSessionId, qc]);

  const handleNewChat = useCallback(() => {
    if (availableAgents.length > 1) {
      setAgentPickerOpen(true);
      return;
    }
    setSelectedAgentId(null);
    setActiveSessionId(null);
  }, [availableAgents.length]);

  const handlePickAgent = useCallback((agent: Agent) => {
    setSelectedAgentId(agent.id);
    setActiveSessionId(null);
  }, []);

  useEffect(() => {
    if (!selectRequest) return;
    setSelectedAgentId(null);
    setActiveSessionId(selectRequest.id);
    consumeSelect();
  }, [selectRequest, consumeSelect]);

  const handleDeleteActive = useCallback(() => {
    if (!activeSession) return;
    Alert.alert(
      'Delete this chat?',
      activeSession.title || 'Untitled chat',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Delete',
          style: 'destructive',
          onPress: () => {
            const id = activeSession.id;
            setActiveSessionId(null);
            deleteSession.mutate(id);
          },
        },
      ],
      { cancelable: true },
    );
  }, [activeSession, deleteSession]);

  const disabled = !currentAgent || availability === 'none' || isArchived === true;
  const disabledReason = !currentAgent
    ? 'No agent selected'
    : availability === 'none'
      ? 'No agents in this workspace'
      : isArchived
        ? 'This chat is archived'
        : undefined;

  return (
    <View className="flex-1 bg-background">
      <Header
        center={
          <ChatTitleButton
            currentSession={activeSession}
            currentAgent={currentAgent}
            onPress={() => {
              if (!wsSlug) return;
              router.push({
                pathname: '/[workspace]/chat-sessions',
                params: { workspace: wsSlug },
              });
            }}
          />
        }
        right={
          <ChatSessionActions
            showMore={!!activeSession}
            onMorePress={handleDeleteActive}
            onNewPress={handleNewChat}
          />
        }
      />
      {availability === 'none' ? <NoAgentBanner /> : null}
      <KeyboardAvoidingView
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        className="flex-1"
      >
        <ChatMessageList
          messages={messages}
          loading={messagesLoading}
          hasSessions={sessions.length > 0}
          agentName={currentAgent?.name}
          onPickPrompt={(text) => setDraft(draftKey, text)}
          pendingTask={pendingTask}
          liveTaskMessages={liveTaskMessages}
          availability={presenceAvailability}
        />
        <OfflineBanner agentName={currentAgent?.name} availability={presenceAvailability} />
        <ChatComposer
          value={draft}
          onChangeText={(next) => setDraft(draftKey, next)}
          onSend={handleSend}
          onStop={handleStop}
          sending={sending}
          disabled={disabled}
          disabledReason={disabledReason}
        />
      </KeyboardAvoidingView>

      <AgentPickerSheet
        visible={agentPickerOpen}
        agents={availableAgents}
        currentAgentId={currentAgent?.id ?? null}
        onPick={handlePickAgent}
        onClose={() => setAgentPickerOpen(false)}
      />
    </View>
  );
}

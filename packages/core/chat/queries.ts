import { infiniteQueryOptions, queryOptions } from '@tanstack/react-query';
import { api } from '../api';
import type { TaskMessagePayload } from '../types/events';
import type { ChatSession } from '../types/chat';

export const chatKeys = {
  all: (wsId: string) => ['chat', wsId] as const,
  sessions: (wsId: string) => [...chatKeys.all(wsId), 'sessions'] as const,
  session: (wsId: string, id: string) => [...chatKeys.all(wsId), 'session', id] as const,
  messagesAll: () => ['chat', 'messages'] as const,
  messages: (sessionId: string) => [...chatKeys.messagesAll(), sessionId] as const,
  messagesPageAll: () => ['chat', 'messages-page'] as const,
  messagesPage: (sessionId: string) => [...chatKeys.messagesPageAll(), sessionId] as const,
  pendingTaskAll: () => ['chat', 'pending-task'] as const,
  pendingTask: (sessionId: string) => [...chatKeys.pendingTaskAll(), sessionId] as const,
  draftRestoresAll: () => ['chat', 'draft-restores'] as const,
  draftRestores: (sessionId: string) => [...chatKeys.draftRestoresAll(), sessionId] as const,
  pendingTasks: (wsId: string) => [...chatKeys.all(wsId), 'pending-tasks'] as const,
  pinnedAgents: (wsId: string) => [...chatKeys.all(wsId), 'pinned-agents'] as const,
  pendingTasksHasAny: (wsId: string) =>
    [...chatKeys.all(wsId), 'pending-tasks', 'has-any'] as const,
  taskMessagesAll: () => ['task-messages'] as const,
  taskMessages: (taskId: string) => [...chatKeys.taskMessagesAll(), taskId] as const,
};

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isTaskMessageTaskId(taskId: string | null | undefined): taskId is string {
  return typeof taskId === 'string' && UUID_PATTERN.test(taskId);
}

export function chatSessionsOptions(wsId: string) {
  return queryOptions({
    queryKey: chatKeys.sessions(wsId),
    queryFn: () => api.listChatSessions({ status: 'all' }),
    staleTime: Infinity,
  });
}

function sessionActivityTime(s: ChatSession): number {
  return new Date(s.last_message?.created_at ?? s.updated_at).getTime();
}

export function sortChatSessions(sessions: ChatSession[]): ChatSession[] {
  return [...sessions].sort((a, b) => {
    const ap = a.pinned ? 1 : 0;
    const bp = b.pinned ? 1 : 0;
    if (ap !== bp) return bp - ap;
    return sessionActivityTime(b) - sessionActivityTime(a);
  });
}

export function countUnreadChatSessions(sessions: ChatSession[]): number {
  return sessions.filter((s) => s.has_unread && s.status !== 'archived').length;
}

export function chatPinnedAgentsOptions(wsId: string) {
  return queryOptions({
    queryKey: chatKeys.pinnedAgents(wsId),
    queryFn: () => api.listChatPinnedAgents(),
    staleTime: Infinity,
  });
}

export function chatSessionOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: chatKeys.session(wsId, id),
    queryFn: () => api.getChatSession(id),
    enabled: !!id,
    staleTime: Infinity,
  });
}

export function chatMessagesOptions(sessionId: string) {
  return queryOptions({
    queryKey: chatKeys.messages(sessionId),
    queryFn: () => api.listChatMessages(sessionId),
    enabled: !!sessionId,
    staleTime: Infinity,
  });
}

export function chatMessagesPageOptions(sessionId: string, limit = 50) {
  return infiniteQueryOptions({
    queryKey: chatKeys.messagesPage(sessionId),
    queryFn: ({ pageParam }) => api.listChatMessagesPage(sessionId, { before: pageParam, limit }),
    initialPageParam: null as { created_at: string; id: string } | null,
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? (lastPage.next_cursor ?? undefined) : undefined,
    enabled: !!sessionId,
    staleTime: Infinity,
  });
}

export function pendingChatTaskOptions(sessionId: string) {
  return queryOptions({
    queryKey: chatKeys.pendingTask(sessionId),
    queryFn: () => api.getPendingChatTask(sessionId),
    enabled: !!sessionId,
    staleTime: Infinity,
  });
}

export function chatDraftRestoresOptions(sessionId: string) {
  return queryOptions({
    queryKey: chatKeys.draftRestores(sessionId),
    queryFn: () => api.listChatDraftRestores(sessionId),
    enabled: !!sessionId,
    staleTime: 0,
  });
}

export function taskMessagesOptions(taskId: string) {
  return queryOptions({
    queryKey: chatKeys.taskMessages(taskId),
    queryFn: () => api.listTaskMessages(taskId),
    enabled: isTaskMessageTaskId(taskId),
    staleTime: Infinity,
  });
}

export function highestTaskMessageSeq(
  messages: readonly TaskMessagePayload[] | undefined,
): number | null {
  if (!messages || messages.length === 0) return null;
  let highest: number | null = null;
  for (const message of messages) {
    if (highest === null || message.seq > highest) highest = message.seq;
  }
  return highest;
}

export function mergeTaskMessagesBySeq(
  existing: readonly TaskMessagePayload[],
  incoming: readonly TaskMessagePayload[],
): TaskMessagePayload[] {
  if (incoming.length === 0) return existing as TaskMessagePayload[];
  const knownSeqs = new Set(existing.map((m) => m.seq));
  const fresh = incoming.filter((m) => !knownSeqs.has(m.seq));
  if (fresh.length === 0) return existing as TaskMessagePayload[];
  return [...existing, ...fresh].sort((a, b) => a.seq - b.seq);
}

export function pendingChatTasksOptions(wsId: string) {
  return queryOptions({
    queryKey: chatKeys.pendingTasks(wsId),
    queryFn: () => api.listPendingChatTasks(),
    staleTime: Infinity,
  });
}

export function hasPendingChatTasksOptions(wsId: string) {
  return queryOptions({
    queryKey: chatKeys.pendingTasksHasAny(wsId),
    queryFn: () => api.hasAnyPendingChatTasks(),
    staleTime: Infinity,
  });
}

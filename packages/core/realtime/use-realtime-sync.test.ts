import { QueryClient, type InfiniteData } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { setApiInstance } from '../api';
import type { ApiClient } from '../api/client';
import { chatKeys } from '../chat/queries';
import { inboxKeys } from '../inbox/queries';
import { issueKeys } from '../issues/queries';
import { notificationPreferenceKeys } from '../notification-preferences/queries';
import { workspaceKeys } from '../workspace/queries';
import type {
  ChatDonePayload,
  ChatMessage,
  ChatPendingTask,
  ChatMessagesPage,
  ChatSession,
  InboxItem,
  Workspace,
} from '../types';
import {
  applyChatCancelFinalizedToCache,
  applyChatDoneToCache,
  applyChatSessionUpdatedToCache,
  applyWorkspaceUpdatedToCache,
  handleInboxNew,
  handleSelfMemberRemoved,
  invalidateChatMessageQueries,
  refetchPendingChatAggregate,
  resolveInboxSourceSlug,
} from './use-realtime-sync';
import { registerWorkspaceAccessRevokedHandler } from '../platform/workspace-access-revoked';

const sessionId = 'session-1';
const taskId = 'task-1';
const messagesKey = chatKeys.messages(sessionId);
const pendingKey = chatKeys.pendingTask(sessionId);

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
}

function userMessage(): ChatMessage {
  return {
    id: 'msg-user',
    chat_session_id: sessionId,
    role: 'user',
    content: 'hello',
    task_id: null,
    created_at: '2026-05-13T05:00:00Z',
  };
}

function donePayload(overrides: Partial<ChatDonePayload> = {}): ChatDonePayload {
  return {
    chat_session_id: sessionId,
    task_id: taskId,
    message_id: 'msg-assistant',
    content: 'done',
    elapsed_ms: 1234,
    created_at: '2026-05-13T05:00:02Z',
    ...overrides,
  };
}

describe('applyChatDoneToCache', () => {
  it('writes the assistant message before clearing pending task', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [userMessage()]);
    qc.setQueryData<ChatPendingTask>(pendingKey, {
      task_id: taskId,
      status: 'running',
    });

    const setQueryData = vi.spyOn(qc, 'setQueryData');

    applyChatDoneToCache(qc, donePayload());

    expect(setQueryData.mock.calls[0]?.[0]).toEqual(messagesKey);
    expect(setQueryData.mock.calls[2]?.[0]).toEqual(pendingKey);
    expect(qc.getQueryData<ChatPendingTask>(pendingKey)).toEqual({});
    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([
      userMessage(),
      {
        id: 'msg-assistant',
        chat_session_id: sessionId,
        role: 'assistant',
        content: 'done',
        task_id: taskId,
        created_at: '2026-05-13T05:00:02Z',
        elapsed_ms: 1234,
        message_kind: 'message',
      },
    ]);
  });

  it('carries message_kind=no_response on the inline assistant message', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [userMessage()]);

    applyChatDoneToCache(qc, donePayload({ content: '', message_kind: 'no_response' }));

    const msgs = qc.getQueryData<ChatMessage[]>(messagesKey);
    expect(msgs?.[1]?.message_kind).toBe('no_response');
  });

  it('does not duplicate a replayed chat done event', () => {
    const qc = createQueryClient();
    const assistant: ChatMessage = {
      id: 'msg-assistant',
      chat_session_id: sessionId,
      role: 'assistant',
      content: 'done',
      task_id: taskId,
      created_at: '2026-05-13T05:00:02Z',
      elapsed_ms: 1234,
    };
    qc.setQueryData<ChatMessage[]>(messagesKey, [userMessage(), assistant]);
    qc.setQueryData<ChatPendingTask>(pendingKey, {
      task_id: taskId,
      status: 'running',
    });

    applyChatDoneToCache(qc, donePayload());

    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([userMessage(), assistant]);
    expect(qc.getQueryData<ChatPendingTask>(pendingKey)).toEqual({});
  });

  it('falls back to invalidation-only when older servers omit message fields', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [userMessage()]);
    qc.setQueryData<ChatPendingTask>(pendingKey, {
      task_id: taskId,
      status: 'running',
    });

    applyChatDoneToCache(qc, donePayload({ message_id: undefined, content: undefined }));

    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([userMessage()]);
    expect(qc.getQueryData<ChatPendingTask>(pendingKey)).toEqual({});
  });
});

describe('applyChatSessionUpdatedToCache', () => {
  const WS_ID = 'ws-1';

  function makeSession(overrides: Partial<ChatSession> = {}): ChatSession {
    return {
      id: 's1',
      workspace_id: WS_ID,
      agent_id: 'agent-1',
      creator_id: 'user-1',
      title: 'Session 1',
      status: 'active',
      has_unread: true,
      unread_count: 2,
      created_at: '2026-07-10T00:00:00Z',
      updated_at: '2026-07-10T00:00:00Z',
      ...overrides,
    };
  }

  it('applies an explicit null project_id from another tab', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [
      makeSession({ project_id: 'project-1' }),
    ]);

    applyChatSessionUpdatedToCache(qc, WS_ID, {
      chat_session_id: 's1',
      project_id: null,
    });

    const row = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!;
    expect(row.project_id).toBeNull();
  });

  it('zeroes unread when a session_updated event archives a cached unread row', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [makeSession()]);

    applyChatSessionUpdatedToCache(qc, WS_ID, {
      chat_session_id: 's1',
      status: 'archived',
      updated_at: '2026-07-10T01:00:00Z',
    });

    const row = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!;
    expect(row.status).toBe('archived');
    expect(row.unread_count).toBe(0);
    expect(row.has_unread).toBe(false);
  });

  it('does not resurrect unread when a session_updated event reactivates a row', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [
      makeSession({ status: 'archived', has_unread: false, unread_count: 0 }),
    ]);

    applyChatSessionUpdatedToCache(qc, WS_ID, {
      chat_session_id: 's1',
      status: 'active',
      updated_at: '2026-07-10T02:00:00Z',
    });

    const row = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!;
    expect(row.status).toBe('active');
    expect(row.unread_count).toBe(0);
    expect(row.has_unread).toBe(false);
  });

  it('leaves unread untouched on a rename-only event', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(WS_ID), [makeSession()]);

    applyChatSessionUpdatedToCache(qc, WS_ID, {
      chat_session_id: 's1',
      title: 'Renamed',
    });

    const row = qc.getQueryData<ChatSession[]>(chatKeys.sessions(WS_ID))![0]!;
    expect(row.title).toBe('Renamed');
    expect(row.status).toBe('active');
    expect(row.unread_count).toBe(2);
    expect(row.has_unread).toBe(true);
  });
});

describe('invalidateChatMessageQueries', () => {
  it('invalidates both legacy and paged chat message caches', () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    invalidateChatMessageQueries(qc, sessionId);

    expect(invalidate).toHaveBeenCalledWith({ queryKey: chatKeys.messages(sessionId) });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: chatKeys.messagesPage(sessionId) });
  });
});

describe('applyChatCancelFinalizedToCache', () => {
  function cancelledUserMessage(): ChatMessage {
    return {
      id: 'msg-cancelled-user',
      chat_session_id: sessionId,
      role: 'user',
      content: 'run the thing',
      task_id: taskId,
      created_at: '2026-05-13T05:00:00Z',
    };
  }

  const draftRestoresKey = chatKeys.draftRestores(sessionId);

  it('inserts the late Stopped. assistant row on a stopped outcome', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [cancelledUserMessage()]);

    applyChatCancelFinalizedToCache(qc, {
      outcome: 'stopped',
      chat_session_id: sessionId,
      task_id: taskId,
      message_id: 'msg-stopped',
      content: 'Stopped.',
      created_at: '2026-05-13T05:00:05Z',
      elapsed_ms: 5000,
    });

    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([
      cancelledUserMessage(),
      {
        id: 'msg-stopped',
        chat_session_id: sessionId,
        role: 'assistant',
        content: 'Stopped.',
        task_id: taskId,
        created_at: '2026-05-13T05:00:05Z',
        elapsed_ms: 5000,
        message_kind: 'message',
      },
    ]);
  });

  it('removes the user message and clears the pending task on a restored outcome', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [cancelledUserMessage()]);
    qc.setQueryData<ChatPendingTask>(pendingKey, {
      task_id: taskId,
      status: 'running',
    });

    applyChatCancelFinalizedToCache(qc, {
      outcome: 'restored',
      chat_session_id: sessionId,
      task_id: taskId,
      message_id: 'msg-cancelled-user',
    });

    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([]);
    expect(qc.getQueryData<ChatPendingTask>(pendingKey)).toEqual({});
  });

  it('invalidates the draft-restores query for the initiator', () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    applyChatCancelFinalizedToCache(
      qc,
      {
        outcome: 'restored',
        chat_session_id: sessionId,
        task_id: taskId,
        message_id: 'msg-cancelled-user',
        initiator_user_id: 'user-initiator',
      },
      'user-initiator',
    );

    expect(invalidate).toHaveBeenCalledWith({ queryKey: draftRestoresKey });
  });

  it('does not fetch the restore for a non-initiator recipient of the broadcast', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [cancelledUserMessage()]);
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    applyChatCancelFinalizedToCache(
      qc,
      {
        outcome: 'restored',
        chat_session_id: sessionId,
        task_id: taskId,
        message_id: 'msg-cancelled-user',
        initiator_user_id: 'user-initiator',
      },
      'user-someone-else',
    );

    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([]);
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: draftRestoresKey });
  });

  it('fails closed when the event omits initiator_user_id', () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    applyChatCancelFinalizedToCache(
      qc,
      {
        outcome: 'restored',
        chat_session_id: sessionId,
        task_id: taskId,
        message_id: 'msg-cancelled-user',
      },
      'user-initiator',
    );

    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: draftRestoresKey });
  });

  it('still settles the caches when a restored outcome carries no message id', () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [cancelledUserMessage()]);
    qc.setQueryData<ChatPendingTask>(pendingKey, {
      task_id: taskId,
      status: 'running',
    });

    applyChatCancelFinalizedToCache(qc, {
      outcome: 'restored',
      chat_session_id: sessionId,
      task_id: taskId,
    });

    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([cancelledUserMessage()]);
    expect(qc.getQueryData<ChatPendingTask>(pendingKey)).toEqual({});
  });
});

describe('refetchPendingChatAggregate (cross-session pending leak guard)', () => {
  const wsId = 'ws-1';

  it("invalidates the aggregate instead of optimistically writing it, so another member's workspace-broadcast task:* event can't flip this user's has_pending", () => {
    const qc = createQueryClient();
    qc.setQueryData(chatKeys.pendingTasksHasAny(wsId), { has_pending: false });
    qc.setQueryData(chatKeys.pendingTasks(wsId), { tasks: [] });
    const invalidate = vi.spyOn(qc, 'invalidateQueries');
    const setData = vi.spyOn(qc, 'setQueryData');

    refetchPendingChatAggregate(qc, wsId);

    expect(qc.getQueryData(chatKeys.pendingTasksHasAny(wsId))).toEqual({
      has_pending: false,
    });
    expect(qc.getQueryData(chatKeys.pendingTasks(wsId))).toEqual({ tasks: [] });
    expect(setData).not.toHaveBeenCalled();

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: chatKeys.pendingTasks(wsId),
    });
  });

  it('no-ops without a workspace id (no accidental cross-workspace invalidation)', () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    refetchPendingChatAggregate(qc, undefined);

    expect(invalidate).not.toHaveBeenCalled();
  });
});

describe('applyWorkspaceUpdatedToCache', () => {
  const wsId = 'ws-1';

  function workspace(overrides: Partial<Workspace> = {}): Workspace {
    return {
      id: wsId,
      name: 'Test',
      slug: 'test',
      description: null,
      context: null,
      settings: {},
      repos: [],
      issue_prefix: 'TES',
      avatar_url: null,
      created_at: '2026-05-18T00:00:00Z',
      updated_at: '2026-05-18T00:00:00Z',
      ...overrides,
    };
  }

  it('invalidates issue cache when issue_prefix changes', () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [workspace({ issue_prefix: 'TES' })]);
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    applyWorkspaceUpdatedToCache(qc, {
      workspace: workspace({ issue_prefix: 'NEW' }),
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: issueKeys.all(wsId),
    });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: workspaceKeys.list(),
    });
    expect(qc.getQueryData<Workspace[]>(workspaceKeys.list())?.[0]?.issue_prefix).toBe('NEW');
  });

  it('does not invalidate issue cache when only non-prefix fields change', () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ issue_prefix: 'TES', name: 'Old name' }),
    ]);
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    applyWorkspaceUpdatedToCache(qc, {
      workspace: workspace({ issue_prefix: 'TES', name: 'New name' }),
    });

    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: issueKeys.all(wsId),
    });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: workspaceKeys.list(),
    });
    expect(qc.getQueryData<Workspace[]>(workspaceKeys.list())?.[0]?.name).toBe('New name');
  });

  it("invalidates issue cache when the workspace isn't in the cached list yet", () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    applyWorkspaceUpdatedToCache(qc, {
      workspace: workspace({ issue_prefix: 'NEW' }),
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: issueKeys.all(wsId),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workspaceKeys.list(),
    });
  });
});

describe('applyChatDoneToCache paged messages', () => {
  it('patches page zero and skips older pages without duplicating replayed events', () => {
    const qc = createQueryClient();
    const older = userMessage();
    const latest: ChatMessage = {
      id: 'msg-latest',
      chat_session_id: sessionId,
      role: 'user',
      content: 'latest',
      task_id: null,
      created_at: '2026-05-13T05:00:01Z',
    };
    qc.setQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sessionId), {
      pages: [
        {
          messages: [latest],
          limit: 1,
          has_more: true,
          next_cursor: { created_at: latest.created_at, id: latest.id },
        },
        { messages: [older], limit: 1, has_more: false, next_cursor: null },
      ],
      pageParams: [null, { created_at: latest.created_at, id: latest.id }],
    });

    applyChatDoneToCache(qc, donePayload());
    applyChatDoneToCache(qc, donePayload());

    const paged = qc.getQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sessionId));

    expect(paged?.pages[0]?.messages.map((m) => m.id)).toEqual(['msg-latest', 'msg-assistant']);
    expect(paged?.pages[1]?.messages.map((m) => m.id)).toEqual(['msg-user']);
  });
});
describe('resolveInboxSourceSlug', () => {
  function workspace(overrides: Partial<Workspace> = {}): Workspace {
    return {
      id: 'ws-a',
      name: 'Workspace A',
      slug: 'workspace-a',
      description: null,
      context: null,
      settings: {},
      repos: [],
      issue_prefix: 'WSA',
      avatar_url: null,
      created_at: '2026-05-18T00:00:00Z',
      updated_at: '2026-05-18T00:00:00Z',
      ...overrides,
    };
  }

  it("resolves the inbox item's source workspace, not another cached one", async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ id: 'ws-b', slug: 'workspace-b', name: 'Workspace B' }),
      workspace(),
    ]);

    await expect(resolveInboxSourceSlug(qc, 'ws-a')).resolves.toBe('workspace-a');
  });

  it('returns null instead of falling back when the workspace is unknown', async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ id: 'ws-b', slug: 'workspace-b' }),
    ]);

    await expect(resolveInboxSourceSlug(qc, 'ws-a')).resolves.toBeNull();
  });

  it('returns null for an empty workspace id without touching the cache', async () => {
    const qc = createQueryClient();
    const ensure = vi.spyOn(qc, 'ensureQueryData');

    await expect(resolveInboxSourceSlug(qc, '')).resolves.toBeNull();
    expect(ensure).not.toHaveBeenCalled();
  });

  it('returns null when the workspace list cannot be fetched', async () => {
    const qc = createQueryClient();
    vi.spyOn(qc, 'ensureQueryData').mockRejectedValueOnce(new Error('network down'));

    await expect(resolveInboxSourceSlug(qc, 'ws-a')).resolves.toBeNull();
  });
});

describe('handleInboxNew', () => {
  function workspace(overrides: Partial<Workspace> = {}): Workspace {
    return {
      id: 'ws-a',
      name: 'Workspace A',
      slug: 'workspace-a',
      description: null,
      context: null,
      settings: {},
      repos: [],
      issue_prefix: 'WSA',
      avatar_url: null,
      created_at: '2026-05-18T00:00:00Z',
      updated_at: '2026-05-18T00:00:00Z',
      ...overrides,
    };
  }

  function inboxItem(overrides: Partial<InboxItem> = {}): InboxItem {
    return {
      id: 'item-1',
      workspace_id: 'ws-a',
      recipient_type: 'member',
      recipient_id: 'member-1',
      actor_type: 'member',
      actor_id: 'member-2',
      type: 'mentioned',
      severity: 'info',
      issue_id: 'issue-1',
      title: 'Mentioned you',
      body: 'in a comment',
      issue_status: null,
      read: false,
      archived: false,
      created_at: '2026-05-18T00:00:00Z',
      details: null,
      ...overrides,
    };
  }

  function stubDesktopAPI() {
    const showNotification = vi.fn();
    (globalThis as Record<string, unknown>).desktopAPI = { showNotification };
    return showNotification;
  }

  afterEach(() => {
    delete (globalThis as Record<string, unknown>).desktopAPI;
  });

  it("still shows the banner when the slug can't be resolved, with an empty slug so the click is a no-op", async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ id: 'ws-b', slug: 'workspace-b' }),
    ]);
    qc.setQueryData(notificationPreferenceKeys.all('ws-a'), {
      preferences: { system_notifications: 'all' },
    });
    const showNotification = stubDesktopAPI();

    await handleInboxNew(qc, inboxItem());

    expect(showNotification).toHaveBeenCalledWith({
      slug: '',
      itemId: 'item-1',
      issueKey: 'issue-1',
      title: 'Mentioned you',
      body: 'in a comment',
    });
  });

  it("invalidates the ITEM's workspace inbox cache and resolves its slug, not the active workspace's", async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ id: 'ws-b', slug: 'workspace-b' }),
      workspace(),
    ]);
    qc.setQueryData(notificationPreferenceKeys.all('ws-a'), {
      preferences: { system_notifications: 'all' },
    });
    const invalidate = vi.spyOn(qc, 'invalidateQueries');
    const showNotification = stubDesktopAPI();

    await handleInboxNew(qc, inboxItem());

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxKeys.all('ws-a'),
    });
    expect(showNotification).toHaveBeenCalledWith(expect.objectContaining({ slug: 'workspace-a' }));
  });

  it("honors the SOURCE workspace's mute preference", async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [workspace()]);
    qc.setQueryData(notificationPreferenceKeys.all('ws-a'), {
      preferences: { system_notifications: 'muted' },
    });
    const showNotification = stubDesktopAPI();

    await handleInboxNew(qc, inboxItem());

    expect(showNotification).not.toHaveBeenCalled();
  });

  afterEach(() => {
    setApiInstance(undefined as unknown as ApiClient);
  });

  it("fetches the SOURCE workspace's preference using its slug when the cache is cold", async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ id: 'ws-b', slug: 'workspace-b', name: 'Workspace B' }),
      workspace(),
    ]);
    const getNotificationPreferences = vi
      .fn()
      .mockResolvedValue({ preferences: { system_notifications: 'all' } });
    setApiInstance({ getNotificationPreferences } as unknown as ApiClient);
    const showNotification = stubDesktopAPI();

    await handleInboxNew(qc, inboxItem());

    expect(getNotificationPreferences).toHaveBeenCalledWith('workspace-a');
    expect(showNotification).toHaveBeenCalledWith(expect.objectContaining({ slug: 'workspace-a' }));
  });

  it('suppresses the banner when the SOURCE workspace is muted on a cold cache', async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [workspace()]);
    const getNotificationPreferences = vi
      .fn()
      .mockResolvedValue({ preferences: { system_notifications: 'muted' } });
    setApiInstance({ getNotificationPreferences } as unknown as ApiClient);
    const showNotification = stubDesktopAPI();

    await handleInboxNew(qc, inboxItem());

    expect(getNotificationPreferences).toHaveBeenCalledWith('workspace-a');
    expect(showNotification).not.toHaveBeenCalled();
  });

  it("never fetches the active workspace's preference when the source slug can't be resolved", async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ id: 'ws-b', slug: 'workspace-b' }),
    ]);
    const getNotificationPreferences = vi
      .fn()
      .mockResolvedValue({ preferences: { system_notifications: 'muted' } });
    setApiInstance({ getNotificationPreferences } as unknown as ApiClient);
    const showNotification = stubDesktopAPI();

    await handleInboxNew(qc, inboxItem());

    expect(getNotificationPreferences).not.toHaveBeenCalled();
    expect(showNotification).toHaveBeenCalledWith(expect.objectContaining({ slug: '' }));
  });

  let webBanners: { title: string; options?: NotificationOptions }[] = [];
  class FakeNotification {
    static permission: NotificationPermission = 'granted';
    onclick: (() => void) | null = null;
    close = vi.fn();
    constructor(
      public title: string,
      public options?: NotificationOptions,
    ) {
      webBanners.push({ title, options });
    }
  }
  function installBrowserNotification(permission: NotificationPermission = 'granted') {
    webBanners = [];
    FakeNotification.permission = permission;
    (globalThis as Record<string, unknown>).window = {
      Notification: FakeNotification,
      focus: vi.fn(),
    };
  }

  afterEach(() => {
    delete (globalThis as Record<string, unknown>).window;
  });

  it('shows a browser banner on web (no desktopAPI) when granted and not muted', async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [workspace()]);
    qc.setQueryData(notificationPreferenceKeys.all('ws-a'), {
      preferences: { system_notifications: 'all' },
    });
    installBrowserNotification('granted');

    await handleInboxNew(qc, inboxItem());

    expect(webBanners).toHaveLength(1);
    expect(webBanners[0]?.title).toBe('Mentioned you');
  });

  it('shows no browser banner when the SOURCE workspace is muted', async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [workspace()]);
    qc.setQueryData(notificationPreferenceKeys.all('ws-a'), {
      preferences: { system_notifications: 'muted' },
    });
    installBrowserNotification('granted');

    await handleInboxNew(qc, inboxItem());

    expect(webBanners).toHaveLength(0);
  });

  it('shows no browser banner when permission is not granted', async () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [workspace()]);
    qc.setQueryData(notificationPreferenceKeys.all('ws-a'), {
      preferences: { system_notifications: 'all' },
    });
    installBrowserNotification('default');

    await handleInboxNew(qc, inboxItem());

    expect(webBanners).toHaveLength(0);
  });
});

describe('handleSelfMemberRemoved', () => {
  afterEach(() => {
    registerWorkspaceAccessRevokedHandler(null);
  });

  it('notifies the platform revocation hook BEFORE relocating away', () => {
    const order: string[] = [];
    registerWorkspaceAccessRevokedHandler((wsId) => order.push(`revoke:${wsId}`));

    handleSelfMemberRemoved('acme', 'ws-lost', {
      relocate: (wsId) => order.push(`relocate:${wsId}`),
    });

    expect(order).toEqual(['revoke:ws-lost', 'relocate:ws-lost']);
  });

  it('does nothing without an active workspace context', () => {
    const revoked = vi.fn();
    const relocate = vi.fn();
    registerWorkspaceAccessRevokedHandler(revoked);

    handleSelfMemberRemoved(null, null, { relocate });
    handleSelfMemberRemoved('acme', null, { relocate });
    handleSelfMemberRemoved(null, 'ws-lost', { relocate });

    expect(revoked).not.toHaveBeenCalled();
    expect(relocate).not.toHaveBeenCalled();
  });
});

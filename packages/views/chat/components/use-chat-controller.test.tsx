import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import type { Agent, ChatSession, Project } from '@goosar/core/types';

interface QueuedRestore {
  id: string;
  content: string;
  attachments?: unknown[];
  sessionId: string;
}

const h = vi.hoisted(() => {
  const store = {
    activeSessionId: null as string | null,
    selectedAgentId: null as string | null,
    selectedProjectId: null as string | null,
    setActiveSession: vi.fn((id: string | null) => {
      store.activeSessionId = id;
    }),
    setSelectedAgentId: vi.fn((id: string | null) => {
      store.selectedAgentId = id;
    }),
    setSelectedProjectId: vi.fn((id: string | null) => {
      store.selectedProjectId = id;
    }),
    appliedDraftRestoreIds: [] as string[],
    markDraftRestoreApplied: vi.fn((id: string) => {
      if (!store.appliedDraftRestoreIds.includes(id)) {
        store.appliedDraftRestoreIds = [...store.appliedDraftRestoreIds, id];
      }
    }),
    forgetDraftRestoreApplied: vi.fn((id: string) => {
      store.appliedDraftRestoreIds = store.appliedDraftRestoreIds.filter((x) => x !== id);
    }),
    pendingSendRestores: {} as Record<string, QueuedRestore[]>,
    enqueuePendingSendRestore: vi.fn((r: QueuedRestore) => {
      const existing = store.pendingSendRestores[r.sessionId] ?? [];
      if (existing.some((q) => q.id === r.id)) return;
      store.pendingSendRestores = {
        ...store.pendingSendRestores,
        [r.sessionId]: [...existing, r],
      };
    }),
    dequeuePendingSendRestore: vi.fn((sessionId: string, restoreId: string) => {
      const existing = store.pendingSendRestores[sessionId] ?? [];
      const remaining = existing.filter((q) => q.id !== restoreId);
      const next = { ...store.pendingSendRestores };
      if (remaining.length > 0) next[sessionId] = remaining;
      else delete next[sessionId];
      store.pendingSendRestores = next;
    }),
  };
  return {
    store,
    archivedMutate: vi.fn(),
    markReadMutate: vi.fn(),
    createSessionMutate: vi.fn(async () => ({ id: 'new-session' })),
    appForeground: { value: true },
    consumeRestoreMutate: vi.fn(),
    setProjectMutate: vi.fn(),
    removeFromCaches: vi.fn(),
    sessions: [] as ChatSession[],
    agents: [] as Agent[],
    projects: [] as Project[],
    draftRestores: null as {
      restores: { id: string; chat_session_id: string; content: string }[];
    } | null,
    pollingInterval: false as number | false,
    capturedRefetchIntervals: {
      messages: undefined as unknown,
      pendingTask: undefined as unknown,
    },
  };
});

vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-1' }));
vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (sel: (s: { user: { id: string } }) => unknown) => sel({ user: { id: 'user-1' } }),
}));
vi.mock('@goosar/core/workspace/queries', () => ({
  agentListOptions: () => ({ queryKey: ['agents'] }),
  memberListOptions: () => ({ queryKey: ['members'] }),
}));
vi.mock('@goosar/core/projects/queries', () => ({
  projectListOptions: () => ({ queryKey: ['projects'] }),
}));
vi.mock('@goosar/views/issues/components', () => ({ canAssignAgent: () => true }));
vi.mock('@goosar/core/api', () => ({
  api: { sendChatMessage: vi.fn(), cancelTaskById: vi.fn() },
  dispatchReasonCode: () => undefined,
}));
vi.mock('@goosar/core/agents', () => ({
  useAgentPresenceDetail: () => ({ availability: 'online' }),
  useWorkspaceAgentAvailability: () => 'available',
}));
vi.mock('@goosar/core/hooks/use-file-upload', () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));
vi.mock('@goosar/core/chat/mutations', () => ({
  useCreateChatSession: () => ({ mutateAsync: h.createSessionMutate }),
  useMarkChatSessionRead: () => ({ mutate: h.markReadMutate }),
  useSetChatSessionArchived: () => ({ mutate: h.archivedMutate }),
  useSetChatSessionProject: () => ({
    mutate: h.setProjectMutate,
    isPending: false,
  }),
  useConsumeChatDraftRestore: () => ({ mutate: h.consumeRestoreMutate }),
}));
vi.mock('../../common/use-app-foreground', () => ({
  useAppForeground: () => h.appForeground.value,
}));
vi.mock('@goosar/core/chat', () => ({
  useChatStore: Object.assign((sel: (s: typeof h.store) => unknown) => sel(h.store), {
    getState: () => h.store,
  }),
}));
vi.mock('@goosar/core/realtime', () => ({
  removeChatMessageFromCaches: h.removeFromCaches,
  useRealtimePollingInterval: () => h.pollingInterval,
}));
vi.mock('@goosar/core/logger', () => ({
  createLogger: () => ({ info: vi.fn(), warn: vi.fn(), error: vi.fn(), debug: vi.fn() }),
}));
vi.mock('../../i18n', () => ({ useT: () => ({ t: () => 'x' }) }));
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>();
  return {
    ...actual,
    useQuery: (options: { queryKey?: unknown[]; refetchInterval?: unknown }) => {
      const key = options.queryKey ?? [];
      if (key.includes('agents')) return { data: h.agents };
      if (key.includes('members')) {
        return { data: [{ user_id: 'user-1', role: 'admin' }] };
      }
      if (key.includes('sessions')) return { data: h.sessions, isSuccess: true };
      if (key.includes('projects')) return { data: h.projects, isSuccess: true };
      if (key.includes('draft-restores')) return { data: h.draftRestores };
      if (key.includes('pending-task')) {
        h.capturedRefetchIntervals.pendingTask = options.refetchInterval;
        return { data: undefined };
      }
      return { data: null };
    },
    useInfiniteQuery: (options: { refetchInterval?: unknown }) => {
      h.capturedRefetchIntervals.messages = options.refetchInterval;
      return {
        data: undefined,
        isLoading: false,
        fetchNextPage: vi.fn(),
        hasNextPage: false,
        isFetchingNextPage: false,
      };
    },
    useQueryClient: () => ({
      getQueryData: vi.fn(),
      setQueryData: vi.fn(),
      invalidateQueries: vi.fn(),
    }),
  };
});

import { useChatController } from './use-chat-controller';
import { api } from '@goosar/core/api';

function makeSession(
  overrides: Partial<ChatSession> & Pick<ChatSession, 'id' | 'agent_id'>,
): ChatSession {
  return {
    workspace_id: 'ws-1',
    creator_id: 'user-1',
    title: `Chat ${overrides.id}`,
    status: 'active',
    has_unread: false,
    unread_count: 0,
    last_message: null,
    pinned: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    ...overrides,
  };
}

const agentA = { id: 'agent-a', name: 'Alpha' } as unknown as Agent;
const agentB = { id: 'agent-b', name: 'Beta' } as unknown as Agent;

const sA = makeSession({ id: 'sA', agent_id: 'agent-a', updated_at: '2026-07-08T03:00:00Z' });
const sB = makeSession({ id: 'sB', agent_id: 'agent-b', updated_at: '2026-07-08T02:00:00Z' });
const sC = makeSession({ id: 'sC', agent_id: 'agent-a', updated_at: '2026-07-08T01:00:00Z' });

function setup(activeSessionId: string | null, sessions: ChatSession[], agents: Agent[]) {
  h.store.activeSessionId = activeSessionId;
  h.store.selectedAgentId = null;
  h.store.selectedProjectId = null;
  h.sessions = sessions;
  h.agents = agents;
  h.projects = [];
  const { result } = renderHook(() => useChatController());
  h.store.setActiveSession.mockClear();
  h.store.setSelectedAgentId.mockClear();
  h.store.setSelectedProjectId.mockClear();
  h.archivedMutate.mockClear();
  return result;
}

describe('useChatController project context', () => {
  const project = {
    id: 'project-a',
    workspace_id: 'ws-1',
    title: 'Project Alpha',
  } as Project;
  const otherProject = {
    id: 'project-b',
    workspace_id: 'ws-1',
    title: 'Project Beta',
  } as Project;

  beforeEach(() => {
    h.store.setActiveSession.mockClear();
    h.store.setSelectedProjectId.mockClear();
    h.createSessionMutate.mockClear();
    h.setProjectMutate.mockClear();
    h.createSessionMutate.mockResolvedValue({ id: 'new-session' });
  });

  afterEach(() => {
    h.store.selectedProjectId = null;
    h.projects = [];
  });

  it('removes the project from the current chat without leaving it', () => {
    const projectSession = makeSession({
      id: 'project-session',
      agent_id: 'agent-a',
      project_id: project.id,
    });
    h.store.activeSessionId = projectSession.id;
    h.store.selectedProjectId = project.id;
    h.sessions = [projectSession];
    h.agents = [agentA];
    h.projects = [project];

    const { result } = renderHook(() => useChatController());
    h.store.setActiveSession.mockClear();
    h.store.setSelectedProjectId.mockClear();

    act(() => result.current.handleProjectChange(null));

    expect(h.setProjectMutate).toHaveBeenCalledWith({
      sessionId: projectSession.id,
      projectId: null,
    });
    expect(h.store.setSelectedProjectId).not.toHaveBeenCalled();
    expect(h.store.setActiveSession).not.toHaveBeenCalled();
  });

  it('starts a fresh chat when switching an existing chat to another project', () => {
    const projectSession = makeSession({
      id: 'project-session',
      agent_id: 'agent-a',
      project_id: project.id,
    });
    h.store.activeSessionId = projectSession.id;
    h.store.selectedProjectId = project.id;
    h.sessions = [projectSession];
    h.agents = [agentA];
    h.projects = [project, otherProject];

    const { result } = renderHook(() => useChatController());
    h.store.setActiveSession.mockClear();
    h.store.setSelectedProjectId.mockClear();

    act(() => result.current.handleProjectChange(otherProject.id));

    expect(h.setProjectMutate).not.toHaveBeenCalled();
    expect(h.store.setSelectedProjectId).toHaveBeenCalledWith(otherProject.id);
    expect(h.store.setActiveSession).toHaveBeenCalledWith(null);
  });

  it("pins the fresh chat to the open session's agent when selectedAgentId is stale", () => {
    const projectSession = makeSession({
      id: 'project-session',
      agent_id: agentB.id,
      project_id: project.id,
    });
    h.store.activeSessionId = projectSession.id;
    h.store.selectedAgentId = agentA.id; 
    h.store.selectedProjectId = project.id;
    h.sessions = [projectSession];
    h.agents = [agentA, agentB];
    h.projects = [project, otherProject];

    const { result } = renderHook(() => useChatController());
    h.store.setActiveSession.mockClear();
    h.store.setSelectedAgentId.mockClear();
    h.store.setSelectedProjectId.mockClear();

    act(() => result.current.handleProjectChange(otherProject.id));

    expect(h.store.setSelectedAgentId).toHaveBeenCalledWith(agentB.id);
    expect(h.store.setSelectedProjectId).toHaveBeenCalledWith(otherProject.id);
    expect(h.store.setActiveSession).toHaveBeenCalledWith(null);
  });

  it('does not write project changes into the new-chat draft while an active session resolves', () => {
    h.store.activeSessionId = 'loading-session';
    h.store.selectedProjectId = project.id;
    h.sessions = [];
    h.agents = [agentA];
    h.projects = [project];

    const { result } = renderHook(() => useChatController());
    h.store.setActiveSession.mockClear();
    h.store.setSelectedProjectId.mockClear();

    act(() => result.current.handleProjectChange(null));

    expect(h.setProjectMutate).not.toHaveBeenCalled();
    expect(h.store.setSelectedProjectId).not.toHaveBeenCalled();
    expect(h.store.setActiveSession).not.toHaveBeenCalled();
  });

  it('persists the selected project when lazy-creating a session', async () => {
    h.store.activeSessionId = null;
    h.store.selectedAgentId = agentA.id;
    h.store.selectedProjectId = project.id;
    h.sessions = [];
    h.agents = [agentA];
    h.projects = [project];
    vi.mocked(api.sendChatMessage).mockResolvedValue({
      message_id: 'message-1',
      task_id: 'task-1',
      created_at: new Date(0).toISOString(),
    } as Awaited<ReturnType<typeof api.sendChatMessage>>);

    const { result } = renderHook(() => useChatController());
    await act(async () => {
      await result.current.handleSend('hello', undefined, vi.fn());
    });

    expect(h.createSessionMutate).toHaveBeenCalledWith(
      expect.objectContaining({ project_id: project.id }),
    );
  });

  it('does not inherit the current session project when starting a new chat', () => {
    const projectSession = makeSession({
      id: 'project-session',
      agent_id: 'agent-a',
      project_id: project.id,
    });
    h.store.activeSessionId = projectSession.id;
    h.store.selectedProjectId = project.id;
    h.sessions = [projectSession];
    h.agents = [agentA, agentB];
    h.projects = [project];

    const { result } = renderHook(() => useChatController());
    h.store.setActiveSession.mockClear();
    h.store.setSelectedProjectId.mockClear();

    act(() => result.current.handleStartNewChat(agentB));

    expect(h.store.setSelectedProjectId).toHaveBeenCalledWith(null);
    expect(h.store.setActiveSession).toHaveBeenCalledWith(null);
  });

  it('clears the current session project when using the plain new-chat action', () => {
    const projectSession = makeSession({
      id: 'project-session',
      agent_id: 'agent-a',
      project_id: project.id,
    });
    h.store.activeSessionId = projectSession.id;
    h.store.selectedProjectId = project.id;
    h.sessions = [projectSession];
    h.agents = [agentA];
    h.projects = [project];

    const { result } = renderHook(() => useChatController());
    h.store.setActiveSession.mockClear();
    h.store.setSelectedProjectId.mockClear();

    act(() => result.current.handleNewChat());

    expect(h.store.setSelectedProjectId).toHaveBeenCalledWith(null);
    expect(h.store.setActiveSession).toHaveBeenCalledWith(null);
  });

  it('keeps a historical session project out of the next-chat draft state', () => {
    const projectSession = makeSession({
      id: 'project-session',
      agent_id: 'agent-a',
      project_id: project.id,
    });
    h.store.activeSessionId = null;
    h.store.selectedProjectId = null;
    h.sessions = [projectSession];
    h.agents = [agentA];
    h.projects = [project];

    const { result } = renderHook(() => useChatController());
    h.store.setActiveSession.mockClear();
    h.store.setSelectedProjectId.mockClear();

    act(() => result.current.handleSelectSession(projectSession));

    expect(h.store.setSelectedProjectId).not.toHaveBeenCalled();
    expect(h.store.setActiveSession).toHaveBeenCalledWith(projectSession.id);
  });
});

describe('useChatController.advanceSelectionAfterArchive', () => {
  beforeEach(() => {
    h.store.setActiveSession.mockClear();
    h.store.setSelectedAgentId.mockClear();
    h.archivedMutate.mockClear();
  });

  it('advances to the next chat and syncs the selected agent across agents', () => {
    const result = setup('sA', [sA, sB, sC], [agentA, agentB]);
    act(() => result.current.advanceSelectionAfterArchive(sA));

    expect(h.store.setActiveSession).toHaveBeenCalledWith('sB');
    expect(h.store.setSelectedAgentId).toHaveBeenCalledWith('agent-b');
  });

  it('does not touch the selected agent when the next chat is the same agent', () => {
    const a1 = makeSession({ id: 'a1', agent_id: 'agent-a', updated_at: '2026-07-08T03:00:00Z' });
    const a2 = makeSession({ id: 'a2', agent_id: 'agent-a', updated_at: '2026-07-08T02:00:00Z' });
    const result = setup('a1', [a1, a2], [agentA, agentB]);
    act(() => result.current.advanceSelectionAfterArchive(a1));

    expect(h.store.setActiveSession).toHaveBeenCalledWith('a2');
    expect(h.store.setSelectedAgentId).not.toHaveBeenCalled();
  });

  it('falls back to the previous chat when archiving the last open one', () => {
    const result = setup('sC', [sA, sB, sC], [agentA, agentB]);
    act(() => result.current.advanceSelectionAfterArchive(sC));

    expect(h.store.setActiveSession).toHaveBeenCalledWith('sB');
  });

  it('clears the selection when archiving the only chat', () => {
    const only = makeSession({ id: 'only', agent_id: 'agent-a' });
    const result = setup('only', [only], [agentA]);
    act(() => result.current.advanceSelectionAfterArchive(only));

    expect(h.store.setActiveSession).toHaveBeenCalledWith(null);
    expect(h.store.setSelectedAgentId).not.toHaveBeenCalled();
  });

  it('is a no-op when the archived chat is not the open one', () => {
    const result = setup('sB', [sA, sB, sC], [agentA, agentB]);
    act(() => result.current.advanceSelectionAfterArchive(sA));

    expect(h.store.setActiveSession).not.toHaveBeenCalled();
    expect(h.store.setSelectedAgentId).not.toHaveBeenCalled();
  });
});

describe('useChatController.archiveSession', () => {
  it('fires the archive mutation for the given session', () => {
    const result = setup('sA', [sA, sB, sC], [agentA, agentB]);
    act(() => result.current.archiveSession('sA'));

    expect(h.archivedMutate).toHaveBeenCalledWith({ sessionId: 'sA', archived: true });
  });
});

describe('useChatController auto mark-read', () => {
  const unread = makeSession({
    id: 'sA',
    agent_id: 'agent-a',
    has_unread: true,
    unread_count: 2,
  });

  beforeEach(() => {
    vi.useFakeTimers();
    h.store.activeSessionId = null;
    h.store.selectedAgentId = null;
    h.markReadMutate.mockClear();
    h.appForeground.value = true;
    h.sessions = [unread];
    h.agents = [agentA];
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('marks read a session that stays active past the tick', () => {
    h.store.activeSessionId = 'sA';
    renderHook(() => useChatController());

    expect(h.markReadMutate).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).toHaveBeenCalledWith('sA');
  });

  it('does NOT mark read a session that was only momentarily active on mount', () => {
    h.store.activeSessionId = 'sA';
    const { rerender } = renderHook(() => useChatController());

    h.store.activeSessionId = null;
    rerender();

    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).not.toHaveBeenCalled();
  });
});

describe('useChatController auto mark-read — foreground gating (MUL-4485)', () => {
  const unreadActive = makeSession({ id: 'sU', agent_id: 'agent-a', has_unread: true });

  beforeEach(() => {
    vi.useFakeTimers();
    h.markReadMutate.mockClear();
    h.appForeground.value = true;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function renderController(activeSessionId: string | null, sessions: ChatSession[]) {
    h.store.activeSessionId = activeSessionId;
    h.store.selectedAgentId = null;
    h.sessions = sessions;
    h.agents = [agentA];
    return renderHook(() => useChatController({ isActive: true }));
  }

  it('marks the active unread session read while the app is in the foreground', () => {
    renderController('sU', [unreadActive]);
    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).toHaveBeenCalledWith('sU');
  });

  it('does NOT mark read while the app is backgrounded, so the reply stays unread', () => {
    h.appForeground.value = false;
    renderController('sU', [unreadActive]);
    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).not.toHaveBeenCalled();
  });

  it('marks read once the user returns to the foreground', () => {
    h.appForeground.value = false;
    const { rerender } = renderController('sU', [unreadActive]);
    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).not.toHaveBeenCalled();

    act(() => {
      h.appForeground.value = true;
      rerender();
    });
    act(() => vi.advanceTimersByTime(1));
    expect(h.markReadMutate).toHaveBeenCalledWith('sU');
  });
});

describe('useChatController durable draft restores (#5219)', () => {
  beforeEach(() => {
    h.draftRestores = null;
    h.consumeRestoreMutate.mockClear();
    h.removeFromCaches.mockClear();
    h.store.appliedDraftRestoreIds = [];
    h.store.markDraftRestoreApplied.mockClear();
    h.store.forgetDraftRestoreApplied.mockClear();
    h.store.pendingSendRestores = {};
    h.store.enqueuePendingSendRestore.mockClear();
    h.store.dequeuePendingSendRestore.mockClear();
    h.appForeground.value = true;
  });

  it('hands a fetched restore to the composer and consumes it after the hand-off', () => {
    h.draftRestores = {
      restores: [{ id: 'msg-1', chat_session_id: 'sA', content: 'run the thing' }],
    };
    const result = setup('sA', [sA, sB, sC], [agentA, agentB]);

    expect(result.current.restoreDraftRequest).toEqual({
      id: 'msg-1',
      content: 'run the thing',
      attachments: undefined,
      sessionId: 'sA',
      serverRestoreId: 'msg-1',
    });
    expect(h.removeFromCaches).toHaveBeenCalledWith(expect.anything(), 'sA', 'msg-1');
    expect(h.consumeRestoreMutate).not.toHaveBeenCalled();

    act(() => {
      result.current.handleRestoreDraftApplied();
    });

    expect(h.store.markDraftRestoreApplied).toHaveBeenCalledWith('msg-1');
    expect(h.consumeRestoreMutate).toHaveBeenCalledWith(
      { sessionId: 'sA', restoreId: 'msg-1' },
      expect.anything(),
    );
    expect(result.current.restoreDraftRequest).toBeNull();
  });

  it('reconciles a row whose consume was lost instead of re-offering it', () => {
    h.store.appliedDraftRestoreIds = ['msg-1'];
    h.draftRestores = {
      restores: [{ id: 'msg-1', chat_session_id: 'sA', content: 'run the thing' }],
    };
    const result = setup('sA', [sA, sB, sC], [agentA, agentB]);

    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.consumeRestoreMutate).toHaveBeenCalledWith(
      { sessionId: 'sA', restoreId: 'msg-1' },
      expect.anything(),
    );
  });

  it('leaves a restore belonging to another session pending', () => {
    h.draftRestores = {
      restores: [{ id: 'msg-2', chat_session_id: 'sB', content: 'other session draft' }],
    };
    const result = setup('sA', [sA, sB, sC], [agentA, agentB]);

    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.consumeRestoreMutate).not.toHaveBeenCalled();
  });

  it('does NOT offer or consume a restore while the app is backgrounded', () => {
    h.appForeground.value = false;
    h.draftRestores = {
      restores: [{ id: 'msg-1', chat_session_id: 'sA', content: 'run the thing' }],
    };
    const result = setup('sA', [sA, sB, sC], [agentA, agentB]);

    expect(result.current.restoreDraftRequest).toBeNull();
    expect(h.consumeRestoreMutate).not.toHaveBeenCalled();
  });

  it('offers the restore once the surface returns to the foreground', () => {
    h.appForeground.value = false;
    h.draftRestores = {
      restores: [{ id: 'msg-1', chat_session_id: 'sA', content: 'run the thing' }],
    };
    h.store.activeSessionId = 'sA';
    h.store.selectedAgentId = null;
    h.sessions = [sA, sB, sC];
    h.agents = [agentA, agentB];
    const { result, rerender } = renderHook(() => useChatController());

    expect(result.current.restoreDraftRequest).toBeNull();

    act(() => {
      h.appForeground.value = true;
      rerender();
    });

    expect(result.current.restoreDraftRequest?.serverRestoreId).toBe('msg-1');
  });
});

describe('useChatController.handleSend — compose target tracking', () => {
  beforeEach(() => {
    h.store.setActiveSession.mockClear();
    h.createSessionMutate.mockClear();
    h.createSessionMutate.mockResolvedValue({ id: 'new-session' });
    vi.mocked(api.sendChatMessage).mockResolvedValue({
      message_id: 'msg-1',
      task_id: 'task-1',
      created_at: new Date(0).toISOString(),
    } as unknown as Awaited<ReturnType<typeof api.sendChatMessage>>);
  });

  function sendFrom(activeSessionId: string | null, whileSending?: () => void) {
    h.store.activeSessionId = activeSessionId;
    h.store.selectedAgentId = 'agent-a';
    h.sessions = [sA];
    h.agents = [agentA];
    const { result } = renderHook(() => useChatController());
    h.store.setActiveSession.mockClear();
    const commitInput = vi.fn();
    return {
      commitInput,
      send: () =>
        act(async () => {
          const pending = result.current.handleSend('hello', undefined, commitInput);
          whileSending?.();
          await pending;
        }),
    };
  }

  it("scrubs the composer after a new chat's first send", async () => {
    const { commitInput, send } = sendFrom(null);
    await send();

    expect(commitInput).toHaveBeenCalledWith(
      expect.objectContaining({ clearEditor: true, extraDraftKeys: ['new-session'] }),
    );
    expect(h.store.setActiveSession).toHaveBeenCalledWith('new-session');
  });

  it('scrubs the composer even if the agent picker moved mid-send', async () => {
    const { commitInput, send } = sendFrom(null, () => {
      h.store.selectedAgentId = 'agent-b';
    });
    await send();

    expect(commitInput).toHaveBeenCalledWith(
      expect.objectContaining({ clearEditor: true, extraDraftKeys: ['new-session'] }),
    );
    expect(h.createSessionMutate).toHaveBeenCalledWith(
      expect.objectContaining({ agent_id: 'agent-a' }),
    );
    expect(h.store.setActiveSession).toHaveBeenCalledWith('new-session');
  });

  it('keeps the input when session create fails, and opens nothing', async () => {
    h.createSessionMutate.mockRejectedValue(new Error('create failed'));
    const { commitInput, send } = sendFrom(null);
    await send();

    expect(commitInput).not.toHaveBeenCalled();
    expect(h.store.setActiveSession).not.toHaveBeenCalled();
  });

  it('leaves the composer alone when the user navigated to another session mid-send', async () => {
    const { commitInput, send } = sendFrom(null, () => {
      h.store.activeSessionId = 'sB';
    });
    await send();

    expect(commitInput).toHaveBeenCalledWith(
      expect.objectContaining({ clearEditor: false, extraDraftKeys: ['new-session'] }),
    );
    expect(h.store.setActiveSession).not.toHaveBeenCalled();
  });

  it('still clears the sent draft when sending from an existing session', async () => {
    const { commitInput, send } = sendFrom('sA');
    await send();

    expect(commitInput).toHaveBeenCalledWith(
      expect.objectContaining({ clearEditor: true, extraDraftKeys: ['sA'] }),
    );
  });
});

describe('useChatController degraded realtime polling (#120)', () => {
  afterEach(() => {
    h.pollingInterval = false;
    h.capturedRefetchIntervals = { messages: undefined, pendingTask: undefined };
  });

  it('does not poll messages or the pending-task while realtime is connected', () => {
    h.pollingInterval = false;
    setup('sA', [sA], [agentA]);

    expect(h.capturedRefetchIntervals.messages).toBe(false);
    expect(h.capturedRefetchIntervals.pendingTask).toBe(false);
  });

  it('polls messages and the pending-task on the shared interval while degraded', () => {
    h.pollingInterval = 15_000;
    setup('sA', [sA], [agentA]);

    expect(h.capturedRefetchIntervals.messages).toBe(15_000);
    expect(h.capturedRefetchIntervals.pendingTask).toBe(15_000);
  });
});

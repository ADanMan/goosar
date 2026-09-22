import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook } from '@testing-library/react';

const h = vi.hoisted(() => {
  const store = {
    isOpen: true,
    isExpanded: false,
    activeSessionId: null as string | null,
    selectedAgentId: null as string | null,
    selectedProjectId: null as string | null,
    setOpen: vi.fn(),
    setActiveSession: vi.fn(),
    setSelectedAgentId: vi.fn(),
    setSelectedProjectId: vi.fn(),
  };
  return {
    store,
    sessions: [] as unknown[],
    agents: [] as unknown[],
    projects: [] as unknown[],
    pollingInterval: false as number | false,
    capturedRefetchIntervals: {
      messages: undefined as unknown,
      pendingTask: undefined as unknown,
      sessions: undefined as unknown,
    },
  };
});

vi.mock('../../i18n', () => ({ useT: () => ({ t: () => 'x' }) }));
vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-1' }));
vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (sel: (s: { user: { id: string } | null }) => unknown) =>
    sel({ user: { id: 'user-1' } }),
}));
vi.mock('@goosar/core/chat', () => ({
  useChatStore: Object.assign((sel: (s: typeof h.store) => unknown) => sel(h.store), {
    getState: () => h.store,
  }),
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
}));
vi.mock('@goosar/core/agents', () => ({
  useAgentPresenceDetail: () => ({ availability: 'online' }),
  useWorkspaceAgentAvailability: () => 'available',
}));
vi.mock('../../common/use-app-foreground', () => ({ useAppForeground: () => true }));
vi.mock('./use-chat-draft-restore', () => ({
  useChatDraftRestore: () => ({
    restoreDraftRequest: null,
    enqueueLocalRestore: vi.fn(),
    handleRestoreDraftApplied: vi.fn(),
  }),
}));
vi.mock('./use-chat-context-items', () => ({ useChatContextItems: () => [] }));
vi.mock('./use-chat-resize', () => ({
  useChatResize: () => ({
    renderWidth: 360,
    renderHeight: 520,
    isAtMax: false,
    boundsReady: true,
    isDragging: false,
    toggleExpand: vi.fn(),
    startDrag: vi.fn(),
  }),
}));
vi.mock('./use-chat-project-context-support', () => ({
  useChatProjectContextSupport: () => true,
}));
vi.mock('@goosar/core/chat/mutations', () => ({
  useCreateChatSession: () => ({ mutateAsync: vi.fn() }),
  useMarkChatSessionRead: () => ({ mutate: vi.fn() }),
  useSetChatSessionArchived: () => ({ mutate: vi.fn() }),
  useSetChatSessionProject: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateChatSession: () => ({ mutate: vi.fn() }),
}));
vi.mock('@goosar/core/realtime', () => ({
  removeChatMessageFromCaches: vi.fn(),
  useRealtimePollingInterval: () => h.pollingInterval,
}));
vi.mock('@goosar/core/logger', () => ({
  createLogger: () => ({ info: vi.fn(), warn: vi.fn(), error: vi.fn(), debug: vi.fn() }),
  noopLogger: { info: vi.fn(), warn: vi.fn(), error: vi.fn(), debug: vi.fn() },
}));
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>();
  return {
    ...actual,
    useQuery: (options: { queryKey?: unknown[]; refetchInterval?: unknown }) => {
      const key = (options.queryKey ?? []).map(String);
      if (key.includes('agents')) return { data: h.agents };
      if (key.includes('members')) return { data: [] };
      if (key.includes('projects')) return { data: h.projects, isSuccess: true };
      if (key.includes('sessions')) {
        h.capturedRefetchIntervals.sessions = options.refetchInterval;
        return { data: h.sessions, isSuccess: true };
      }
      if (key.includes('pending-task')) {
        h.capturedRefetchIntervals.pendingTask = options.refetchInterval;
        return { data: undefined };
      }
      return { data: undefined };
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

import { ChatWindow } from './chat-window';

beforeEach(() => {
  h.store.isOpen = true;
  h.store.isExpanded = false;
  h.store.activeSessionId = null;
  h.store.selectedAgentId = null;
  h.store.selectedProjectId = null;
  h.sessions = [];
  h.agents = [];
  h.projects = [];
  h.pollingInterval = false;
  h.capturedRefetchIntervals = { messages: undefined, pendingTask: undefined, sessions: undefined };
});

describe('ChatWindow degraded realtime polling (#120)', () => {
  it('does not poll the messages query or the pending-task query while realtime is connected', () => {
    h.pollingInterval = false;

    renderHook(() => ChatWindow());

    expect(h.capturedRefetchIntervals.messages).toBe(false);
    expect(h.capturedRefetchIntervals.pendingTask).toBe(false);
  });

  it('polls the messages query and the pending-task query on the shared interval while degraded', () => {
    h.pollingInterval = 15_000;

    renderHook(() => ChatWindow());

    expect(h.capturedRefetchIntervals.messages).toBe(15_000);
    expect(h.capturedRefetchIntervals.pendingTask).toBe(15_000);
  });

  it('keeps the session list on the same degraded interval as messages/pending-task (no drift)', () => {
    h.pollingInterval = 15_000;

    renderHook(() => ChatWindow());

    expect(h.capturedRefetchIntervals.sessions).toBe(15_000);
    expect(h.capturedRefetchIntervals.sessions).toBe(h.capturedRefetchIntervals.messages);
    expect(h.capturedRefetchIntervals.sessions).toBe(h.capturedRefetchIntervals.pendingTask);
  });

  it('polls nothing while the window is closed, even degraded', () => {
    h.pollingInterval = 15_000;
    h.store.isOpen = false;

    renderHook(() => ChatWindow());

    expect(h.capturedRefetchIntervals.messages).toBe(false);
    expect(h.capturedRefetchIntervals.pendingTask).toBe(false);
    expect(h.capturedRefetchIntervals.sessions).toBe(false);
  });
});

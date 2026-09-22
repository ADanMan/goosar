import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import enOnboarding from '../locales/en/onboarding.json';
import enCommon from '../locales/en/common.json';
import { OnboardingGuidesAutoClose } from './onboarding-guides-autoclose';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const AGENT_GUIDE_TITLE = 'Step 2 — Create your first Goosar Agent';
const INSTALL_GUIDE_TITLE = 'Step 1 — Connect a runtime to start using agents';
const AGENT_GUIDE_TITLE_RU = 'Шаг 2 — Создайте первого агента Goosar';
const INSTALL_GUIDE_TITLE_RU = 'Шаг 1 — Подключите среду выполнения, чтобы запустить агентов';

const mockUser = {
  id: 'user-1',
  name: 'Test',
  email: 'test@llm.example.com',
  avatar_url: null,
  onboarded_at: '2026-01-01T00:00:00Z',
  onboarding_questionnaire: {},
  starter_content_state: null,
  language: null,
  profile_description: '',
  created_at: '',
  updated_at: '',
};
vi.mock('@goosar/core/auth', () => ({
  useAuthStore: Object.assign(
    (selector?: (s: { user: typeof mockUser }) => unknown) => {
      const state = { user: mockUser };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockUser }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

const wsState = vi.hoisted(() => ({ id: 'ws-default' }));
vi.mock('@goosar/core/paths', async () => {
  const actual = await vi.importActual<typeof import('@goosar/core/paths')>('@goosar/core/paths');
  return {
    ...actual,
    useCurrentWorkspace: () => ({
      id: wsState.id,
      slug: 'test-ws',
      name: 'Test WS',
    }),
  };
});

const wsEventState = vi.hoisted(() => ({
  handler: null as ((payload: unknown) => void) | null,
}));
vi.mock('@goosar/core/realtime', () => ({
  useWSEvent: (_event: string, handler: (payload: unknown) => void) => {
    wsEventState.handler = handler;
  },
}));

const mockListAgents = vi.fn();
const mockCreateAgent = vi.fn();
const mockListIssues = vi.fn();
const mockListRuntimes = vi.fn();
const mockUpdateAgent = vi.fn();
const mockUpdateIssue = vi.fn();

vi.mock('@goosar/core/api', () => ({
  api: {
    listAgents: (...args: unknown[]) => mockListAgents(...args),
    createAgent: (...args: unknown[]) => mockCreateAgent(...args),
    listIssues: (...args: unknown[]) => mockListIssues(...args),
    listRuntimes: (...args: unknown[]) => mockListRuntimes(...args),
    updateAgent: (...args: unknown[]) => mockUpdateAgent(...args),
    updateIssue: (...args: unknown[]) => mockUpdateIssue(...args),
  },
}));

function renderAutoClose() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<OnboardingGuidesAutoClose />, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={TEST_RESOURCES}>
          {children}
        </I18nProvider>
      </QueryClientProvider>
    ),
  });
}

interface FixtureIssue {
  id: string;
  title: string;
  status: string;
  metadata?: Record<string, string>;
}

function makeListIssuesFake(fixtures: FixtureIssue[]) {
  return (params: { metadata?: Record<string, unknown>; limit?: number } = {}) => {
    let filtered = fixtures;
    if (params.metadata) {
      filtered = fixtures.filter((issue) =>
        Object.entries(params.metadata!).every(
          ([key, value]) => (issue.metadata ?? {})[key] === value,
        ),
      );
    }
    if (params.limit) filtered = filtered.slice(0, params.limit);
    return Promise.resolve({ issues: filtered, total: filtered.length });
  };
}

function guideIssues(): FixtureIssue[] {
  return [
    {
      id: 'issue-install',
      title: INSTALL_GUIDE_TITLE,
      status: 'in_progress',
      metadata: { onboarding_seed: 'install_runtime' },
    },
    {
      id: 'issue-agent',
      title: AGENT_GUIDE_TITLE,
      status: 'todo',
      metadata: { onboarding_seed: 'create_agent_guide' },
    },
  ];
}

function serverProvisionedHelper() {
  return [
    {
      id: 'agent-helper',
      name: 'Goosar Helper',
      system_key: 'goosar_helper',
      visibility: 'workspace',
      archived_at: null,
      runtime_id: 'rt-bee',
      mcp_config: {},
    },
  ];
}

let wsCounter = 0;

beforeEach(() => {
  wsEventState.handler = null;
  wsCounter += 1;
  wsState.id = `ws-autoclose-${wsCounter}`;
  mockListAgents.mockReset();
  mockCreateAgent.mockReset();
  mockListIssues.mockReset();
  mockListRuntimes.mockReset();
  mockUpdateAgent.mockReset();
  mockUpdateIssue.mockReset();
  mockListAgents.mockResolvedValue(serverProvisionedHelper());
  mockListIssues.mockImplementation(makeListIssuesFake(guideIssues()));
  mockUpdateIssue.mockResolvedValue({ id: 'issue-any' });
  mockUpdateAgent.mockImplementation(async (_id: string, patch: Record<string, unknown>) => ({
    ...serverProvisionedHelper()[0],
    ...patch,
  }));
});

describe('OnboardingGuidesAutoClose', () => {
  it('does nothing until a daemon:register event arrives', async () => {
    renderAutoClose();
    expect(wsEventState.handler).not.toBeNull();
    await Promise.resolve();
    expect(mockListAgents).not.toHaveBeenCalled();
    expect(mockUpdateIssue).not.toHaveBeenCalled();
  });

  it('never creates an agent — provisioning belongs to the server (#208)', async () => {
    renderAutoClose();
    wsEventState.handler!({ runtime_id: 'rt-bee' });

    await waitFor(() => expect(mockUpdateIssue).toHaveBeenCalledTimes(2));
    expect(mockCreateAgent).not.toHaveBeenCalled();
    expect(mockListRuntimes).not.toHaveBeenCalled();
  });

  it('closes both guide issues once the workspace has an agent', async () => {
    renderAutoClose();
    wsEventState.handler!({ runtime_id: 'rt-bee' });

    await waitFor(() =>
      expect(mockListIssues).toHaveBeenCalledWith(
        expect.objectContaining({
          workspace_id: wsState.id,
          assignee_id: 'user-1',
          metadata: { onboarding_seed: 'create_agent_guide' },
          limit: 1,
        }),
      ),
    );

    await waitFor(() => expect(mockUpdateIssue).toHaveBeenCalledTimes(2));
    const updated = mockUpdateIssue.mock.calls.map(([id, data]) => [id, data]);
    expect(updated).toContainEqual(['issue-agent', { status: 'done' }]);
    expect(updated).toContainEqual(['issue-install', { status: 'done' }]);
  });

  it('closes the guides once for a burst of daemon:register events', async () => {
    renderAutoClose();
    wsEventState.handler!({});
    wsEventState.handler!({});

    await waitFor(() => expect(mockUpdateIssue).toHaveBeenCalledTimes(2));

    wsEventState.handler!({});
    await Promise.resolve();
    expect(mockUpdateIssue).toHaveBeenCalledTimes(2);
  });

  it('waits when the workspace still has no agent (no runtime registered yet)', async () => {
    mockListAgents.mockResolvedValue([]);
    renderAutoClose();
    wsEventState.handler!({});

    await waitFor(() => expect(mockListAgents).toHaveBeenCalledTimes(1));
    await Promise.resolve();
    expect(mockListIssues).not.toHaveBeenCalled();
    expect(mockUpdateIssue).not.toHaveBeenCalled();
  });

  it('ignores an archived-only agent list', async () => {
    mockListAgents.mockResolvedValue([
      {
        id: 'agent-archived',
        name: 'Goosar Helper',
        system_key: 'goosar_helper',
        visibility: 'workspace',
        archived_at: '2026-01-02T00:00:00Z',
      },
    ]);
    renderAutoClose();
    wsEventState.handler!({});

    await waitFor(() => expect(mockListAgents).toHaveBeenCalledTimes(1));
    await Promise.resolve();
    expect(mockUpdateIssue).not.toHaveBeenCalled();
  });

  it('no-ops for users without an open create-agent guide (not the runtime-skipped cohort)', async () => {
    mockListIssues.mockImplementation(makeListIssuesFake([]));
    renderAutoClose();
    wsEventState.handler!({});

    await waitFor(() => expect(mockListIssues).toHaveBeenCalledTimes(2));
    await Promise.resolve();
    expect(mockUpdateIssue).not.toHaveBeenCalled();
  });

  it('gates on the metadata stamp even when the guide titles were renamed', async () => {
    mockListIssues.mockImplementation(
      makeListIssuesFake([
        {
          id: 'issue-install-renamed',
          title: 'Some future install title',
          status: 'in_progress',
          metadata: { onboarding_seed: 'install_runtime' },
        },
        {
          id: 'issue-agent-renamed',
          title: 'Some future guide title',
          status: 'todo',
          metadata: { onboarding_seed: 'create_agent_guide' },
        },
      ]),
    );
    renderAutoClose();
    wsEventState.handler!({});

    await waitFor(() => expect(mockUpdateIssue).toHaveBeenCalledTimes(2));
    const updated = mockUpdateIssue.mock.calls.map(([id, data]) => [id, data]);
    expect(updated).toContainEqual(['issue-agent-renamed', { status: 'done' }]);
    expect(updated).toContainEqual(['issue-install-renamed', { status: 'done' }]);
  });

  it('gates on a Russian-titled unstamped guide issue (legacy title fallback)', async () => {
    mockListIssues.mockImplementation(
      makeListIssuesFake([
        {
          id: 'issue-install-ru',
          title: INSTALL_GUIDE_TITLE_RU,
          status: 'in_progress',
        },
        { id: 'issue-agent-ru', title: AGENT_GUIDE_TITLE_RU, status: 'todo' },
      ]),
    );
    renderAutoClose();
    wsEventState.handler!({});

    await waitFor(() => expect(mockUpdateIssue).toHaveBeenCalledTimes(2));
    const updated = mockUpdateIssue.mock.calls.map(([id, data]) => [id, data]);
    expect(updated).toContainEqual(['issue-agent-ru', { status: 'done' }]);
    expect(updated).toContainEqual(['issue-install-ru', { status: 'done' }]);
  });

  it('re-checks the guide probe once after a miss (daemon:register during seeding)', async () => {
    vi.useFakeTimers();
    try {
      const seeded = { current: false };
      mockListIssues.mockImplementation(
        (params?: { metadata?: Record<string, unknown>; limit?: number }) =>
          makeListIssuesFake(seeded.current ? guideIssues() : [])(params),
      );
      renderAutoClose();
      wsEventState.handler!({});

      await vi.advanceTimersByTimeAsync(0);
      expect(mockUpdateIssue).not.toHaveBeenCalled();

      seeded.current = true;
      await vi.advanceTimersByTimeAsync(15_000);
      expect(mockUpdateIssue).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('re-checks once when the Helper has not appeared yet', async () => {
    vi.useFakeTimers();
    try {
      const provisioned = { current: false };
      mockListAgents.mockImplementation(async () =>
        provisioned.current ? serverProvisionedHelper() : [],
      );
      renderAutoClose();
      wsEventState.handler!({});

      await vi.advanceTimersByTimeAsync(0);
      expect(mockUpdateIssue).not.toHaveBeenCalled();

      provisioned.current = true;
      await vi.advanceTimersByTimeAsync(15_000);
      expect(mockUpdateIssue).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('seeds the corporate MCP presets onto a freshly provisioned Helper', async () => {
    mockListAgents.mockResolvedValue([{ ...serverProvisionedHelper()[0], mcp_config: null }]);
    renderAutoClose();
    wsEventState.handler!({});

    await waitFor(() => expect(mockUpdateAgent).toHaveBeenCalledTimes(1));
    const [agentId, patch] = mockUpdateAgent.mock.calls[0]!;
    expect(agentId).toBe('agent-helper');
    expect(
      (patch as { mcp_config?: { mcpServers?: object } }).mcp_config?.mcpServers,
    ).toBeDefined();
    expect((patch as { runtime_id?: string }).runtime_id).toBeUndefined();
  });

  it('gives up after the single re-check also misses', async () => {
    vi.useFakeTimers();
    try {
      mockListIssues.mockImplementation(makeListIssuesFake([]));
      renderAutoClose();
      wsEventState.handler!({});

      await vi.advanceTimersByTimeAsync(0);
      const callsAfterFirstRun = mockListIssues.mock.calls.length;
      expect(callsAfterFirstRun).toBeGreaterThan(0);

      await vi.advanceTimersByTimeAsync(15_000);
      const callsAfterRecheck = mockListIssues.mock.calls.length;
      expect(callsAfterRecheck).toBeGreaterThan(callsAfterFirstRun);

      await vi.advanceTimersByTimeAsync(60_000);
      expect(mockListIssues.mock.calls.length).toBe(callsAfterRecheck);
      expect(mockUpdateIssue).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });
});

import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { SupportedLocale } from '@goosar/core/i18n';
import enOnboarding from '../locales/en/onboarding.json';
import enCommon from '../locales/en/common.json';
import koOnboarding from '../locales/ko/onboarding.json';
import koCommon from '../locales/ko/common.json';
import jaOnboarding from '../locales/ja/onboarding.json';
import jaCommon from '../locales/ja/common.json';
import ruOnboarding from '../locales/ru/onboarding.json';
import ruCommon from '../locales/ru/common.json';
import { NavigationProvider } from '../navigation';
import type { NavigationAdapter } from '../navigation';
import { useWelcomeStore } from '@goosar/core/onboarding';
import { WelcomeAfterOnboarding } from './welcome-after-onboarding';

const TEST_RESOURCES = {
  en: { common: enCommon, onboarding: enOnboarding },
  ko: { common: koCommon, onboarding: koOnboarding },
  ja: { common: jaCommon, onboarding: jaOnboarding },
  ru: { common: ruCommon, onboarding: ruOnboarding },
};

const mockUser = {
  id: 'user-1',
  name: 'Test',
  email: 'test@goosar.ru',
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

const mockListAgents = vi.fn();
const mockCreateAgent = vi.fn();

function serverHelper(overrides: Record<string, unknown> = {}) {
  return {
    id: 'agent-1',
    name: 'Goosar Helper',
    system_key: 'goosar_helper',
    description: 'Built-in workspace assistant.',
    avatar_url: null,
    visibility: 'workspace',
    archived_at: null,
    runtime_id: 'rt-1',
    mcp_config: {},
    ...overrides,
  };
}
const mockUpdateAgent = vi.fn();
const mockCreateIssue = vi.fn();
const mockCreateComment = vi.fn();
const mockGetWorkspace = vi.fn();
const mockListRuntimes = vi.fn();
const mockListMembers = vi.fn();
const mockSetIssueMetadataKey = vi.fn();

vi.mock('@goosar/core/paths', async () => {
  const actual = await vi.importActual<typeof import('@goosar/core/paths')>('@goosar/core/paths');
  return {
    ...actual,
    useCurrentWorkspace: () => ({
      id: 'ws-1',
      slug: 'test-ws',
      name: 'Test WS',
    }),
  };
});

vi.mock('@goosar/core/api', () => ({
  api: {
    getBaseUrl: () => 'http://127.0.0.1:8080',
    listAgents: (...args: unknown[]) => mockListAgents(...args),
    createAgent: (...args: unknown[]) => mockCreateAgent(...args),
    updateAgent: (...args: unknown[]) => mockUpdateAgent(...args),
    createIssue: (...args: unknown[]) => mockCreateIssue(...args),
    createComment: (...args: unknown[]) => mockCreateComment(...args),
    getWorkspace: (...args: unknown[]) => mockGetWorkspace(...args),
    listRuntimes: (...args: unknown[]) => mockListRuntimes(...args),
    listMembers: (...args: unknown[]) => mockListMembers(...args),
    setIssueMetadataKey: (...args: unknown[]) => mockSetIssueMetadataKey(...args),
  },
}));

const mockPush = vi.fn();
const navigationAdapter: NavigationAdapter = {
  push: (path: string) => mockPush(path),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: '/test',
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => `https://test.local${path}`,
};

function I18nWrapper({
  children,
  locale = 'en',
}: {
  children: ReactNode;
  locale?: SupportedLocale;
}) {
  return (
    <I18nProvider locale={locale} resources={TEST_RESOURCES}>
      <NavigationProvider value={navigationAdapter}>{children}</NavigationProvider>
    </I18nProvider>
  );
}

function renderWelcome({ locale = 'en' }: { locale?: SupportedLocale } = {}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData(['workspaces', 'list'], [{ id: 'ws-1', slug: 'test-ws' }]);
  return render(<WelcomeAfterOnboarding />, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={qc}>
        <I18nWrapper locale={locale}>{children}</I18nWrapper>
      </QueryClientProvider>
    ),
  });
}

beforeEach(() => {
  mockListAgents.mockReset();
  mockCreateAgent.mockReset();
  mockUpdateAgent.mockReset();
  mockCreateIssue.mockReset();
  mockCreateComment.mockReset();
  mockGetWorkspace.mockReset();
  mockListRuntimes.mockReset();
  mockListMembers.mockReset();
  mockSetIssueMetadataKey.mockReset();
  mockListRuntimes.mockResolvedValue([]);
  mockUpdateAgent.mockImplementation(async (_id: string, patch: Record<string, unknown>) => ({
    ...serverHelper(),
    ...patch,
  }));
  mockListMembers.mockResolvedValue([{ user_id: 'user-1', perimeter_access: true }]);
  mockSetIssueMetadataKey.mockResolvedValue({ metadata: {} });
  mockPush.mockReset();
  useWelcomeStore.getState().reset();
});

describe('WelcomeAfterOnboarding', () => {
  it('renders nothing when no welcome signal is present', () => {
    const { container } = renderWelcome();
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing when the signal points at a different workspace', () => {
    useWelcomeStore.getState().set({
      workspaceId: 'ws-2',
      choice: 'skip',
    });
    const { container } = renderWelcome();
    expect(container.firstChild).toBeNull();
    expect(mockCreateIssue).not.toHaveBeenCalled();
  });

  describe('runtime path', () => {
    it('renders the server-provisioned Helper in a blocking modal with starter cards', async () => {
      mockListAgents.mockResolvedValueOnce([serverHelper()]);
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'runtime',
        runtimeId: 'rt-1',
      });

      renderWelcome();

      expect(screen.getByText(/Preparing your Helper/i)).toBeInTheDocument();

      await waitFor(() => {
        expect(screen.getByText(/welcome to Goosar/i)).toBeInTheDocument();
      });

      expect(mockListAgents).toHaveBeenCalledWith({ workspace_id: 'ws-1' });
      expect(mockCreateAgent).not.toHaveBeenCalled();

      expect(screen.getByText('Introduce Goosar to me')).toBeInTheDocument();
      expect(screen.getByText('Walk me through the core features')).toBeInTheDocument();
      expect(screen.getByText('Show me what Goosar can do for me — as slides')).toBeInTheDocument();
    });

    it('shows the existing Helper without mutating it', async () => {
      mockListAgents.mockResolvedValueOnce([
        serverHelper({ id: 'agent-existing', description: '' }),
      ]);
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'runtime',
        runtimeId: 'rt-1',
      });

      renderWelcome();
      await waitFor(() => {
        expect(screen.getByText(/welcome to Goosar/i)).toBeInTheDocument();
      });

      expect(mockCreateAgent).not.toHaveBeenCalled();
      expect(mockUpdateAgent).not.toHaveBeenCalled();
    });

    it('binds the Helper to the runtime the user picked', async () => {
      mockListAgents.mockResolvedValue([serverHelper({ runtime_id: 'rt-server-chose' })]);
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'runtime',
        runtimeId: 'rt-1',
      });

      renderWelcome();
      await waitFor(() => {
        expect(screen.getByText(/welcome to Goosar/i)).toBeInTheDocument();
      });

      expect(mockUpdateAgent).toHaveBeenCalledTimes(1);
      const [agentId, patch] = mockUpdateAgent.mock.calls[0]!;
      expect(agentId).toBe('agent-1');
      expect((patch as { runtime_id?: string }).runtime_id).toBe('rt-1');
      expect(mockCreateAgent).not.toHaveBeenCalled();
    });

    it('surfaces a retry — never a create — when the workspace has no Helper yet', async () => {
      mockListAgents.mockResolvedValue([]);
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'runtime',
        runtimeId: 'rt-1',
      });

      renderWelcome();

      const retry = await screen.findByRole('button', { name: /try again/i });
      expect(mockCreateAgent).not.toHaveBeenCalled();

      mockListAgents.mockResolvedValue([serverHelper()]);
      fireEvent.click(retry);
      await waitFor(() => {
        expect(screen.getByText(/welcome to Goosar/i)).toBeInTheDocument();
      });
      expect(mockCreateAgent).not.toHaveBeenCalled();
    });

    it('selecting cards then clicking Assign creates one issue per pick and navigates to the first', async () => {
      mockListAgents.mockResolvedValueOnce([serverHelper({ description: '' })]);
      mockCreateIssue
        .mockResolvedValueOnce({
          id: 'issue-intro',
          workspace_id: 'ws-1',
        })
        .mockResolvedValueOnce({
          id: 'issue-welcome',
          workspace_id: 'ws-1',
        });
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'runtime',
        runtimeId: 'rt-1',
      });

      renderWelcome();
      await waitFor(() => expect(screen.getByText('Introduce Goosar to me')).toBeInTheDocument());

      const ctaEmpty = screen.getByRole('button', { name: /pick one or more/i });
      expect(ctaEmpty).toBeDisabled();

      fireEvent.click(screen.getByText('Introduce Goosar to me'));
      fireEvent.click(screen.getByText('Show me what Goosar can do for me — as slides'));

      const cta = await screen.findByRole('button', { name: /assign 2/i });
      expect(cta).not.toBeDisabled();
      fireEvent.click(cta);

      await waitFor(() => expect(mockCreateIssue).toHaveBeenCalledTimes(2));
      const titles = mockCreateIssue.mock.calls.map(([args]) => args.title);
      expect(titles).toEqual([
        'Introduce Goosar to me',
        'Show me what Goosar can do for me — as slides',
      ]);
      mockCreateIssue.mock.calls.forEach(([args]) => {
        expect(args.assignee_type).toBe('agent');
        expect(args.assignee_id).toBe('agent-1');
      });

      const gotIt = await screen.findByRole('button', { name: /got it/i });
      expect(mockPush).not.toHaveBeenCalled();
      fireEvent.click(gotIt);

      await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/test-ws/issues/issue-intro'));
    });

    it('uses Korean persisted Helper and starter issue artifacts under ko locale', async () => {
      mockListAgents.mockResolvedValueOnce([serverHelper({ description: '' })]);
      mockCreateIssue.mockResolvedValueOnce({
        id: 'issue-intro',
        workspace_id: 'ws-1',
      });
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'runtime',
        runtimeId: 'rt-1',
      });

      renderWelcome({ locale: 'ko' });

      await waitFor(() =>
        expect(screen.getByText('Goosar를 간단히 소개해 주세요')).toBeInTheDocument(),
      );

      expect(mockCreateAgent).not.toHaveBeenCalled();

      fireEvent.click(screen.getByText('Goosar를 간단히 소개해 주세요'));
      fireEvent.click(await screen.findByRole('button', { name: /작업 1개를 나에게 할당/i }));

      await waitFor(() => expect(mockCreateIssue).toHaveBeenCalledTimes(1));
      const [issueArgs] = mockCreateIssue.mock.calls[0]!;
      expect(issueArgs.title).toBe('Goosar를 간단히 소개해 주세요');
      expect(issueArgs.description).toContain('Goosar를 1-2문단으로 간단히 소개해 주세요');
    });

    it('uses Japanese persisted Helper and starter issue artifacts under ja locale', async () => {
      mockListAgents.mockResolvedValueOnce([serverHelper({ description: '' })]);
      mockCreateIssue.mockResolvedValueOnce({
        id: 'issue-intro',
        workspace_id: 'ws-1',
      });
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'runtime',
        runtimeId: 'rt-1',
      });

      renderWelcome({ locale: 'ja' });

      await waitFor(() =>
        expect(screen.getByText('Goosar を簡単に紹介してください')).toBeInTheDocument(),
      );

      expect(mockCreateAgent).not.toHaveBeenCalled();

      fireEvent.click(screen.getByText('Goosar を簡単に紹介してください'));
      fireEvent.click(
        await screen.findByRole('button', {
          name: /1 件のタスクを私に割り当てる/,
        }),
      );

      await waitFor(() => expect(mockCreateIssue).toHaveBeenCalledTimes(1));
      const [issueArgs] = mockCreateIssue.mock.calls[0]!;
      expect(issueArgs.title).toBe('Goosar を簡単に紹介してください');
      expect(issueArgs.description).toContain('Goosar を1〜2段落で簡単に紹介してください');
    });
  });

  describe('skip path', () => {
    it('provisions install-runtime → agent-guide → follow-up comment, then opens the celebration Modal', async () => {
      mockCreateIssue
        .mockResolvedValueOnce({
          id: 'issue-install',
          identifier: 'MUL-1',
          workspace_id: 'ws-1',
        })
        .mockResolvedValueOnce({
          id: 'issue-agent',
          identifier: 'MUL-2',
          workspace_id: 'ws-1',
        });
      mockCreateComment.mockResolvedValueOnce({ id: 'comment-1' });

      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome();

      expect(screen.getByText(/Setting up your workspace/i)).toBeInTheDocument();

      await waitFor(() => {
        expect(screen.getByText(/Welcome to Goosar/i)).toBeInTheDocument();
      });

      expect(mockCreateIssue).toHaveBeenCalledTimes(2);
      expect(mockCreateComment).toHaveBeenCalledTimes(1);

      const [firstCall] = mockCreateIssue.mock.calls;
      expect(firstCall![0].title).toBe('Step 1 — Connect a runtime to start using agents');
      expect(firstCall![0].status).toBe('in_progress');
      expect(firstCall![0].assignee_type).toBe('member');
      expect(firstCall![0].assignee_id).toBe('user-1');

      const [secondCall] = mockCreateIssue.mock.calls.slice(1);
      expect(secondCall![0].title).toBe('Step 2 — Create your first Goosar Agent');
      expect(secondCall![0].status).toBe('todo');
      expect(secondCall![0].description).toContain('[MUL-1](mention://issue/issue-install)');

      const [commentIssueId, commentContent] = mockCreateComment.mock.calls[0]!;
      expect(commentIssueId).toBe('issue-install');
      expect(commentContent).toContain('[MUL-2](mention://issue/issue-agent)');

      expect(mockSetIssueMetadataKey).toHaveBeenCalledWith(
        'issue-install',
        'onboarding_seed',
        'install_runtime',
      );
      expect(mockSetIssueMetadataKey).toHaveBeenCalledWith(
        'issue-agent',
        'onboarding_seed',
        'create_agent_guide',
      );
    });

    it('still seeds both guides when the metadata stamp write fails', async () => {
      mockSetIssueMetadataKey.mockRejectedValue(new Error('boom'));
      mockCreateIssue
        .mockResolvedValueOnce({
          id: 'issue-install',
          identifier: 'MUL-1',
          workspace_id: 'ws-1',
        })
        .mockResolvedValueOnce({
          id: 'issue-agent',
          identifier: 'MUL-2',
          workspace_id: 'ws-1',
        });
      mockCreateComment.mockResolvedValueOnce({ id: 'comment-1' });

      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome();

      await waitFor(() => {
        expect(screen.getByText(/Welcome to Goosar/i)).toBeInTheDocument();
      });
      expect(mockCreateIssue).toHaveBeenCalledTimes(2);
      expect(mockCreateComment).toHaveBeenCalledTimes(1);
    });

    it('silently dismisses without showing the Modal when provisioning fails', async () => {
      mockCreateIssue.mockRejectedValueOnce(new Error('network down'));
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome();

      await waitFor(() => expect(useWelcomeStore.getState().dismissed).toBe(true));
      expect(screen.queryByText(/Welcome to Goosar/i)).not.toBeInTheDocument();
    });

    it('uses Korean persisted skip-path issue and comment artifacts under ko locale', async () => {
      mockCreateIssue
        .mockResolvedValueOnce({
          id: 'issue-install',
          identifier: 'MUL-1',
          workspace_id: 'ws-1',
        })
        .mockResolvedValueOnce({
          id: 'issue-agent',
          identifier: 'MUL-2',
          workspace_id: 'ws-1',
        });
      mockCreateComment.mockResolvedValueOnce({ id: 'comment-1' });

      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome({ locale: 'ko' });

      await waitFor(() => {
        expect(screen.getByText(/Goosar에 오신 것을 환영합니다/i)).toBeInTheDocument();
      });

      expect(mockCreateIssue).toHaveBeenCalledTimes(2);
      const [installCall, guideCall] = mockCreateIssue.mock.calls;
      expect(installCall![0].title).toBe('1단계 — agent를 사용하려면 runtime 연결하기');
      expect(installCall![0].description).toContain('Goosar에 오신 것을 환영합니다.');
      expect(guideCall![0].title).toBe('2단계 — 첫 Goosar Agent 만들기');
      expect(guideCall![0].description).toContain('runtime이 online 상태가 되면');
      expect(guideCall![0].description).toContain('[MUL-1](mention://issue/issue-install)');

      const [commentIssueId, commentContent] = mockCreateComment.mock.calls[0]!;
      expect(commentIssueId).toBe('issue-install');
      expect(commentContent).toContain('다음 단계:');
      expect(commentContent).toContain('[MUL-2](mention://issue/issue-agent)');
    });

    it('uses Japanese persisted skip-path issue and comment artifacts under ja locale', async () => {
      mockCreateIssue
        .mockResolvedValueOnce({
          id: 'issue-install',
          identifier: 'MUL-1',
          workspace_id: 'ws-1',
        })
        .mockResolvedValueOnce({
          id: 'issue-agent',
          identifier: 'MUL-2',
          workspace_id: 'ws-1',
        });
      mockCreateComment.mockResolvedValueOnce({ id: 'comment-1' });

      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome({ locale: 'ja' });

      await waitFor(() => {
        expect(screen.getByText(/Goosar へようこそ/)).toBeInTheDocument();
      });

      expect(mockCreateIssue).toHaveBeenCalledTimes(2);
      const [installCall, guideCall] = mockCreateIssue.mock.calls;
      expect(installCall![0].title).toBe('ステップ1 — agent を使うために runtime を接続する');
      expect(installCall![0].description).toContain('Goosar へようこそ。');
      expect(guideCall![0].title).toBe('ステップ2 — 最初の Goosar Agent を作成する');
      expect(guideCall![0].description).toContain('runtime が online になったら');
      expect(guideCall![0].description).toContain('[MUL-1](mention://issue/issue-install)');

      const [commentIssueId, commentContent] = mockCreateComment.mock.calls[0]!;
      expect(commentIssueId).toBe('issue-install');
      expect(commentContent).toContain('次のステップ:');
      expect(commentContent).toContain('[MUL-2](mention://issue/issue-agent)');
    });

    it('uses Russian persisted skip-path issue and comment artifacts under ru locale', async () => {
      mockCreateIssue
        .mockResolvedValueOnce({
          id: 'issue-install',
          identifier: 'MUL-1',
          workspace_id: 'ws-1',
        })
        .mockResolvedValueOnce({
          id: 'issue-agent',
          identifier: 'MUL-2',
          workspace_id: 'ws-1',
        });
      mockCreateComment.mockResolvedValueOnce({ id: 'comment-1' });

      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome({ locale: 'ru' });

      await waitFor(() => {
        expect(screen.getByText(/Добро пожаловать в Goosar/i)).toBeInTheDocument();
      });

      expect(mockCreateIssue).toHaveBeenCalledTimes(2);
      const [installCall, guideCall] = mockCreateIssue.mock.calls;
      expect(installCall![0].title).toBe(
        'Шаг 1 — Подключите среду выполнения, чтобы запустить агентов',
      );
      expect(installCall![0].description).toContain('Добро пожаловать в Goosar.');
      expect(guideCall![0].title).toBe('Шаг 2 — Создайте первого агента Goosar');
      expect(guideCall![0].description).toContain('Когда среда выполнения будет online');
      expect(guideCall![0].description).toContain('[MUL-1](mention://issue/issue-install)');

      const [commentIssueId, commentContent] = mockCreateComment.mock.calls[0]!;
      expect(commentIssueId).toBe('issue-install');
      expect(commentContent).toContain('Следующий шаг:');
      expect(commentContent).toContain('[MUL-2](mention://issue/issue-agent)');
    });
  });

  describe('skip path with a provisioned Helper (issue #40)', () => {
    it('shows the Helper instead of seeding guide issues and navigates to the agent', async () => {
      mockListAgents.mockResolvedValueOnce([serverHelper()]);
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome();

      await waitFor(() => {
        expect(screen.getByText(/Welcome to Goosar/i)).toBeInTheDocument();
      });

      expect(mockCreateAgent).not.toHaveBeenCalled();
      expect(mockCreateIssue).not.toHaveBeenCalled();
      expect(mockCreateComment).not.toHaveBeenCalled();

      expect(screen.getByText('Goosar Helper')).toBeInTheDocument();
      expect(screen.getByText('Ready')).toBeInTheDocument();

      fireEvent.click(screen.getByRole('button', { name: /got it/i }));
      await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/test-ws/agents/agent-1'));
    });

    it('does not probe runtimes — runtime choice moved to the server', async () => {
      mockListAgents.mockResolvedValueOnce([serverHelper({ description: '' })]);
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome();
      await waitFor(() => {
        expect(screen.getByText(/Welcome to Goosar/i)).toBeInTheDocument();
      });
      expect(mockListRuntimes).not.toHaveBeenCalled();
      expect(mockCreateAgent).not.toHaveBeenCalled();
    });

    it('seeds the corporate MCP presets onto a freshly provisioned Helper', async () => {
      mockListAgents.mockResolvedValue([serverHelper({ mcp_config: null })]);
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome();
      await waitFor(() => {
        expect(screen.getByText(/Welcome to Goosar/i)).toBeInTheDocument();
      });

      expect(mockUpdateAgent).toHaveBeenCalledTimes(1);
      const patch = mockUpdateAgent.mock.calls[0]![1] as {
        mcp_config: { mcpServers: Record<string, { enabled: boolean }> };
      };
      for (const entry of Object.values(patch.mcp_config.mcpServers)) {
        expect(entry.enabled).toBe(false);
      }
      expect(mockCreateIssue).not.toHaveBeenCalled();
    });

    it('falls back to guide seeding when the workspace has no Helper', async () => {
      mockListAgents.mockResolvedValue([]);
      mockCreateIssue
        .mockResolvedValueOnce({
          id: 'issue-install',
          identifier: 'MUL-1',
          workspace_id: 'ws-1',
        })
        .mockResolvedValueOnce({
          id: 'issue-agent',
          identifier: 'MUL-2',
          workspace_id: 'ws-1',
        });
      mockCreateComment.mockResolvedValueOnce({ id: 'comment-1' });
      useWelcomeStore.getState().set({
        workspaceId: 'ws-1',
        choice: 'skip',
      });

      renderWelcome();
      await waitFor(() => {
        expect(screen.getByText(/Welcome to Goosar/i)).toBeInTheDocument();
      });
      expect(mockCreateAgent).not.toHaveBeenCalled();
      expect(mockCreateIssue).toHaveBeenCalledTimes(2);
    });
  });
});

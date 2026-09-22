import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ONBOARDING_STEP_ORDER, useWelcomeStore } from '@goosar/core/onboarding';
import type { AgentRuntime, Workspace } from '@goosar/core/types';
import type { AgentRuntimeStatus } from './agent-row-status';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../locales/en/common.json';
import enOnboarding from '../locales/en/onboarding.json';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const mocks = vi.hoisted(() => ({
  completeOnboarding: vi.fn(),
  saveQuestionnaire: vi.fn(),
  listWorkspaces: vi.fn(),
  setCurrentWorkspace: vi.fn(),
  hasPendingOnboardingCompletion: vi.fn(() => false),
  clearOnboardingCompletionMark: vi.fn(),
  listJoinTargets: vi.fn(async (): Promise<unknown[]> => []),
  getEffectiveConfig: vi.fn(async () => ({ mcp: {} })),
}));

vi.mock('@goosar/core/onboarding', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/onboarding')>()),
  completeOnboarding: mocks.completeOnboarding,
  saveQuestionnaire: mocks.saveQuestionnaire,
  hasPendingOnboardingCompletion: mocks.hasPendingOnboardingCompletion,
  clearOnboardingCompletionMark: mocks.clearOnboardingCompletionMark,
}));

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
  api: {
    listWorkspaces: mocks.listWorkspaces,
    listJoinTargets: mocks.listJoinTargets,
    getEffectiveConfig: mocks.getEffectiveConfig,
  },
}));

vi.mock('@goosar/core/platform', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/platform')>()),
  setCurrentWorkspace: mocks.setCurrentWorkspace,
}));

const AUTH_STATE = {
  user: {
    id: 'u1',
    email: 'user@goosar.ru',
    onboarded_at: null as string | null,
    onboarding_questionnaire: {},
  },
};
vi.mock('@goosar/core/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/auth')>();
  const useAuthStore = Object.assign(
    (selector: (s: typeof AUTH_STATE) => unknown) => selector(AUTH_STATE),
    { getState: () => AUTH_STATE },
  );
  return { ...actual, useAuthStore };
});

const WORKSPACE = {
  id: 'ws_1',
  slug: 'acme',
  name: 'Acme',
} as unknown as Workspace;

const RUNTIME = { id: 'rt_1', provider: 'hermes' } as unknown as AgentRuntime;

vi.mock('./steps/step-welcome', () => ({
  StepWelcome: ({ onNext }: { onNext: () => void }) => (
    <button onClick={onNext}>stub-welcome-next</button>
  ),
}));
vi.mock('./steps/step-about-you', () => ({
  StepAboutYou: ({ onAdvance }: { onAdvance: () => void }) => (
    <button onClick={onAdvance}>stub-about-next</button>
  ),
}));
vi.mock('./steps/step-workspace', () => ({
  StepWorkspace: ({ onCreated }: { onCreated: (ws: Workspace) => void }) => (
    <button onClick={() => onCreated(WORKSPACE)}>stub-workspace-create</button>
  ),
}));
vi.mock('./steps/step-runtime-connect', () => ({
  StepRuntimeConnect: ({ onNext }: { onNext: (rt: AgentRuntime | null) => void }) => (
    <div data-testid="step-runtime">
      <button onClick={() => onNext(RUNTIME)}>stub-pick-runtime</button>
      <button onClick={() => onNext(null)}>stub-skip-runtime</button>
    </div>
  ),
}));
vi.mock('./steps/step-platform-fork', () => ({
  StepPlatformFork: () => <div data-testid="step-platform-fork" />,
}));
vi.mock('./steps/step-prepare-workspace', () => ({
  StepPrepareWorkspace: ({
    status,
    onAdvance,
    agentStatus,
    onRetryAgent,
    onSkipAgent,
    llmGateway,
    onRetryLlmGateway,
    pxProxy,
  }: {
    status: unknown;
    onAdvance: () => void;
    agentStatus?: AgentRuntimeStatus | null;
    onRetryAgent?: () => void | Promise<void>;
    onSkipAgent?: () => void;
    llmGateway?: { verdict: string; source: string } | null;
    onRetryLlmGateway?: () => void | Promise<void>;
    pxProxy?: { state: string } | null;
  }) => (
    <div
      data-testid="step-prepare-workspace"
      data-status={JSON.stringify(status)}
      data-agent-status={JSON.stringify(agentStatus ?? null)}
      data-llm-gateway={JSON.stringify(llmGateway ?? null)}
      data-px-proxy={JSON.stringify(pxProxy ?? null)}
    >
      <button
        onClick={() => {
          const blocking =
            agentStatus != null &&
            agentStatus.state !== 'ready' &&
            agentStatus.state !== 'external' &&
            agentStatus.state !== 'unsupported';
          if (!blocking) onAdvance();
        }}
      >
        stub-prepare-advance
      </button>
      <button onClick={() => void onRetryAgent?.()}>stub-retry-agent</button>
      <button onClick={onSkipAgent}>stub-skip-agent</button>
      <button onClick={() => void onRetryLlmGateway?.()}>stub-retry-llm-gateway</button>
    </div>
  ),
}));
vi.mock('./steps/step-choose-role', () => ({
  StepChooseRole: ({
    onJoined,
    onCreateInstead,
  }: {
    onJoined: (ws: Workspace) => void;
    onCreateInstead?: () => void;
  }) => (
    <div data-testid="step-choose-role">
      <button onClick={() => onJoined(WORKSPACE)}>stub-join-role</button>
      <button onClick={onCreateInstead}>stub-create-instead</button>
    </div>
  ),
}));
vi.mock('./steps/step-work-tools', () => ({
  StepWorkTools: ({
    wsId,
    onFinish,
    onBack,
    installedMcpNames,
  }: {
    wsId: string;
    onFinish: () => void;
    onBack?: () => void;
    installedMcpNames?: string[];
  }) => (
    <div
      data-testid="step-work-tools"
      data-ws={wsId}
      data-installed={JSON.stringify(installedMcpNames ?? null)}
    >
      <button onClick={onFinish}>stub-work-tools-finish</button>
      <button onClick={onBack}>stub-work-tools-back</button>
    </div>
  ),
}));

vi.mock('./steps/step-completion-recovery', () => ({
  StepCompletionRecovery: ({
    onRecovered,
    onStartOver,
  }: {
    onRecovered: () => void;
    onStartOver: () => void;
  }) => (
    <div data-testid="step-completion-recovery">
      <button onClick={onRecovered}>stub-recovered</button>
      <button onClick={onStartOver}>stub-start-over</button>
    </div>
  ),
}));

import { OnboardingFlow } from './onboarding-flow';

function renderFlow() {
  const onComplete = vi.fn();
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const { unmount } = render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES as never}>
        <OnboardingFlow onComplete={onComplete} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onComplete, unmount };
}

async function advanceToRuntimeStep(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByText('stub-welcome-next'));
  await user.click(screen.getByText('stub-about-next'));
  await user.click(screen.getByText('stub-workspace-create'));
  expect(screen.getByTestId('step-runtime')).toBeInTheDocument();
}

describe('ONBOARDING_STEP_ORDER', () => {
  it('places work_tools immediately after runtime, as the final step', () => {
    const runtimeIdx = ONBOARDING_STEP_ORDER.indexOf('runtime');
    expect(ONBOARDING_STEP_ORDER.indexOf('work_tools')).toBe(runtimeIdx + 1);
    expect(ONBOARDING_STEP_ORDER[ONBOARDING_STEP_ORDER.length - 1]).toBe('work_tools');
  });
});

describe('OnboardingFlow — prepare-workspace step (issue #188)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWelcomeStore.getState().reset();
    mocks.listWorkspaces.mockResolvedValue([]);
    mocks.completeOnboarding.mockResolvedValue(undefined);
    mocks.saveQuestionnaire.mockResolvedValue(undefined);
  });

  function renderFlowWithProvisioning(
    extra: {
      agentStatus?: AgentRuntimeStatus | null;
      onRetryAgent?: () => void | Promise<void>;
      onSkipAgent?: () => void;
      llmGateway?: { verdict: string; source: string } | null;
      onRetryLlmGateway?: () => void | Promise<void>;
      pxProxy?: { state: string } | null;
    } = {},
  ) {
    const onComplete = vi.fn();
    const onWorkspaceProvisioning = vi.fn();
    const onRetryProvisioning = vi.fn();
    const onRetryAgent = extra.onRetryAgent ?? vi.fn();
    const onSkipAgent = extra.onSkipAgent ?? vi.fn();
    const onRetryLlmGateway = extra.onRetryLlmGateway ?? vi.fn();
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={TEST_RESOURCES as never}>
          <OnboardingFlow
            onComplete={onComplete}
            provisioningStatus={null}
            onRetryProvisioning={onRetryProvisioning}
            onWorkspaceProvisioning={onWorkspaceProvisioning}
            agentStatus={extra.agentStatus}
            onRetryAgent={onRetryAgent}
            onSkipAgent={onSkipAgent}
            llmGateway={extra.llmGateway as never}
            onRetryLlmGateway={onRetryLlmGateway}
            pxProxy={extra.pxProxy as never}
          />
        </I18nProvider>
      </QueryClientProvider>,
    );
    return {
      onComplete,
      onWorkspaceProvisioning,
      onRetryProvisioning,
      onRetryAgent,
      onSkipAgent,
      onRetryLlmGateway,
    };
  }

  it('mounts between workspace and runtime, and calls onWorkspaceProvisioning right after workspace creation', async () => {
    const user = userEvent.setup();
    const { onWorkspaceProvisioning } = renderFlowWithProvisioning();

    await user.click(screen.getByText('stub-welcome-next'));
    await user.click(screen.getByText('stub-about-next'));
    await user.click(screen.getByText('stub-workspace-create'));

    expect(onWorkspaceProvisioning).toHaveBeenCalledWith(WORKSPACE.id);
    expect(screen.getByTestId('step-prepare-workspace')).toBeInTheDocument();
    expect(screen.queryByTestId('step-runtime')).toBeNull();
  });

  it('advances to the runtime step once the prepare step reports done', async () => {
    const user = userEvent.setup();
    renderFlowWithProvisioning();
    await user.click(screen.getByText('stub-welcome-next'));
    await user.click(screen.getByText('stub-about-next'));
    await user.click(screen.getByText('stub-workspace-create'));
    expect(screen.getByTestId('step-prepare-workspace')).toBeInTheDocument();

    await user.click(screen.getByText('stub-prepare-advance'));

    expect(screen.getByTestId('step-runtime')).toBeInTheDocument();
    expect(screen.queryByTestId('step-prepare-workspace')).toBeNull();
  });

  it('does not mount the prepare step when the desktop shell injects nothing (web)', async () => {
    const user = userEvent.setup();
    renderFlow();
    await advanceToRuntimeStep(user);
    expect(screen.queryByTestId('step-prepare-workspace')).toBeNull();
    expect(screen.getByTestId('step-runtime')).toBeInTheDocument();
  });

  it('passes llmGateway/pxProxy through and neither blocks advancing', async () => {
    const user = userEvent.setup();
    const { onRetryLlmGateway } = renderFlowWithProvisioning({
      llmGateway: { verdict: 'unreachable', source: 'agent' },
      pxProxy: { state: 'not_found' },
    });
    await user.click(screen.getByText('stub-welcome-next'));
    await user.click(screen.getByText('stub-about-next'));
    await user.click(screen.getByText('stub-workspace-create'));

    const step = screen.getByTestId('step-prepare-workspace');
    expect(step.dataset.llmGateway).toBe(
      JSON.stringify({ verdict: 'unreachable', source: 'agent' }),
    );
    expect(step.dataset.pxProxy).toBe(JSON.stringify({ state: 'not_found' }));

    await user.click(screen.getByText('stub-retry-llm-gateway'));
    expect(onRetryLlmGateway).toHaveBeenCalledTimes(1);

    await user.click(screen.getByText('stub-prepare-advance'));
    expect(screen.getByTestId('step-runtime')).toBeInTheDocument();
  });
});

describe('OnboardingFlow — work_tools routing', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWelcomeStore.getState().reset();
    mocks.listWorkspaces.mockResolvedValue([]);
    mocks.completeOnboarding.mockResolvedValue(undefined);
    mocks.saveQuestionnaire.mockResolvedValue(undefined);
  });

  it('picking a runtime completes onboarding and parks the signal AT PICK TIME, then shows work_tools', async () => {
    const user = userEvent.setup();
    const { onComplete } = renderFlow();
    await advanceToRuntimeStep(user);

    await user.click(screen.getByText('stub-pick-runtime'));

    await waitFor(() =>
      expect(mocks.completeOnboarding).toHaveBeenCalledWith('full', WORKSPACE.id),
    );
    expect(useWelcomeStore.getState().signal).toEqual({
      workspaceId: WORKSPACE.id,
      choice: 'runtime',
      runtimeId: RUNTIME.id,
    });
    const step = await screen.findByTestId('step-work-tools');
    expect(step).toHaveAttribute('data-ws', WORKSPACE.id);
    expect(onComplete).not.toHaveBeenCalled();
  });

  it('finishing work_tools only navigates — completion already happened at the pick', async () => {
    const user = userEvent.setup();
    const { onComplete } = renderFlow();
    await advanceToRuntimeStep(user);
    await user.click(screen.getByText('stub-pick-runtime'));
    await screen.findByTestId('step-work-tools');

    await user.click(screen.getByText('stub-work-tools-finish'));

    expect(onComplete).toHaveBeenCalledWith(WORKSPACE, undefined);
    expect(mocks.completeOnboarding).toHaveBeenCalledTimes(1);
    expect(useWelcomeStore.getState().signal).toEqual({
      workspaceId: WORKSPACE.id,
      choice: 'runtime',
      runtimeId: RUNTIME.id,
    });
  });

  it('auto-skips work_tools when the runtime step is skipped — the skip exit is unchanged', async () => {
    const user = userEvent.setup();
    const { onComplete } = renderFlow();
    await advanceToRuntimeStep(user);

    await user.click(screen.getByText('stub-skip-runtime'));

    await waitFor(() =>
      expect(mocks.completeOnboarding).toHaveBeenCalledWith('runtime_skipped', WORKSPACE.id),
    );
    expect(useWelcomeStore.getState().signal).toEqual({
      workspaceId: WORKSPACE.id,
      choice: 'skip',
    });
    expect(onComplete).toHaveBeenCalledWith(WORKSPACE, undefined);
    expect(screen.queryByTestId('step-work-tools')).toBeNull();
  });

  it('Back from work_tools returns to the runtime step', async () => {
    const user = userEvent.setup();
    renderFlow();
    await advanceToRuntimeStep(user);
    await user.click(screen.getByText('stub-pick-runtime'));
    expect(screen.getByTestId('step-work-tools')).toBeInTheDocument();

    await user.click(screen.getByText('stub-work-tools-back'));

    expect(screen.getByTestId('step-runtime')).toBeInTheDocument();
    expect(screen.queryByTestId('step-work-tools')).toBeNull();
  });

  it('stays on the runtime step when completing onboarding fails at the pick', async () => {
    mocks.completeOnboarding.mockRejectedValue(new Error('boom'));
    const user = userEvent.setup();
    const { onComplete } = renderFlow();
    await advanceToRuntimeStep(user);

    await user.click(screen.getByText('stub-pick-runtime'));

    await waitFor(() => expect(mocks.completeOnboarding).toHaveBeenCalledTimes(1));
    expect(onComplete).not.toHaveBeenCalled();
    expect(useWelcomeStore.getState().signal).toBeNull();
    expect(screen.queryByTestId('step-work-tools')).toBeNull();
    expect(screen.getByTestId('step-runtime')).toBeInTheDocument();
  });
});

describe('OnboardingFlow — undelivered completion (issue #258)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWelcomeStore.getState().reset();
    mocks.listWorkspaces.mockResolvedValue([WORKSPACE]);
    mocks.completeOnboarding.mockResolvedValue(undefined);
    mocks.saveQuestionnaire.mockResolvedValue(undefined);
    mocks.hasPendingOnboardingCompletion.mockReturnValue(false);
    AUTH_STATE.user.onboarded_at = null;
  });

  afterEach(() => {
    AUTH_STATE.user.onboarded_at = null;
  });

  it('re-delivers instead of replaying the scenario when this machine holds the mark', () => {
    mocks.hasPendingOnboardingCompletion.mockReturnValue(true);

    renderFlow();

    expect(screen.getByTestId('step-completion-recovery')).toBeInTheDocument();
    expect(screen.queryByText('stub-welcome-next')).toBeNull();
    expect(mocks.hasPendingOnboardingCompletion).toHaveBeenCalledWith('u1');
  });

  it('opens the ordinary scenario for a genuinely new user', () => {
    renderFlow();

    expect(screen.getByText('stub-welcome-next')).toBeInTheDocument();
    expect(screen.queryByTestId('step-completion-recovery')).toBeNull();
  });

  it('ignores the mark once the server reports the user onboarded', () => {
    mocks.hasPendingOnboardingCompletion.mockReturnValue(true);
    AUTH_STATE.user.onboarded_at = '2026-09-01T00:00:00Z';

    renderFlow();

    expect(screen.queryByTestId('step-completion-recovery')).toBeNull();
    expect(screen.getByText('stub-welcome-next')).toBeInTheDocument();
  });

  it('keeps the mark on start over — it is dropped only against server proof', async () => {
    mocks.hasPendingOnboardingCompletion.mockReturnValue(true);
    const user = userEvent.setup();
    renderFlow();

    await user.click(screen.getByText('stub-start-over'));

    expect(mocks.clearOnboardingCompletionMark).not.toHaveBeenCalled();
    expect(screen.getByText('stub-welcome-next')).toBeInTheDocument();
    expect(screen.queryByTestId('step-completion-recovery')).toBeNull();
  });

  it('exits into the workspace on recovery without replaying the welcome scenario', async () => {
    mocks.hasPendingOnboardingCompletion.mockReturnValue(true);
    const user = userEvent.setup();
    const { onComplete } = renderFlow();
    await waitFor(() => expect(mocks.listWorkspaces).toHaveBeenCalled());

    await user.click(screen.getByText('stub-recovered'));

    expect(onComplete).toHaveBeenCalledWith(WORKSPACE);
    expect(useWelcomeStore.getState().signal).toBeNull();
    expect(mocks.completeOnboarding).not.toHaveBeenCalled();
  });

  it('hands over the workspace even when the list has not resolved yet', async () => {
    mocks.hasPendingOnboardingCompletion.mockReturnValue(true);
    const pending = deferredWorkspaces();
    mocks.listWorkspaces.mockReturnValue(pending.promise);
    const user = userEvent.setup();
    const { onComplete } = renderFlow();

    await user.click(screen.getByText('stub-recovered'));
    pending.settle([WORKSPACE]);

    await waitFor(() => expect(onComplete).toHaveBeenCalledWith(WORKSPACE));
    expect(onComplete).not.toHaveBeenCalledWith(undefined);
  });

  it('does not navigate out of a flow that is already gone', async () => {
    mocks.hasPendingOnboardingCompletion.mockReturnValue(true);
    const pending = deferredWorkspaces();
    mocks.listWorkspaces.mockReturnValue(pending.promise);
    const user = userEvent.setup();
    const { onComplete, unmount } = renderFlow();

    await user.click(screen.getByText('stub-recovered'));
    unmount();
    pending.settle([WORKSPACE]);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    await new Promise((r) => setTimeout(r, 0));
    await new Promise((r) => setTimeout(r, 0));

    expect(onComplete).not.toHaveBeenCalled();
  });
});

function deferredWorkspaces() {
  let settle!: (value: Workspace[]) => void;
  const promise = new Promise<Workspace[]>((res) => {
    settle = res;
  });
  return { promise, settle };
}

describe('OnboardingFlow — joining an existing role workspace (R-16d)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWelcomeStore.getState().reset();
    mocks.listWorkspaces.mockResolvedValue([]);
    mocks.completeOnboarding.mockResolvedValue(undefined);
    mocks.saveQuestionnaire.mockResolvedValue(undefined);
    mocks.listJoinTargets.mockResolvedValue([]);
    mocks.getEffectiveConfig.mockResolvedValue({ mcp: {} });
    mocks.hasPendingOnboardingCompletion.mockReturnValue(false);
    AUTH_STATE.user.onboarded_at = null;
  });

  async function reachWorkspaceStep(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByText('stub-welcome-next'));
    await user.click(screen.getByText('stub-about-next'));
  }

  it('keeps the create-workspace step when the deployment offers no roles', async () => {
    const user = userEvent.setup();
    renderFlow();
    await reachWorkspaceStep(user);

    expect(await screen.findByText('stub-workspace-create')).toBeInTheDocument();
    expect(screen.queryByTestId('step-choose-role')).toBeNull();
  });

  it('replaces create-workspace with the role picker when roles are open', async () => {
    mocks.listJoinTargets.mockResolvedValue([
      { id: 'ws_1', slug: 'hr', name: 'HR', description: 'People', member_count: 3 },
    ]);
    const user = userEvent.setup();
    renderFlow();
    await reachWorkspaceStep(user);

    expect(await screen.findByTestId('step-choose-role')).toBeInTheDocument();
    expect(screen.queryByText('stub-workspace-create')).toBeNull();
  });

  it('lets a person insist on creating a workspace anyway', async () => {
    mocks.listJoinTargets.mockResolvedValue([
      { id: 'ws_1', slug: 'hr', name: 'HR', description: '', member_count: 1 },
    ]);
    const user = userEvent.setup();
    renderFlow();
    await reachWorkspaceStep(user);
    await user.click(await screen.findByText('stub-create-instead'));

    expect(await screen.findByText('stub-workspace-create')).toBeInTheDocument();
  });

  it('does not narrow to an empty list while the role config is pending', async () => {
    mocks.listJoinTargets.mockResolvedValue([
      { id: 'ws_1', slug: 'hr', name: 'HR', description: '', member_count: 1 },
    ]);
    mocks.getEffectiveConfig.mockReturnValue(new Promise(() => {}));
    const user = userEvent.setup();
    renderFlow();
    await reachWorkspaceStep(user);
    await user.click(await screen.findByText('stub-join-role'));
    await user.click(screen.getByText('stub-pick-runtime'));

    const step = await screen.findByTestId('step-work-tools');
    expect(step.getAttribute('data-installed')).not.toBe('[]');
  });

  it('narrows the work-tools step to what the chosen role enables', async () => {
    mocks.listJoinTargets.mockResolvedValue([
      { id: 'ws_1', slug: 'hr', name: 'HR', description: '', member_count: 1 },
    ]);
    mocks.getEffectiveConfig.mockResolvedValue({
      mcp: { 'ews-mcp': { enabled: true }, 'bitrix24-mcp': { enabled: false } },
    });
    const user = userEvent.setup();
    renderFlow();
    await reachWorkspaceStep(user);
    await user.click(await screen.findByText('stub-join-role'));
    await user.click(screen.getByText('stub-pick-runtime'));

    const step = await screen.findByTestId('step-work-tools');
    await waitFor(() => expect(step.getAttribute('data-installed')).toContain('ews-mcp'));
    expect(step.getAttribute('data-installed')).not.toContain('bitrix24-mcp');
  });

  it('keeps the public-index services offered after a role join', async () => {
    mocks.listJoinTargets.mockResolvedValue([
      { id: 'ws_1', slug: 'hr', name: 'HR', description: '', member_count: 1 },
    ]);
    mocks.getEffectiveConfig.mockResolvedValue({
      mcp: { 'ews-mcp': { enabled: true } },
    });
    const user = userEvent.setup();
    renderFlow();
    await reachWorkspaceStep(user);
    await user.click(await screen.findByText('stub-join-role'));
    await user.click(screen.getByText('stub-pick-runtime'));

    const step = await screen.findByTestId('step-work-tools');
    await waitFor(() => {
      const installed = JSON.parse(step.getAttribute('data-installed') ?? 'null') as
        string[] | null;
      expect(installed).toEqual(expect.arrayContaining(['atlassian', 'fetch', 'mcp-gateway']));
    });
  });
});

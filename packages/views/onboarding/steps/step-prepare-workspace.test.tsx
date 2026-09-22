import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enOnboarding from '../../locales/en/onboarding.json';
import type { ProvisioningStatus } from '../provisioning-status';
import { StepPrepareWorkspace, type PxProxyRowStatus } from './step-prepare-workspace';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

function renderStep(props: {
  status: ProvisioningStatus | null;
  onRetry?: () => void | Promise<void>;
  onAdvance?: () => void;
}) {
  const onAdvance = props.onAdvance ?? vi.fn();
  const onRetry = props.onRetry ?? vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepPrepareWorkspace status={props.status} onRetry={onRetry} onAdvance={onAdvance} />
    </I18nProvider>,
  );
  return { onAdvance, onRetry };
}

function syncingStatus(overrides: Partial<ProvisioningStatus> = {}): ProvisioningStatus {
  return {
    configured: true,
    state: 'syncing',
    byType: {
      skill: { installed: 1, total: 4, failed: 0 },
      'mcp-server': { installed: 0, total: 2, failed: 0 },
      runtime: { installed: 0, total: 1, failed: 0 },
    },
    summary: { installed: 1, total: 7, failed: 0 },
    packages: [],
    ...overrides,
  };
}

describe('StepPrepareWorkspace', () => {
  it('renders three progress rows from byType', () => {
    renderStep({ status: syncingStatus() });

    expect(screen.getByText('1/4')).toBeInTheDocument();
    expect(screen.getByText('0/2')).toBeInTheDocument();
    expect(screen.getByText('0/1')).toBeInTheDocument();
  });

  it('shows an unavailable-package count on the matching row while still syncing', () => {
    renderStep({
      status: syncingStatus({
        byType: {
          skill: { installed: 1, total: 1, failed: 0 },
          'mcp-server': { installed: 0, total: 1, failed: 0 },
          runtime: { installed: 0, total: 0, failed: 0 },
        },
        unavailablePackages: [
          {
            key: 'mcp-server:ews-mcp@0.1.0',
            reason: 'package not found: ews-mcp@0.1.0',
          },
        ],
      }),
    });

    expect(screen.getByText(/0\/1.*1 unavailable: ews-mcp/)).toBeInTheDocument();
  });

  it("still auto-advances on 'ok' despite an unavailable package — it is not a step failure", () => {
    const onAdvance = vi.fn();
    renderStep({
      status: syncingStatus({
        state: 'ok',
        byType: {
          skill: { installed: 1, total: 1, failed: 0 },
          'mcp-server': { installed: 1, total: 1, failed: 0 },
          runtime: { installed: 0, total: 0, failed: 0 },
        },
        summary: { installed: 2, total: 2, failed: 0 },
        unavailablePackages: [
          {
            key: 'mcp-server:ews-mcp@0.1.0',
            reason: 'package not found: ews-mcp@0.1.0',
          },
        ],
      }),
      onAdvance,
    });

    expect(onAdvance).toHaveBeenCalled();
  });

  it('attributes each unavailable entry to its own package-type row only', () => {
    renderStep({
      status: syncingStatus({
        unavailablePackages: [
          {
            key: 'skill:mail-triage@1.0.0',
            reason: 'dependency unavailable: mail-triage requires ews-mcp@0.1.0',
          },
        ],
      }),
    });

    expect(screen.getByText(/1\/4.*1 unavailable: mail-triage/)).toBeInTheDocument();
    expect(screen.getByText('0/2')).toBeInTheDocument();
    expect(screen.getByText('0/1')).toBeInTheDocument();
  });

  it('auto-advances without any click when state becomes ok', async () => {
    const onAdvance = vi.fn();
    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace status={syncingStatus()} onRetry={vi.fn()} onAdvance={onAdvance} />
      </I18nProvider>,
    );
    expect(onAdvance).not.toHaveBeenCalled();

    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={syncingStatus({
            state: 'ok',
            byType: {
              skill: { installed: 4, total: 4, failed: 0 },
              'mcp-server': { installed: 2, total: 2, failed: 0 },
              runtime: { installed: 1, total: 1, failed: 0 },
            },
            summary: { installed: 7, total: 7, failed: 0 },
          })}
          onRetry={vi.fn()}
          onAdvance={onAdvance}
        />
      </I18nProvider>,
    );

    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it('does not advance while still syncing', () => {
    const { onAdvance } = renderStep({ status: syncingStatus() });
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it('shows a translated reason and a working retry on failure, never a raw reasonCode', async () => {
    const user = userEvent.setup();
    const { onRetry, onAdvance } = renderStep({
      status: syncingStatus({ state: 'fail', reasonCode: 'catalog_unreachable' }),
    });

    expect(screen.queryByText('catalog_unreachable')).toBeNull();
    expect(
      screen.getByText(enOnboarding.step_prepare_workspace.reasons.catalog_unreachable),
    ).toBeInTheDocument();

    const retryButton = screen.getByRole('button', {
      name: enOnboarding.step_prepare_workspace.retry,
    });
    await user.click(retryButton);
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it('falls back to a generic translated reason for an unrecognized reasonCode — never the raw code', () => {
    renderStep({
      status: syncingStatus({ state: 'fail', reasonCode: 'some_new_backend_code' }),
    });

    expect(screen.queryByText('some_new_backend_code')).toBeNull();
    expect(
      screen.getByText(enOnboarding.step_prepare_workspace.reasons.unknown),
    ).toBeInTheDocument();
  });

  it('passes through immediately without a progress screen when configured is false', () => {
    const onAdvance = vi.fn();
    renderStep({
      status: { ...syncingStatus(), configured: false },
      onAdvance,
    });

    expect(onAdvance).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('1/4')).toBeNull();
  });

  it('skip advances immediately with an honest label while sync keeps running in the background', async () => {
    const user = userEvent.setup();
    const { onAdvance, onRetry } = renderStep({ status: syncingStatus() });

    const skipButton = screen.getByRole('button', {
      name: enOnboarding.step_prepare_workspace.skip,
    });
    await user.click(skipButton);

    expect(onAdvance).toHaveBeenCalledTimes(1);
    expect(onRetry).not.toHaveBeenCalled();
  });

  it('does not auto-advance and shows an honest waiting state while a daemon restart is pending, even though state is ok', () => {
    const onAdvance = vi.fn();
    renderStep({
      status: syncingStatus({
        state: 'ok',
        byType: {
          skill: { installed: 4, total: 4, failed: 0 },
          'mcp-server': { installed: 2, total: 2, failed: 0 },
          runtime: { installed: 1, total: 1, failed: 0 },
        },
        summary: { installed: 7, total: 7, failed: 0 },
        restartPending: true,
      }),
      onAdvance,
    });

    expect(onAdvance).not.toHaveBeenCalled();
    expect(
      screen.getByText(enOnboarding.step_prepare_workspace.lede_restart_pending),
    ).toBeInTheDocument();
    expect(
      screen.getByText(enOnboarding.step_prepare_workspace.hint_restart_pending),
    ).toBeInTheDocument();
    expect(screen.getByText('4/4')).toBeInTheDocument();
    expect(screen.getByText('2/2')).toBeInTheDocument();
    expect(screen.getByText('1/1')).toBeInTheDocument();
  });

  it('auto-advances once the pending restart clears (restartPending flips to false)', () => {
    const onAdvance = vi.fn();
    const okStatus = syncingStatus({
      state: 'ok',
      byType: {
        skill: { installed: 4, total: 4, failed: 0 },
        'mcp-server': { installed: 2, total: 2, failed: 0 },
        runtime: { installed: 1, total: 1, failed: 0 },
      },
      summary: { installed: 7, total: 7, failed: 0 },
      restartPending: true,
    });
    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace status={okStatus} onRetry={vi.fn()} onAdvance={onAdvance} />
      </I18nProvider>,
    );
    expect(onAdvance).not.toHaveBeenCalled();

    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={{ ...okStatus, restartPending: false }}
          onRetry={vi.fn()}
          onAdvance={onAdvance}
        />
      </I18nProvider>,
    );

    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it('does not stall while status is still loading (null)', () => {
    const onAdvance = vi.fn();
    renderStep({ status: null, onAdvance });
    expect(onAdvance).not.toHaveBeenCalled();
    expect(screen.queryByText('1/4')).toBeNull();
  });
});

describe('StepPrepareWorkspace — ProgressRow with total === 0 (T-15)', () => {
  it('shows done (no spinner) for an empty category (total 0, installed 0)', () => {
    renderStep({
      status: syncingStatus({
        byType: {
          skill: { installed: 1, total: 4, failed: 0 },
          'mcp-server': { installed: 0, total: 2, failed: 0 },
          runtime: { installed: 0, total: 0, failed: 0 },
        },
      }),
    });

    const row = screen.getByText('0/0').closest('div') as HTMLElement;
    expect(row.querySelector('.animate-spin')).toBeNull();
    expect(row.querySelector('.text-success')).not.toBeNull();
  });

  it('still spins for a nonempty category with installed < total', () => {
    renderStep({ status: syncingStatus() });

    const row = screen.getByText('0/2').closest('div') as HTMLElement;
    expect(row.querySelector('.animate-spin')).not.toBeNull();
  });

  it('shows done once installed reaches total (unchanged behavior)', () => {
    renderStep({
      status: syncingStatus({
        byType: {
          skill: { installed: 4, total: 4, failed: 0 },
          'mcp-server': { installed: 0, total: 2, failed: 0 },
          runtime: { installed: 0, total: 1, failed: 0 },
        },
      }),
    });

    const row = screen.getByText('4/4').closest('div') as HTMLElement;
    expect(row.querySelector('.animate-spin')).toBeNull();
    expect(row.querySelector('.text-success')).not.toBeNull();
  });
});

describe('StepPrepareWorkspace — agent row', () => {
  function renderWithAgent(
    agentStatus: import('../agent-row-status').AgentRuntimeStatus | null,
    extra: {
      onAdvance?: () => void;
      onRetryAgent?: () => void | Promise<void>;
      onSkipAgent?: () => void;
    } = {},
  ) {
    const onAdvance = extra.onAdvance ?? vi.fn();
    const onRetryAgent = extra.onRetryAgent ?? vi.fn();
    const onSkipAgent = extra.onSkipAgent ?? vi.fn();
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={syncingStatus()}
          onRetry={vi.fn()}
          onAdvance={onAdvance}
          agentStatus={agentStatus}
          onRetryAgent={onRetryAgent}
          onSkipAgent={onSkipAgent}
        />
      </I18nProvider>,
    );
    return { onAdvance, onRetryAgent, onSkipAgent };
  }

  it('renders the waiting state', () => {
    renderWithAgent({ state: 'checking' });
    expect(screen.getByText('hermes agent')).toBeInTheDocument();
    expect(screen.getByText('Waiting…')).toBeInTheDocument();
  });

  it('renders the installing state', () => {
    renderWithAgent({ state: 'installing' });
    expect(screen.getByText('Installing…')).toBeInTheDocument();
  });

  it('renders the ready state with the version', () => {
    renderWithAgent({ state: 'ready', version: '1.4.0' });
    expect(screen.getByText('Installed (v1.4.0)')).toBeInTheDocument();
    expect(screen.queryByText('Retry')).toBeNull();
  });

  it('renders the error state with a Retry button that calls onRetryAgent', async () => {
    const user = userEvent.setup();
    const { onRetryAgent } = renderWithAgent({
      state: 'not_installed',
      detail: 'installer exited with code 1',
    });
    expect(screen.getByText('Failed')).toBeInTheDocument();
    const retryButtons = screen.getAllByText('Retry');
    const lastRetryButton = retryButtons[retryButtons.length - 1];
    if (!lastRetryButton) throw new Error('expected a Retry button');
    await user.click(lastRetryButton);
    expect(onRetryAgent).toHaveBeenCalledTimes(1);
  });

  it('offers Продолжить без агента on error and calls onSkipAgent', async () => {
    const user = userEvent.setup();
    const { onSkipAgent } = renderWithAgent({
      state: 'not_installed',
      detail: 'timed out after 5 minutes',
    });
    const skipButton = screen.getByText('Continue without the agent');
    await user.click(skipButton);
    expect(onSkipAgent).toHaveBeenCalledTimes(1);
  });

  it("renders needs_config as 'installed, LLM not configured' with Retry, not Failed", async () => {
    const user = userEvent.setup();
    const { onRetryAgent } = renderWithAgent({ state: 'needs_config' });
    expect(screen.getByText('Installed, LLM not configured')).toBeInTheDocument();
    expect(screen.queryByText('Failed')).toBeNull();
    expect(screen.queryByText('Continue without the agent')).toBeNull();
    const retryButtons = screen.getAllByText('Retry');
    const lastRetryButton = retryButtons[retryButtons.length - 1];
    if (!lastRetryButton) throw new Error('expected a Retry button');
    await user.click(lastRetryButton);
    expect(onRetryAgent).toHaveBeenCalledTimes(1);
  });

  it('does not offer a retry/skip control on the unsupported (Windows) state', () => {
    renderWithAgent({
      state: 'unsupported',
      detail: 'not supported on Windows',
    });
    expect(
      screen.getByText('Not supported on Windows yet — see hermes-agent#38.'),
    ).toBeInTheDocument();
    expect(screen.queryByText('Continue without the agent')).toBeNull();
  });

  it('does not block auto-advance when agentStatus is never passed (backward compatible)', () => {
    const onAdvance = vi.fn();
    const okStatus = syncingStatus({ state: 'ok', restartPending: false });
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace status={okStatus} onRetry={vi.fn()} onAdvance={onAdvance} />
      </I18nProvider>,
    );
    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it('blocks completion until the agent is ready, then advances', () => {
    const onAdvance = vi.fn();
    const okStatus = syncingStatus({ state: 'ok', restartPending: false });
    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={okStatus}
          onRetry={vi.fn()}
          onAdvance={onAdvance}
          agentStatus={{ state: 'installing' }}
        />
      </I18nProvider>,
    );
    expect(onAdvance).not.toHaveBeenCalled();

    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={okStatus}
          onRetry={vi.fn()}
          onAdvance={onAdvance}
          agentStatus={{ state: 'ready', version: '1.4.0' }}
        />
      </I18nProvider>,
    );
    expect(onAdvance).toHaveBeenCalledTimes(1);
  });
});

describe('StepPrepareWorkspace — AI gateway row (T-25)', () => {
  function renderWithGateway(
    llmGateway: { verdict: string; source: 'agent' | 'server' } | null,
    onRetryLlmGateway = vi.fn(),
  ) {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={syncingStatus()}
          onRetry={vi.fn()}
          onAdvance={vi.fn()}
          llmGateway={llmGateway as never}
          onRetryLlmGateway={onRetryLlmGateway}
        />
      </I18nProvider>,
    );
    return { onRetryLlmGateway };
  }

  it('omits the row entirely when there is no signal yet', () => {
    renderWithGateway(null);
    expect(screen.queryByText('AI gateway')).toBeNull();
  });

  it('shows Connected with no retry button when ok', () => {
    renderWithGateway({ verdict: 'ok', source: 'agent' });
    expect(screen.getByText('AI gateway')).toBeInTheDocument();
    expect(screen.getByText('Connected')).toBeInTheDocument();
    expect(screen.queryByText('Retry')).toBeNull();
  });

  it('shows auth_rejected and degraded as visually distinct, non-ok states', () => {
    const { unmount } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={syncingStatus()}
          onRetry={vi.fn()}
          onAdvance={vi.fn()}
          llmGateway={{ verdict: 'auth_rejected', source: 'agent' }}
        />
      </I18nProvider>,
    );
    expect(screen.getByText('Key rejected')).toBeInTheDocument();
    expect(screen.queryByText('Connected')).toBeNull();
    unmount();

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={syncingStatus()}
          onRetry={vi.fn()}
          onAdvance={vi.fn()}
          llmGateway={{ verdict: 'degraded', source: 'agent' }}
        />
      </I18nProvider>,
    );
    expect(screen.getByText('Reachable, erroring')).toBeInTheDocument();
    expect(screen.queryByText('Connected')).toBeNull();
  });

  it("marks a server-sourced verdict as checking the server key, not the agent's", () => {
    renderWithGateway({ verdict: 'ok', source: 'server' });
    expect(screen.getByText(/server key checked, not the agent's/)).toBeInTheDocument();
  });

  it('offers Retry for a non-ok verdict and calls onRetryLlmGateway', async () => {
    const user = userEvent.setup();
    const { onRetryLlmGateway } = renderWithGateway({
      verdict: 'unreachable',
      source: 'agent',
    });
    await user.click(screen.getByText('Retry'));
    expect(onRetryLlmGateway).toHaveBeenCalledTimes(1);
  });

  it("names the failing host instead of a bare 'Unreachable'", () => {
    renderWithGateway({
      verdict: 'unreachable',
      source: 'agent',
      host: 'llm.example.com',
    } as never);
    expect(screen.getByText('Timeout llm.example.com')).toBeInTheDocument();
  });

  it('names the failing host for auth_rejected too', () => {
    renderWithGateway({
      verdict: 'auth_rejected',
      source: 'agent',
      host: 'api.example.com',
    } as never);
    expect(screen.getByText('401 api.example.com')).toBeInTheDocument();
  });

  it('shows the switch-to-stand-address hint when provided', () => {
    renderWithGateway({
      verdict: 'unreachable',
      source: 'agent',
      host: 'llm.example.com',
      standApiBaseHint: 'api.deepseek.com',
    } as never);
    expect(
      screen.getByText(/switch to the stand's address api\.deepseek\.com/),
    ).toBeInTheDocument();
  });

  it('omits the hint when there is none', () => {
    renderWithGateway({
      verdict: 'unreachable',
      source: 'agent',
      host: 'llm.example.com',
    } as never);
    expect(screen.queryByText(/switch to the stand/)).toBeNull();
  });
});

describe('StepPrepareWorkspace — px proxy row (T-17)', () => {
  function renderWithPx(pxProxy: PxProxyRowStatus | null) {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepPrepareWorkspace
          status={syncingStatus()}
          onRetry={vi.fn()}
          onAdvance={vi.fn()}
          pxProxy={pxProxy}
        />
      </I18nProvider>,
    );
  }

  it('omits the row entirely when there is no signal yet', () => {
    renderWithPx(null);
    expect(screen.queryByText('px proxy')).toBeNull();
  });

  it("shows 'Not needed' when the resolved route doesn't go through px", () => {
    renderWithPx({ state: 'not_required' });
    expect(screen.getByText('px proxy')).toBeInTheDocument();
    expect(screen.getByText('Not needed')).toBeInTheDocument();
  });

  it('shows the installed version when installed', () => {
    renderWithPx({ state: 'installed', version: '0.11.0' });
    expect(screen.getByText('Installed (0.11.0)')).toBeInTheDocument();
  });

  it("shows 'Not found' when required but missing", () => {
    renderWithPx({ state: 'not_found' });
    expect(screen.getByText('Not found')).toBeInTheDocument();
  });

  it("shows no MCP-check sub-line when the check hasn't run or was skipped", () => {
    renderWithPx({ state: 'installed', mcpCheck: 'skipped' });
    expect(screen.queryByText(/MCP through px/)).toBeNull();
    renderWithPx({ state: 'installed', mcpCheck: null });
    expect(screen.queryByText(/MCP through px/)).toBeNull();
  });

  it('shows the ok MCP-check verdict', () => {
    renderWithPx({ state: 'installed', mcpCheck: 'ok' });
    expect(screen.getByText(/MCP through px: starts/)).toBeInTheDocument();
  });

  it('shows the proxy_unreachable MCP-check verdict', () => {
    renderWithPx({ state: 'installed', mcpCheck: 'proxy_unreachable' });
    expect(screen.getByText(/MCP through px: proxy unreachable/)).toBeInTheDocument();
  });

  it('shows the mcp_failed verdict with its reason', () => {
    renderWithPx({
      state: 'installed',
      mcpCheck: { failed: 'ModuleNotFoundError: mcp' },
    });
    expect(screen.getByText(/failed: ModuleNotFoundError: mcp/)).toBeInTheDocument();
  });
});

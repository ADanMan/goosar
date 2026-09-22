import { describe, it, expect, beforeEach, vi } from 'vitest';
import { cleanup, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithI18n } from '../../test/i18n';
import { NavigationProvider } from '../../navigation';
import type { NavigationAdapter } from '../../navigation';

const queryKeys = vi.hoisted(() => [] as unknown[][]);
const dashboardDataRef = vi.hoisted(() => ({ current: false }));
const manyAgentsRef = vi.hoisted(() => ({ current: false }));

function todayIso() {
  return new Date().toISOString().slice(0, 10);
}

vi.mock('@tanstack/react-query', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-query')>('@tanstack/react-query');
  return {
    ...actual,
    useQuery: (opts: { queryKey: unknown[] }) => {
      queryKeys.push(opts.queryKey);
      if (dashboardDataRef.current) {
        if (opts.queryKey[0] === 'workspaces' && opts.queryKey[2] === 'agents') {
          return {
            data: manyAgentsRef.current
              ? Array.from({ length: 12 }, (_, i) => ({
                  id: `bulk-${i}`,
                  name: `Bulk Agent ${i}`,
                }))
              : [{ id: 'agent-1', name: 'Agent One' }],
            isLoading: false,
            isSuccess: true,
          };
        }
        const kind = opts.queryKey[2];
        const bulkRows = !manyAgentsRef.current
          ? null
          : kind === 'by-agent'
            ? Array.from({ length: 12 }, (_, i) => ({
                agent_id: `bulk-${i}`,
                provider: 'anthropic',
                model: 'claude-sonnet-4-6',
                input_tokens: (12 - i) * 1_000,
                output_tokens: 0,
                cache_read_tokens: 0,
                cache_write_tokens: 0,
                task_count: 12 - i,
              }))
            : kind === 'agent-runtime'
              ? Array.from({ length: 12 }, (_, i) => ({
                  agent_id: `bulk-${i}`,
                  total_seconds: (12 - i) * 600,
                  task_count: 12 - i,
                  failed_count: 12 - i,
                }))
              : kind === 'failures-by-agent'
                ? Array.from({ length: 12 }, (_, i) => [
                    {
                      agent_id: `bulk-${i}`,
                      failure_reason: '',
                      task_count: 100,
                    },
                    {
                      agent_id: `bulk-${i}`,
                      failure_reason: 'timeout',
                      task_count: 12 - i,
                    },
                  ]).flat()
                : null;
        if (bulkRows) {
          return { data: bulkRows, isLoading: false, isSuccess: true };
        }
        const data =
          kind === 'daily'
            ? [
                {
                  date: todayIso(),
                  provider: 'anthropic',
                  model: 'claude-sonnet-4-6',
                  input_tokens: 1_000,
                  output_tokens: 2_000,
                  cache_read_tokens: 0,
                  cache_write_tokens: 0,
                  task_count: 2,
                },
              ]
            : kind === 'agent-runtime'
              ? [
                  {
                    agent_id: 'agent-1',
                    total_seconds: 3 * 3_600 + 17 * 60,
                    task_count: 12,
                    failed_count: 1,
                  },
                ]
              : kind === 'runtime-daily'
                ? [
                    {
                      date: todayIso(),
                      total_seconds: 3 * 3_600 + 17 * 60,
                      task_count: 12,
                      failed_count: 1,
                    },
                  ]
                : 
                  kind === 'failures-daily'
                  ? [
                      { date: todayIso(), failure_reason: '', task_count: 6 },
                      {
                        date: todayIso(),
                        failure_reason: 'agent_error.provider_auth_or_access',
                        task_count: 3,
                      },
                      { date: todayIso(), failure_reason: 'timeout', task_count: 1 },
                    ]
                  : kind === 'failures-by-agent'
                    ? [
                        { agent_id: 'agent-1', failure_reason: '', task_count: 6 },
                        {
                          agent_id: 'agent-1',
                          failure_reason: 'agent_error.provider_auth_or_access',
                          task_count: 3,
                        },
                        {
                          agent_id: 'agent-1',
                          failure_reason: 'timeout',
                          task_count: 1,
                        },
                        {
                          agent_id: '0f9d1c2e-private-agent-uuid',
                          failure_reason: 'agent_error.provider_auth_or_access',
                          task_count: 2,
                        },
                      ]
                    : [];
        return { data, isLoading: false, isSuccess: true };
      }
      return { data: undefined, isLoading: true };
    },
  };
});

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/api', () => ({
  api: { getBaseUrl: () => 'https://example.test' },
}));

vi.mock('@goosar/core/paths', () => ({
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));

const tzRef = vi.hoisted(() => ({ current: 'UTC' as string | null }));

vi.mock('@goosar/core/auth', () => {
  type AuthState = { user: { timezone: string | null } | null };
  const state = (): AuthState => ({ user: { timezone: tzRef.current } });
  const useAuthStore = Object.assign(
    (sel?: (s: AuthState) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});

vi.mock('@goosar/core/runtimes/custom-pricing-store', () => {
  const state = () => ({ pricings: {} });
  const useCustomPricingStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useCustomPricingStore };
});

import { DashboardPage } from './dashboard-page';

const navAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: '/acme/usage',
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => `https://example.test${path}`,
};

function renderDashboard() {
  return renderWithI18n(
    <NavigationProvider value={navAdapter}>
      <DashboardPage />
    </NavigationProvider>,
  );
}

function offenderCard(): HTMLElement {
  return screen.getByRole('list', { name: 'Top offenders' }).parentElement as HTMLElement;
}

function offenderBar(index: number): HTMLElement {
  const rows = within(screen.getByRole('list', { name: 'Top offenders' })).getAllByRole('listitem');
  return within(rows[index] as HTMLElement).getByRole('img');
}

describe('DashboardPage — viewing timezone drives the query key', () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = false;
    cleanup();
  });

  function tzSegments(): unknown[] {
    return queryKeys.filter((k) => k[0] === 'dashboard').map((k) => k[k.length - 1]);
  }

  it('uses the stored timezone in every dashboard query key', () => {
    tzRef.current = 'UTC';
    renderWithI18n(<DashboardPage />);

    const tzs = tzSegments();
    expect(tzs.length).toBeGreaterThan(0);
    expect(tzs.every((tz) => tz === 'UTC')).toBe(true);
  });

  it('flips the query key when the stored timezone changes', () => {
    tzRef.current = 'UTC';
    renderWithI18n(<DashboardPage />);
    const utcKeys = queryKeys.filter((k) => k[0] === 'dashboard');

    queryKeys.length = 0;
    cleanup();

    tzRef.current = 'Asia/Tokyo';
    renderWithI18n(<DashboardPage />);
    const tokyoKeys = queryKeys.filter((k) => k[0] === 'dashboard');

    expect(utcKeys.length).toBe(tokyoKeys.length);
    expect(utcKeys.length).toBeGreaterThan(0);
    for (let i = 0; i < utcKeys.length; i++) {
      expect(utcKeys[i]).not.toEqual(tokyoKeys[i]);
    }
  });

  it('renders every workspace KPI as an animated number', () => {
    dashboardDataRef.current = true;
    tzRef.current = 'UTC';

    const { container } = renderDashboard();
    const flows = Array.from(container.querySelectorAll('number-flow-react'));

    expect(flows).toHaveLength(5);
    expect(flows.map((flow) => flow.getAttribute('aria-label'))).toEqual(
      expect.arrayContaining(['$0.03', '3K', '12']),
    );
    expect(container).toHaveTextContent('3h 17m');
    expect(
      flows.every(
        (flow) =>
          (flow as HTMLElement & { respectMotionPreference?: boolean }).respectMotionPreference ===
          true,
      ),
    ).toBe(true);
  });
});

describe('DashboardPage — failure visibility', () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    tzRef.current = 'UTC';
    cleanup();
  });

  it('states the error rate with its denominator spelled out', () => {
    renderDashboard();

    expect(screen.getByText('4 of 10 runs failed · 40%')).toBeInTheDocument();
    expect(screen.getByText('1 failed')).toBeInTheDocument();
  });

  it('breaks failures down by class and links the offending agent to its runs', () => {
    renderDashboard();

    const byClass = within(screen.getByRole('list', { name: 'Failure mix' }));
    expect(byClass.getAllByRole('listitem').map((li) => li.textContent)).toEqual([
      'Auth3',
      'Timeout1',
    ]);
    expect(screen.getByText('Failure mix · 4')).toBeInTheDocument();

    const byAgent = within(screen.getByRole('list', { name: 'Top offenders' }));
    const link = byAgent.getByRole('link', { name: /Agent One/ });
    expect(link).toHaveAttribute('href', '/acme/agents/agent-1?view=overview');
    const row = byAgent.getAllByRole('listitem')[0] as HTMLElement;
    expect(within(row).getByText('4')).toBeInTheDocument();
    expect(within(row).getByText('10')).toBeInTheDocument();
    expect(within(row).getByText('40%')).toBeInTheDocument();
    expect(within(row).getByRole('img')).toHaveAccessibleName('Auth 3 · Timeout 1');
  });

  it('moves the bar onto whichever metric the list is ranked by', async () => {
    const user = userEvent.setup();
    renderDashboard();

    expect(offenderBar(1).style.width).toBe('50%');

    await user.click(within(offenderCard()).getByRole('button', { name: 'Rate' }));

    expect(offenderBar(1).style.width).toBe('100%');
  });

  it('says which metric the list is ranked by without relying on colour', async () => {
    const user = userEvent.setup();
    renderDashboard();

    const group = screen.getByRole('group', { name: 'Rank offenders by' });
    expect(within(group).getByRole('button', { name: 'Failures' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(within(group).getByRole('button', { name: 'Rate' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );

    await user.click(within(group).getByRole('button', { name: 'Rate' }));

    expect(within(group).getByRole('button', { name: 'Rate' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('keeps a two-run agent from hijacking the rate ranking', async () => {
    const user = userEvent.setup();
    renderDashboard();

    await user.click(within(offenderCard()).getByRole('button', { name: 'Rate' }));

    const rows = within(screen.getByRole('list', { name: 'Top offenders' }))
      .getAllByRole('listitem')
      .map((li) => li.textContent);
    expect(rows[0]).toMatch(/Agent One/);
    expect(rows[1]).toMatch(/Other agents/);
  });

  it('reveals the raw failure_reason values behind the class summary', async () => {
    const user = userEvent.setup();
    renderDashboard();

    expect(screen.queryByText('agent_error.provider_auth_or_access')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Show error codes' }));

    expect(screen.getByText('agent_error.provider_auth_or_access')).toBeInTheDocument();
  });

  it('adds Errors to the trend toggle without disturbing the other metrics', async () => {
    const user = userEvent.setup();
    const { container } = renderDashboard();

    await user.click(screen.getByRole('button', { name: 'Errors' }));

    expect(container).toHaveTextContent('Daily errors');
  });
});

describe("DashboardPage — the Errors list never exposes an agent the viewer can't see", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    tzRef.current = 'UTC';
    cleanup();
  });

  it('folds an unresolvable agent into an anonymous row instead of printing its UUID', () => {
    const { container } = renderDashboard();

    expect(container).not.toHaveTextContent('0f9d1c2e-private-agent-uuid');

    const byAgent = within(screen.getByRole('list', { name: 'Top offenders' }));
    expect(byAgent.getByText('Other agents')).toBeInTheDocument();
    expect(byAgent.queryByRole('link', { name: /Other agents/ })).not.toBeInTheDocument();
  });
});

describe('DashboardPage — Errors card placement and density', () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    manyAgentsRef.current = false;
    tzRef.current = 'UTC';
    cleanup();
  });

  it('renders the Errors card after the leaderboard, at the bottom of the page', () => {
    renderDashboard();

    const leaderboard = screen.getByRole('heading', { name: 'Leaderboard' });
    const errors = screen.getByRole('heading', { name: 'Errors' });

    expect(
      leaderboard.compareDocumentPosition(errors) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it('caps the offender list and expands it on demand', async () => {
    manyAgentsRef.current = true;
    const user = userEvent.setup();
    renderDashboard();

    const list = () => screen.getByRole('list', { name: 'Top offenders' });
    expect(within(list()).getAllByRole('listitem')).toHaveLength(8);
    await user.click(screen.getByRole('button', { name: 'Show all 12' }));
    expect(within(list()).getAllByRole('listitem')).toHaveLength(12);

    await user.click(screen.getByRole('button', { name: 'Show top 8' }));
    expect(within(list()).getAllByRole('listitem')).toHaveLength(8);
  });

  it('shows no expand affordance when every offender already fits', () => {
    renderDashboard();

    expect(screen.queryByRole('button', { name: /Show all/ })).not.toBeInTheDocument();
  });
});

describe('DashboardPage — leaderboard density', () => {
  beforeEach(() => {
    queryKeys.length = 0;
    dashboardDataRef.current = true;
    manyAgentsRef.current = false;
    tzRef.current = 'UTC';
    cleanup();
  });

  it('ranks the top 10 agents and keeps the tail behind a toggle', async () => {
    manyAgentsRef.current = true;
    const user = userEvent.setup();
    renderDashboard();

    const list = () => within(screen.getByRole('list', { name: 'Leaderboard' }));
    expect(list().getAllByRole('listitem')).toHaveLength(10);
    expect(list().getByText('Bulk Agent 0')).toBeInTheDocument();
    expect(list().queryByText('Bulk Agent 10')).not.toBeInTheDocument();
    expect(list().queryByText('Bulk Agent 11')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Show all' }));
    expect(list().getAllByRole('listitem')).toHaveLength(12);
    expect(list().getByText('Bulk Agent 11')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Show top 10' }));
    expect(list().getAllByRole('listitem')).toHaveLength(10);
  });

  it('keeps the cap honest when the ranking metric changes', async () => {
    manyAgentsRef.current = true;
    const user = userEvent.setup();
    renderDashboard();

    const card = screen.getByRole('list', { name: 'Leaderboard' }).parentElement as HTMLElement;
    await user.click(within(card).getByRole('button', { name: 'Time' }));

    const list = within(screen.getByRole('list', { name: 'Leaderboard' }));
    expect(list.getAllByRole('listitem')).toHaveLength(10);
  });

  it('shows no expand affordance when every agent already fits', () => {
    renderDashboard();

    const list = within(screen.getByRole('list', { name: 'Leaderboard' }));
    expect(list.getAllByRole('listitem')).toHaveLength(1);
    expect(screen.queryByRole('button', { name: 'Show all' })).not.toBeInTheDocument();
  });
});

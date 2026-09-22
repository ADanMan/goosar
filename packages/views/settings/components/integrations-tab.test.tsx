// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ApiError } from '@goosar/core/api';
import { configStore } from '@goosar/core/config';
import { COMPOSIO_MCP_APPS_FLAG } from '@goosar/core/feature-flags';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enSettings from '../../locales/en/settings.json';

const composioErrorRef = vi.hoisted(() => ({ current: null as Error | null }));
const dataRef = vi.hoisted(() => ({
  current: {} as Record<string, unknown>,
}));
const queryCallsRef = vi.hoisted(() => ({
  current: [] as { queryKey: unknown[]; enabled?: boolean }[],
}));

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    queryCallsRef.current.push(opts);
    if (opts.enabled === false) {
      return { data: undefined, error: null, isError: false, isSuccess: false };
    }
    const key = JSON.stringify(opts.queryKey);
    const error = key.includes('composio') ? composioErrorRef.current : null;
    const match = Object.keys(dataRef.current).find((k) => key.includes(k));
    const data = match ? dataRef.current[match] : undefined;
    return {
      data,
      error,
      isError: error != null,
      isSuccess: error == null && data !== undefined,
    };
  },
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/composio', () => ({
  composioToolkitsOptions: () => ({ queryKey: ['composio', 'toolkits'] }),
  composioConnectionsOptions: () => ({ queryKey: ['composio', 'connections'] }),
}));

vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'workspace-1' }));

vi.mock('@goosar/core/github', () => ({
  githubInstallationsOptions: (wsId: string) => ({
    queryKey: ['github', wsId, 'installations'],
  }),
}));

vi.mock('@goosar/core/slack', () => ({
  slackInstallationsOptions: (wsId: string) => ({
    queryKey: ['slack', wsId, 'installations'],
  }),
}));

vi.mock('@goosar/core/vcs', () => ({
  vcsConnectionsOptions: (wsId: string) => ({ queryKey: ['vcs', wsId, 'connections'] }),
}));

const searchParamsRef = vi.hoisted(() => ({
  current: new URLSearchParams('tab=integrations'),
}));
const replaceMock = vi.hoisted(() => vi.fn());

vi.mock('../../navigation', () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: replaceMock,
    back: vi.fn(),
    pathname: '/acme/settings',
    searchParams: searchParamsRef.current,
    getShareableUrl: (path: string) => path,
  }),
}));

vi.mock('./composio-tab', () => ({ ComposioTab: () => <div data-testid="composio-tab" /> }));
vi.mock('./slack-tab', () => ({ SlackTab: () => <div data-testid="slack-tab" /> }));
vi.mock('./vcs-tab', () => ({ VCSTab: () => <div data-testid="vcs-tab" /> }));
vi.mock('./github-tab', () => ({ GitHubTab: () => <div data-testid="github-tab" /> }));

import { IntegrationsTab } from './integrations-tab';

function renderTab() {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <IntegrationsTab />
    </I18nProvider>,
  );
}

describe('Settings IntegrationsTab', () => {
  beforeEach(() => {
    cleanup();
    queryCallsRef.current = [];
    composioErrorRef.current = null;
    dataRef.current = {};
    searchParamsRef.current = new URLSearchParams('tab=integrations');
    replaceMock.mockClear();
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
    configStore.getState().setAuthConfig({ allowSignup: true, vcsIntegrationAvailable: false });
  });

  it('renders one row per available integration and no panel', () => {
    renderTab();

    for (const id of ['github', 'slack', 'composio']) {
      expect(screen.getByTestId(`integration-row-${id}`)).toBeInTheDocument();
    }
    for (const panel of ['github-tab', 'slack-tab', 'composio-tab']) {
      expect(screen.queryByTestId(panel)).toBeNull();
    }
  });

  it("opens an integration's own panel from its «Configure» action", async () => {
    renderTab();

    const row = screen.getByTestId('integration-row-slack');
    await userEvent.click(
      within(row).getByRole('button', { name: enSettings.integrations.configure }),
    );

    expect(screen.getByTestId('slack-tab')).toBeInTheDocument();
    expect(screen.queryByTestId('integration-row-github')).toBeNull();
  });

  it('reports a connected integration and who may configure it', () => {
    dataRef.current = {
      '"github"': { configured: true, installations: [{ account_login: 'acme' }] },
    };
    renderTab();

    const row = screen.getByTestId('integration-row-github');
    expect(row).toHaveTextContent(enSettings.integrations.status.connected);
    expect(row).toHaveTextContent(enSettings.integrations.configurable_by.workspace_admin);
  });

  it('points at the deployment administrator when the deployment has no credentials', () => {
    dataRef.current = { '"slack"': { configured: false, installations: [] } };
    renderTab();

    const row = screen.getByTestId('integration-row-slack');
    expect(row).toHaveTextContent(enSettings.integrations.status.not_configured);
    expect(row).toHaveTextContent(enSettings.integrations.configurable_by.deployment_admin);
  });

  it('names Composio a per-member integration', () => {
    dataRef.current = { '"composio","connections"': [] };
    renderTab();

    expect(screen.getByTestId('integration-row-composio')).toHaveTextContent(
      enSettings.integrations.configurable_by.member,
    );
  });

  it('hides Composio and disables the toolkits query when the feature flag is off', () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: false });

    renderTab();

    expect(screen.queryByTestId('integration-row-composio')).toBeNull();
    const composioCall = queryCallsRef.current.find((c) =>
      JSON.stringify(c.queryKey).includes('composio'),
    );
    expect(composioCall?.enabled).toBe(false);
  });

  it('hides Composio when the feature flag is on but the server reports 503', () => {
    composioErrorRef.current = new ApiError('unavailable', 503, 'Service Unavailable');

    renderTab();

    expect(screen.queryByTestId('integration-row-composio')).toBeNull();
  });

  it('hides the Git providers row when the deployment reports it unavailable', () => {
    renderTab();

    expect(screen.queryByTestId('integration-row-vcs')).toBeNull();
  });

  it('opens the Composio panel when the OAuth callback lands on the tab', () => {
    searchParamsRef.current = new URLSearchParams('tab=integrations&connected=notion');

    renderTab();

    expect(screen.getByTestId('composio-tab')).toBeInTheDocument();
  });

  it('opens the Composio panel when the callback reports a failure', () => {
    searchParamsRef.current = new URLSearchParams('tab=integrations&error=composio_connect_failed');

    renderTab();

    expect(screen.getByTestId('composio-tab')).toBeInTheDocument();
  });

  it('opens the GitHub panel when the App install callback lands on the tab', () => {
    searchParamsRef.current = new URLSearchParams('tab=integrations&github_connected=1');

    renderTab();

    expect(screen.getByTestId('github-tab')).toBeInTheDocument();
  });

  it('opens the GitHub panel when the install callback reports a failure', () => {
    searchParamsRef.current = new URLSearchParams('tab=integrations&github_error=invalid_state');

    renderTab();

    expect(screen.getByTestId('github-tab')).toBeInTheDocument();
  });

  it('strips the GitHub callback params after consuming them', () => {
    searchParamsRef.current = new URLSearchParams('tab=integrations&github_connected=1');

    renderTab();

    expect(replaceMock).toHaveBeenCalledWith('/acme/settings?tab=integrations');
  });

  it('opens the row when the callback params arrive after the first render', () => {
    const view = renderTab();
    expect(screen.queryByTestId('composio-tab')).toBeNull();

    searchParamsRef.current = new URLSearchParams('tab=integrations&connected=notion');
    view.rerender(
      <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
        <IntegrationsTab />
      </I18nProvider>,
    );

    expect(screen.getByTestId('composio-tab')).toBeInTheDocument();
  });

  it('shows the Git providers row on a self-hosted deployment that enables it', () => {
    configStore.getState().setAuthConfig({ allowSignup: true, vcsIntegrationAvailable: true });

    renderTab();

    expect(screen.getByTestId('integration-row-vcs')).toBeInTheDocument();
  });
});

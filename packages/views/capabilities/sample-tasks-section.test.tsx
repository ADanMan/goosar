// @vitest-environment jsdom

import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { WorkspaceSampleTask } from '@goosar/core/types';
import enCommon from '../locales/en/common.json';
import enOnboarding from '../locales/en/onboarding.json';
import enWorkspace from '../locales/en/workspace.json';
import { NavigationProvider, type NavigationAdapter } from '../navigation';

const TEST_RESOURCES = {
  en: { common: enCommon, onboarding: enOnboarding, workspace: enWorkspace },
};

const mocks = vi.hoisted(() => ({ createIssue: vi.fn() }));

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
  api: { createIssue: mocks.createIssue },
}));

vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-1' }));

vi.mock('@goosar/core/paths', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/paths')>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace('acme') };
});

import type { ServiceCredentialsView } from './use-service-credentials';
import { SampleTasksSection } from './sample-tasks-section';

const HELPER = {
  id: 'agent_helper',
  name: 'Goosar Helper',
} as unknown as ServiceCredentialsView['helper'];

const PLAIN_TASK: WorkspaceSampleTask = {
  key: 'reconcile-table',
  title: 'Merge scattered data into one spreadsheet',
  prompt: 'Merge the attached files into one XLSX.',
  requires: [],
};

const MAIL_TASK: WorkspaceSampleTask = {
  key: 'mail-digest',
  title: "Work through today's mail",
  prompt: "Go through today's mail and split it three ways.",
  requires: ['ews-mcp'],
};

function credentials(overrides: Partial<ServiceCredentialsView> = {}): ServiceCredentialsView {
  return {
    statuses: [],
    missing: [],
    signature: '',
    isLoading: false,
    helper: HELPER,
    ...overrides,
  };
}

function renderSection(
  tasks: WorkspaceSampleTask[],
  view: ServiceCredentialsView = credentials(),
  adapterOverrides: Partial<NavigationAdapter> = {},
) {
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: '/acme/capabilities',
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => path,
    ...adapterOverrides,
  };
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={client}>
        <NavigationProvider value={navigation}>{children}</NavigationProvider>
      </QueryClientProvider>
    </I18nProvider>
  );
  render(
    wrapper({
      children: <SampleTasksSection tasks={tasks} credentials={view} />,
    }),
  );
  return { navigation };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.createIssue.mockResolvedValue({
    id: 'issue-77',
    identifier: 'ACME-77',
  });
});

describe('the demonstration runs for real', () => {
  it('renders nothing for a role whose template carries no tasks', () => {
    renderSection([]);
    expect(screen.queryByText('Try it right now')).toBeNull();
  });

  it("files a real issue for the member's own Helper and opens it in a new tab", async () => {
    const openInNewTab = vi.fn();
    const { navigation } = renderSection([PLAIN_TASK], credentials(), {
      openInNewTab,
    });

    await userEvent.click(screen.getByRole('button', { name: /run it/i }));

    await waitFor(() => expect(mocks.createIssue).toHaveBeenCalledTimes(1));
    expect(mocks.createIssue).toHaveBeenCalledWith({
      title: PLAIN_TASK.title,
      description: PLAIN_TASK.prompt,
      status: 'todo',
      priority: 'high',
      assignee_type: 'agent',
      assignee_id: 'agent_helper',
    });
    await waitFor(() =>
      expect(openInNewTab).toHaveBeenCalledWith('/acme/issues/issue-77', 'ACME-77', {
        activate: true,
      }),
    );
    expect(navigation.push).not.toHaveBeenCalled();
    expect(await screen.findByText(/ACME-77 created/i)).toBeInTheDocument();
  });

  it('falls back to a plain navigation on a shell without tabs (web)', async () => {
    const { navigation } = renderSection([PLAIN_TASK]);

    await userEvent.click(screen.getByRole('button', { name: /run it/i }));

    await waitFor(() => expect(navigation.push).toHaveBeenCalledWith('/acme/issues/issue-77'));
  });

  it('keeps the task runnable when the server refuses', async () => {
    mocks.createIssue.mockRejectedValue(new Error('boom'));
    renderSection([PLAIN_TASK]);

    await userEvent.click(screen.getByRole('button', { name: /run it/i }));

    expect(await screen.findByText(/task could not be created/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /run it/i })).toBeEnabled();
  });
});

describe('the refusal when credentials are missing (B5 / A5)', () => {
  it('names the service and offers no button instead of filing a doomed task', async () => {
    renderSection(
      [MAIL_TASK],
      credentials({
        statuses: [{ preset: 'outlook', admin: 'provided', personal: 'missing' }],
      }),
    );

    expect(screen.getByText(/your key is needed/i)).toBeInTheDocument();
    expect(screen.getByText(/outlook/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /run it/i })).toBeNull();
    expect(mocks.createIssue).not.toHaveBeenCalled();
  });

  it('leads to the one place the value is entered', async () => {
    const { navigation } = renderSection(
      [MAIL_TASK],
      credentials({
        statuses: [{ preset: 'outlook', admin: 'provided', personal: 'missing' }],
      }),
    );

    await userEvent.click(screen.getByRole('button', { name: /where to enter/i }));
    expect(navigation.push).toHaveBeenCalledWith('/acme/agents/agent_helper?view=mcp_config');
  });

  it('runs a task whose service the member has already set up', async () => {
    renderSection(
      [MAIL_TASK],
      credentials({
        statuses: [{ preset: 'outlook', admin: 'provided', personal: 'filled' }],
      }),
    );

    await userEvent.click(screen.getByRole('button', { name: /run it/i }));
    await waitFor(() => expect(mocks.createIssue).toHaveBeenCalledTimes(1));
  });

  it('says so plainly when the member has no assistant yet, rather than throwing on click', () => {
    renderSection([PLAIN_TASK], credentials({ helper: null }));

    expect(screen.getByText(/assistant .* is not ready yet/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /run it/i })).toBeNull();
  });
});

describe('tasks whose template key is blank', () => {
  it("keeps each card's result to itself", async () => {
    const blank = (title: string): WorkspaceSampleTask => ({
      key: '',
      title,
      prompt: 'Do the thing.',
      requires: [],
    });
    renderSection([blank('First'), blank('Second')]);

    const user = userEvent.setup();
    const [firstRun] = screen.getAllByRole('button', { name: /run it/i });
    await user.click(firstRun as HTMLElement);

    await waitFor(() => expect(screen.getAllByText(/Task ACME-77 created/)).toHaveLength(1));
  });
});

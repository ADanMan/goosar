// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enAgents from '../../locales/en/agents.json';
import enSettings from '../../locales/en/settings.json';
import { DeploymentMcpSection } from './deployment-mcp-section';

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, settings: enSettings },
};
const COPY = enSettings.deployment.mcp;

const mockList = vi.hoisted(() => vi.fn());
const mockCreate = vi.hoisted(() => vi.fn());

const { FakeApiError } = vi.hoisted(() => ({
  FakeApiError: class FakeApiError extends Error {
    status: number;
    constructor(status: number) {
      super('forbidden');
      this.status = status;
    }
  },
}));

vi.mock('@goosar/core/api', () => ({
  ApiError: FakeApiError,
  api: {
    listDeploymentMcpServers: (...args: unknown[]) => mockList(...args),
    createDeploymentMcpServer: (...args: unknown[]) => mockCreate(...args),
    updateDeploymentMcpServer: vi.fn(),
    deleteDeploymentMcpServer: vi.fn(),
  },
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function renderSection() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <DeploymentMcpSection />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockCreate.mockResolvedValue(undefined);
});

describe('DeploymentMcpSection', () => {
  it('states how many workspaces took a record — the blast radius of an edit', async () => {
    mockList.mockResolvedValue([
      {
        id: 's1',
        name: 'corp-jira',
        transport: 'http',
        enabled_workspaces: 3,
      },
    ]);
    renderSection();

    expect(await screen.findByText('corp-jira')).toBeTruthy();
    const reach = COPY.enabled_workspaces.replace('{{count}}', '3');
    expect(document.body.textContent).toContain(reach);
  });

  it('says plainly that a record nobody enabled reaches nobody', async () => {
    mockList.mockResolvedValue([
      { id: 's1', name: 'corp-jira', transport: 'http', enabled_workspaces: 0 },
    ]);
    renderSection();

    expect(await screen.findByText('corp-jira')).toBeTruthy();
    expect(document.body.textContent).toContain(COPY.not_enabled_anywhere);
  });

  it('claims nothing about reach when the server did not state it', async () => {
    mockList.mockResolvedValue([{ id: 's1', name: 'corp-jira' }]);
    renderSection();

    expect(await screen.findByText('corp-jira')).toBeTruthy();
    expect(document.body.textContent).not.toContain(COPY.not_enabled_anywhere);
  });

  it('renders nothing when the caller is not a deployment admin', async () => {
    mockList.mockRejectedValue(new FakeApiError(403));
    const { container } = renderSection();

    await waitFor(() => expect(mockList).toHaveBeenCalled());
    expect(screen.queryByText('corp-jira')).toBeNull();
    expect(container.textContent).not.toContain(COPY.empty_title);
  });

  it('says the configuration is write-only, because an edit cannot prefill it', async () => {
    mockList.mockResolvedValue([]);
    renderSection();

    expect(await screen.findByText(COPY.empty_title)).toBeTruthy();
    expect(screen.getByText(COPY.write_only_note)).toBeTruthy();
  });

  it('warns that a delete reaches every workspace and agent that had the record', async () => {
    mockList.mockResolvedValue([
      { id: 's1', name: 'corp-jira', transport: 'http', enabled_workspaces: 2 },
    ]);
    renderSection();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: COPY.remove_server }));
    const expected = COPY.delete_description.replace('{{name}}', 'corp-jira');
    await waitFor(() => expect(document.body.textContent).toContain(COPY.delete_title));
    expect(document.body.textContent).toContain(expected);
  });
});

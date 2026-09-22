// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import type { Agent, MemberWithUser, RuntimeDevice } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import { WorkspaceSlugProvider } from '@goosar/core/paths';
import { configStore } from '@goosar/core/config';
import { COMPOSIO_MCP_APPS_FLAG } from '@goosar/core/feature-flags';
import { NavigationProvider, type NavigationAdapter } from '../../navigation';
import enCommon from '../../locales/en/common.json';
import enAgents from '../../locales/en/agents.json';

const navigationStub: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: '/',
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => path,
};

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('./model-dropdown', () => ({
  ModelDropdown: () => null,
}));

vi.mock('../../runtimes/components/provider-logo', () => ({
  ProviderLogo: () => null,
}));

vi.mock('../../common/actor-avatar', () => ({
  ActorAvatar: () => null,
}));

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { CreateAgentDialog } from './create-agent-dialog';

const ME = 'user-me';
const OTHER = 'user-other';

const members: MemberWithUser[] = [
  {
    id: 'm-me',
    user_id: ME,
    workspace_id: 'ws-1',
    role: 'member',
    name: 'Me',
    email: 'me@example.com',
    avatar_url: null,
    created_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'm-other',
    user_id: OTHER,
    workspace_id: 'ws-1',
    role: 'member',
    name: 'Other',
    email: 'other@example.com',
    avatar_url: null,
    created_at: '2026-01-01T00:00:00Z',
  },
];

function makeRuntime(overrides: Partial<RuntimeDevice>): RuntimeDevice {
  return {
    id: 'rt',
    workspace_id: 'ws-1',
    daemon_id: null,
    name: 'Test Runtime',
    runtime_mode: 'local',
    provider: 'claude',
    launch_header: '',
    status: 'online',
    device_info: 'host.local',
    metadata: {},
    owner_id: ME,
    visibility: 'private',
    last_seen_at: '2026-04-27T11:59:50Z',
    created_at: '2026-04-01T00:00:00Z',
    updated_at: '2026-04-01T00:00:00Z',
    ...overrides,
  };
}

function makeTemplate(runtimeId: string): Agent {
  return {
    id: 'agent-template',
    workspace_id: 'ws-1',
    runtime_id: runtimeId,
    name: 'Template Agent',
    description: '',
    instructions: '',
    avatar_url: null,
    runtime_mode: 'local',
    runtime_config: {},
    custom_args: [],
    visibility: 'private',
    permission_mode: 'private',
    invocation_targets: [],
    status: 'idle',
    max_concurrent_tasks: 1,
    model: '',
    owner_id: ME,
    skills: [],
    created_at: '2026-04-01T00:00:00Z',
    updated_at: '2026-04-01T00:00:00Z',
    archived_at: null,
    archived_by: null,
  };
}

function renderDialog(runtimes: RuntimeDevice[], template?: Agent) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onCreate = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <WorkspaceSlugProvider slug="test-ws">
          <NavigationProvider value={navigationStub}>
            <CreateAgentDialog
              runtimes={runtimes}
              members={members}
              currentUserId={ME}
              template={template}
              onClose={onClose}
              onCreate={onCreate}
            />
          </NavigationProvider>
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { onCreate, onClose };
}

describe('CreateAgentDialog runtime visibility gate', () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => {
    cleanup();
    document.body.innerHTML = '';
  });

  it("disables another member's private runtime in the picker", () => {
    const mine = makeRuntime({
      id: 'rt-mine',
      name: 'My Runtime',
      owner_id: ME,
      visibility: 'private',
    });
    const othersPrivate = makeRuntime({
      id: 'rt-others-private',
      name: 'Others Private',
      owner_id: OTHER,
      visibility: 'private',
    });
    renderDialog([mine, othersPrivate]);

    fireEvent.click(screen.getByText('All'));
    fireEvent.click(screen.getByText('My Runtime', { selector: 'span.truncate' }));

    const disabledRow = screen.getByText('Others Private').closest('button') as HTMLButtonElement;
    expect(disabledRow).not.toBeNull();
    expect(disabledRow.disabled).toBe(true);
    expect(disabledRow.title).toMatch(/Private runtime/i);
  });

  it("lets a plain member pick another member's public runtime", () => {
    const mine = makeRuntime({
      id: 'rt-mine',
      name: 'My Runtime',
      owner_id: ME,
      visibility: 'private',
    });
    const othersPublic = makeRuntime({
      id: 'rt-others-public',
      name: 'Others Public',
      owner_id: OTHER,
      visibility: 'public',
    });
    renderDialog([mine, othersPublic]);

    fireEvent.click(screen.getByText('All'));
    fireEvent.click(screen.getByText('My Runtime', { selector: 'span.truncate' }));

    const publicRow = screen.getByText('Others Public').closest('button') as HTMLButtonElement;
    expect(publicRow).not.toBeNull();
    expect(publicRow.disabled).toBe(false);
  });

  it('defaults the selected runtime to a usable one, not a locked private', () => {
    const othersPrivate = makeRuntime({
      id: 'rt-others-private',
      name: 'Others Private',
      owner_id: OTHER,
      visibility: 'private',
    });
    const mine = makeRuntime({
      id: 'rt-mine',
      name: 'My Runtime',
      owner_id: ME,
      visibility: 'private',
    });
    renderDialog([othersPrivate, mine]);

    expect(screen.queryByText('Others Private', { selector: 'span.truncate' })).toBeNull();
    expect(screen.getByText('My Runtime', { selector: 'span.truncate' })).toBeInTheDocument();
  });

  it("in duplicate mode, does not pre-fill the template's runtime when it's now locked", async () => {
    const othersPrivate = makeRuntime({
      id: 'rt-others-private',
      name: 'Others Private',
      owner_id: OTHER,
      visibility: 'private',
    });
    const mine = makeRuntime({
      id: 'rt-mine',
      name: 'My Runtime',
      owner_id: ME,
      visibility: 'private',
    });
    const template = makeTemplate('rt-others-private');
    const { onCreate } = renderDialog([othersPrivate, mine], template);

    expect(screen.getByText('My Runtime', { selector: 'span.truncate' })).toBeInTheDocument();
    expect(screen.queryByText('Others Private', { selector: 'span.truncate' })).toBeNull();

    fireEvent.click(screen.getByText('Create'));
    await new Promise((r) => setTimeout(r, 0));
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCreate.mock.calls[0]?.[0].runtime_id).toBe('rt-mine');
  });

  it('disables Create when the selected runtime is locked (template + no usable fallback)', () => {
    const onlyOthersPrivate = makeRuntime({
      id: 'rt-only-others-private',
      name: 'Only Others Private',
      owner_id: OTHER,
      visibility: 'private',
    });
    const template = makeTemplate('rt-only-others-private');
    renderDialog([onlyOthersPrivate], template);

    const createBtn = screen.getAllByRole('button').find((b) => b.textContent === 'Create');
    expect(createBtn).toBeDefined();
    expect((createBtn as HTMLButtonElement).disabled).toBe(true);
  });
});

describe('CreateAgentDialog access picker (MUL-4010, feature-flag gated)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    configStore.getState().setFeatureFlags({});
  });

  afterEach(() => {
    cleanup();
    document.body.innerHTML = '';
    configStore.getState().setFeatureFlags({});
  });

  it('keeps the legacy Workspace/Personal toggle when the flag is OFF', async () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: false });
    const mine = makeRuntime({ id: 'rt-mine', name: 'My Runtime', owner_id: ME });
    const { onCreate } = renderDialog([mine]);

    expect(screen.getByText(/All members can assign/i)).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText('e.g. Deep Research Agent'), {
      target: { value: 'Legacy Agent' },
    });
    fireEvent.click(screen.getByText('Create'));
    await new Promise((r) => setTimeout(r, 0));

    const payload = onCreate.mock.calls[0]?.[0];
    expect(payload).toBeDefined();
    expect(payload.visibility).toBe('workspace');
    expect(payload.permission_mode).toBeUndefined();
    expect(payload.invocation_targets).toBeUndefined();
  });

  it('submits permission_mode=public_to + workspace target when the flag is ON (default)', async () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
    const mine = makeRuntime({ id: 'rt-mine', name: 'My Runtime', owner_id: ME });
    const { onCreate } = renderDialog([mine]);

    expect(screen.getByText('Only you can run this agent')).toBeInTheDocument();
    expect(screen.getByText('Entire workspace')).toBeInTheDocument();
    expect(screen.getByText('Specific people')).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText('e.g. Deep Research Agent'), {
      target: { value: 'Access Agent' },
    });
    fireEvent.click(screen.getByText('Create'));
    await new Promise((r) => setTimeout(r, 0));

    const payload = onCreate.mock.calls[0]?.[0];
    expect(payload).toBeDefined();
    expect(payload.visibility).toBeUndefined();
    expect(payload.permission_mode).toBe('public_to');
    expect(payload.invocation_targets).toEqual([{ target_type: 'workspace' }]);
  });

  it('submits permission_mode=private with empty targets when Private is chosen', async () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
    const mine = makeRuntime({ id: 'rt-mine', name: 'My Runtime', owner_id: ME });
    const { onCreate } = renderDialog([mine]);

    fireEvent.change(screen.getByPlaceholderText('e.g. Deep Research Agent'), {
      target: { value: 'Private Agent' },
    });
    fireEvent.click(screen.getByText('Only you can run this agent'));
    fireEvent.click(screen.getByText('Create'));
    await new Promise((r) => setTimeout(r, 0));

    const payload = onCreate.mock.calls[0]?.[0];
    expect(payload).toBeDefined();
    expect(payload.permission_mode).toBe('private');
    expect(payload.invocation_targets).toEqual([]);
  });

  it('does not create when specific-people scope has no member', async () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
    const mine = makeRuntime({ id: 'rt-mine', name: 'My Runtime', owner_id: ME });
    const { onCreate } = renderDialog([mine]);

    fireEvent.change(screen.getByPlaceholderText('e.g. Deep Research Agent'), {
      target: { value: 'Empty Public Agent' },
    });
    fireEvent.click(screen.getByRole('radio', { name: /^Specific people/i }));
    fireEvent.click(screen.getByText('Create'));
    await new Promise((r) => setTimeout(r, 0));

    expect(onCreate).not.toHaveBeenCalled();
  });

  it('includes ticked members in the invocation_targets payload', async () => {
    configStore.getState().setFeatureFlags({ [COMPOSIO_MCP_APPS_FLAG]: true });
    const mine = makeRuntime({ id: 'rt-mine', name: 'My Runtime', owner_id: ME });
    const { onCreate } = renderDialog([mine]);

    fireEvent.change(screen.getByPlaceholderText('e.g. Deep Research Agent'), {
      target: { value: 'Shared Agent' },
    });
    fireEvent.click(screen.getByRole('radio', { name: /^Specific people/i }));
    fireEvent.click(screen.getByRole('checkbox', { name: /^Other/ }));
    fireEvent.click(screen.getByText('Create'));
    await new Promise((r) => setTimeout(r, 0));

    const payload = onCreate.mock.calls[0]?.[0];
    expect(payload).toBeDefined();
    expect(payload.permission_mode).toBe('public_to');
    expect(payload.invocation_targets).toEqual([{ target_type: 'member', target_id: OTHER }]);
  });
});

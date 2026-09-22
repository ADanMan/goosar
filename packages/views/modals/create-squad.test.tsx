// @vitest-environment jsdom

import type { ReactNode } from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { Agent, MemberWithUser, Squad } from '@goosar/core/types';
import enCommon from '../locales/en/common.json';
import enModals from '../locales/en/modals.json';
import enAgents from '../locales/en/agents.json';
import enIssues from '../locales/en/issues.json';

const TEST_RESOURCES = {
  en: { common: enCommon, modals: enModals, agents: enAgents, issues: enIssues },
};

const ME = 'user-me';
const OTHER = 'user-other';

const mocks = vi.hoisted(() => ({
  agents: [] as Agent[],
  members: [] as MemberWithUser[],
  createSquad: vi.fn(),
  addSquadMember: vi.fn(),
  navigationPush: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  toastWarning: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey?: unknown[] }) => {
    const key = opts.queryKey ?? [];
    if (Array.isArray(key) && key.includes('agents')) {
      return { data: mocks.agents };
    }
    if (Array.isArray(key) && key.includes('members')) {
      return { data: mocks.members };
    }
    return { data: [] };
  },
  useQueryClient: () => ({ invalidateQueries: mocks.invalidate }),
}));

vi.mock('@goosar/core/workspace/queries', () => ({
  agentListOptions: () => ({ queryKey: ['agents'] }),
  memberListOptions: () => ({ queryKey: ['members'] }),
  workspaceKeys: { squads: (id: string) => ['squads', id] },
}));

vi.mock('@goosar/core/api', () => ({
  api: {
    createSquad: (...args: unknown[]) => mocks.createSquad(...args),
    addSquadMember: (...args: unknown[]) => mocks.addSquadMember(...args),
  },
}));

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (s: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: ME } }),
}));

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/paths', () => ({
  useWorkspacePaths: () => ({
    squadDetail: (id: string) => `/test-ws/squads/${id}`,
  }),
}));

vi.mock('@goosar/core/utils', () => ({
  isImeComposing: () => false,
}));

vi.mock('../navigation', () => ({
  useNavigation: () => ({ push: mocks.navigationPush }),
}));

vi.mock('../common/actor-avatar', () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid={`avatar-${actorId}`} />,
}));

vi.mock('../common/avatar-upload-control', () => ({
  AvatarUploadControl: ({
    value,
    onUploaded,
  }: {
    value: string | null;
    onUploaded: (v: string) => void;
  }) => (
    <button
      type="button"
      data-testid="avatar-picker"
      data-value={value ?? ''}
      onClick={() => onUploaded('https://example.com/avatar.png')}
    >
      avatar
    </button>
  ),
}));

vi.mock('../agents/components/char-counter', () => ({
  CharCounter: ({ length, max }: { length: number; max: number }) => (
    <span data-testid="char-counter">
      {length}/{max}
    </span>
  ),
}));

vi.mock('@goosar/ui/components/ui/popover', () => ({
  Popover: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PopoverTrigger: ({
    children,
    className,
    onClick,
    render,
  }: {
    children?: ReactNode;
    className?: string;
    onClick?: () => void;
    render?: ReactNode;
  }) => {
    if (render !== undefined) return <>{render}</>;
    return (
      <button type="button" className={className} onClick={onClick}>
        {children}
      </button>
    );
  },
  PopoverContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock('@goosar/ui/components/ui/dialog', () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
}));

vi.mock('@goosar/ui/components/ui/button', () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = 'button',
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: 'button' | 'submit' | 'reset';
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock('@goosar/ui/components/ui/input', () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));

vi.mock('@goosar/ui/components/ui/label', () => ({
  Label: ({ children, className }: { children: ReactNode; className?: string }) => (
    <label className={className}>{children}</label>
  ),
}));

vi.mock('../editor/extensions/pinyin-match', () => ({
  matchesPinyin: () => false,
}));

vi.mock('sonner', () => ({
  toast: {
    success: (...args: unknown[]) => mocks.toastSuccess(...args),
    error: (...args: unknown[]) => mocks.toastError(...args),
    warning: (...args: unknown[]) => mocks.toastWarning(...args),
  },
}));

import { CreateSquadModal } from './create-squad';

function makeAgent(
  overrides: Partial<Agent> & { id: string; name: string; owner_id: string | null },
): Agent {
  return {
    workspace_id: 'ws-1',
    runtime_id: 'rt-1',
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
    skills: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeMember(user_id: string, name: string): MemberWithUser {
  return {
    id: `m-${user_id}`,
    user_id,
    workspace_id: 'ws-1',
    role: 'member',
    name,
    email: `${user_id}@example.com`,
    avatar_url: null,
    created_at: '2026-01-01T00:00:00Z',
  };
}

function makeSquad(overrides: Partial<Squad> = {}): Squad {
  return {
    id: 'sq-new',
    workspace_id: 'ws-1',
    name: 'New Squad',
    description: '',
    instructions: '',
    avatar_url: null,
    leader_id: 'agent-mine-1',
    creator_id: ME,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function getSubmitButton(): HTMLButtonElement {
  const btn = screen.getAllByRole('button').find((b) => b.textContent === 'Create Squad');
  if (!btn) throw new Error('Create Squad submit button not found');
  return btn as HTMLButtonElement;
}

function firstMatch(label: string): HTMLElement {
  const matches = screen.getAllByText(label);
  if (matches.length === 0) throw new Error(`no match for "${label}"`);
  return matches[0]!;
}

function lastMatch(label: string): HTMLElement {
  const matches = screen.getAllByText(label);
  if (matches.length === 0) throw new Error(`no match for "${label}"`);
  return matches[matches.length - 1]!;
}

function renderModal() {
  const onClose = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CreateSquadModal onClose={onClose} />
    </I18nProvider>,
  );
  return { onClose };
}

const myAgent = makeAgent({ id: 'agent-mine-1', name: 'MineAgentOne', owner_id: ME });
const myAgent2 = makeAgent({ id: 'agent-mine-2', name: 'MineAgentTwo', owner_id: ME });
const otherAgent = makeAgent({ id: 'agent-other-1', name: 'OtherAgentOne', owner_id: OTHER });
const wsMember = makeMember(OTHER, 'Workspace Pal');

describe('CreateSquadModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.agents = [otherAgent, myAgent, myAgent2];
    mocks.members = [wsMember];
  });

  it('binds the name and description inputs in the identity row', () => {
    renderModal();
    const name = screen.getByPlaceholderText(/e\.g\. Frontend Team/i) as HTMLInputElement;
    fireEvent.change(name, { target: { value: 'Platform Team' } });
    expect(name.value).toBe('Platform Team');

    const desc = screen.getByPlaceholderText(/Describe what this squad/i) as HTMLInputElement;
    fireEvent.change(desc, { target: { value: 'We own infra' } });
    expect(desc.value).toBe('We own infra');
    expect(screen.getByTestId('char-counter').textContent).toMatch(/^12\//);
  });

  it("renders the leader picker with 'My Agents' before 'Workspace Agents'", () => {
    renderModal();
    const myGroupLabels = screen.getAllByText('My Agents');
    const wsGroupLabels = screen.getAllByText('Workspace Agents');
    expect(myGroupLabels.length).toBeGreaterThanOrEqual(1);
    expect(wsGroupLabels.length).toBeGreaterThanOrEqual(1);

    const all = Array.from(document.querySelectorAll('*'));
    const myIdx = all.findIndex((n) => n.textContent === 'My Agents');
    const wsIdx = all.findIndex((n) => n.textContent === 'Workspace Agents');
    expect(myIdx).toBeLessThan(wsIdx);
  });

  it('auto-clears an additional-members entry when the same agent is picked as leader', async () => {
    renderModal();
    fireEvent.click(lastMatch('MineAgentTwo'));

    await waitFor(() => {
      expect(screen.getAllByText('MineAgentTwo').length).toBeGreaterThanOrEqual(2);
    });

    fireEvent.click(firstMatch('MineAgentTwo'));

    mocks.createSquad.mockResolvedValue(makeSquad({ leader_id: 'agent-mine-2' }));
    fireEvent.change(screen.getByPlaceholderText(/e\.g\. Frontend Team/i), {
      target: { value: 'Platform' },
    });
    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.createSquad).toHaveBeenCalledTimes(1);
    });
    expect(mocks.addSquadMember).not.toHaveBeenCalled();
  });

  it('removes a member promoted to leader from selectedMembers so switching leader away does not resurrect it', async () => {
    renderModal();
    fireEvent.click(lastMatch('MineAgentTwo'));
    await waitFor(() => {
      expect(screen.getAllByText('MineAgentTwo').length).toBeGreaterThanOrEqual(2);
    });

    fireEvent.click(firstMatch('MineAgentTwo'));

    fireEvent.click(firstMatch('MineAgentOne'));

    mocks.createSquad.mockResolvedValue(makeSquad({ id: 'sq-3', leader_id: 'agent-mine-1' }));
    fireEvent.change(screen.getByPlaceholderText(/e\.g\. Frontend Team/i), {
      target: { value: 'Swap Squad' },
    });
    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.createSquad).toHaveBeenCalledWith({
        name: 'Swap Squad',
        description: undefined,
        leader_id: 'agent-mine-1',
        avatar_url: undefined,
      });
    });
    expect(mocks.addSquadMember).not.toHaveBeenCalled();
  });

  it('on success with no additional members fires exactly one success toast and navigates', async () => {
    renderModal();
    fireEvent.change(screen.getByPlaceholderText(/e\.g\. Frontend Team/i), {
      target: { value: 'Solo Squad' },
    });
    fireEvent.click(firstMatch('MineAgentOne'));

    mocks.createSquad.mockResolvedValue(makeSquad({ id: 'sq-1', leader_id: 'agent-mine-1' }));

    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.createSquad).toHaveBeenCalledWith({
        name: 'Solo Squad',
        description: undefined,
        leader_id: 'agent-mine-1',
        avatar_url: undefined,
      });
    });
    await waitFor(() => {
      expect(mocks.toastSuccess).toHaveBeenCalledTimes(1);
    });
    expect(mocks.addSquadMember).not.toHaveBeenCalled();
    expect(mocks.toastWarning).not.toHaveBeenCalled();
    expect(mocks.navigationPush).toHaveBeenCalledWith('/test-ws/squads/sq-1');
  });

  it('on success with partial member failure shows success + warning toasts and still navigates', async () => {
    renderModal();
    fireEvent.change(screen.getByPlaceholderText(/e\.g\. Frontend Team/i), {
      target: { value: 'Mixed Squad' },
    });
    fireEvent.click(firstMatch('MineAgentOne'));

    fireEvent.click(lastMatch('OtherAgentOne'));
    fireEvent.click(lastMatch('Workspace Pal'));

    mocks.createSquad.mockResolvedValue(makeSquad({ id: 'sq-2', leader_id: 'agent-mine-1' }));
    mocks.addSquadMember
      .mockResolvedValueOnce({}) 
      .mockRejectedValueOnce(new Error('boom')); 

    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.createSquad).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(mocks.addSquadMember).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(mocks.toastSuccess).toHaveBeenCalledTimes(1);
    });
    expect(mocks.toastWarning).toHaveBeenCalledTimes(1);
    expect(mocks.navigationPush).toHaveBeenCalledWith('/test-ws/squads/sq-2');
  });

  it('on createSquad failure shows an error toast, does not navigate, and re-enables submit', async () => {
    renderModal();
    fireEvent.change(screen.getByPlaceholderText(/e\.g\. Frontend Team/i), {
      target: { value: 'Boom Squad' },
    });
    fireEvent.click(firstMatch('MineAgentOne'));

    mocks.createSquad.mockRejectedValueOnce(new Error('server down'));

    fireEvent.click(getSubmitButton());

    await waitFor(() => {
      expect(mocks.toastError).toHaveBeenCalledTimes(1);
    });
    expect(mocks.navigationPush).not.toHaveBeenCalled();
    const button = getSubmitButton();
    expect(button.disabled).toBe(false);
  });
});

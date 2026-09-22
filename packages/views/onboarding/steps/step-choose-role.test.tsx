// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enOnboarding from '../../locales/en/onboarding.json';
import enWorkspace from '../../locales/en/workspace.json';

type JoinTarget = {
  id: string;
  slug: string;
  name: string;
  description: string;
  member_count: number;
};

const state = vi.hoisted(() => ({
  targets: [] as JoinTarget[],
  isLoading: false,
  workspaceCreationDisabled: false,
  workspaces: [] as { id: string; slug: string }[],
}));
const joinMutate = vi.hoisted(() => vi.fn());

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({ data: state.targets, isLoading: state.isLoading }),
  useQueryClient: () => ({ fetchQuery: async () => state.workspaces }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/workspace/queries', () => ({
  joinTargetListOptions: () => ({ queryKey: ['join-targets'] }),
  workspaceListOptions: () => ({ queryKey: ['workspaces'] }),
}));

vi.mock('@goosar/core/workspace/mutations', () => ({
  useJoinWorkspace: () => ({ mutate: joinMutate }),
}));

vi.mock('@goosar/core/config', () => ({
  useConfigStore: (selector: (s: typeof state) => unknown) => selector(state),
}));

vi.mock('@goosar/views/platform', () => ({
  DragStrip: () => <div data-testid="drag-strip" />,
}));

import { StepChooseRole } from './step-choose-role';

function renderStep(props: Partial<Parameters<typeof StepChooseRole>[0]> = {}) {
  const onJoined = props.onJoined ?? vi.fn();
  const view = render(
    <I18nProvider
      locale="en"
      resources={{
        en: { common: enCommon, onboarding: enOnboarding, workspace: enWorkspace },
      }}
    >
      <StepChooseRole {...props} onJoined={onJoined} />
    </I18nProvider>,
  );
  return { ...view, onJoined };
}

describe('StepChooseRole (R-16d)', () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();
    state.targets = [
      {
        id: 'ws-1',
        slug: 'legal',
        name: 'Legal',
        description: 'Contracts',
        member_count: 3,
      },
    ];
    state.isLoading = false;
    state.workspaceCreationDisabled = false;
    state.workspaces = [{ id: 'ws-1', slug: 'legal' }];
  });

  it('hands the joined workspace back to the flow', async () => {
    joinMutate.mockImplementation(
      (_id: string, opts: { onSuccess: (r: unknown) => Promise<void> }) =>
        opts.onSuccess({ id: 'ws-1', slug: 'legal' }),
    );

    const { onJoined } = renderStep();
    await userEvent.click(screen.getByRole('button', { name: enWorkspace.join_page.join }));

    await waitFor(() => expect(onJoined).toHaveBeenCalledWith({ id: 'ws-1', slug: 'legal' }));
  });

  it('keeps the window draggable', () => {
    renderStep();
    expect(screen.getByTestId('drag-strip')).toBeInTheDocument();
  });

  it('hides «create one instead» where the deployment forbids creation', () => {
    state.workspaceCreationDisabled = true;
    renderStep({ onCreateInstead: vi.fn() });
    expect(screen.queryByRole('button', { name: enWorkspace.join_page.create_instead })).toBeNull();
  });

  it('offers «create one instead» where creation is allowed', () => {
    renderStep({ onCreateInstead: vi.fn() });
    expect(
      screen.getByRole('button', { name: enWorkspace.join_page.create_instead }),
    ).toBeInTheDocument();
  });

  it('says so when there is no role to join', () => {
    state.targets = [];
    renderStep();
    expect(screen.getByText(enOnboarding.step_choose_role.empty)).toBeInTheDocument();
  });

  it('says nothing about an empty list while the list is still loading', () => {
    state.targets = [];
    state.isLoading = true;
    renderStep();
    expect(screen.queryByText(enOnboarding.step_choose_role.empty)).toBeNull();
  });
});

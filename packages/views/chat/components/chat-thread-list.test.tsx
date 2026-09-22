import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { Agent, ChatSession } from '@goosar/core/types';
import enChat from '../../locales/en/chat.json';
import enIssues from '../../locales/en/issues.json';

const setActiveSession = vi.fn();
const archiveMutate = vi.fn();

vi.mock('../../common/actor-avatar', () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid={`avatar-${actorId}`} />,
}));

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/agents', () => ({
  useWorkspacePresenceMap: () => ({ byAgent: new Map() }),
}));

vi.mock('@goosar/core/api', () => ({
  api: { cancelTaskById: vi.fn() },
}));

vi.mock('@goosar/core/chat', () => ({
  useChatStore: (selector: (s: { setActiveSession: typeof setActiveSession }) => unknown) =>
    selector({ setActiveSession }),
}));

vi.mock('@goosar/core/chat/mutations', () => ({
  useDeleteChatSession: () => ({ mutate: vi.fn(), isPending: false }),
  useSetChatSessionPinned: () => ({ mutate: vi.fn(), isPending: false }),
  useSetChatSessionArchived: () => ({ mutate: archiveMutate, isPending: false }),
}));

vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>();
  return {
    ...actual,
    useQuery: () => ({ data: { tasks: [] } }),
    useQueryClient: () => ({ setQueryData: vi.fn(), invalidateQueries: vi.fn() }),
  };
});

import { ChatThreadList } from './chat-thread-list';

const TEST_RESOURCES = { en: { chat: enChat, issues: enIssues } };

function makeSession(overrides: Partial<ChatSession> & Pick<ChatSession, 'id'>): ChatSession {
  return {
    workspace_id: 'ws-1',
    agent_id: 'agent-1',
    creator_id: 'user-1',
    title: `Chat ${overrides.id}`,
    status: 'active',
    has_unread: false,
    unread_count: 0,
    last_message: null,
    pinned: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    ...overrides,
  };
}

const agent = { id: 'agent-1', name: 'Alpha' } as unknown as Agent;

const sessions: ChatSession[] = [
  makeSession({ id: 's1', updated_at: '2026-07-08T03:00:00Z' }),
  makeSession({ id: 's2', updated_at: '2026-07-08T02:00:00Z' }),
  makeSession({ id: 's3', updated_at: '2026-07-08T01:00:00Z' }),
];

function renderList(activeSessionId: string | null, onArchive = vi.fn()) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatThreadList
        sessions={sessions}
        agents={[agent]}
        activeSessionId={activeSessionId}
        onSelectSession={vi.fn()}
        onArchive={onArchive}
      />
    </I18nProvider>,
  );
  return onArchive;
}

const ARCHIVE_LABEL = enChat.list.archive;

describe('ChatThreadList archive delegation', () => {
  beforeEach(() => {
    setActiveSession.mockClear();
    archiveMutate.mockClear();
  });

  it('delegates the history-row Archive action to onArchive with that session', () => {
    const onArchive = renderList('s2');
    const archiveButtons = screen.getAllByRole('button', { name: ARCHIVE_LABEL });
    fireEvent.click(archiveButtons[1]!);

    expect(onArchive).toHaveBeenCalledTimes(1);
    expect(onArchive.mock.calls[0]![0]).toMatchObject({ id: 's2' });
  });

  it("passes the archived row's session even when it isn't the open one", () => {
    const onArchive = renderList('s1');
    const archiveButtons = screen.getAllByRole('button', { name: ARCHIVE_LABEL });
    fireEvent.click(archiveButtons[2]!);

    expect(onArchive).toHaveBeenCalledTimes(1);
    expect(onArchive.mock.calls[0]![0]).toMatchObject({ id: 's3' });
  });

  it('does not flip archive status or move selection itself', () => {
    renderList('s2');
    const archiveButtons = screen.getAllByRole('button', { name: ARCHIVE_LABEL });
    fireEvent.click(archiveButtons[1]!);

    expect(setActiveSession).not.toHaveBeenCalled();
    expect(archiveMutate).not.toHaveBeenCalled();
  });
});

describe('ChatThreadList no_response preview (MUL-4351)', () => {
  it("shows a localized 'no text reply' preview instead of the fallback body", () => {
    const session = makeSession({
      id: 'nr1',
      last_message: {
        content: 'The agent finished this turn without a text reply.',
        role: 'assistant',
        created_at: '2026-07-08T03:00:00Z',
        message_kind: 'no_response',
      },
    });
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatThreadList
          sessions={[session]}
          agents={[agent]}
          activeSessionId={null}
          onSelectSession={vi.fn()}
          onArchive={vi.fn()}
        />
      </I18nProvider>,
    );
    expect(screen.getByText(enChat.list.no_response_preview)).toBeInTheDocument();
    expect(
      screen.queryByText('The agent finished this turn without a text reply.'),
    ).not.toBeInTheDocument();
  });
});

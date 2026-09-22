// @vitest-environment jsdom

import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { Agent, WorkspaceSampleTask } from '@goosar/core/types';
import enChat from '../../locales/en/chat.json';
import ruChat from '../../locales/ru/chat.json';
import { HELPER_STARTER_PROMPTS } from '../../onboarding/templates';

const mocks = vi.hoisted(() => ({ getWorkspaceCapabilities: vi.fn() }));

vi.mock('@goosar/core/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@goosar/core/api')>()),
  api: { getWorkspaceCapabilities: mocks.getWorkspaceCapabilities },
}));

vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-1' }));

import { ChatStarterCards, resolveStarterCards, useChatStarterCards } from './chat-starter-cards';

const TEST_RESOURCES = { en: { chat: enChat }, ru: { chat: ruChat } };

const task = (over: Partial<WorkspaceSampleTask> = {}): WorkspaceSampleTask => ({
  key: 'mail-digest',
  title: "Work through today's mail",
  prompt: "Go through today's mail. Answer in a comment on this issue.",
  requires: [],
  ...over,
});

describe('resolveStarterCards', () => {
  it('takes its openers from the role template, in template order', () => {
    const cards = resolveStarterCards({
      sampleTasks: [
        task({ key: 'a', title: 'Which clients are due a message' }),
        task({ key: 'b', title: 'A brief on a company before a meeting' }),
      ],
      lang: 'en',
    });

    expect(cards.map((c) => c.key)).toEqual(['a', 'b']);
    expect(cards[0]!.label).toBe('Which clients are due a message');
  });

  it("sends the task's title, not its issue-shaped prompt", () => {
    const cards = resolveStarterCards({ sampleTasks: [task()], lang: 'en' });
    expect(cards[0]!.message).toBe("Work through today's mail");
  });

  it('caps the set so the empty state stays an empty state', () => {
    const many = Array.from({ length: 6 }, (_, i) => task({ key: `k${i}`, title: `Task ${i}` }));
    expect(resolveStarterCards({ sampleTasks: many, lang: 'en' })).toHaveLength(3);
  });

  it('skips a template row with no title instead of drawing a blank card', () => {
    const cards = resolveStarterCards({
      sampleTasks: [task({ key: 'blank', title: '  ' }), task({ key: 'ok' })],
      lang: 'en',
    });
    expect(cards.map((c) => c.key)).toEqual(['ok']);
  });

  it('keeps cards distinguishable when the template leaves keys blank', () => {
    const cards = resolveStarterCards({
      sampleTasks: [task({ key: '', title: 'One' }), task({ key: '', title: 'Two' })],
      lang: 'en',
    });
    expect(new Set(cards.map((c) => c.key)).size).toBe(2);
  });

  it('falls back to the onboarding openers for a workspace with no role', () => {
    const cards = resolveStarterCards({ sampleTasks: [], lang: 'ru' });

    expect(cards.map((c) => c.key)).toEqual(['intro', 'tour']);
    expect(cards[0]!.label).toBe(HELPER_STARTER_PROMPTS.intro.title.ru);
    expect(cards[0]!.message).toBe(HELPER_STARTER_PROMPTS.intro.prompt.ru);
  });

  it('falls back the same way while the role is still loading', () => {
    expect(resolveStarterCards({ sampleTasks: undefined, lang: 'en' })).toHaveLength(2);
  });
});

function renderCards(
  cards: ReturnType<typeof resolveStarterCards>,
  onPick = vi.fn(),
  locale: 'en' | 'ru' = 'en',
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nProvider locale={locale} resources={TEST_RESOURCES}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </I18nProvider>
  );
  render(wrapper({ children: <ChatStarterCards cards={cards} onPick={onPick} /> }));
  return { onPick };
}

describe('ChatStarterCards', () => {
  it("sends the card's message as the first user message on click", async () => {
    const cards = resolveStarterCards({ sampleTasks: [task()], lang: 'en' });
    const { onPick } = renderCards(cards);

    await userEvent.click(screen.getByRole('button', { name: /work through today's mail/i }));

    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick).toHaveBeenCalledWith("Work through today's mail");
  });

  it('locks the set while a pick is in flight, so one click is one chat', async () => {
    let releaseSend: () => void = () => {};
    const onPick = vi.fn(() => new Promise<void>((resolve) => (releaseSend = resolve)));
    const cards = resolveStarterCards({
      sampleTasks: [task({ key: 'a', title: 'One' }), task({ key: 'b', title: 'Two' })],
      lang: 'en',
    });
    renderCards(cards, onPick);

    await userEvent.click(screen.getByRole('button', { name: 'One' }));
    await userEvent.click(screen.getByRole('button', { name: 'Two' }));
    expect(onPick).toHaveBeenCalledTimes(1);

    releaseSend();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Two' })).toBeEnabled());
  });

  it('renders nothing at all when there is nothing to offer', () => {
    const { container } = render(<ChatStarterCards cards={[]} onPick={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });
});

const HELPER = {
  id: 'agent_helper',
  name: 'Goosar Helper',
  system_key: 'goosar_helper',
  visibility: 'workspace',
  archived_at: null,
  owner_id: null,
} as unknown as Agent;

function renderProbe(agent: Agent | null) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Probe() {
    const cards = useChatStarterCards(agent);
    return <span data-testid="labels">{cards.map((c) => c.label).join('|')}</span>;
  }
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={client}>
        <Probe />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe('useChatStarterCards', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getWorkspaceCapabilities.mockResolvedValue({
      role_title: 'Finance',
      role_summary: '',
      capabilities: [],
      sample_tasks: [task({ key: 'close', title: 'Check that the month closes' })],
    });
  });

  it("offers the role's openers when the conversation is with the Helper", async () => {
    renderProbe(HELPER);
    await waitFor(() =>
      expect(screen.getByTestId('labels')).toHaveTextContent('Check that the month closes'),
    );
  });

  it('offers nothing while the role is still loading', async () => {
    let resolveRole: (value: unknown) => void = () => {};
    mocks.getWorkspaceCapabilities.mockReturnValue(
      new Promise((resolve) => (resolveRole = resolve)),
    );
    renderProbe(HELPER);

    expect(screen.getByTestId('labels')).toBeEmptyDOMElement();

    resolveRole({
      role_title: 'Finance',
      role_summary: '',
      capabilities: [],
      sample_tasks: [task({ key: 'close', title: 'Check that the month closes' })],
    });
    await waitFor(() =>
      expect(screen.getByTestId('labels')).toHaveTextContent('Check that the month closes'),
    );
  });

  it('offers nothing to a hand-made agent, and asks the server nothing', async () => {
    const coder = { ...HELPER, system_key: '', name: 'Refactor bot' } as Agent;
    renderProbe(coder);

    await waitFor(() => expect(screen.getByTestId('labels')).toBeEmptyDOMElement());
    expect(mocks.getWorkspaceCapabilities).not.toHaveBeenCalled();
  });

  it('offers nothing before an agent has resolved', () => {
    renderProbe(null);
    expect(screen.getByTestId('labels')).toBeEmptyDOMElement();
  });
});

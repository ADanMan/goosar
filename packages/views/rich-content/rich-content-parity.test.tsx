/**
 * Паритет RichContent между пятью поверхностями.
 *
 * Один завершённый markdown-фикстур должен давать ОДИНАКОВЫЕ семантические
 * блоки в чате (сообщение пользователя, живой и сохранённый ответ
 * ассистента), в описании issue и в комментарии. Плотность/CSS могут
 * отличаться, возможности — нет. Mermaid-блок должен быть диаграммой везде,
 * а не блоком кода в части случаев.
 *
 * Поверхности проверяются через их настоящие точки входа — ReadonlyContent
 * для issue/комментария и ChatMessageList для трёх строк чата, — поэтому
 * регрессия, вернувшая рендерер только для чата, будет здесь поймана.
 */

import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';

const { resolveIssueIdentifierMock, mermaidRenderMock } = vi.hoisted(() => ({
  resolveIssueIdentifierMock: vi.fn(),
  mermaidRenderMock: vi.fn(),
}));

vi.mock('../issues/hooks', () => ({
  useResolveIssueIdentifier: (identifier: string) => resolveIssueIdentifierMock(identifier),
}));

vi.mock('../i18n', async () => {
  const editor = (await import('../locales/en/editor.json')).default;
  const chat = (await import('../locales/en/chat.json')).default;
  return {
    useT: (ns?: string) => ({
      t: (select: (bundle: Record<string, unknown>) => string) =>
        select((ns === 'chat' ? chat : editor) as Record<string, unknown>),
    }),
    useTimeAgo: () => 'just now',
  };
});

vi.mock('@goosar/core/api', () => ({
  api: { getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock('@goosar/core/paths', () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
    projectDetail: (id: string) => `/test/projects/${id}`,
  }),
  useWorkspaceSlug: () => 'test',
}));

vi.mock('../navigation', () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
  useAppOrigin: () => null,
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock('../issues/components/issue-mention-card', () => ({
  IssueMentionCard: ({ issueId, fallbackLabel }: { issueId: string; fallbackLabel?: string }) => (
    <span data-testid="issue-mention">{fallbackLabel ?? issueId}</span>
  ),
}));

vi.mock('../projects/components/project-chip', () => ({
  ProjectChip: ({ projectId }: { projectId: string }) => (
    <span data-testid="project-chip">{projectId}</span>
  ),
}));

vi.mock('../editor/link-hover-card', () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    render: mermaidRenderMock,
  },
}));

vi.mock('react-virtuoso', () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
    components,
    context,
  }: {
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
    components?: { Footer?: (p: { context?: unknown }) => ReactElement | null };
    context?: unknown;
  }) => {
    const Footer = components?.Footer;
    return (
      <div>
        {data.map((item, i) => (
          <div key={computeItemKey(i, item)} data-row-key={computeItemKey(i, item)}>
            {itemContent(i, item)}
          </div>
        ))}
        {Footer ? <Footer context={context} /> : null}
      </div>
    );
  },
}));

Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
  value: () => ({
    fillStyle: '#000',
    fillRect: vi.fn(),
    getImageData: () => ({ data: new Uint8ClampedArray([12, 34, 56, 255]) }),
  }),
});

import { ReadonlyContent } from '../editor/readonly-content';
import { ChatMessageList } from '../chat/components/chat-message-list';
import { taskMessagesOptions } from '@goosar/core/chat/queries';

const MERMAID_FIXTURE = [
  '```mermaid',
  'flowchart LR',
  '    HTML["HTML"] --> WEB["网页"]',
  '    CSS["CSS"] --> WEB',
  '```',
].join('\n');

const TASK_ID = '11111111-1111-4111-8111-111111111111';

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
}

function withClient(ui: ReactElement, client: QueryClient) {
  return <QueryClientProvider client={client}>{ui}</QueryClientProvider>;
}

function assistantMessage(content: string) {
  return {
    id: 'msg-assistant-1',
    role: 'assistant' as const,
    content,
    task_id: TASK_ID,
    attachments: [],
    elapsed_ms: 1200,
  };
}

function userMessage(content: string) {
  return {
    id: 'msg-user-1',
    role: 'user' as const,
    content,
    task_id: null,
    attachments: [],
  };
}

function seedTimeline(client: QueryClient, text: string) {
  client.setQueryData(taskMessagesOptions(TASK_ID).queryKey, [
    {
      task_id: TASK_ID,
      issue_id: null,
      seq: 1,
      type: 'text',
      content: text,
    },
  ] as never);
}

beforeEach(() => {
  vi.clearAllMocks();
  mermaidRenderMock.mockResolvedValue({
    svg: '<svg viewBox="0 0 123 45"><g><text>diagram</text></g></svg>',
  });
  resolveIssueIdentifierMock.mockReturnValue(null);
});

const mermaidLeaf = (container: HTMLElement): Element | null =>
  container.querySelector('.mermaid-diagram');

async function expectMermaidRendered(container: HTMLElement) {
  await waitFor(() => {
    expect(mermaidLeaf(container)).not.toBeNull();
  });
  await waitFor(() => {
    expect(mermaidLeaf(container)?.querySelector('svg')).not.toBeNull();
  });
  expect(container.querySelector('code.hljs')).toBeNull();
}

describe('Mermaid parity across the five surfaces', () => {
  it('renders a diagram in an Issue description', async () => {
    const { container } = render(<ReadonlyContent content={MERMAID_FIXTURE} />);
    await expectMermaidRendered(container);
  });

  it('renders a diagram in a Comment', async () => {
    const { container } = render(<ReadonlyContent content={MERMAID_FIXTURE} attachments={[]} />);
    await expectMermaidRendered(container);
  });

  it('renders a diagram in a Chat user message', async () => {
    const client = makeClient();
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[userMessage(MERMAID_FIXTURE)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );
    await expectMermaidRendered(container);
  });

  it('renders a diagram in a persisted Chat assistant message', async () => {
    const client = makeClient();
    seedTimeline(client, MERMAID_FIXTURE);
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[assistantMessage(MERMAID_FIXTURE)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );
    await expectMermaidRendered(container);
  });

  it('renders a diagram in a live (streaming) Chat assistant row', async () => {
    const client = makeClient();
    seedTimeline(client, MERMAID_FIXTURE);
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: 'running' } as never}
          availability={undefined}
        />,
        client,
      ),
    );
    await expectMermaidRendered(container);
  });
});

describe('streaming fence gate in Chat', () => {
  it('does not instantiate Mermaid while the fence is still open', async () => {
    const client = makeClient();
    seedTimeline(client, '```mermaid\nflowchart LR\n  A --> B\n');
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: 'running' } as never}
          availability={undefined}
        />,
        client,
      ),
    );

    await waitFor(() => {
      expect(container.querySelector('code')).not.toBeNull();
    });
    expect(mermaidRenderMock).not.toHaveBeenCalled();
    expect(mermaidLeaf(container)).toBeNull();
  });

  it('upgrades in place once the closing fence arrives', async () => {
    const client = makeClient();
    seedTimeline(client, '```mermaid\nflowchart LR\n  A --> B\n');
    const { container, rerender } = render(
      withClient(
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: 'running' } as never}
          availability={undefined}
        />,
        client,
      ),
    );
    expect(mermaidRenderMock).not.toHaveBeenCalled();

    seedTimeline(client, '```mermaid\nflowchart LR\n  A --> B\n```');
    rerender(
      withClient(
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: 'running' } as never}
          availability={undefined}
        />,
        client,
      ),
    );

    await waitFor(() => {
      expect(mermaidLeaf(container)).not.toBeNull();
    });
  });

  it('keeps a settled-but-unclosed fence as source', async () => {
    const client = makeClient();
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[assistantMessage('```mermaid\nflowchart LR\n  A --> B\n')] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );
    await waitFor(() => {
      expect(container.querySelector('code')).not.toBeNull();
    });
    expect(mermaidRenderMock).not.toHaveBeenCalled();
  });
});

describe('live → persisted row identity', () => {
  it('keeps one row key across the handoff and does not re-run Mermaid', async () => {
    const client = makeClient();
    seedTimeline(client, MERMAID_FIXTURE);

    const live = (
      <ChatMessageList
        messages={[]}
        pendingTask={{ task_id: TASK_ID, status: 'running' } as never}
        availability={undefined}
      />
    );
    const { container, rerender } = render(withClient(live, client));

    await waitFor(() => expect(mermaidLeaf(container)).not.toBeNull());
    const liveKey = container.querySelector('[data-row-key]')?.getAttribute('data-row-key');
    const rendersWhileLive = mermaidRenderMock.mock.calls.length;
    expect(liveKey).toBe(`task:${TASK_ID}`);

    rerender(
      withClient(
        <ChatMessageList
          messages={[assistantMessage(MERMAID_FIXTURE)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );

    await waitFor(() => {
      expect(screen.getByText(/Replied in/i)).toBeInTheDocument();
    });

    const persistedKey = container.querySelector('[data-row-key]')?.getAttribute('data-row-key');
    expect(persistedKey).toBe(liveKey);

    expect(mermaidRenderMock.mock.calls.length).toBe(rendersWhileLive);
    expect(mermaidLeaf(container)).not.toBeNull();
  });

  it('gives a persisted assistant message a task-scoped row key', () => {
    const client = makeClient();
    const { container } = render(
      withClient(
        <ChatMessageList
          messages={[assistantMessage('hi')] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    );
    expect(container.querySelector('[data-row-key]')?.getAttribute('data-row-key')).toBe(
      `task:${TASK_ID}`,
    );
  });
});

describe('semantic parity beyond Mermaid', () => {
  const FIXTURE = [
    'A [link](https://example.com) and a mention [MUL-7](mention://issue/MUL-7).',
    '',
    '```html',
    '<b>preview</b>',
    '```',
    '',
    '```ts',
    'const a = 1;',
    '```',
    '',
    '<mark>highlighted</mark>',
  ].join('\n');

  function renderReadonly() {
    return render(<ReadonlyContent content={FIXTURE} />).container;
  }

  function renderChatUser() {
    const client = makeClient();
    return render(
      withClient(
        <ChatMessageList
          messages={[userMessage(FIXTURE)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    ).container;
  }

  it('produces the same block set in Issue/Comment and Chat', async () => {
    resolveIssueIdentifierMock.mockImplementation((id: string) =>
      id === 'MUL-7' ? { id: 'issue-7', identifier: 'MUL-7' } : null,
    );

    const readonly = renderReadonly();
    const chat = renderChatUser();

    for (const container of [readonly, chat]) {
      await waitFor(() => {
        expect(container.querySelector('iframe')).not.toBeNull();
      });
      expect(container.querySelector('iframe')?.getAttribute('sandbox')).toBe('allow-scripts');
      expect(container.querySelector('code.hljs')).not.toBeNull();
      expect(within(container).getByTestId('issue-mention')).toBeInTheDocument();
      expect(container.querySelector('mark')?.textContent).toBe('highlighted');
      expect(container.querySelector('a[href="https://example.com"]')).not.toBeNull();
    }
  });

  it('does not dispatch htmlbars / mermaidx to rich blocks on either surface', async () => {
    const near = '```htmlbars\n<b>x</b>\n```\n\n```mermaidx\ngraph TD\n```';
    const readonly = render(<ReadonlyContent content={near} />).container;
    const client = makeClient();
    const chat = render(
      withClient(
        <ChatMessageList
          messages={[userMessage(near)] as never}
          pendingTask={null}
          availability={undefined}
        />,
        client,
      ),
    ).container;

    for (const container of [readonly, chat]) {
      expect(container.querySelector('iframe')).toBeNull();
      expect(container.querySelectorAll('code.hljs').length).toBe(2);
    }
    expect(mermaidRenderMock).not.toHaveBeenCalled();
  });
});

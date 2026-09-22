import { describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import { chatKeys } from '@goosar/core/chat/queries';
import type { TaskMessagePayload } from '@goosar/core/types';
import type { ReactElement } from 'react';
import enChat from '../../locales/en/chat.json';

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

import { ChatMessageList } from './chat-message-list';

const TEST_RESOURCES = { en: { chat: enChat } };
const TASK_ID = '6af44cbe-80ab-4dfe-b07d-bd3cfd588f4d';

function taskMsg(
  seq: number,
  type: TaskMessagePayload['type'],
  extra: Partial<TaskMessagePayload> = {},
): TaskMessagePayload {
  return { task_id: TASK_ID, seq, type, ...extra } as TaskMessagePayload;
}

const INITIAL_MESSAGES: TaskMessagePayload[] = [
  taskMsg(0, 'text', { content: 'Looking into it. ' }),
  taskMsg(1, 'tool_use', { tool: 'Bash', input: { command: 'go test ./...' } }),
  taskMsg(2, 'tool_result', { tool: 'Bash', output: 'ok' }),
];

function renderList(qc: QueryClient) {
  qc.setQueryData(chatKeys.taskMessages(TASK_ID), INITIAL_MESSAGES);
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: 'running' }}
          availability={undefined}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function pushTaskMessage(qc: QueryClient, msg: TaskMessagePayload) {
  act(() => {
    qc.setQueryData<TaskMessagePayload[]>(chatKeys.taskMessages(TASK_ID), (old = []) => [
      ...old,
      msg,
    ]);
  });
}

describe('ChatMessageList live timeline (MUL-3960 regression)', () => {
  it('does not remount the live timeline when a streamed message arrives', async () => {
    const qc = new QueryClient();
    renderList(qc);

    const foldTrigger = await screen.findByText('2 steps');
    const footerBefore = foldTrigger.closest('div');

    pushTaskMessage(qc, taskMsg(3, 'tool_use', { tool: 'Read', input: { file_path: '/tmp/x' } }));

    const updatedTrigger = await screen.findByText('3 steps');
    expect(updatedTrigger.closest('div')).toBe(footerBefore);
    expect(document.contains(foldTrigger)).toBe(true);
  });

  it('keeps the process fold closed by the user across streamed messages', async () => {
    const qc = new QueryClient();
    renderList(qc);

    const foldTrigger = await screen.findByText('2 steps');
    expect(screen.getByText('Bash')).toBeInTheDocument();
    act(() => {
      foldTrigger.click();
    });
    expect(screen.queryByText('Bash')).not.toBeInTheDocument();

    pushTaskMessage(qc, taskMsg(3, 'tool_use', { tool: 'Read', input: { file_path: '/tmp/x' } }));

    await screen.findByText('3 steps');
    expect(screen.queryByText('Bash')).not.toBeInTheDocument();
  });

  it('applies an embedded surface content transform to streamed text', async () => {
    const qc = new QueryClient();
    qc.setQueryData(chatKeys.taskMessages(TASK_ID), [
      taskMsg(0, 'text', {
        content: 'Draft ready.\n<agent_draft>{"name":"Hidden protocol"}</agent_draft>',
      }),
    ]);

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={qc}>
          <ChatMessageList
            messages={[]}
            pendingTask={{ task_id: TASK_ID, status: 'running' }}
            availability="online"
            transformContent={(content) =>
              content.replace(/<agent_draft>[\s\S]*?<\/agent_draft>/g, '')
            }
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(await screen.findByText('Draft ready.')).toBeInTheDocument();
    expect(screen.queryByText(/Hidden protocol/)).not.toBeInTheDocument();
  });
});

describe('ChatMessageList failure copy (MUL-5370 regression)', () => {
  function renderFailure(reason: string, content = 'skill bundle unavailable: skill "x"') {
    return render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={new QueryClient()}>
          <ChatMessageList
            messages={[
              {
                id: 'm1',
                chat_session_id: 's1',
                role: 'assistant',
                content,
                task_id: null,
                created_at: new Date(0).toISOString(),
                failure_reason: reason,
              },
            ]}
            pendingTask={undefined}
            availability="online"
          />
        </QueryClientProvider>
      </I18nProvider>,
    );
  }

  const FALLBACK = enChat.message_list.failure.fallback;

  it('renders dedicated copy for a stalled skill bundle download', async () => {
    renderFailure('skill_bundle_unavailable');
    expect(
      await screen.findByText(enChat.message_list.failure.skill_bundle_unavailable),
    ).toBeInTheDocument();
    expect(screen.queryByText(FALLBACK)).not.toBeInTheDocument();
  });

  it('renders dedicated copy for a refined reason the map names', async () => {
    renderFailure('agent_error.provider_network');
    expect(
      await screen.findByText(enChat.message_list.failure.provider_network),
    ).toBeInTheDocument();
  });

  it('degrades an unnamed refined reason to its agent_error family', async () => {
    renderFailure('agent_error.unknown');
    expect(await screen.findByText(enChat.message_list.failure.agent_error)).toBeInTheDocument();
    expect(screen.queryByText(FALLBACK)).not.toBeInTheDocument();
  });

  it('degrades a reason newer than this build to its agent_error family', async () => {
    renderFailure('agent_error.some_future_bucket');
    expect(await screen.findByText(enChat.message_list.failure.agent_error)).toBeInTheDocument();
  });

  it('still falls back when neither the reason nor its family is known', async () => {
    renderFailure('something_entirely_new');
    expect(await screen.findByText(FALLBACK)).toBeInTheDocument();
  });
});

describe('ChatMessageList runtime failure causes (#26)', () => {
  const AGENT_MISSING =
    "hermes session/new failed: session/new: Internal error (code=-32603, data=agent 'hermes' not found and no usable default_agent)";
  const LLM_NOT_CONFIGURED =
    'hermes session/new failed: session/new: /Users/e/.hermes/config.user.yaml: set llm.api_key (code=-32603, data=llm_not_configured)';

  function renderRuntimeFailure(content: string) {
    return render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={new QueryClient()}>
          <ChatMessageList
            messages={[
              {
                id: 'm1',
                chat_session_id: 's1',
                role: 'assistant',
                content,
                task_id: null,
                created_at: new Date(0).toISOString(),
                failure_reason: 'agent_error',
              },
            ]}
            pendingTask={undefined}
            availability="online"
          />
        </QueryClientProvider>
      </I18nProvider>,
    );
  }

  it("names the unresolved-agent cause instead of 'please try again'", async () => {
    renderRuntimeFailure(AGENT_MISSING);

    expect(
      await screen.findByText(enChat.message_list.failure.agent_unresolved),
    ).toBeInTheDocument();
    expect(screen.queryByText(enChat.message_list.failure.agent_error)).not.toBeInTheDocument();
  });

  it("shows the runtime's own message verbatim for llm_not_configured", async () => {
    renderRuntimeFailure(LLM_NOT_CONFIGURED);

    expect(
      await screen.findByText('/Users/e/.hermes/config.user.yaml: set llm.api_key'),
    ).toBeInTheDocument();
    expect(
      screen.getByText(enChat.message_list.failure.llm_not_configured_lead),
    ).toBeInTheDocument();
  });

  it('points at the in-app setting for llm_not_configured, verbatim path or not', async () => {
    renderRuntimeFailure(LLM_NOT_CONFIGURED);
    expect(
      await screen.findByText(enChat.message_list.failure.llm_not_configured_action),
    ).toBeInTheDocument();
  });

  it('falls back to its own copy when the runtime sent only the generic label', async () => {
    renderRuntimeFailure(
      'hermes session/new failed: session/new: Internal error (code=-32603, data=llm_not_configured)',
    );

    expect(
      await screen.findByText(enChat.message_list.failure.llm_not_configured),
    ).toBeInTheDocument();
  });

  it('leaves the family copy alone for a frame it cannot classify', async () => {
    renderRuntimeFailure('hermes prompt failed: prompt: upstream closed (code=-32000)');

    expect(await screen.findByText(enChat.message_list.failure.agent_error)).toBeInTheDocument();
  });

  it('keeps the raw frame reachable under the details fold', async () => {
    const user = userEvent.setup();
    renderRuntimeFailure(AGENT_MISSING);

    await user.click(await screen.findByText(enChat.message_list.show_details));

    expect(await screen.findByText(AGENT_MISSING)).toBeInTheDocument();
  });
});

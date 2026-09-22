// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import { chatKeys } from '@goosar/core/chat/queries';
import type { TaskMessagePayload } from '@goosar/core/types';
import type { ReactElement } from 'react';
import enChat from '../../locales/en/chat.json';

const listTaskMessages = vi.hoisted(() => vi.fn());
vi.mock('@goosar/core/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/api')>();
  return {
    ...actual,
    api: { ...actual.api, listTaskMessages },
  };
});

const rt = vi.hoisted(() => ({ pollingInterval: false as number | false }));
vi.mock('@goosar/core/realtime', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/realtime')>();
  return {
    ...actual,
    useRealtimePollingInterval: () => rt.pollingInterval,
  };
});

vi.mock('react-virtuoso', () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
  }: {
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
  }) => (
    <div>
      {data.map((item, i) => (
        <div key={computeItemKey(i, item)}>{itemContent(i, item)}</div>
      ))}
    </div>
  ),
}));

import { ChatMessageList } from './chat-message-list';

const TEST_RESOURCES = { en: { chat: enChat } };
const TASK_ID = '6af44cbe-80ab-4dfe-b07d-bd3cfd588f4d';

function taskMsg(seq: number): TaskMessagePayload {
  return {
    task_id: TASK_ID,
    seq,
    type: 'text',
    content: `m${seq}`,
  } as TaskMessagePayload;
}

function renderList(qc: QueryClient, isVisible = true) {
  qc.setQueryData(chatKeys.taskMessages(TASK_ID), [taskMsg(0), taskMsg(1)]);
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: 'running' }}
          availability={undefined}
          isVisible={isVisible}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  listTaskMessages.mockReset();
  listTaskMessages.mockResolvedValue([taskMsg(0), taskMsg(1)]);
  rt.pollingInterval = false;
});

describe('ChatMessageList live transcript fallback (#257)', () => {
  it('does not touch the network while the realtime connection is healthy', async () => {
    const qc = new QueryClient();
    renderList(qc);

    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(listTaskMessages).not.toHaveBeenCalled();
  });

  it('tails from the highest cached seq instead of re-reading the run', async () => {
    rt.pollingInterval = 50;
    const qc = new QueryClient();
    renderList(qc);

    await waitFor(() => expect(listTaskMessages.mock.calls.length).toBeGreaterThanOrEqual(2), {
      timeout: 3000,
    });
    expect(listTaskMessages.mock.calls[0]).toEqual([TASK_ID]);
    expect(listTaskMessages.mock.calls[1]).toEqual([TASK_ID, { since: 1 }]);
  });

  it('stays silent while the chat window is closed', async () => {
    rt.pollingInterval = 20;
    const qc = new QueryClient();
    renderList(qc, false);

    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(listTaskMessages).not.toHaveBeenCalled();
  });
});

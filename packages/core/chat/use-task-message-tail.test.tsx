/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { focusManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { chatKeys } from './queries';
import { useTaskMessageTail } from './use-task-message-tail';
import type { TaskMessagePayload } from '../types/events';

const listTaskMessages = vi.fn();
vi.mock('../api', () => ({
  api: {
    listTaskMessages: (...args: unknown[]) => listTaskMessages(...args),
  },
}));

const rt = vi.hoisted(() => ({ pollingInterval: false as number | false }));
vi.mock('../realtime', () => ({
  useRealtimePollingInterval: () => rt.pollingInterval,
}));

const TASK_ID = '4a2e8d1c-7f9b-4e2a-9c1d-123456789abc';

const msg = (seq: number): TaskMessagePayload => ({
  task_id: TASK_ID,
  issue_id: 'issue-1',
  seq,
  type: 'text',
  content: `m${seq}`,
});

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

beforeEach(() => {
  listTaskMessages.mockReset();
  listTaskMessages.mockResolvedValue([]);
  rt.pollingInterval = false;
  focusManager.setFocused(undefined);
});

afterEach(() => {
  vi.clearAllMocks();
  focusManager.setFocused(undefined);
});

describe('useTaskMessageTail (#257)', () => {
  it('does not run while the realtime transport is healthy', async () => {
    const qc = newClient();
    renderHook(() => useTaskMessageTail(TASK_ID, true), {
      wrapper: wrapper(qc),
    });

    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(listTaskMessages).not.toHaveBeenCalled();
  });

  it('does not run for a surface that is not showing the transcript', async () => {
    rt.pollingInterval = 10;
    const qc = newClient();
    renderHook(() => useTaskMessageTail(TASK_ID, false), {
      wrapper: wrapper(qc),
    });

    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(listTaskMessages).not.toHaveBeenCalled();
  });

  it('heals once, then asks only for messages newer than the cache', async () => {
    rt.pollingInterval = 50;
    const qc = newClient();
    qc.setQueryData(chatKeys.taskMessages(TASK_ID), [msg(1), msg(2)]);
    listTaskMessages.mockResolvedValue([msg(1), msg(2), msg(3)]);

    renderHook(() => useTaskMessageTail(TASK_ID, true), {
      wrapper: wrapper(qc),
    });

    await waitFor(() => expect(listTaskMessages.mock.calls.length).toBeGreaterThanOrEqual(2), {
      timeout: 2000,
    });
    expect(listTaskMessages.mock.calls[0]).toEqual([TASK_ID]);
    expect(listTaskMessages.mock.calls[1]).toEqual([TASK_ID, { since: 3 }]);
  });

  it('stays unhealed when the full read fails, and reads fully again', async () => {
    rt.pollingInterval = 50;
    const qc = newClient();
    qc.setQueryData(chatKeys.taskMessages(TASK_ID), [msg(1), msg(2)]);
    listTaskMessages
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValue([msg(1), msg(2), msg(3)]);

    renderHook(() => useTaskMessageTail(TASK_ID, true), {
      wrapper: wrapper(qc),
    });

    await waitFor(() => expect(listTaskMessages.mock.calls.length).toBeGreaterThanOrEqual(2), {
      timeout: 2000,
    });
    expect(listTaskMessages.mock.calls[0]).toEqual([TASK_ID]);
    expect(listTaskMessages.mock.calls[1]).toEqual([TASK_ID]);
  });

  it('merges the delta into the shared cache instead of replacing it', async () => {
    rt.pollingInterval = 10;
    const qc = newClient();
    qc.setQueryData(chatKeys.taskMessages(TASK_ID), [msg(1)]);
    listTaskMessages.mockResolvedValue([msg(2)]);

    renderHook(() => useTaskMessageTail(TASK_ID, true), {
      wrapper: wrapper(qc),
    });

    await waitFor(() =>
      expect(qc.getQueryData<TaskMessagePayload[]>(chatKeys.taskMessages(TASK_ID))).toHaveLength(2),
    );
    expect(
      qc.getQueryData<TaskMessagePayload[]>(chatKeys.taskMessages(TASK_ID))?.map((m) => m.seq),
    ).toEqual([1, 2]);
  });

  it('skips ticks while the document is hidden', async () => {
    rt.pollingInterval = 10;
    focusManager.setFocused(false);
    const qc = newClient();

    renderHook(() => useTaskMessageTail(TASK_ID, true), {
      wrapper: wrapper(qc),
    });

    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(listTaskMessages).not.toHaveBeenCalled();

    focusManager.setFocused(true);
    await waitFor(() => expect(listTaskMessages).toHaveBeenCalled());
  });

  it('never asks the server about an optimistic task id', async () => {
    rt.pollingInterval = 10;
    const qc = newClient();

    renderHook(() => useTaskMessageTail('optimistic-optimistic-1778739487737', true), {
      wrapper: wrapper(qc),
    });

    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(listTaskMessages).not.toHaveBeenCalled();
  });
});

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

describe('useAgentRuntimeStatus', () => {
  const getAgentRuntime = vi.fn();
  const retryAgentRuntime = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    // @ts-expect-error — test-only partial window.daemonAPI
    window.daemonAPI = { getAgentRuntime, retryAgentRuntime };
  });

  afterEach(() => {
    vi.useRealTimers();
    // @ts-expect-error — reset between tests
    delete window.daemonAPI;
  });

  async function importHook() {
    return await import('./use-agent-runtime-status');
  }

  it('maps a resolved ready status to the row shape', async () => {
    getAgentRuntime.mockResolvedValue({
      state: 'ready',
      version: '1.4.0',
      binPath: '/bin/hermes',
      configPath: '/cfg',
    });
    const { useAgentRuntimeStatus } = await importHook();
    const { result } = renderHook(() => useAgentRuntimeStatus());

    await waitFor(() =>
      expect(result.current.status).toEqual({ state: 'ready', version: '1.4.0' }),
    );
  });

  it('keeps polling while checking, stops once settled', async () => {
    getAgentRuntime
      .mockResolvedValueOnce({ state: 'checking' })
      .mockResolvedValueOnce({ state: 'not_installed', detail: 'timed out' });
    const { useAgentRuntimeStatus } = await importHook();
    const { result } = renderHook(() => useAgentRuntimeStatus());

    await waitFor(() => expect(result.current.status?.state).toBe('checking'));
    expect(getAgentRuntime).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    await waitFor(() => expect(result.current.status?.state).toBe('not_installed'));
    const callsAfterSettled = getAgentRuntime.mock.calls.length;

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(getAgentRuntime.mock.calls.length).toBe(callsAfterSettled);
  });

  it('retry shows installing while in flight, then the resolved state', async () => {
    getAgentRuntime.mockResolvedValue({ state: 'not_installed', detail: 'failed' });
    const { useAgentRuntimeStatus } = await importHook();
    const { result } = renderHook(() => useAgentRuntimeStatus());
    await waitFor(() => expect(result.current.status?.state).toBe('not_installed'));

    let resolveRetry!: (v: unknown) => void;
    retryAgentRuntime.mockReturnValue(
      new Promise((resolve) => {
        resolveRetry = resolve;
      }),
    );

    let retryPromise!: Promise<void>;
    act(() => {
      retryPromise = result.current.retry();
    });
    await waitFor(() => expect(result.current.status?.state).toBe('installing'));

    await act(async () => {
      resolveRetry({ state: 'ready', version: '1.4.0', binPath: '/b', configPath: '/c' });
      await retryPromise;
    });

    expect(result.current.status).toEqual({ state: 'ready', version: '1.4.0' });
  });
});

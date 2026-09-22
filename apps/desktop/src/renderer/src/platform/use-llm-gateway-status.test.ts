import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

describe('useLlmGatewayStatus', () => {
  const getLlmGatewayStatus = vi.fn();
  const retryLlmGatewayStatus = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    // @ts-expect-error — test-only partial window.daemonAPI
    window.daemonAPI = { getLlmGatewayStatus, retryLlmGatewayStatus };
  });

  afterEach(() => {
    // @ts-expect-error — reset between tests
    delete window.daemonAPI;
  });

  async function importHook() {
    return await import('./use-llm-gateway-status');
  }

  it('starts null and resolves the initial probe', async () => {
    getLlmGatewayStatus.mockResolvedValue({ verdict: 'ok', source: 'agent' });
    const { useLlmGatewayStatus } = await importHook();
    const { result } = renderHook(() => useLlmGatewayStatus());

    expect(result.current.status).toBeNull();
    await waitFor(() => expect(result.current.status).toEqual({ verdict: 'ok', source: 'agent' }));
    expect(getLlmGatewayStatus).toHaveBeenCalledTimes(1);
  });

  it('stays null when the preload predates this API', async () => {
    // @ts-expect-error — simulate an older preload with no method at all
    window.daemonAPI = {};
    const { useLlmGatewayStatus } = await importHook();
    const { result } = renderHook(() => useLlmGatewayStatus());
    expect(result.current.status).toBeNull();
  });

  it('retry re-runs the probe and updates status', async () => {
    getLlmGatewayStatus.mockResolvedValue({
      verdict: 'unreachable',
      source: 'agent',
    });
    retryLlmGatewayStatus.mockResolvedValue({ verdict: 'ok', source: 'agent' });
    const { useLlmGatewayStatus } = await importHook();
    const { result } = renderHook(() => useLlmGatewayStatus());
    await waitFor(() => expect(result.current.status?.verdict).toBe('unreachable'));

    await act(async () => {
      await result.current.retry();
    });

    expect(retryLlmGatewayStatus).toHaveBeenCalledTimes(1);
    expect(result.current.status).toEqual({ verdict: 'ok', source: 'agent' });
  });
});

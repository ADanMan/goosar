import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';

describe('usePxProxyStatus', () => {
  const getPxProxyStatus = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    // @ts-expect-error — test-only partial window.daemonAPI
    window.daemonAPI = { getPxProxyStatus };
  });

  afterEach(() => {
    // @ts-expect-error — reset between tests
    delete window.daemonAPI;
  });

  async function importHook() {
    return await import('./use-px-proxy-status');
  }

  it('starts null and resolves the status', async () => {
    getPxProxyStatus.mockResolvedValue({ state: 'installed' });
    const { usePxProxyStatus } = await importHook();
    const { result } = renderHook(() => usePxProxyStatus());

    expect(result.current).toBeNull();
    await waitFor(() => expect(result.current).toEqual({ state: 'installed' }));
    expect(getPxProxyStatus).toHaveBeenCalledTimes(1);
  });

  it('stays null when the preload predates this API', async () => {
    // @ts-expect-error — simulate an older preload with no method at all
    window.daemonAPI = {};
    const { usePxProxyStatus } = await importHook();
    const { result } = renderHook(() => usePxProxyStatus());
    expect(result.current).toBeNull();
  });
});

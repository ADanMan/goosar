import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ProvisioningStatus } from '@goosar/views/onboarding';

const mocks = vi.hoisted(() => ({
  getProvisioningStatus: vi.fn(),
  setProvisioningWorkspace: vi.fn(),
  retryProvisioning: vi.fn(),
}));

vi.mock('./provisioning-bridge', () => ({
  getProvisioningStatus: mocks.getProvisioningStatus,
  setProvisioningWorkspace: mocks.setProvisioningWorkspace,
  retryProvisioning: mocks.retryProvisioning,
}));

import { useProvisioningStatus } from './use-provisioning-status';

function syncingStatus(overrides: Partial<ProvisioningStatus> = {}): ProvisioningStatus {
  return {
    configured: true,
    state: 'syncing',
    byType: {
      skill: { installed: 1, total: 4, failed: 0 },
      'mcp-server': { installed: 0, total: 2, failed: 0 },
      runtime: { installed: 0, total: 1, failed: 0 },
    },
    summary: { installed: 1, total: 7, failed: 0 },
    packages: [],
    ...overrides,
  };
}

describe('useProvisioningStatus', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('clears the provisioning workspace and never polls while workspaceId is null', () => {
    renderHook(() => useProvisioningStatus(null));
    expect(mocks.setProvisioningWorkspace).toHaveBeenCalledWith(null);
    expect(mocks.getProvisioningStatus).not.toHaveBeenCalled();
  });

  it('sets the provisioning workspace and polls status once workspaceId is set', async () => {
    mocks.getProvisioningStatus.mockResolvedValue(syncingStatus());
    const { result } = renderHook(() => useProvisioningStatus('ws-1'));

    expect(mocks.setProvisioningWorkspace).toHaveBeenCalledWith('ws-1');
    await waitFor(() => expect(result.current.status).not.toBeNull());
    expect(result.current.status?.state).toBe('syncing');
  });

  it('keeps polling while syncing and stops once state is ok', async () => {
    mocks.getProvisioningStatus
      .mockResolvedValueOnce(syncingStatus())
      .mockResolvedValueOnce(syncingStatus({ state: 'ok' }));
    const { result } = renderHook(() => useProvisioningStatus('ws-1'));

    await waitFor(() => expect(result.current.status?.state).toBe('syncing'));
    expect(mocks.getProvisioningStatus).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    await waitFor(() => expect(result.current.status?.state).toBe('ok'));
    const callsAfterOk = mocks.getProvisioningStatus.mock.calls.length;

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(mocks.getProvisioningStatus.mock.calls.length).toBe(callsAfterOk);
  });

  it('retry forces a resync and refreshes status', async () => {
    mocks.getProvisioningStatus.mockResolvedValue(syncingStatus({ state: 'fail' }));
    mocks.retryProvisioning.mockResolvedValue(undefined);
    const { result } = renderHook(() => useProvisioningStatus('ws-1'));
    await waitFor(() => expect(result.current.status?.state).toBe('fail'));

    mocks.getProvisioningStatus.mockResolvedValue(syncingStatus({ state: 'syncing' }));
    await act(async () => {
      await result.current.retry();
    });

    expect(mocks.retryProvisioning).toHaveBeenCalledTimes(1);
    expect(result.current.status?.state).toBe('syncing');
  });

  it('re-sets the provisioning workspace when workspaceId changes', async () => {
    mocks.getProvisioningStatus.mockResolvedValue(syncingStatus());
    const { rerender } = renderHook(
      ({ wsId }: { wsId: string | null }) => useProvisioningStatus(wsId),
      { initialProps: { wsId: 'ws-1' } },
    );
    expect(mocks.setProvisioningWorkspace).toHaveBeenLastCalledWith('ws-1');

    rerender({ wsId: 'ws-2' });
    expect(mocks.setProvisioningWorkspace).toHaveBeenLastCalledWith('ws-2');
  });
});

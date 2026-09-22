/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { setApiInstance } from '../api';
import type { ApiClient } from '../api/client';
import {
  deploymentAdminKeys,
  useDeactivateDeploymentUser,
  useReactivateDeploymentUser,
} from './admin';

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

const RESULT = {
  user_id: 'u-1',
  email: 'leaver@corp.example',
  name: 'Leaver',
  deactivated: true,
  deactivated_at: '2026-09-08T09:00:00Z',
  token_version: 3,
  revoked_tokens: 2,
  closed_connections: 1,
};

describe('deployment offboarding mutations', () => {
  let qc: QueryClient;
  let deactivateDeploymentUser: ReturnType<typeof vi.fn>;
  let reactivateDeploymentUser: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    deactivateDeploymentUser = vi.fn().mockResolvedValue(RESULT);
    reactivateDeploymentUser = vi
      .fn()
      .mockResolvedValue({ ...RESULT, deactivated: false, deactivated_at: null });
    setApiInstance({
      deactivateDeploymentUser,
      reactivateDeploymentUser,
    } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it('invalidates the workspace roster after deactivating', async () => {
    const invalidate = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useDeactivateDeploymentUser(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync('u-1');
    });

    expect(deactivateDeploymentUser).toHaveBeenCalledWith('u-1');
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: deploymentAdminKeys.workspaces,
    });
    expect(deploymentAdminKeys.workspaceMembers('ws-1').slice(0, 2)).toEqual([
      ...deploymentAdminKeys.workspaces,
    ]);
  });

  it('invalidates the workspace roster after reactivating', async () => {
    const invalidate = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useReactivateDeploymentUser(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync('u-1');
    });

    expect(reactivateDeploymentUser).toHaveBeenCalledWith('u-1');
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: deploymentAdminKeys.workspaces,
    });
  });

  it('surfaces the server refusal instead of swallowing it', async () => {
    deactivateDeploymentUser.mockRejectedValue(new Error('409 conflict'));
    const { result } = renderHook(() => useDeactivateDeploymentUser(), {
      wrapper: createWrapper(qc),
    });

    await expect(
      act(async () => {
        await result.current.mutateAsync('u-1');
      }),
    ).rejects.toThrow('409 conflict');
  });
});

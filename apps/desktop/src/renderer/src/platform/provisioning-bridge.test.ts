import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ProvisioningStatus } from '@goosar/views/onboarding';
import {
  getProvisioningStatus,
  retryProvisioning,
  setProvisioningWorkspace,
} from './provisioning-bridge';

const STATUS: ProvisioningStatus = {
  configured: true,
  state: 'ok',
  byType: {
    skill: { installed: 4, total: 4, failed: 0 },
    'mcp-server': { installed: 2, total: 2, failed: 0 },
    runtime: { installed: 1, total: 1, failed: 0 },
  },
  summary: { installed: 7, total: 7, failed: 0 },
  packages: [],
};

interface DaemonAPIWithProvisioning {
  getProvisioningStatus: ReturnType<typeof vi.fn>;
  setProvisioningWorkspace: ReturnType<typeof vi.fn>;
  retryProvisioning: ReturnType<typeof vi.fn>;
}

function daemonAPIMock(): DaemonAPIWithProvisioning {
  return window.daemonAPI as unknown as DaemonAPIWithProvisioning;
}

describe('provisioning-bridge', () => {
  const originalDaemonAPI = window.daemonAPI;

  afterEach(() => {
    window.daemonAPI = originalDaemonAPI;
  });

  describe('with a preload that carries the provisioning bridge', () => {
    beforeEach(() => {
      window.daemonAPI = {
        ...originalDaemonAPI,
        getProvisioningStatus: vi.fn().mockResolvedValue(STATUS),
        setProvisioningWorkspace: vi.fn(),
        retryProvisioning: vi.fn().mockResolvedValue(undefined),
      } as unknown as typeof window.daemonAPI;
    });

    it('forwards setProvisioningWorkspace with the workspace id', () => {
      setProvisioningWorkspace('ws-1');
      expect(daemonAPIMock().setProvisioningWorkspace).toHaveBeenCalledWith('ws-1');
    });

    it('forwards setProvisioningWorkspace(null) to clear the context', () => {
      setProvisioningWorkspace(null);
      expect(daemonAPIMock().setProvisioningWorkspace).toHaveBeenCalledWith(null);
    });

    it('returns the resolved status', async () => {
      await expect(getProvisioningStatus()).resolves.toEqual(STATUS);
    });

    it('forwards retryProvisioning', async () => {
      await retryProvisioning();
      expect(daemonAPIMock().retryProvisioning).toHaveBeenCalledTimes(1);
    });

    it('resolves null instead of throwing when the status call rejects', async () => {
      daemonAPIMock().getProvisioningStatus.mockRejectedValue(new Error('ipc down'));
      await expect(getProvisioningStatus()).resolves.toBeNull();
    });
  });

  describe('with a preload from before issue #188 (methods missing)', () => {
    beforeEach(() => {
      const rest = { ...(originalDaemonAPI as unknown as Record<string, unknown>) };
      delete rest.getProvisioningStatus;
      delete rest.setProvisioningWorkspace;
      delete rest.retryProvisioning;
      window.daemonAPI = rest as unknown as typeof window.daemonAPI;
    });

    it('setProvisioningWorkspace no-ops without throwing', () => {
      expect(() => setProvisioningWorkspace('ws-1')).not.toThrow();
    });

    it('getProvisioningStatus resolves null without throwing', async () => {
      await expect(getProvisioningStatus()).resolves.toBeNull();
    });

    it('retryProvisioning resolves without throwing', async () => {
      await expect(retryProvisioning()).resolves.toBeUndefined();
    });
  });
});

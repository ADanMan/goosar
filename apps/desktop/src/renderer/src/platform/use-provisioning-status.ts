import { useCallback, useEffect, useState } from 'react';
import type { ProvisioningStatus } from '@goosar/views/onboarding';
import {
  getProvisioningStatus,
  retryProvisioning,
  setProvisioningWorkspace,
} from './provisioning-bridge';

const POLL_INTERVAL_MS = 2000;

function isPolling(status: ProvisioningStatus | null): boolean {
  if (status === null) return true;
  if (status.configured === false) return false;
  return status.state === 'idle' || status.state === 'syncing';
}

export function useProvisioningStatus(workspaceId: string | null): {
  status: ProvisioningStatus | null;
  retry: () => Promise<void>;
} {
  useEffect(() => {
    setProvisioningWorkspace(workspaceId);
  }, [workspaceId]);

  return useProvisioningPoll(workspaceId);
}

export function useProvisioningPoll(workspaceId: string | null): {
  status: ProvisioningStatus | null;
  retry: () => Promise<void>;
} {
  const [status, setStatus] = useState<ProvisioningStatus | null>(null);

  useEffect(() => {
    setStatus(null);
    if (!workspaceId) return undefined;

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const poll = async () => {
      const next = await getProvisioningStatus();
      if (cancelled) return;
      setStatus(next);
      if (isPolling(next)) {
        timer = setTimeout(() => void poll(), POLL_INTERVAL_MS);
      }
    };

    void poll();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [workspaceId]);

  const retry = useCallback(async () => {
    await retryProvisioning();
    const next = await getProvisioningStatus();
    setStatus(next);
  }, []);

  return { status, retry };
}

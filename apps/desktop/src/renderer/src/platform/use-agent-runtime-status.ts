import { useCallback, useEffect, useRef, useState } from 'react';
import type { AgentRuntimeStatus as SharedAgentRuntimeStatus } from '../../../shared/agent-runtime-types';
import type { AgentRuntimeStatus as AgentRowStatus } from '@goosar/views/onboarding';

const POLL_INTERVAL_MS = 2000;

function toRowStatus(status: SharedAgentRuntimeStatus): AgentRowStatus {
  switch (status.state) {
    case 'checking':
      return { state: 'checking' };
    case 'unsupported':
      return { state: 'unsupported', detail: status.detail };
    case 'not_installed':
      return { state: 'not_installed', detail: status.detail };
    case 'external':
      return { state: 'external' };
    case 'needs_config':
      return { state: 'needs_config', version: status.version };
    case 'ready':
      return { state: 'ready', version: status.version };
  }
}

function isSettled(status: SharedAgentRuntimeStatus | null): boolean {
  return status !== null && status.state !== 'checking';
}

export function useAgentRuntimeStatus(): {
  status: AgentRowStatus | null;
  retry: () => Promise<void>;
} {
  const [status, setStatus] = useState<SharedAgentRuntimeStatus | null>(null);
  const [retrying, setRetrying] = useState(false);
  const retryingRef = useRef(false);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const poll = async () => {
      const api = window.daemonAPI;
      const next = api?.getAgentRuntime ? await api.getAgentRuntime() : null;
      if (cancelled) return;
      setStatus(next);
      if (!retryingRef.current && !isSettled(next)) {
        timer = setTimeout(() => void poll(), POLL_INTERVAL_MS);
      }
    };

    void poll();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, []);

  const retry = useCallback(async () => {
    const api = window.daemonAPI;
    if (!api?.retryAgentRuntime) return;
    retryingRef.current = true;
    setRetrying(true);
    try {
      const next = await api.retryAgentRuntime();
      setStatus(next);
    } finally {
      retryingRef.current = false;
      setRetrying(false);
    }
  }, []);

  const rowStatus =
    status === null
      ? null
      : retrying
        ? { ...toRowStatus(status), state: 'installing' as const }
        : toRowStatus(status);

  return { status: rowStatus, retry };
}

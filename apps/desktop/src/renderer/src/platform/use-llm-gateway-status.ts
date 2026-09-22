import { useCallback, useEffect, useState } from 'react';
import type { LlmGatewayRowStatus } from '@goosar/views/onboarding';

export function useLlmGatewayStatus(): {
  status: LlmGatewayRowStatus | null;
  retry: () => Promise<void>;
} {
  const [status, setStatus] = useState<LlmGatewayRowStatus | null>(null);

  useEffect(() => {
    let cancelled = false;
    void window.daemonAPI
      ?.getLlmGatewayStatus?.()
      .then((next) => {
        if (!cancelled) setStatus(next);
      })
      .catch(() => {
        // Unable to answer — keep null rather than invent a verdict.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const retry = useCallback(async () => {
    const api = window.daemonAPI;
    if (!api?.retryLlmGatewayStatus) return;
    try {
      const next = await api.retryLlmGatewayStatus();
      setStatus(next);
    } catch {
      // Leave the previous verdict standing rather than clearing it.
    }
  }, []);

  return { status, retry };
}

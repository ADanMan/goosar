import { useEffect, useState } from 'react';
import type { PxProxyRowStatus } from '@goosar/views/onboarding';

export function usePxProxyStatus(): PxProxyRowStatus | null {
  const [status, setStatus] = useState<PxProxyRowStatus | null>(null);

  useEffect(() => {
    let cancelled = false;
    void window.daemonAPI
      ?.getPxProxyStatus?.()
      .then((next) => {
        if (!cancelled) setStatus(next);
      })
      .catch(() => {
        // Unable to answer — keep null rather than invent a state.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return status;
}

import { useEffect, useState } from 'react';
import type { LlmExistingConnectionInfo } from '@goosar/views/onboarding';

export function useExistingLlmConnection(): LlmExistingConnectionInfo | null | undefined {
  const [state, setState] = useState<LlmExistingConnectionInfo | null | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    const read = window.daemonAPI?.getLlmRuntimeConfig;
    if (!read) {
      setState(null);
      return;
    }
    void read()
      .then((config) => {
        if (cancelled) return;
        const apiBase = config?.apiBase?.trim() ?? '';
        const model = config?.model?.trim() ?? '';
        setState(
          apiBase.length > 0 && model.length > 0
            ? { apiBase, model, hasKey: config?.hasKey === true }
            : null,
        );
      })
      .catch(() => {
        if (!cancelled) setState(null);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return state;
}

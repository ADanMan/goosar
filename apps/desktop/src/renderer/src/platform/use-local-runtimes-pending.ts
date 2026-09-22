import { useEffect, useState } from 'react';
import type { DaemonStatus } from '../../../shared/daemon-types';

export function useLocalRuntimesPending(): boolean {
  const [pending, setPending] = useState(true);
  useEffect(() => {
    const apply = (s: DaemonStatus) => {
      setPending(
        s.state === 'starting' ||
          s.state === 'installing_cli' ||
          (s.state === 'running' && (s.agents?.length ?? 0) > 0),
      );
    };
    let cancelled = false;
    window.daemonAPI
      .getStatus()
      .then((s) => {
        if (!cancelled) apply(s);
      })
      .catch(() => {
        // No daemon status available — leave `pending` at its initial true so
        // the step relies on the runtime step's absolute hard-timeout ceiling
        // rather than flashing empty prematurely.
      });
    const unsubscribe = window.daemonAPI.onStatusChange(apply);
    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, []);
  return pending;
}

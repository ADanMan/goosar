import { useEffect, useState } from 'react';
import type { PerimeterMachineState } from '@goosar/views/onboarding';

export function usePerimeterMachine(): PerimeterMachineState | undefined {
  const [state, setState] = useState<PerimeterMachineState | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    window.daemonAPI
      ?.getPerimeter?.()
      .then((view) => {
        if (cancelled) return;
        setState({ caBundleMissing: view.caBundlePresent !== true });
      })
      .catch(() => {
        // Unable to answer — keep undefined rather than invent facts.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return state;
}

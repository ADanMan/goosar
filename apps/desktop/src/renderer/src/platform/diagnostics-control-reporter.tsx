import { useEffect } from 'react';
import { useConfigStore, featureFlagEnabled } from '@goosar/core/config';
import { HANG_STACK_CAPTURE_FLAG } from '../../../shared/diagnostics-control';

export function DiagnosticsControlReporter() {
  const stackCaptureEnabled = useConfigStore((state) =>
    featureFlagEnabled(state.featureFlags, HANG_STACK_CAPTURE_FLAG),
  );

  useEffect(() => {
    window.desktopAPI.setDiagnosticsControl({ stackCaptureEnabled });
  }, [stackCaptureEnabled]);

  return null;
}

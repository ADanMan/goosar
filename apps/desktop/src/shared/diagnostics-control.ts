export const DIAGNOSTICS_CONTROL_CHANNEL = 'diagnostics:control';

export const HANG_STACK_CAPTURE_FLAG = 'desktop_hang_stack_capture';

export interface DiagnosticsControl {
  stackCaptureEnabled: boolean;
}

export const DIAGNOSTICS_CONTROL_OFF: DiagnosticsControl = {
  stackCaptureEnabled: false,
};

export function parseDiagnosticsControl(value: unknown): DiagnosticsControl {
  if (!value || typeof value !== 'object') return DIAGNOSTICS_CONTROL_OFF;
  const input = value as Record<string, unknown>;
  return { stackCaptureEnabled: input.stackCaptureEnabled === true };
}

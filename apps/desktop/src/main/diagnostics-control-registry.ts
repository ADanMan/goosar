import { parseDiagnosticsControl, type DiagnosticsControl } from '../shared/diagnostics-control';

export interface DiagnosticsControlRegistry<T> {
  apply: (target: T, rawControl: unknown) => void;
  isStackCaptureEnabled: (target: T) => boolean;
}

export interface DiagnosticsControlHandlers<T> {
  warm: (target: T) => void;
  cool: (target: T) => void;
}

export function createDiagnosticsControlRegistry<T extends object>({
  warm,
  cool,
}: DiagnosticsControlHandlers<T>): DiagnosticsControlRegistry<T> {
  const controls = new WeakMap<T, DiagnosticsControl>();

  const enabled = (target: T) => controls.get(target)?.stackCaptureEnabled === true;

  return {
    apply(target, rawControl) {
      const next = parseDiagnosticsControl(rawControl);
      const was = enabled(target);
      controls.set(target, next);
      if (next.stackCaptureEnabled === was) return;
      if (next.stackCaptureEnabled) warm(target);
      else cool(target);
    },

    isStackCaptureEnabled: enabled,
  };
}

// Снятие JS-стека зависшего рендерера.

import {
  HANG_STACK_MAX_FRAMES,
  sanitizeHangStackFrames,
  type HangStackFrame,
} from '../shared/hang-stack';

export type { HangStackFrame };

export interface CdpDebugger {
  isAttached(): boolean;
  attach(protocolVersion?: string): void;
  detach(): void;
  sendCommand(method: string, commandParams?: Record<string, unknown>): Promise<unknown>;
  on(
    event: 'message',
    listener: (event: unknown, method: string, params: Record<string, unknown>) => void,
  ): unknown;
  off(
    event: 'message',
    listener: (event: unknown, method: string, params: Record<string, unknown>) => void,
  ): unknown;
}

export const ALLOWED_CDP_METHODS = [
  'Debugger.enable',
  'Debugger.disable',
  'Debugger.pause',
  'Debugger.resume',
] as const;

export interface CaptureHangStackOptions {
  timeoutMs?: number;
  maxFrames?: number;
}

const DEFAULT_TIMEOUT_MS = 2000;

export function sendDebuggerCommand(dbg: CdpDebugger, method: string): Promise<unknown> {
  if (!(ALLOWED_CDP_METHODS as readonly string[]).includes(method)) {
    return Promise.reject(
      new Error(
        `Forbidden CDP method "${method}": hang stack capture may only send ${ALLOWED_CDP_METHODS.join(' / ')}`,
      ),
    );
  }
  return dbg.sendCommand(method);
}

export async function warmDebuggerChannel(dbg: CdpDebugger): Promise<boolean> {
  let attachedHere = false;
  try {
    if (!dbg.isAttached()) {
      dbg.attach('1.3');
      attachedHere = true;
    }
    await sendDebuggerCommand(dbg, 'Debugger.enable');
    return true;
  } catch {
    if (attachedHere) detachQuietly(dbg);
    return false;
  }
}

export async function coolDebuggerChannel(dbg: CdpDebugger): Promise<void> {
  if (!dbg.isAttached()) return;
  try {
    await sendDebuggerCommand(dbg, 'Debugger.disable');
  } catch {
    // Disable is a courtesy to the renderer; detach is the contract. A failed
    // disable must not leave the channel attached, or revoking the kill switch
    // would not actually revoke anything.
  } finally {
    detachQuietly(dbg);
  }
}

function detachQuietly(dbg: CdpDebugger): void {
  try {
    dbg.detach();
  } catch {
    // Already detached / renderer gone.
  }
}

export async function captureHangStack(
  dbg: CdpDebugger,
  options: CaptureHangStackOptions = {},
): Promise<HangStackFrame[] | null> {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const maxFrames = options.maxFrames ?? HANG_STACK_MAX_FRAMES;

  if (!dbg.isAttached()) return null;

  let onMessage:
    ((event: unknown, method: string, params: Record<string, unknown>) => void) | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;

  try {
    const paused = new Promise<HangStackFrame[] | null>((resolve) => {
      onMessage = (_event, method, params) => {
        if (method !== 'Debugger.paused') return;
        resolve(sanitizeHangStackFrames(params.callFrames, maxFrames));
      };
      dbg.on('message', onMessage);
      timer = setTimeout(() => resolve(null), timeoutMs);
    });

    await sendDebuggerCommand(dbg, 'Debugger.pause');
    return await paused;
  } catch {
    return null;
  } finally {
    if (timer) clearTimeout(timer);
    if (onMessage) {
      try {
        dbg.off('message', onMessage);
      } catch {
        // Listener cleanup is best-effort.
      }
    }
    try {
      await sendDebuggerCommand(dbg, 'Debugger.resume');
    } catch {
      // The renderer may already be gone; nothing left to resume.
    }
  }
}

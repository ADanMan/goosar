export type RendererRecoveryWindow = {
  isDestroyed: () => boolean;
  on: (event: 'unresponsive' | 'responsive', handler: () => void) => unknown;
  webContents: {
    on: (event: string, handler: (...args: any[]) => void) => unknown;
    reload: () => void;
  };
};

type ReloadPromptPayload = {
  kind: 'render-process-gone' | 'preload-error' | 'unresponsive';
  context: Record<string, unknown>;
};

type ReloadPromptResult = 'reload' | 'dismiss';

type RendererRecoveryOptions = {
  isDev: boolean;
  showReloadPrompt: (payload: ReloadPromptPayload) => Promise<ReloadPromptResult>;
  getDiagnosticContext?: () => Record<string, unknown>;
  persistBreadcrumb?: (payload: ReloadPromptPayload) => void;
  clearBreadcrumb?: () => void;
  captureStack?: () => Promise<unknown>;
  log?: (tag: string, ...args: unknown[]) => void;
  unresponsivePromptDelayMs?: number;
};

const noopDevLog = () => undefined;

export function installRendererRecoveryHandlers(
  window: RendererRecoveryWindow,
  {
    isDev,
    showReloadPrompt,
    getDiagnosticContext,
    persistBreadcrumb,
    clearBreadcrumb,
    captureStack,
    log = noopDevLog,
    unresponsivePromptDelayMs = 1500,
  }: RendererRecoveryOptions,
) {
  let unresponsivePromptTimer: ReturnType<typeof setTimeout> | null = null;
  let unresponsiveBreadcrumbWritten = false;
  const mergeDiagnosticContext = (context: Record<string, unknown>) => ({
    ...readDiagnosticContext(getDiagnosticContext),
    ...context,
  });
  const maybePromptReload = (payload: ReloadPromptPayload) => {
    if (isDev) return;
    void showReloadPrompt(payload).then((result) => {
      if (result === 'reload' && !window.isDestroyed()) {
        window.webContents.reload();
      }
    });
  };

  window.webContents.on('render-process-gone', (_event, details) => {
    if (isDev) log('process-gone', JSON.stringify(details));
    if (!isRecoverableRendererExit(details)) return;
    const payload: ReloadPromptPayload = {
      kind: 'render-process-gone',
      context: mergeDiagnosticContext({ details }),
    };
    persistBreadcrumb?.(payload);
    maybePromptReload(payload);
  });

  window.webContents.on('preload-error', (_event, preloadPath, error) => {
    if (isDev) log('preload-error', `path=${preloadPath} err=${formatError(error)}`);
    maybePromptReload({
      kind: 'preload-error',
      context: mergeDiagnosticContext({ preloadPath, error: formatError(error) }),
    });
  });

  window.on('unresponsive', () => {
    if (isDev || unresponsivePromptTimer) return;
    unresponsivePromptTimer = setTimeout(() => {
      unresponsivePromptTimer = null;
      void reportHang();
    }, unresponsivePromptDelayMs);
  });

  const reportHang = async () => {
    const stack = captureStack ? await readStack(captureStack, log) : null;
    const payload: ReloadPromptPayload = {
      kind: 'unresponsive',
      context: mergeDiagnosticContext(stack ? { stack } : {}),
    };
    persistBreadcrumb?.(payload);
    unresponsiveBreadcrumbWritten = true;
    maybePromptReload(payload);
  };

  window.on('responsive', () => {
    if (unresponsivePromptTimer) {
      clearTimeout(unresponsivePromptTimer);
      unresponsivePromptTimer = null;
    }
    if (unresponsiveBreadcrumbWritten) {
      clearBreadcrumb?.();
      unresponsiveBreadcrumbWritten = false;
    }
  });
}

export function createElectronReloadPrompt(
  showMessageBox: (options: {
    type: 'warning';
    buttons: string[];
    defaultId: number;
    cancelId: number;
    title: string;
    message: string;
    detail: string;
  }) => Promise<{ response: number }>,
) {
  return async (payload: ReloadPromptPayload): Promise<ReloadPromptResult> => {
    const result = await showMessageBox({
      type: 'warning',
      buttons: ['Reload', 'Dismiss'],
      defaultId: 0,
      cancelId: 1,
      title: 'Goosar needs to reload',
      message: rendererRecoveryMessage(payload.kind),
      detail: rendererRecoveryDetail(payload),
    });
    return result.response === 0 ? 'reload' : 'dismiss';
  };
}

function isRecoverableRendererExit(details: unknown) {
  if (!details || typeof details !== 'object') return false;
  const reason = (details as { reason?: unknown }).reason;
  return (
    reason === 'crashed' ||
    reason === 'oom' ||
    reason === 'abnormal-exit' ||
    reason === 'launch-failed' ||
    reason === 'integrity-failure'
  );
}

function rendererRecoveryMessage(kind: ReloadPromptPayload['kind']) {
  switch (kind) {
    case 'render-process-gone':
      return 'The desktop window stopped unexpectedly.';
    case 'preload-error':
      return 'The desktop window could not finish starting.';
    case 'unresponsive':
      return 'The desktop window has been stuck for a few seconds.';
  }
}

function rendererRecoveryDetail(payload: ReloadPromptPayload) {
  const guidance = [
    'Click Reload to refresh this window and keep using Goosar.',
    'If this keeps happening, please tell us what you were doing right before this message appeared and whether Reload recovered the window.',
  ];

  if (payload.kind === 'unresponsive') {
    guidance.push(
      'For macOS reports, an Activity Monitor sample of the Goosar Helper (Renderer) process helps us find what blocked the app.',
    );
  }

  return [
    ...guidance,
    '',
    'Diagnostic details:',
    `kind: ${payload.kind}`,
    `context: ${JSON.stringify(payload.context)}`,
  ].join('\n');
}

async function readStack(
  captureStack: () => Promise<unknown>,
  log: (tag: string, ...args: unknown[]) => void,
): Promise<unknown> {
  try {
    return (await captureStack()) ?? null;
  } catch (error) {
    log('stack-capture-failed', formatError(error));
    return null;
  }
}

function readDiagnosticContext(getDiagnosticContext: (() => Record<string, unknown>) | undefined) {
  if (!getDiagnosticContext) return {};
  try {
    return getDiagnosticContext();
  } catch {
    return {};
  }
}

function formatError(error: unknown) {
  return error instanceof Error ? (error.stack ?? error.message) : String(error);
}

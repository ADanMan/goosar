import { useMemo } from 'react';
import { isRouteErrorResponse, useLocation, useRouteError } from 'react-router-dom';
import { AlertTriangle, Compass, RotateCw, Send, X } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { useModalStore } from '@goosar/core/modals';
import { useT } from '@goosar/views/i18n';
import { useTabStore } from '@/stores/tab-store';

type DesktopAppInfo = {
  version?: string;
  os?: string;
};

export function formatRouteErrorReport({
  error,
  url,
  appInfo,
  trigger,
}: {
  error: unknown;
  url: string;
  appInfo?: DesktopAppInfo;
  trigger: string;
}) {
  const normalized = normalizeError(error);
  return [
    'kind: desktop_route_error',
    `trigger: ${trigger}`,
    `url: ${url}`,
    `app_version: ${appInfo?.version ?? 'unknown'}`,
    `runtime_os: ${appInfo?.os ?? 'unknown'}`,
    '',
    'context:',
    `- name: ${normalized.name}`,
    `- message: ${normalized.message}`,
    '',
    'stack:',
    '```',
    normalized.stack ?? '<no stack>',
    '```',
    '',
    'TODO: promote error context to structured feedback fields once the feedback API supports them.',
  ].join('\n');
}

function useRecoveryRoute(): string | null {
  const activeWorkspaceSlug = useTabStore((state) => state.activeWorkspaceSlug);
  return activeWorkspaceSlug ? `/${activeWorkspaceSlug}/issues` : null;
}

export function DesktopRouteErrorPage() {
  const error = useRouteError();

  if (isRouteErrorResponse(error) && error.status === 404) {
    return <DesktopNotFoundPage />;
  }
  return <DesktopUnexpectedErrorPage error={error} />;
}

function DesktopNotFoundPage() {
  const { t } = useT('settings');
  const location = useLocation();
  const recoveryRoute = useRecoveryRoute();

  return (
    <div
      role="alert"
      className="flex h-full min-h-[20rem] flex-col items-center justify-center gap-4 p-8 text-center"
    >
      <div className="rounded-full bg-muted p-3 text-muted-foreground">
        <Compass className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-2">
        <h2 className="text-lg font-semibold">{t(($) => $.desktop.route_error.not_found_title)}</h2>
        <p className="max-w-lg text-sm text-muted-foreground">
          {t(($) => $.desktop.route_error.not_found_description)}
        </p>
        <p className="max-w-lg truncate font-mono text-xs text-muted-foreground">
          {location.pathname}
        </p>
      </div>
      <div className="flex gap-2">
        {recoveryRoute ? (
          <Button
            type="button"
            variant="outline"
            onClick={() =>
              useTabStore.getState().navigateActiveSession(recoveryRoute, { replace: true })
            }
          >
            {t(($) => $.desktop.route_error.go_to_issues)}
          </Button>
        ) : null}
        <Button type="button" onClick={() => useTabStore.getState().closeActiveTab()}>
          <X className="mr-2 h-4 w-4" aria-hidden="true" />
          {t(($) => $.desktop.route_error.close_tab)}
        </Button>
      </div>
    </div>
  );
}

function DesktopUnexpectedErrorPage({ error }: { error: unknown }) {
  const { t } = useT('settings');
  const location = useLocation();
  const recoveryRoute = useRecoveryRoute();
  const report = useMemo(
    () =>
      formatRouteErrorReport({
        error,
        url:
          typeof window !== 'undefined'
            ? `${window.location.origin}${location.pathname}${location.search}${location.hash}`
            : location.pathname,
        appInfo: typeof window !== 'undefined' ? window.desktopAPI?.appInfo : undefined,
        trigger: 'route-errorElement',
      }),
    [error, location.hash, location.pathname, location.search],
  );
  const message = normalizeError(error).message;

  return (
    <div
      role="alert"
      className="flex h-full min-h-[20rem] flex-col items-center justify-center gap-4 p-8 text-center"
    >
      <div className="rounded-full bg-destructive/10 p-3 text-destructive">
        <AlertTriangle className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-2">
        <h2 className="text-lg font-semibold">{t(($) => $.desktop.route_error.unexpected_title)}</h2>
        <p className="max-w-lg text-sm text-muted-foreground">
          {t(($) => $.desktop.route_error.unexpected_description)}
        </p>
        <p className="max-w-lg truncate text-xs text-muted-foreground">{message}</p>
      </div>
      <div className="flex gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => useTabStore.getState().reloadActiveTab()}
        >
          <RotateCw className="mr-2 h-4 w-4" aria-hidden="true" />
          {t(($) => $.desktop.route_error.reload_tab)}
        </Button>
        {recoveryRoute ? (
          <Button
            type="button"
            variant="outline"
            onClick={() =>
              useTabStore.getState().navigateActiveSession(recoveryRoute, { replace: true })
            }
          >
            {t(($) => $.desktop.route_error.go_to_issues)}
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          onClick={() => useTabStore.getState().closeActiveTab()}
        >
          <X className="mr-2 h-4 w-4" aria-hidden="true" />
          {t(($) => $.desktop.route_error.close_tab)}
        </Button>
        <Button
          type="button"
          onClick={() =>
            useModalStore.getState().open('feedback', {
              initialMessage: report,
              kind: 'bug',
            })
          }
        >
          <Send className="mr-2 h-4 w-4" aria-hidden="true" />
          {t(($) => $.desktop.route_error.report_error)}
        </Button>
      </div>
    </div>
  );
}

function normalizeError(error: unknown): { name: string; message: string; stack?: string } {
  if (error instanceof Error) {
    return {
      name: error.name || 'Error',
      message: error.message || 'Unknown route error',
      stack: error.stack,
    };
  }
  if (typeof error === 'string') {
    return { name: 'Error', message: error };
  }
  return { name: 'Error', message: 'Unknown route error', stack: safeJson(error) };
}

function safeJson(value: unknown) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

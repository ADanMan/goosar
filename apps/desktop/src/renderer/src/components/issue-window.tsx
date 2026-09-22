import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { createMemoryRouter, RouterProvider, useParams, useRouteError } from 'react-router-dom';
import { AlertTriangle, RotateCw, X } from 'lucide-react';
import { useAuthStore } from '@goosar/core/auth';
import { setCurrentWorkspace } from '@goosar/core/platform';
import { WorkspaceSlugProvider } from '@goosar/core/paths';
import { workspaceBySlugOptions } from '@goosar/core/workspace';
import { Button } from '@goosar/ui/components/ui/button';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { ModalRegistry } from '@goosar/views/modals/registry';
import { RealtimeStatusIndicator, WorkspacePresencePrefetch } from '@goosar/views/layout';
import { DragStrip } from '@goosar/views/platform';
import type { IssueWindowContext } from '../../../shared/issue-window';
import { IssueDetailPage } from '../pages/issue-detail-page';
import { IssueWindowNavigationProvider } from '../platform/issue-window-navigation';
import { useT } from '@goosar/views/i18n';

export function IssueWindow({ context }: { context: IssueWindowContext }) {
  const router = useMemo(
    () =>
      createMemoryRouter(
        [
          {
            path: ':workspaceSlug/issues/:id',
            element: <IssueWindowRoute />,
            errorElement: <IssueWindowRouteError />,
          },
        ],
        { initialEntries: [context.path] },
      ),
    [context.path],
  );

  return <RouterProvider router={router} />;
}

function IssueWindowRoute() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>();
  const user = useAuthStore((state) => state.user);
  const { data: workspace, isFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug ?? ''),
    enabled: !!user && !!workspaceSlug,
  });

  if (workspace && workspaceSlug) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  }

  if (!isFetched) {
    return (
      <IssueWindowFrame>
        <div className="flex min-h-0 flex-1 items-center justify-center">
          <GoosarIcon className="size-6 animate-pulse" />
        </div>
      </IssueWindowFrame>
    );
  }

  if (!workspace || !workspaceSlug) {
    return <IssueWindowUnavailable />;
  }

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      <IssueWindowNavigationProvider>
        <WorkspacePresencePrefetch />
        <IssueWindowFrame>
          <IssueDetailPage onDelete={() => window.desktopAPI.closeWindow()} />
        </IssueWindowFrame>
        {/* The detached issue window is a separate top-level branch of the
         *  tree with no dashboard shell, so it mounted neither of the two
         *  indicators the shells carry. It IS under CoreProvider/WSProvider,
         *  so the degraded state is known here — it simply had nowhere to
         *  show. Without it this is the one window where a user cannot learn
         *  the connection is dead, and reads an unchanging sub-issue list as
         *  "the agent has not started" (#257). */}
        <RealtimeStatusIndicator />
        <ModalRegistry />
      </IssueWindowNavigationProvider>
    </WorkspaceSlugProvider>
  );
}

function IssueWindowFrame({ children }: { children: React.ReactNode }) {
  return (
    <div
      data-dedicated-issue-window="true"
      className="flex h-screen min-h-0 flex-col bg-page-canvas text-foreground"
    >
      <DragStrip />
      <div className="flex min-h-0 flex-1 overflow-hidden">{children}</div>
    </div>
  );
}

function IssueWindowUnavailable() {
  const { t } = useT('layout');
  return (
    <IssueWindowFrame>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-4 p-8 text-center">
        <div className="rounded-full bg-muted p-3 text-muted-foreground">
          <AlertTriangle className="size-6" aria-hidden="true" />
        </div>
        <div className="space-y-1">
          <h1 className="text-lg font-semibold">{t(($) => $.issue_window.unavailable_title)}</h1>
          <p className="text-sm text-muted-foreground">
            {t(($) => $.issue_window.unavailable_description)}
          </p>
        </div>
        <Button variant="outline" onClick={() => window.desktopAPI.closeWindow()}>
          {t(($) => $.issue_window.close_window)}
        </Button>
      </div>
    </IssueWindowFrame>
  );
}

function IssueWindowRouteError() {
  const { t } = useT('layout');
  const error = useRouteError();
  const message = error instanceof Error ? error.message : t(($) => $.issue_window.unknown_error);

  return (
    <IssueWindowFrame>
      <div
        role="alert"
        className="flex min-h-0 flex-1 flex-col items-center justify-center gap-4 p-8 text-center"
      >
        <div className="rounded-full bg-destructive/10 p-3 text-destructive">
          <AlertTriangle className="size-6" aria-hidden="true" />
        </div>
        <div className="space-y-1">
          <h1 className="text-lg font-semibold">{t(($) => $.issue_window.error_title)}</h1>
          <p className="max-w-lg truncate text-sm text-muted-foreground">{message}</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => window.location.reload()}>
            <RotateCw className="size-4" aria-hidden="true" />
            {t(($) => $.issue_window.reload)}
          </Button>
          <Button variant="outline" onClick={() => window.desktopAPI.closeWindow()}>
            <X className="size-4" aria-hidden="true" />
            {t(($) => $.issue_window.close_window)}
          </Button>
        </div>
      </div>
    </IssueWindowFrame>
  );
}

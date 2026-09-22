'use client';

import { ArrowLeft, LogOut } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@goosar/ui/components/ui/button';
import type { Workspace } from '@goosar/core/types';
import { useConfigStore } from '@goosar/core/config';
import { joinTargetListOptions } from '@goosar/core/workspace/queries';
import { useLogout } from '../auth';
import { DragStrip } from '../platform';
import { useT } from '../i18n';
import { CreateWorkspaceForm } from './create-workspace-form';

export function NewWorkspacePage({
  onSuccess,
  onBack,
  onJoinInstead,
}: {
  onSuccess: (workspace: Workspace) => void;
  onBack?: () => void;
  onJoinInstead?: () => void;
}) {
  const { t } = useT('workspace');
  const logout = useLogout();
  const workspaceCreationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);
  const { data: joinTargets = [] } = useQuery({
    ...joinTargetListOptions(),
    enabled: !!onJoinInstead,
  });
  const showJoinLink = !!onJoinInstead && joinTargets.length > 0;

  return (
    <div className="relative flex min-h-svh flex-col bg-background">
      <DragStrip />
      {onBack && (
        <Button
          variant="ghost"
          size="sm"
          className="absolute top-16 left-12 text-muted-foreground"
          onClick={onBack}
        >
          <ArrowLeft />
          {t(($) => $.new_page.back)}
        </Button>
      )}
      <Button
        variant="ghost"
        size="sm"
        className="absolute top-16 right-12 text-muted-foreground hover:text-destructive"
        onClick={logout}
      >
        <LogOut />
        {t(($) => $.new_page.log_out)}
      </Button>

      <div className="flex flex-1 flex-col items-center justify-center px-6 pb-12">
        <div className="flex w-full max-w-md flex-col items-center gap-6">
          {workspaceCreationDisabled ? (
            <div className="text-center">
              <h1 className="text-3xl font-semibold tracking-tight">
                {t(($) => $.creation_disabled.title)}
              </h1>
              <p className="mt-3 text-muted-foreground">
                {t(($) => $.creation_disabled.description)}
              </p>
            </div>
          ) : (
            <>
              <div className="text-center">
                <h1 className="text-3xl font-semibold tracking-tight">
                  {t(($) => $.new_page.title)}
                </h1>
                <p className="mt-3 text-muted-foreground">{t(($) => $.new_page.description)}</p>
              </div>
              <CreateWorkspaceForm onSuccess={onSuccess} />
              <p className="text-center text-xs text-muted-foreground">
                {t(($) => $.new_page.invite_hint)}
              </p>
            </>
          )}
          {showJoinLink && (
            <Button variant="link" className="text-muted-foreground" onClick={onJoinInstead}>
              {t(($) => $.new_page.join_instead)}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

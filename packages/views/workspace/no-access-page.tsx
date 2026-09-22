'use client';

import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@goosar/ui/components/ui/button';
import { resolvePostAuthDestination, useHasOnboarded } from '@goosar/core/paths';
import { workspaceListOptions } from '@goosar/core/workspace/queries';
import { useNavigation } from '../navigation';
import { useLogout } from '../auth';
import { DragStrip } from '../platform';
import { useT } from '../i18n';

export function NoAccessPage() {
  const { t } = useT('workspace');
  const nav = useNavigation();
  const logout = useLogout();
  const hasOnboarded = useHasOnboarded();
  const { data: workspaces = [] } = useQuery(workspaceListOptions());

  useEffect(() => {
    if (typeof document === 'undefined') return;
    document.cookie = 'last_workspace_slug=; path=/; max-age=0; SameSite=Lax';
  }, []);

  const recover = () => {
    nav.replace(resolvePostAuthDestination(workspaces, hasOnboarded));
  };

  return (
    <div className="flex min-h-svh flex-col">
      <DragStrip />
      <div className="flex flex-1 flex-col items-center justify-center gap-6 px-6 pb-12 text-center">
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight">{t(($) => $.no_access.title)}</h1>
          <p className="max-w-md text-muted-foreground">{t(($) => $.no_access.description)}</p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button onClick={recover}>{t(($) => $.no_access.go_to_workspaces)}</Button>
          <Button variant="outline" onClick={logout}>
            {t(($) => $.no_access.sign_in_different)}
          </Button>
        </div>
      </div>
    </div>
  );
}

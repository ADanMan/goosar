'use client';

import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigationStore } from '@goosar/core/navigation';
import { useAuthStore } from '@goosar/core/auth';
import {
  paths,
  resolvePostAuthDestination,
  useCurrentWorkspace,
  useHasOnboarded,
} from '@goosar/core/paths';
import { workspaceListOptions } from '@goosar/core/workspace';
import { useRecentIssuesStore } from '@goosar/core/issues/stores';
import { useNavigation } from '../navigation';

export function useDashboardGuard() {
  const { pathname, replace } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useCurrentWorkspace();
  const hasOnboarded = useHasOnboarded();
  const { data: workspaces = [], isFetched: workspaceListFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      replace(paths.login());
      return;
    }
    if (!workspaceListFetched) return;
    if (!workspace) {
      replace(resolvePostAuthDestination(workspaces, hasOnboarded));
    }
  }, [user, isLoading, workspaceListFetched, workspace, workspaces, hasOnboarded, replace]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  useEffect(() => {
    if (!workspaceListFetched) return;
    useRecentIssuesStore.getState().pruneWorkspaces(workspaces.map((w) => w.id));
  }, [workspaceListFetched, workspaces]);

  return { user, isLoading, workspace };
}

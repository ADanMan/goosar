'use client';

import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@goosar/core/auth';
import { workspaceListOptions } from '@goosar/core/workspace';
import { paths, resolvePostAuthDestination, useHasOnboarded } from '@goosar/core/paths';
import type { Workspace } from '@goosar/core/types';

const LOADING_FALLBACK_HREF = '/issues';

export function resolveDashboardCtaHref({
  isAuthenticated,
  isWorkspaceListFetched,
  workspaces,
  hasOnboarded,
}: {
  isAuthenticated: boolean;
  isWorkspaceListFetched: boolean;
  workspaces: Workspace[] | undefined;
  hasOnboarded: boolean;
}): string {
  if (!isAuthenticated) return paths.login();
  if (!isWorkspaceListFetched || !workspaces) return LOADING_FALLBACK_HREF;
  return resolvePostAuthDestination(workspaces, hasOnboarded);
}

export function useDashboardCtaHref(): string {
  const user = useAuthStore((s) => s.user);
  const hasOnboarded = useHasOnboarded();

  const { data, isFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  return resolveDashboardCtaHref({
    isAuthenticated: !!user,
    isWorkspaceListFetched: isFetched,
    workspaces: data,
    hasOnboarded,
  });
}

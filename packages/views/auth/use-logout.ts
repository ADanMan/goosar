'use client';

import { useCallback } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useAuthStore } from '@goosar/core/auth';
import { workspaceKeys } from '@goosar/core/workspace/queries';
import { clearWorkspaceStorage, defaultStorage } from '@goosar/core/platform';
import { resetAllRegisteredDrafts } from '@goosar/core/drafts/cleanup-registry';
import { paths } from '@goosar/core/paths';
import type { Workspace } from '@goosar/core/types';
import { useNavigation } from '../navigation';

export function useLogout() {
  const queryClient = useQueryClient();
  const authLogout = useAuthStore((s) => s.logout);
  const { push } = useNavigation();

  return useCallback(() => {
    resetAllRegisteredDrafts();

    const cachedWorkspaces = queryClient.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];
    for (const ws of cachedWorkspaces) {
      clearWorkspaceStorage(defaultStorage, ws.slug);
    }

    if (typeof document !== 'undefined') {
      document.cookie = 'last_workspace_slug=; path=/; max-age=0; SameSite=Lax';
    }

    defaultStorage.removeItem('goosar_tabs');

    queryClient.clear();
    authLogout();

    push(paths.login());
  }, [queryClient, authLogout, push]);
}

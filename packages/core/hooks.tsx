'use client';

import { useCurrentWorkspace } from './paths/hooks';

export function useWorkspaceId(): string {
  const ws = useCurrentWorkspace();
  if (!ws)
    throw new Error(
      'useWorkspaceId: no workspace selected — ensure component renders inside a workspace route',
    );
  return ws.id;
}

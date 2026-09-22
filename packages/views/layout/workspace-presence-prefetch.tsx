'use client';

import { useWorkspaceId } from '@goosar/core';
import { useWorkspacePresencePrefetch } from '@goosar/core/agents';

export function WorkspacePresencePrefetch() {
  const wsId = useWorkspaceId();
  useWorkspacePresencePrefetch(wsId);
  return null;
}

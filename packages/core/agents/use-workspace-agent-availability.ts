'use client';

import { useQuery } from '@tanstack/react-query';
import { useWorkspaceId } from '../hooks';
import { useAuthStore } from '../auth';
import { agentListOptions, memberListOptions } from '../workspace/queries';
import { canAssignAgentToIssue } from '../permissions';

export type WorkspaceAgentAvailability = 'loading' | 'none' | 'available';

export function useWorkspaceAgentAvailability(): WorkspaceAgentAvailability {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: agents, isFetched: agentsFetched } = useQuery(agentListOptions(wsId));
  const { data: members, isFetched: membersFetched } = useQuery(memberListOptions(wsId));

  if (!agentsFetched || !membersFetched) return 'loading';

  const rawRole = members?.find((m) => m.user_id === userId)?.role;
  const role = rawRole === 'owner' || rawRole === 'admin' || rawRole === 'member' ? rawRole : null;

  const hasVisibleAgent = (agents ?? []).some(
    (a) => !a.archived_at && canAssignAgentToIssue(a, { userId: userId ?? null, role }).allowed,
  );

  return hasVisibleAgent ? 'available' : 'none';
}

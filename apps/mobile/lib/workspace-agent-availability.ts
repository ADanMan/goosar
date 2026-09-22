// Трёхсостоянная доступность агентов для чата в текущем воркспейсе; зеркало
// веб-хука.
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@/data/auth-store';
import { useWorkspaceStore } from '@/data/workspace-store';
import { agentListOptions } from '@/data/queries/agents';
import { memberListOptions } from '@/data/queries/members';
import { canAssignAgent } from './can-assign-agent';

export type WorkspaceAgentAvailability = 'loading' | 'none' | 'available';

export function useWorkspaceAgentAvailability(): WorkspaceAgentAvailability {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id);

  const { data: agents, isFetched: agentsFetched } = useQuery(agentListOptions(wsId));
  const { data: members, isFetched: membersFetched } = useQuery(memberListOptions(wsId));

  if (!agentsFetched || !membersFetched) return 'loading';

  const memberRole = members?.find((m) => m.user_id === userId)?.role;

  const hasVisibleAgent = (agents ?? []).some(
    (a) => !a.archived_at && canAssignAgent(a, userId, memberRole),
  );

  return hasVisibleAgent ? 'available' : 'none';
}

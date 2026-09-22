import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api';
import { useWorkspaceId } from '../hooks';
import type { Agent } from '../types';
import { workspaceKeys } from '../workspace/queries';

export function useUpdateAgentAllowlist(agentId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation<Agent, Error, string[], { previous?: Agent[] }>({
    mutationFn: (allowlist) => api.updateAgent(agentId, { composio_toolkit_allowlist: allowlist }),
    onMutate: async (allowlist) => {
      const queryKey = workspaceKeys.agents(wsId);
      await qc.cancelQueries({ queryKey });
      const previous = qc.getQueryData<Agent[]>(queryKey);
      qc.setQueryData<Agent[]>(queryKey, (old) =>
        old?.map((a) =>
          a.id === agentId ? ({ ...a, composio_toolkit_allowlist: allowlist } as Agent) : a,
        ),
      );
      return { previous };
    },
    onError: (_error, _allowlist, context) => {
      if (context?.previous) {
        qc.setQueryData(workspaceKeys.agents(wsId), context.previous);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}

import { queryOptions } from '@tanstack/react-query';
import { api } from '../api';
import { issueKeys } from '../issues/queries';

export function agentCompletedIssueCountOptions(wsId: string) {
  return queryOptions({
    queryKey: [...issueKeys.all(wsId), 'agent-done-count'] as const,
    queryFn: async () => {
      const res = await api.listIssues({
        workspace_id: wsId,
        statuses: ['done'],
        assignee_types: ['agent', 'squad'],
        limit: 1,
      });
      return res.total;
    },
    staleTime: 30_000,
  });
}

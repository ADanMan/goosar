import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const agentTaskSnapshotOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["agent-task-snapshot", wsId] as const,
    queryFn: ({ signal }) => api.listAgentTaskSnapshot({ signal }),
    enabled: !!wsId,
  });

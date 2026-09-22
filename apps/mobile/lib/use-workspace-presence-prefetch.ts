// Прогрев трёх запросов присутствия агентов при входе в воркспейс, чтобы
// аватары не мигали без индикатора.
import { useQuery } from '@tanstack/react-query';
import { useWorkspaceStore } from '@/data/workspace-store';
import { agentListOptions } from '@/data/queries/agents';
import { runtimeListOptions } from '@/data/queries/runtimes';
import { agentTaskSnapshotOptions } from '@/data/queries/agent-task-snapshot';

export function useWorkspacePresencePrefetch(): void {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });
  useQuery({ ...agentTaskSnapshotOptions(wsId), enabled: !!wsId });
}

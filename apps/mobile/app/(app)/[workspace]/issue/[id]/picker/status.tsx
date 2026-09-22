// Выбор статуса для существующей задачи — formSheet. Самодостаточный маршрут:
// читает задачу из кэша, вызывает useUpdateIssue и делает router.back().
import { useLocalSearchParams, router } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { StatusPickerBody } from '@/components/issue/pickers/status-picker-body';
import { issueDetailOptions } from '@/data/queries/issues';
import { useUpdateIssue } from '@/data/mutations/issues';
import { useWorkspaceStore } from '@/data/workspace-store';

export default function IssueStatusPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, id));
  const updateIssue = useUpdateIssue(id);

  return (
    <StatusPickerBody
      value={issue?.status ?? 'todo'}
      onChange={(next) => {
        updateIssue.mutate({ status: next });
        router.back();
      }}
    />
  );
}

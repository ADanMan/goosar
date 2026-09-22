/**
 * Issues, принадлежащие проекту, — список со статусной группировкой.
 * Мобильный клиент сознательно не реализует канбан-доску web-версии:
 * экран телефона слишком узкий для нескольких колонок, а список с
 * группировкой — стандартный паттерн для этой задачи на мобильных.
 * Группировка использует тот же набор BOARD_STATUSES, что и web.
 */

import { useMemo } from 'react';
import { View } from 'react-native';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import type { Issue, IssueStatus } from '@goosar/core/types';
import { Text } from '@/components/ui/text';
import { Button } from '@/components/ui/button';
import { StatusIcon } from '@/components/ui/status-icon';
import { IssueRow } from '@/components/issue/issue-row';
import { IssuesLoading } from '@/components/issue/issues-loading';
import { projectIssuesOptions } from '@/data/queries/projects';
import { useWorkspaceStore } from '@/data/workspace-store';
import { BOARD_STATUSES, STATUS_LABEL } from '@/lib/issue-status';

interface Props {
  projectId: string;
}

export function ProjectRelatedIssues({ projectId }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isLoading, error, refetch } = useQuery(projectIssuesOptions(wsId, projectId));

  const byStatus = useMemo(() => {
    const m = new Map<IssueStatus, Issue[]>();
    for (const status of BOARD_STATUSES) m.set(status, []);
    for (const issue of data ?? []) {
      const list = m.get(issue.status);
      if (list) list.push(issue);
    }
    return m;
  }, [data]);

  const navigateToIssue = (id: string) => {
    if (wsSlug) router.push(`/${wsSlug}/issue/${id}`);
  };

  if (isLoading) return <IssuesLoading />;

  if (error) {
    return (
      <View className="px-4 py-6 gap-3">
        <Text className="text-sm text-destructive">
          Failed to load issues: {error instanceof Error ? error.message : 'unknown error'}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  if ((data?.length ?? 0) === 0) {
    return (
      <View className="px-4 py-6">
        <Text className="text-sm text-muted-foreground">No issues yet.</Text>
      </View>
    );
  }

  return (
    <View>
      {BOARD_STATUSES.map((status) => {
        const issues = byStatus.get(status) ?? [];
        if (issues.length === 0) return null;
        return (
          <View key={status}>
            <SectionHeader status={status} count={issues.length} />
            {issues.map((issue) => (
              <IssueRow key={issue.id} issue={issue} onPress={() => navigateToIssue(issue.id)} />
            ))}
          </View>
        );
      })}
    </View>
  );
}

function SectionHeader({ status, count }: { status: IssueStatus; count: number }) {
  return (
    <View className="flex-row items-center gap-2 px-4 py-2 bg-background">
      <StatusIcon status={status} size={14} />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {STATUS_LABEL[status]}
      </Text>
      <Text className="text-xs text-muted-foreground/60">{count}</Text>
    </View>
  );
}

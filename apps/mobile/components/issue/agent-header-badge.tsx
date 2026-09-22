/**
 * Бейдж статуса агента в шапке экрана задачи (справа). Виден только
 * когда есть хотя бы одна активная задача агента — нужен, потому что
 * строка AgentActivityRow в карточке уходит из виду при скролле, а
 * запуски агента могут идти долго.
 *
 * Тап открывает тот же formSheet-роут со списком запусков.
 */
import { Pressable } from 'react-native';
import { router } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { AvatarStack, type StackActor } from '@/components/ui/avatar-stack';
import { PulseDot } from '@/components/ui/pulse-dot';
import { issueActiveTasksOptions } from '@/data/queries/issues';
import { useWorkspaceStore } from '@/data/workspace-store';

interface Props {
  issueId: string;
}

export function AgentHeaderBadge({ issueId }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data: active = [] } = useQuery(issueActiveTasksOptions(wsId, issueId));

  if (active.length === 0) return null;

  const actors = active.map<StackActor>((t) => ({
    type: 'agent',
    id: t.agent_id,
  }));

  return (
    <Pressable
      onPress={() => {
        if (!wsSlug) return;
        router.push({
          pathname: '/[workspace]/issue/[id]/runs',
          params: { workspace: wsSlug, id: issueId },
        });
      }}
      hitSlop={8}
      accessibilityLabel="Agent working — open runs"
      className="flex-row items-center gap-1.5 px-2 py-1 active:opacity-60"
    >
      <AvatarStack actors={actors} max={2} size={20} />
      <PulseDot size={6} />
    </Pressable>
  );
}

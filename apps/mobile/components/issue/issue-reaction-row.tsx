/**
 * Ряд реакций на уровне задачи, сразу под описанием. Читает issue.reactions
 * из кэша детали, переданного родителем — отдельного запроса нет. Ничего не
 * рендерит при отсутствии реакций; добавление реакции пока не реализовано.
 */
import { useCallback } from 'react';
import { View } from 'react-native';
import type { Issue, IssueReaction } from '@goosar/core/types';
import { ReactionBar } from './reaction-bar';
import { useToggleIssueReaction } from '@/data/mutations/issues';
import { useAuthStore } from '@/data/auth-store';

export function IssueReactionRow({ issue }: { issue: Issue }) {
  const userId = useAuthStore((s) => s.user?.id);
  const reactions: IssueReaction[] = issue.reactions ?? [];
  const toggle = useToggleIssueReaction(issue.id);

  const onToggle = useCallback(
    (emoji: string) => {
      const existing = reactions.find(
        (r) => r.emoji === emoji && r.actor_type === 'member' && r.actor_id === userId,
      );
      toggle.mutate({ emoji, existing });
    },
    [reactions, userId, toggle],
  );

  if (reactions.length === 0) return null;

  return (
    <View className="px-4 pb-3">
      <ReactionBar reactions={reactions} currentUserId={userId} onToggle={onToggle} />
    </View>
  );
}

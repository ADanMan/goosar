/**
 * Общий ряд задачи для всех списочных экранов: мои задачи, задачи
 * воркспейса, связанные задачи проекта. Верстка: [статус?] приоритет
 * идентификатор заголовок … исполнитель. showStatus включается по
 * необходимости — там, где список уже сгруппирован по статусу, повторный
 * значок не нужен. Тип Issue и семантика assignee_type/assignee_id
 * совпадают с веб-версией; исполнитель рендерится, когда оба поля заданы.
 */
import { Pressable, View } from 'react-native';
import type { Issue } from '@goosar/core/types';
import { Text } from '@/components/ui/text';
import { ActorAvatar } from '@/components/ui/actor-avatar';
import { PriorityIcon } from '@/components/ui/priority-icon';
import { StatusIcon } from '@/components/ui/status-icon';

interface Props {
  issue: Issue;
  onPress: () => void;
  showStatus?: boolean;
}

export function IssueRow({ issue, onPress, showStatus = false }: Props) {
  return (
    <Pressable onPress={onPress} className="active:bg-secondary px-4 py-3">
      <View className="flex-row items-center gap-3">
        {showStatus ? <StatusIcon status={issue.status} size={14} /> : null}
        <PriorityIcon priority={issue.priority} size={14} />
        <Text className="text-xs text-muted-foreground shrink-0 w-16">{issue.identifier}</Text>
        <Text className="flex-1 text-sm text-foreground" numberOfLines={1}>
          {issue.title}
        </Text>
        {issue.assignee_type && issue.assignee_id ? (
          <ActorAvatar type={issue.assignee_type} id={issue.assignee_id} size={20} showPresence />
        ) : null}
      </View>
    </Pressable>
  );
}

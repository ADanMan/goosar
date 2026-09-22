/**
 * Компактная шапка экрана задачи: идентификатор мелким лейблом сверху,
 * крупный заголовок и ряд чипов атрибутов (статус, приоритет, исполнитель,
 * лейблы, проект, срок) с открытием пикеров по тапу. Нативный iOS-заголовок
 * навигации всё ещё показывает identifier, тело просто выделяет его заметнее.
 */
import { View } from 'react-native';
import type { Issue } from '@goosar/core/types';
import { Text } from '@/components/ui/text';
import { AttributeRow } from './attribute-row';
import { AgentActivityRow } from './agent-activity-row';

export function IssueHeaderCard({ issue }: { issue: Issue }) {
  return (
    <View className="px-4 pt-4 pb-3 gap-3">
      <Text className="text-xs text-muted-foreground">{issue.identifier}</Text>
      <Text className="text-2xl font-bold text-foreground">{issue.title}</Text>
      {/* Activity row sits between title and attributes — it represents
       *  "who's doing this issue right now / who has done it" (dynamic),
       *  which is higher-IA than the static property chips below.
       *  Conditionally renders null when there are no tasks at all. */}
      <AgentActivityRow issueId={issue.id} />
      <AttributeRow issue={issue} />
    </View>
  );
}

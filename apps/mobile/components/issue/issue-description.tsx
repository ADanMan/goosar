/**
 * Блок описания задачи. Рендерит markdown мобильным рендерером. Пустое
 * описание показывает плейсхолдер вместо схлопывания блока, чтобы верстка
 * над таймлайном не прыгала. Вложения запрашиваются отдельно на задачу,
 * чтобы markdown мог превращать mc://file/<id> в реальные download_url —
 * без этого картинки не грузятся на iOS. TanStack Query дедуплицирует
 * запрос между этим компонентом и CommentCard.
 */
import { View } from 'react-native';
import { useQuery } from '@tanstack/react-query';
import { Text } from '@/components/ui/text';
import { Markdown } from '@/lib/markdown';
import { issueAttachmentsOptions } from '@/data/queries/issues';
import { useWorkspaceStore } from '@/data/workspace-store';

export function IssueDescription({
  issueId,
  description,
}: {
  issueId: string;
  description: string | null;
}) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: attachments } = useQuery(issueAttachmentsOptions(wsId, issueId));

  if (!description || description.trim().length === 0) {
    return (
      <View className="px-4 pb-4">
        <Text className="text-sm text-muted-foreground italic">No description.</Text>
      </View>
    );
  }
  return (
    <View className="px-4 pb-4">
      <Markdown content={description} attachments={attachments} />
    </View>
  );
}

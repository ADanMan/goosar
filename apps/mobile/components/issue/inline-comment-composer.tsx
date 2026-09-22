/**
 * Композер комментария к задаче — тонкая обёртка над общим
 * MessageComposer со своей привязкой: onSubmit ведёт в useCreateComment,
 * цель ответа берётся из useReplyTargetStore, пикер mention открывается
 * по своему маршруту, вложения привязываются к этой задаче. Вся логика
 * UI и состояния — в MessageComposer, тем же компонентом пользуется и
 * композер чата.
 */
import { useCallback } from 'react';
import { useCreateComment } from '@/data/mutations/issues';
import { useReplyTargetStore } from '@/data/stores/reply-target-store';
import { useWorkspaceStore } from '@/data/workspace-store';
import { MessageComposer } from '@/components/composer/message-composer';

export function InlineCommentComposer({ issueId }: { issueId: string }) {
  const createComment = useCreateComment(issueId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const replyTarget = useReplyTargetStore((s) => s.target);
  const clearReplyTarget = useReplyTargetStore((s) => s.clear);

  const onSubmit = useCallback(
    async ({ content, attachmentIds }: { content: string; attachmentIds: string[] }) => {
      try {
        await createComment.mutateAsync({
          content,
          parentId: replyTarget?.commentId,
          attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
        });
      } catch (err) {
        throw err;
      }
    },
    [createComment, replyTarget?.commentId],
  );

  return (
    <MessageComposer
      onSubmit={onSubmit}
      mentionPickerPath={{
        pathname: '/[workspace]/mention-picker',
        params: { workspace: wsSlug ?? '', mode: 'comment' },
      }}
      uploadContext={{ issueId }}
      placeholder="Add a comment…"
      pillLabel="Add a comment, @ to mention…"
      pillIcon="chatbubble-ellipses-outline"
      replyTarget={
        replyTarget
          ? {
              actorName: replyTarget.actorName,
              preview: replyTarget.preview,
            }
          : null
      }
      onClearReplyTarget={clearReplyTarget}
      expandTrigger={replyTarget?.commentId ?? null}
    />
  );
}

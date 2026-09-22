/**
 * Обработчик долгого нажатия на пузырь комментария: отдаёт onLongPress
 * (нативный ActionSheetIOS) и isPressed (подсветка на время показа).
 *
 * Пункты: Reply · React… · Copy · Select Text · Copy Link ·
 * Resolve/Unresolve (только корень треда) · Delete (только своё) · Cancel.
 * Вложенная шторка React… открывается из колбэка завершения основной —
 * iOS не даёт показать второй ActionSheet, пока первый ещё закрывается.
 */
import { useCallback, useState } from 'react';
import { ActionSheetIOS, Alert } from 'react-native';
import { router } from 'expo-router';
import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import type { Reaction, TimelineEntry } from '@goosar/core/types';
import { useAuthStore } from '@/data/auth-store';
import { useWorkspaceStore } from '@/data/workspace-store';
import { useCommentSelectStore } from '@/data/comment-select-store';
import { useReplyTargetStore } from '@/data/stores/reply-target-store';
import { useActorLookup } from '@/data/use-actor-name';
import {
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
} from '@/data/mutations/issues';
import { QUICK_EMOJIS } from '@/lib/quick-emojis';

const QUICK_ROW_SIZE = 5;

export function useCommentLongPress(
  entry: TimelineEntry,
  issueId: string,
  issueIdentifier: string | undefined,
): { onLongPress: () => void; isPressed: boolean } {
  const [isPressed, setIsPressed] = useState(false);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id);
  const toggleReaction = useToggleCommentReaction(issueId);
  const deleteComment = useDeleteComment(issueId);
  const resolveComment = useResolveComment(issueId);
  const { getName } = useActorLookup();

  const onLongPress = useCallback(() => {
    const isOwn = entry.actor_type === 'member' && entry.actor_id === userId;
    const isRoot = !entry.parent_id;
    const resolved = !!entry.resolved_at;
    const hasContent = !!entry.content;
    const webUrl = process.env.EXPO_PUBLIC_WEB_URL;
    const canCopyLink = !!(webUrl && wsSlug && issueIdentifier);
    const reactions = (entry.reactions ?? []) as Reaction[];

    Haptics.selectionAsync().catch(() => {});
    setIsPressed(true);

    type Action =
      | { kind: 'reply' }
      | { kind: 'react' }
      | { kind: 'copy' }
      | { kind: 'select' }
      | { kind: 'copyLink' }
      | { kind: 'resolve' }
      | { kind: 'delete' }
      | { kind: 'cancel' };

    const options: string[] = [];
    const actions: Action[] = [];
    const push = (label: string, action: Action) => {
      options.push(label);
      actions.push(action);
    };

    push('Reply', { kind: 'reply' });
    push('React…', { kind: 'react' });
    if (hasContent) {
      push('Copy', { kind: 'copy' });
      push('Select Text', { kind: 'select' });
    }
    if (canCopyLink) push('Copy Link', { kind: 'copyLink' });
    if (isRoot) {
      push(resolved ? 'Unresolve Thread' : 'Resolve Thread', {
        kind: 'resolve',
      });
    }
    if (isOwn) push('Delete', { kind: 'delete' });
    push('Cancel', { kind: 'cancel' });

    const cancelButtonIndex = options.length - 1;
    const destructiveButtonIndex = isOwn
      ? actions.findIndex((a) => a.kind === 'delete')
      : undefined;

    ActionSheetIOS.showActionSheetWithOptions(
      {
        options,
        cancelButtonIndex,
        ...(destructiveButtonIndex !== undefined && destructiveButtonIndex >= 0
          ? { destructiveButtonIndex }
          : {}),
      },
      (i) => {
        setIsPressed(false);
        const action = actions[i];
        if (!action || action.kind === 'cancel') return;

        switch (action.kind) {
          case 'reply': {
            const actorName = getName(
              entry.actor_type as 'member' | 'agent' | null | undefined,
              entry.actor_id,
            );
            useReplyTargetStore.getState().setTarget({
              commentId: entry.id,
              actorName: actorName || 'comment',
              preview: entry.content ?? '',
            });
            return;
          }
          case 'react':
            presentReactSheet({
              entry,
              reactions,
              userId,
              wsSlug,
              issueId,
              toggle: (emoji, existing) =>
                toggleReaction.mutate({
                  commentId: entry.id,
                  emoji,
                  existing,
                }),
            });
            return;
          case 'copy':
            if (entry.content) {
              Clipboard.setStringAsync(entry.content);
              Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
            }
            return;
          case 'select':
            useCommentSelectStore.getState().setSelecting(entry.id);
            return;
          case 'copyLink': {
            if (!canCopyLink) return;
            const url = `${webUrl}/${wsSlug}/issue/${issueIdentifier}#comment-${entry.id}`;
            Clipboard.setStringAsync(url);
            Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
            return;
          }
          case 'resolve':
            resolveComment.mutate({
              commentId: entry.id,
              resolved: !entry.resolved_at,
            });
            return;
          case 'delete':
            Alert.alert(
              'Delete comment?',
              'This comment will be permanently deleted. Replies in the thread will also be removed. This cannot be undone.',
              [
                { text: 'Cancel', style: 'cancel' },
                {
                  text: 'Delete',
                  style: 'destructive',
                  onPress: () => deleteComment.mutate(entry.id),
                },
              ],
            );
            return;
        }
      },
    );
  }, [
    entry,
    issueId,
    issueIdentifier,
    userId,
    wsSlug,
    toggleReaction,
    deleteComment,
    resolveComment,
  ]);

  return { onLongPress, isPressed };
}

function presentReactSheet(args: {
  entry: TimelineEntry;
  reactions: Reaction[];
  userId: string | undefined;
  wsSlug: string | null;
  issueId: string;
  toggle: (emoji: string, existing: Reaction | undefined) => void;
}) {
  const { entry, reactions, userId, wsSlug, issueId, toggle } = args;
  const emojis = QUICK_EMOJIS.slice(0, QUICK_ROW_SIZE);
  const options = [...emojis, 'More reactions…', 'Cancel'];
  const cancelButtonIndex = options.length - 1;

  ActionSheetIOS.showActionSheetWithOptions({ options, cancelButtonIndex }, (i) => {
    if (i === cancelButtonIndex) return;
    if (i === emojis.length) {
      if (!wsSlug) return;
      router.push({
        pathname: '/[workspace]/issue/[id]/comment/[commentId]/emoji-picker',
        params: {
          workspace: wsSlug,
          id: issueId,
          commentId: entry.id,
        },
      });
      return;
    }
    const emoji = emojis[i];
    if (!emoji) return;
    const existing = reactions.find(
      (r) => r.emoji === emoji && r.actor_type === 'member' && r.actor_id === userId,
    );
    toggle(emoji, existing);
  });
}

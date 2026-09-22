/**
 * Строка таймлайна комментария: пузырь с родительским комментарием и
 * всеми ответами внутри — граница пузыря сама служит индикатором треда,
 * без заголовка «Replying to» и вложенных отступов.
 *
 * Долгое нажатие на пузырь открывает нативный ActionSheetIOS с действиями
 * (Reply, React…, Copy, Select Text, Copy Link, Resolve, Delete).
 * Свёрнутые треды показываются как отдельная строка ResolvedThreadBar.
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Pressable, View } from 'react-native';
import Animated, {
  useAnimatedStyle,
  useSharedValue,
  withDelay,
  withSequence,
  withTiming,
} from 'react-native-reanimated';
import { Ionicons } from '@expo/vector-icons';
import type { Reaction, TimelineEntry } from '@goosar/core/types';
import { Text } from '@/components/ui/text';
import { ActorAvatar } from '@/components/ui/actor-avatar';
import { useActorLookup } from '@/data/use-actor-name';
import { timeAgo } from '@/lib/time-ago';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Markdown } from '@/lib/markdown';
import { CommentAttachmentList } from '@/components/issue/comment-attachment-list';
import {
  discardFailedComment,
  useCreateComment,
  useToggleCommentReaction,
} from '@/data/mutations/issues';
import { useAuthStore } from '@/data/auth-store';
import { useWorkspaceStore } from '@/data/workspace-store';
import { issueAttachmentsOptions } from '@/data/queries/issues';
import { useFailedCommentsStore } from '@/data/stores/failed-comments-store';
import { useColorScheme } from '@/lib/use-color-scheme';
import { THEME } from '@/lib/theme';
import { cn } from '@/lib/utils';
import { ReactionBar } from './reaction-bar';
import { useCommentLongPress } from './comment-context-menu';
import { useCommentSelectStore } from '@/data/comment-select-store';

interface Props {
  entry: TimelineEntry;
  replies?: TimelineEntry[];
  issueId: string;
  issueIdentifier: string | undefined;
  highlightedCommentId?: string | null;
}

export function CommentCard({
  entry,
  replies = [],
  issueId,
  issueIdentifier,
  highlightedCommentId,
}: Props) {
  const resolved = !!entry.resolved_at;
  const [expanded, setExpanded] = useState(false);
  const [pressedEntryId, setPressedEntryId] = useState<string | null>(null);
  const handlePressChange = useCallback((entryId: string, pressed: boolean) => {
    setPressedEntryId((cur) => {
      if (pressed) return entryId;
      return cur === entryId ? null : cur;
    });
  }, []);
  const isHighlighted = pressedEntryId === entry.id || replies.some((r) => r.id === pressedEntryId);
  const selectingId = useCommentSelectStore((s) => s.selectingId);
  const isSelectingHere = selectingId === entry.id || replies.some((r) => r.id === selectingId);

  useEffect(() => {
    if (!resolved || !highlightedCommentId) return;
    if (highlightedCommentId === entry.id || replies.some((r) => r.id === highlightedCommentId)) {
      setExpanded(true);
    }
  }, [resolved, highlightedCommentId, entry.id, replies]);

  if (resolved && !expanded) {
    return <ResolvedThreadBar entry={entry} replies={replies} onExpand={() => setExpanded(true)} />;
  }

  return (
    <View className="px-4">
      <View className="rounded-2xl">
        {/* Bubble uses `surface-1` (L 98%) — extremely subtle elevation
         *  above the page, visible mostly through the rounded edge rather
         *  than the fill (iOS settings cell feel; see Refactoring UI #4
         *  "cards subtle from page"). Internal markdown elements (table
         *  headers / code blocks via markdown-style.ts) use `surface-2`
         *  (L 90%), 8% darker than the bubble — well over the 5%
         *  perceptibility threshold so the inner box is clearly framed.
         *  Border (L 84%) adds 6% on top for the outline. See global.css
         *  for the full 5-tier elevation scale.
         *
         *  Resolved-and-expanded path dims the bubble to 70% so the
         *  "this is settled" signal persists even while reading the
         *  body — mirrors web's muted resolved card visual. */}
        <View
          className={cn(
            'bg-surface-1 rounded-2xl px-4 py-3 gap-3 border-2 border-transparent transition-colors',
            resolved && 'opacity-70',
            isHighlighted && 'border-primary/30',
            isSelectingHere && 'bg-primary/5 border-primary/30',
          )}
        >
          {resolved ? (
            <ResolvedIndicator entry={entry} onCollapse={() => setExpanded(false)} />
          ) : null}
          <CommentBody
            entry={entry}
            issueId={issueId}
            issueIdentifier={issueIdentifier}
            onPressChange={handlePressChange}
          />
          {replies.map((reply) => (
            <View key={reply.id} className="border-t border-border/60 pt-3">
              <CommentBody
                entry={reply}
                issueId={issueId}
                issueIdentifier={issueIdentifier}
                onPressChange={handlePressChange}
              />
              <ReplyHighlightOverlay active={highlightedCommentId === reply.id} />
            </View>
          ))}
        </View>
        <RootHighlightOverlay active={highlightedCommentId === entry.id} />
      </View>
    </View>
  );
}

function ResolvedThreadBar({
  entry,
  replies,
  onExpand,
}: {
  entry: TimelineEntry;
  replies: TimelineEntry[];
  onExpand: () => void;
}) {
  const { getName } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const authorsLabel = useMemo(() => {
    const MAX_NAMED = 2;
    const seen = new Set<string>();
    const ordered: { type: string | null; id: string | null }[] = [];
    for (const e of [entry, ...replies]) {
      const key = `${e.actor_type}:${e.actor_id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      ordered.push({ type: e.actor_type, id: e.actor_id });
    }
    const named = ordered
      .slice(0, MAX_NAMED)
      .map((a) => getName(a.type as 'member' | 'agent' | null | undefined, a.id))
      .join(', ');
    const remaining = ordered.length - MAX_NAMED;
    return remaining > 0 ? `${named} +${remaining}` : named;
  }, [entry, replies, getName]);

  const total = 1 + replies.length;

  return (
    <View className="px-4">
      <Pressable
        onPress={onExpand}
        className="flex-row items-center gap-2.5 px-4 py-3 rounded-2xl bg-surface-1 active:opacity-70"
        accessibilityRole="button"
        accessibilityLabel={`Resolved thread by ${authorsLabel}, ${total} ${total === 1 ? 'message' : 'messages'}. Tap to expand.`}
      >
        <Ionicons name="checkmark-circle" size={18} color={mutedFg} />
        <Text className="flex-1 text-sm text-muted-foreground" numberOfLines={1}>
          Resolved · {total} {total === 1 ? 'message' : 'messages'} by {authorsLabel}
        </Text>
        <Ionicons name="chevron-down" size={14} color={mutedFg} />
      </Pressable>
    </View>
  );
}

function ResolvedIndicator({
  entry,
  onCollapse,
}: {
  entry: TimelineEntry;
  onCollapse: () => void;
}) {
  const { getName } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const resolverName = getName(
    entry.resolved_by_type as 'member' | 'agent' | null | undefined,
    entry.resolved_by_id,
  );

  return (
    <Pressable
      onPress={onCollapse}
      className="flex-row items-center gap-2 active:opacity-60"
      accessibilityRole="button"
      accessibilityLabel="Collapse resolved thread"
    >
      <Ionicons name="checkmark-circle" size={14} color={mutedFg} />
      <Text className="text-xs text-muted-foreground flex-1" numberOfLines={1}>
        Resolved by <Text className="text-xs text-foreground font-medium">{resolverName}</Text>
        {entry.resolved_at ? ` · ${timeAgo(entry.resolved_at)}` : ''}
      </Text>
      <Text className="text-xs text-muted-foreground">Collapse</Text>
    </Pressable>
  );
}

function RootHighlightOverlay({ active }: { active: boolean }) {
  const progress = useSharedValue(0);

  useEffect(() => {
    if (!active) return;
    progress.value = withSequence(
      withTiming(1, { duration: 700 }),
      withDelay(1800, withTiming(0, { duration: 700 })),
    );
  }, [active, progress]);

  const style = useAnimatedStyle(() => ({ opacity: progress.value }));

  return (
    <Animated.View
      pointerEvents="none"
      className="absolute inset-0 rounded-2xl border-2 border-brand/50 bg-brand/5"
      style={style}
    />
  );
}

function ReplyHighlightOverlay({ active }: { active: boolean }) {
  const progress = useSharedValue(0);

  useEffect(() => {
    if (!active) return;
    progress.value = withSequence(
      withTiming(1, { duration: 700 }),
      withDelay(1800, withTiming(0, { duration: 700 })),
    );
  }, [active, progress]);

  const style = useAnimatedStyle(() => ({ opacity: progress.value }));

  return (
    <Animated.View pointerEvents="none" className="absolute inset-0 bg-brand/5" style={style} />
  );
}

function CommentBody({
  entry,
  issueId,
  issueIdentifier,
  onPressChange,
}: {
  entry: TimelineEntry;
  issueId: string;
  issueIdentifier: string | undefined;
  onPressChange?: (entryId: string, pressed: boolean) => void;
}) {
  const isSelecting = useCommentSelectStore((s) => s.selectingId === entry.id);
  const { getName } = useActorLookup();
  const userId = useAuthStore((s) => s.user?.id);
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const toggle = useToggleCommentReaction(issueId);
  const qc = useQueryClient();
  const createComment = useCreateComment(issueId);
  const failed = useFailedCommentsStore((s) => s.failed[entry.id]);
  const { data: attachments } = useQuery(issueAttachmentsOptions(wsId, issueId));

  const name = getName(entry.actor_type as 'member' | 'agent' | null | undefined, entry.actor_id);
  const edited = entry.updated_at && entry.created_at && entry.updated_at !== entry.created_at;

  const reactions: Reaction[] = (entry.reactions ?? []) as Reaction[];

  const onToggleReaction = useCallback(
    (emoji: string) => {
      const existing = reactions.find(
        (r) => r.emoji === emoji && r.actor_type === 'member' && r.actor_id === userId,
      );
      toggle.mutate({ commentId: entry.id, emoji, existing });
    },
    [reactions, userId, toggle, entry.id],
  );

  const handleRetry = useCallback(() => {
    if (!failed || !wsId) return;
    discardFailedComment(qc, wsId, issueId, entry.id);
    createComment.mutate({
      content: failed.content,
      parentId: failed.parentId,
      attachmentIds: failed.attachmentIds,
    });
  }, [failed, qc, wsId, issueId, entry.id, createComment]);

  const handleDiscard = useCallback(() => {
    if (!wsId) return;
    discardFailedComment(qc, wsId, issueId, entry.id);
  }, [qc, wsId, issueId, entry.id]);

  const longPress = useCommentLongPress(entry, issueId, issueIdentifier);

  useEffect(() => {
    if (isSelecting) return;
    onPressChange?.(entry.id, longPress.isPressed);
  }, [longPress.isPressed, entry.id, isSelecting, onPressChange]);

  const body = (
    <View className="gap-2">
      <View className="flex-row items-center gap-2">
        <ActorAvatar
          type={entry.actor_type as 'member' | 'agent'}
          id={entry.actor_id}
          size={24}
          showPresence
        />
        <Text className="text-sm font-medium text-foreground">{name}</Text>
        <Text className="text-xs text-muted-foreground">
          · {timeAgo(entry.created_at)}
          {edited ? ' · (edited)' : ''}
        </Text>
      </View>
      {entry.content ? (
        <Markdown content={entry.content} attachments={attachments} selectable={isSelecting} />
      ) : null}
      <CommentAttachmentList attachments={entry.attachments} content={entry.content} />
      {failed ? (
        <FailedActions error={failed.error} onRetry={handleRetry} onDiscard={handleDiscard} />
      ) : (
        <ReactionBar reactions={reactions} currentUserId={userId} onToggle={onToggleReaction} />
      )}
    </View>
  );

  if (isSelecting) return body;

  return (
    <Pressable onLongPress={longPress.onLongPress} delayLongPress={500}>
      {body}
    </Pressable>
  );
}

function FailedActions({
  error,
  onRetry,
  onDiscard,
}: {
  error: string;
  onRetry: () => void;
  onDiscard: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const destructive = THEME[colorScheme].destructive;
  return (
    <View className="flex-row items-center gap-2 mt-0.5">
      <Ionicons name="alert-circle" size={14} color={destructive} />
      <Text className="flex-1 text-xs text-destructive" numberOfLines={1}>
        {error || "Couldn't send"}
      </Text>
      <Pressable
        onPress={onRetry}
        hitSlop={6}
        accessibilityRole="button"
        accessibilityLabel="Retry sending comment"
      >
        <Text className="text-xs text-primary font-medium">Retry</Text>
      </Pressable>
      <Pressable
        onPress={onDiscard}
        hitSlop={6}
        accessibilityRole="button"
        accessibilityLabel="Discard failed comment"
      >
        <Text className="text-xs text-muted-foreground font-medium">Discard</Text>
      </Pressable>
    </View>
  );
}

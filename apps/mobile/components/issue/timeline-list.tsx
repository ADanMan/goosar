/**
 * Хронологическая лента таймлайна issue — от старых сообщений вверху до
 * новых внизу, над композером. Pull-to-refresh перезапрашивает issue и
 * таймлайн целиком (сервер отдаёт весь таймлайн одним ответом).
 *
 * Построена на FlashList v2: recycling ячеек с markdown-контентом и
 * нативный maintainVisibleContentPosition компенсируют изменение высоты
 * верхних строк при WS-событиях без дёрганья скролла.
 *
 * При диплинке на конкретный комментарий лента приземляется внизу и
 * подсвечивает нужную строку, когда та попадает в окно рендера, —
 * точный scrollToIndex по оценочным высотам markdown-пузырей ненадёжен.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  View,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type ViewToken,
} from 'react-native';
import { FlashList, type FlashListRef } from '@shopify/flash-list';
import { Ionicons } from '@expo/vector-icons';
import type { Issue, TimelineEntry } from '@goosar/core/types';
import { Text } from '@/components/ui/text';
import { IssueHeaderCard } from './issue-header-card';
import { IssueDescription } from './issue-description';
import { IssueReactionRow } from './issue-reaction-row';
import { ActivityRow } from './activity-row';
import { CommentCard } from './comment-card';
import { useLastViewedStore } from '@/data/stores/last-viewed-store';
import { coalesceTimeline } from '@/lib/timeline-coalesce';
import { buildTimelineRows, type TimelineRow } from '@/lib/timeline-thread';
import { useColorScheme } from '@/lib/use-color-scheme';
import { THEME } from '@/lib/theme';
import { useCommentSelectStore } from '@/data/comment-select-store';

interface Props {
  issue: Issue;
  entries: TimelineEntry[] | undefined;
  timelineLoading: boolean;
  refreshing: boolean;
  onRefresh: () => void;
  highlightCommentId?: string;
  highlightNonce?: string;
}

const HIGHLIGHT_HOLD_MS = 5000;

const AT_BOTTOM_SLACK_PX = 80;

const DIVIDER_ID = '__divider__';

export function TimelineList({
  issue,
  entries,
  timelineLoading,
  refreshing,
  onRefresh,
  highlightCommentId,
  highlightNonce,
}: Props) {
  const selectingId = useCommentSelectStore((s) => s.selectingId);

  const data = useMemo<TimelineRow[]>(() => {
    if (!entries) return [];
    return buildTimelineRows(coalesceTimeline(entries));
  }, [entries]);

  const listRef = useRef<FlashListRef<TimelineRow>>(null);
  const lastStampRef = useRef<string | null>(null);
  const [highlightedId, setHighlightedId] = useState<string | null>(null);

  const lastViewedSnapshotRef = useRef<string | null | undefined>(undefined);
  if (lastViewedSnapshotRef.current === undefined) {
    lastViewedSnapshotRef.current = useLastViewedStore.getState().getLastViewed(issue.id) ?? null;
  }
  const dividerAnchorId = useMemo(() => {
    const snapshot = lastViewedSnapshotRef.current;
    if (!snapshot) return null;
    const found = data.find((r) => r.entry.created_at > snapshot);
    return found ? found.entry.id : null;
  }, [data]);
  const dividerScrolledPastRef = useRef(false);

  useEffect(() => {
    if (!highlightCommentId || data.length === 0) return;
    const stamp = `${highlightCommentId}:${highlightNonce ?? ''}`;
    if (lastStampRef.current === stamp) return;
    lastStampRef.current = stamp;

    setHighlightedId(highlightCommentId);

    const fade = setTimeout(() => setHighlightedId(null), HIGHLIGHT_HOLD_MS);
    return () => clearTimeout(fade);
  }, [highlightCommentId, highlightNonce, data.length]);

  const [newCount, setNewCount] = useState(0);
  const isAtBottomRef = useRef(true);
  const lastDataLenRef = useRef(0);
  useEffect(() => {
    const grew = data.length > lastDataLenRef.current;
    const diff = data.length - lastDataLenRef.current;
    lastDataLenRef.current = data.length;
    if (!grew) return;
    if (isAtBottomRef.current) return;
    setNewCount((prev) => prev + diff);
  }, [data.length]);

  const handleScroll = useCallback(
    (e: NativeSyntheticEvent<NativeScrollEvent>) => {
      const { contentOffset, contentSize, layoutMeasurement } = e.nativeEvent;
      const distFromBottom = contentSize.height - (contentOffset.y + layoutMeasurement.height);
      const wasAtBottom = isAtBottomRef.current;
      isAtBottomRef.current = distFromBottom < AT_BOTTOM_SLACK_PX;
      if (!wasAtBottom && isAtBottomRef.current && newCount > 0) {
        setNewCount(0);
      }
    },
    [newCount],
  );

  const onJumpToNew = useCallback(() => {
    listRef.current?.scrollToEnd({ animated: true });
    setNewCount(0);
  }, []);

  const dataWithDivider = useMemo<TimelineRow[]>(() => {
    if (!dividerAnchorId) return data;
    const anchorIdx = data.findIndex((r) => r.entry.id === dividerAnchorId);
    if (anchorIdx <= 0) return data;
    const divider: TimelineRow = {
      entry: {
        id: DIVIDER_ID,
        type: 'activity',
        created_at: '',
        actor_type: '',
        actor_id: '',
      } as unknown as TimelineEntry,
      replies: [],
    };
    return [...data.slice(0, anchorIdx), divider, ...data.slice(anchorIdx)];
  }, [data, dividerAnchorId]);

  const viewabilityConfig = useMemo(() => ({ itemVisiblePercentThreshold: 1 }), []);
  const handleViewableItemsChanged = useCallback(
    ({ viewableItems }: { viewableItems: ViewToken[] }) => {
      if (!dividerAnchorId) return;
      if (dividerScrolledPastRef.current) return;
      const dividerIdx = dataWithDivider.findIndex((r) => r.entry.id === DIVIDER_ID);
      if (dividerIdx < 0) return;
      const minVisibleIdx = viewableItems.reduce(
        (acc, v) => (v.index != null && v.index < acc ? v.index : acc),
        Number.POSITIVE_INFINITY,
      );
      if (minVisibleIdx > dividerIdx) {
        dividerScrolledPastRef.current = true;
      }
    },
    [dividerAnchorId, dataWithDivider],
  );
  const handlerRef = useRef(handleViewableItemsChanged);
  useEffect(() => {
    handlerRef.current = handleViewableItemsChanged;
  }, [handleViewableItemsChanged]);
  const stableViewabilityHandler = useCallback(
    (info: { viewableItems: ViewToken[] }) => handlerRef.current(info),
    [],
  );
  const viewabilityCallbackPairs = useRef([
    {
      viewabilityConfig,
      onViewableItemsChanged: stableViewabilityHandler,
    },
  ]);

  const markViewed = useLastViewedStore((s) => s.markViewed);
  useEffect(() => {
    const issueId = issue.id;
    return () => {
      if (!dividerAnchorId || dividerScrolledPastRef.current) {
        markViewed(issueId);
      }
    };
    // We intentionally bind the cleanup to the issueId-snapshot only —
    // re-running on `dividerAnchorId` changes would lose the original
    // anchor's "scrolled past" state if WS extended the timeline mid-read.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [issue.id]);

  const ListHeader = (
    <View>
      <IssueHeaderCard issue={issue} />
      <IssueDescription issueId={issue.id} description={issue.description} />
      <IssueReactionRow issue={issue} />
      <View className="px-4 pt-4 pb-2 border-t border-border">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          Activity
        </Text>
      </View>
      {timelineLoading && (!entries || entries.length === 0) ? (
        <View className="py-6 items-center">
          <ActivityIndicator />
        </View>
      ) : null}
    </View>
  );

  const hasData = dataWithDivider.length > 0;
  const flashListKey = highlightCommentId && hasData ? `hl-${highlightNonce ?? '0'}` : 'list';

  return (
    <View className="flex-1">
      {/* Outer Pressable owns the "tap anywhere outside the selected
          comment to exit text-selection mode" gesture. Disabled when
          no comment is selected → layout-only wrapper, every tap passes
          through to cells / chips / reactions. Active state captures any
          tap that didn't fire an inner Pressable — selecting CommentBody
          renders without its own Pressable wrapper (see comment-card.tsx
          `if (isSelecting) return body;`), so taps on the selected
          comment dismiss too, matching iOS Notes / iMessage. Scroll
          gestures are unaffected. */}
      <Pressable
        onPress={selectingId ? () => useCommentSelectStore.getState().clear() : undefined}
        disabled={!selectingId}
        style={{ flex: 1 }}
      >
        <FlashList
          key={flashListKey}
          ref={listRef}
          data={dataWithDivider}
          keyExtractor={(row) => row.entry.id}
          ListHeaderComponent={ListHeader}
          keyboardDismissMode="on-drag"
          keyboardShouldPersistTaps="handled"
          maintainVisibleContentPosition={{
            startRenderingFromBottom: !!highlightCommentId,
          }}
          ListHeaderComponentStyle={{ marginBottom: 4 }}
          ItemSeparatorComponent={RowSeparator}
          renderItem={({ item }) => {
            if (item.entry.id === DIVIDER_ID) {
              return <UnreadDivider />;
            }
            return item.entry.type === 'comment' ? (
              <CommentCard
                entry={item.entry}
                replies={item.replies}
                issueId={issue.id}
                issueIdentifier={issue.identifier}
                highlightedCommentId={highlightedId}
              />
            ) : (
              <ActivityRow entry={item.entry} />
            );
          }}
          onScroll={handleScroll}
          onScrollBeginDrag={() => useCommentSelectStore.getState().clear()}
          onMomentumScrollBegin={() => useCommentSelectStore.getState().clear()}
          viewabilityConfigCallbackPairs={viewabilityCallbackPairs.current}
          refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
          contentContainerStyle={{ paddingBottom: 16 }}
        />
      </Pressable>
      {newCount > 0 ? <NewCommentChip count={newCount} onPress={onJumpToNew} /> : null}
    </View>
  );
}

function RowSeparator() {
  return <View style={{ height: 12 }} />;
}

function UnreadDivider() {
  return (
    <View className="flex-row items-center gap-2 px-4">
      <View className="flex-1 h-px bg-destructive/40" />
      <Text className="text-[10px] uppercase tracking-wider font-medium text-destructive">New</Text>
      <View className="flex-1 h-px bg-destructive/40" />
    </View>
  );
}

function NewCommentChip({ count, onPress }: { count: number; onPress: () => void }) {
  const { colorScheme } = useColorScheme();
  const fg = THEME[colorScheme].primaryForeground;
  return (
    <Pressable
      onPress={onPress}
      className="absolute bottom-3 self-center px-3.5 py-1.5 rounded-full bg-primary active:opacity-80 flex-row items-center gap-1.5"
      accessibilityRole="button"
      accessibilityLabel={`Jump to ${count} new ${count === 1 ? 'message' : 'messages'}`}
      style={{
        shadowColor: '#000',
        shadowOffset: { width: 0, height: 2 },
        shadowOpacity: 0.18,
        shadowRadius: 6,
        elevation: 4,
      }}
    >
      <Ionicons name="arrow-down" size={14} color={fg} />
      <Text className="text-xs font-semibold text-primary-foreground">{count} new</Text>
    </Pressable>
  );
}

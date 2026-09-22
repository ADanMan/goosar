// Список сообщений чата: пузыри пользователя и ассистента, старые сверху.
// Новые сообщения автопрокручивают только если пользователь у нижнего края.
import { ActivityIndicator, Pressable, View } from 'react-native';
import { FlashList } from '@shopify/flash-list';
import { Ionicons } from '@expo/vector-icons';
import { useQuery } from '@tanstack/react-query';
import type { ChatMessage, ChatPendingTask, TaskMessagePayload } from '@goosar/core/types';
import type { AgentAvailability } from '@goosar/core/agents';
import { taskMessagesOptions } from '@/data/queries/chat';
import { Text } from '@/components/ui/text';
import { Markdown } from '@/lib/markdown';
import { failureReasonLabel } from '@/lib/failure-reason-label';
import { formatElapsedMs } from '@/lib/format-elapsed';
import { cn } from '@/lib/utils';
import { useChatSelectStore } from '@/data/chat-select-store';
import { useChatMessageLongPress } from './message-long-press';
import { ChatEmptyState } from './chat-empty-state';
import { ChatTimeline } from './chat-timeline';
import { CommentAttachmentList } from '@/components/issue/comment-attachment-list';
import { StatusPill } from './status-pill';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';

interface Props {
  messages: ChatMessage[];
  loading: boolean;
  hasSessions: boolean;
  agentName?: string;
  onPickPrompt: (text: string) => void;
  pendingTask?: ChatPendingTask | null;
  liveTaskMessages?: TaskMessagePayload[];
  availability?: AgentAvailability;
}

export function ChatMessageList({
  messages,
  loading,
  hasSessions,
  agentName,
  onPickPrompt,
  pendingTask,
  liveTaskMessages,
  availability,
}: Props) {
  const selectingId = useChatSelectStore((s) => s.selectingId);

  if (loading && messages.length === 0) {
    return (
      <View className="flex-1 items-center justify-center">
        <ActivityIndicator />
      </View>
    );
  }

  if (messages.length === 0) {
    return (
      <ChatEmptyState hasSessions={hasSessions} agentName={agentName} onPickPrompt={onPickPrompt} />
    );
  }

  const pendingTaskId = pendingTask?.task_id ?? null;
  const pendingAlreadyPersisted =
    !!pendingTaskId && messages.some((m) => m.role === 'assistant' && m.task_id === pendingTaskId);
  const showLiveSection = !!pendingTaskId && !pendingAlreadyPersisted;
  const showLiveTimeline = showLiveSection && (liveTaskMessages?.length ?? 0) > 0;

  return (
    <Pressable
      onPress={selectingId ? () => useChatSelectStore.getState().clear() : undefined}
      disabled={!selectingId}
      style={{ flex: 1 }}
    >
      {/* `key` on first message id forces remount on session switch so
        `startRenderingFromBottom` re-fires and we land at the new
        session's bottom (instead of inheriting the previous session's
        scroll position). Cheap because sessions are switched, not
        re-rendered every keystroke. */}
      <FlashList
        key={messages[0]?.id ?? 'empty'}
        data={messages}
        keyExtractor={(m) => m.id}
        renderItem={({ item }) => <MessageRow message={item} />}
        ItemSeparatorComponent={MessageSeparator}
        ListFooterComponent={
          showLiveSection ? (
            <View style={{ paddingTop: 12 }} className="gap-2">
              {showLiveTimeline ? (
                <ChatTimeline items={liveTaskMessages ?? []} isStreaming />
              ) : null}
              <StatusPill
                pendingTask={pendingTask}
                taskMessages={liveTaskMessages}
                availability={availability}
              />
            </View>
          ) : null
        }
        contentContainerStyle={{
          paddingHorizontal: 16,
          paddingTop: 12,
          paddingBottom: 16,
        }}
        maintainVisibleContentPosition={{
          autoscrollToBottomThreshold: 0.2,
          startRenderingFromBottom: true,
        }}
        onScrollBeginDrag={() => useChatSelectStore.getState().clear()}
        onMomentumScrollBegin={() => useChatSelectStore.getState().clear()}
        keyboardDismissMode="interactive"
        keyboardShouldPersistTaps="handled"
      />
    </Pressable>
  );
}

function MessageSeparator() {
  return <View style={{ height: 12 }} />;
}

function MessageRow({ message }: { message: ChatMessage }) {
  const isUser = message.role === 'user';
  const isFailure = !!message.failure_reason;
  const isSelecting = useChatSelectStore((s) => s.selectingId === message.id);
  const longPress = useChatMessageLongPress(message);

  if (isFailure) {
    return (
      <FailureBubble
        reasonLabel={failureReasonLabel(message.failure_reason)}
        rawError={message.content}
        elapsedMs={message.elapsed_ms ?? null}
        isSelecting={isSelecting}
        longPress={longPress}
      />
    );
  }

  if (isUser) {
    const body = (
      <View
        className={cn(
          'self-end max-w-[80%] gap-1.5 rounded-2xl border-2 px-3.5 py-2 transition-colors',
          isSelecting
            ? 'bg-primary/5 border-primary/30'
            : longPress.isPressed
              ? 'bg-muted border-primary/30'
              : 'bg-muted border-transparent',
        )}
      >
        <Markdown
          content={message.content}
          attachments={message.attachments}
          selectable={isSelecting}
          compact
        />
        <CommentAttachmentList attachments={message.attachments} content={message.content} />
      </View>
    );
    if (isSelecting) return body;
    return (
      <Pressable onLongPress={longPress.onLongPress} delayLongPress={500}>
        {body}
      </Pressable>
    );
  }

  return <AssistantRow message={message} isSelecting={isSelecting} longPress={longPress} />;
}

function AssistantRow({
  message,
  isSelecting,
  longPress,
}: {
  message: ChatMessage;
  isSelecting: boolean;
  longPress: ReturnType<typeof useChatMessageLongPress>;
}) {
  const { data: timeline = [] } = useQuery(taskMessagesOptions(message.task_id));
  const isNoResponse = message.message_kind === 'no_response';
  const body = (
    <View className="gap-1.5">
      {timeline.length > 0 ? <ChatTimeline items={timeline} /> : null}
      {isNoResponse ? (
        <Text className="text-sm italic text-muted-foreground">
          The agent finished this turn without a text reply.
        </Text>
      ) : (
        <Markdown
          content={message.content}
          attachments={message.attachments}
          selectable={isSelecting}
        />
      )}
      {/* Standalone attachment cards for anything not referenced inline. An
          image-only reply is a real ('message') outcome with empty content, so
          it flows through the else-branch above (renders nothing) and the cards
          here ARE the reply. */}
      <CommentAttachmentList attachments={message.attachments} content={message.content} />
      {message.elapsed_ms != null ? (
        <ElapsedCaption
          variant={isNoResponse ? 'finished' : 'replied'}
          elapsedMs={message.elapsed_ms}
        />
      ) : null}
    </View>
  );
  if (isSelecting) return body;
  return (
    <Pressable onLongPress={longPress.onLongPress} delayLongPress={500}>
      {body}
    </Pressable>
  );
}

function ElapsedCaption({
  variant,
  elapsedMs,
}: {
  variant: 'replied' | 'failed' | 'finished';
  elapsedMs: number;
}) {
  const label =
    variant === 'replied'
      ? `Replied in ${formatElapsedMs(elapsedMs)}`
      : variant === 'finished'
        ? `Finished in ${formatElapsedMs(elapsedMs)}`
        : `Failed after ${formatElapsedMs(elapsedMs)}`;
  return <Text className="text-xs text-muted-foreground/80 mt-1">{label}</Text>;
}

function FailureBubble({
  reasonLabel,
  rawError,
  elapsedMs,
  isSelecting,
  longPress,
}: {
  reasonLabel: string;
  rawError: string;
  elapsedMs: number | null;
  isSelecting: boolean;
  longPress: ReturnType<typeof useChatMessageLongPress>;
}) {
  const hasRawError = rawError.trim().length > 0;

  const body = (
    <View className="self-start max-w-[80%]">
      <View
        className={cn(
          'rounded-2xl border-2 bg-destructive/10 px-3.5 py-2 transition-colors',
          isSelecting || longPress.isPressed ? 'border-primary/30' : 'border-destructive/30',
        )}
      >
        <Text className="text-xs font-semibold text-destructive">{reasonLabel}</Text>
        {hasRawError ? (
          <Collapsible>
            <CollapsibleTrigger asChild>
              <View
                accessibilityRole="button"
                accessibilityLabel="Show error details"
                className="mt-1 flex-row items-center gap-1 active:opacity-70"
              >
                <Ionicons name="chevron-forward" size={12} color="#71717a" />
                <Text className="text-xs text-muted-foreground">Show details</Text>
              </View>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <View className="mt-1 rounded bg-muted/40 px-2 py-1.5">
                <Text className="text-xs text-muted-foreground" selectable={isSelecting}>
                  {rawError}
                </Text>
              </View>
            </CollapsibleContent>
          </Collapsible>
        ) : null}
      </View>
      {elapsedMs != null ? <ElapsedCaption variant="failed" elapsedMs={elapsedMs} /> : null}
    </View>
  );
  if (isSelecting) return body;
  return (
    <Pressable onLongPress={longPress.onLongPress} delayLongPress={500}>
      {body}
    </Pressable>
  );
}

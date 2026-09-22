'use client';

import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import { useQuery } from '@tanstack/react-query';
import { Virtuoso, type Components } from 'react-virtuoso';
import { cn } from '@goosar/ui/lib/utils';
import { Skeleton } from '@goosar/ui/components/ui/skeleton';
import { Button } from '@goosar/ui/components/ui/button';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@goosar/ui/components/ui/collapsible';
import { Tooltip, TooltipTrigger, TooltipContent } from '@goosar/ui/components/ui/tooltip';
import { ChevronRight, ChevronDown, Brain, AlertCircle, AlertTriangle, Copy } from 'lucide-react';
import { useScrollFade } from '@goosar/ui/hooks/use-scroll-fade';
import { isTaskMessageTaskId, taskMessagesOptions } from '@goosar/core/chat/queries';
import { useTaskMessageTail } from '@goosar/core/chat/use-task-message-tail';
import { RichContent } from '../../rich-content';
import { RichContentScrollRootProvider } from '../../rich-content/scroll-root';
import { copyText } from '@goosar/ui/lib/clipboard';
import { AttachmentList } from '../../issues/components/comment-card';
import type { AgentAvailability } from '@goosar/core/agents';
import {
  parseRuntimeFailure,
  resolveFailureReasonKey,
  runtimeFailureCause,
  usesRuntimeMessage,
  type RuntimeFailureCause,
} from '@goosar/core/agents';
import type { ChatMessage, ChatPendingTask, TaskMessagePayload } from '@goosar/core/types';
import type { ChatTimelineItem } from '@goosar/core/chat';
import { buildTimeline } from '../../common/task-transcript';
import { TaskStatusPill } from './task-status-pill';
import { formatElapsedMs } from '../lib/format';
import { splitTimeline, extractCopyText } from '../lib/copy-text';
import { useT } from '../../i18n';

interface ChatMessageListProps {
  messages: ChatMessage[];
  pendingTask: ChatPendingTask | null | undefined;
  availability: AgentAvailability | undefined;
  firstItemIndex?: number;
  hasOlderMessages?: boolean;
  isFetchingOlderMessages?: boolean;
  onLoadOlderMessages?: () => void;
  isVisible?: boolean;
  transformContent?: (content: string) => string;
}

interface ChatListContext {
  isFetchingOlderMessages: boolean;
  showStatusPill: boolean;
  pendingTask: ChatPendingTask | null | undefined;
  liveTaskMessages: readonly TaskMessagePayload[] | undefined;
  availability: AgentAvailability | undefined;
}

type ChatRenderItem =
  | { key: string; kind: 'message'; message: ChatMessage; taskId: string | null }
  | { key: string; kind: 'live'; taskId: string };

function messageRowKey(message: ChatMessage): string {
  return message.role === 'assistant' && message.task_id ? `task:${message.task_id}` : message.id;
}

function ChatListHeader({ context }: { context?: ChatListContext }) {
  const { t } = useT('chat');
  return (
    <div className="mx-auto w-full max-w-4xl px-5 pt-4">
      {context?.isFetchingOlderMessages && (
        <div className="text-center text-xs text-muted-foreground">
          {t(($) => $.message_list.loading_older)}
        </div>
      )}
    </div>
  );
}

function ChatListFooter({ context }: { context?: ChatListContext }) {
  if (!context) return null;
  if (!context.showStatusPill || !context.pendingTask) return null;
  return (
    <div className="mx-auto w-full max-w-4xl px-5 pb-4 space-y-4">
      <TaskStatusPill
        pendingTask={context.pendingTask}
        taskMessages={context.liveTaskMessages ?? []}
        availability={context.availability}
      />
    </div>
  );
}

const LIST_COMPONENTS: Components<ChatRenderItem, ChatListContext> = {
  Header: ChatListHeader,
  Footer: ChatListFooter,
};

export function ChatMessageList({
  messages,
  pendingTask,
  availability,
  firstItemIndex = 0,
  hasOlderMessages = false,
  isFetchingOlderMessages = false,
  onLoadOlderMessages,
  transformContent,
  isVisible = true,
}: ChatMessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [scrollContainerEl, setScrollContainerEl] = useState<HTMLDivElement | null>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);
  const setScrollContainerRef = useCallback((node: HTMLDivElement | null) => {
    scrollRef.current = node;
    setScrollContainerEl(node);
  }, []);
  const fadeStyle = useScrollFade(scrollRef, 16);

  const pendingTaskId = pendingTask?.task_id ?? null;

  const pendingAlreadyPersisted =
    !!pendingTaskId && messages.some((m) => m.role === 'assistant' && m.task_id === pendingTaskId);

  const showLiveTimeline = !!pendingTaskId && !pendingAlreadyPersisted;
  const canFetchLiveTimeline = isTaskMessageTaskId(pendingTaskId) && !pendingAlreadyPersisted;
  const { data: liveTaskMessages } = useQuery({
    ...taskMessagesOptions(pendingTaskId ?? ''),
    enabled: canFetchLiveTimeline,
  });
  useTaskMessageTail(pendingTaskId ?? '', canFetchLiveTimeline && isVisible);
  const hasLive = showLiveTimeline && (liveTaskMessages?.length ?? 0) > 0;
  const showStatusPill = !!pendingTaskId && !pendingAlreadyPersisted && !!pendingTask;

  const renderItems: ChatRenderItem[] = useMemo(() => {
    const items: ChatRenderItem[] = messages.map((message) => ({
      key: messageRowKey(message),
      kind: 'message' as const,
      message,
      taskId: message.task_id ?? null,
    }));
    if (hasLive && pendingTaskId) {
      items.push({ key: `task:${pendingTaskId}`, kind: 'live', taskId: pendingTaskId });
    }
    return items;
  }, [messages, hasLive, pendingTaskId]);

  const firstIndex = renderItems.length > 0 ? firstItemIndex : 0;

  const listContext: ChatListContext = {
    isFetchingOlderMessages,
    showStatusPill,
    pendingTask,
    liveTaskMessages,
    availability,
  };

  return (
    <div
      ref={setScrollContainerRef}
      data-tab-scroll-root
      style={fadeStyle}
      className="flex-1 overflow-y-auto"
    >
      {!scrollContainerEl ? (
        <div className="mx-auto w-full max-w-4xl px-5 pt-4 space-y-3">
          <ChatMessageSkeleton />
        </div>
      ) : (
        <RichContentScrollRootProvider scrollRoot={scrollContainerEl}>
          <Virtuoso
            customScrollParent={scrollContainerEl}
            data={renderItems}
            firstItemIndex={firstIndex}
            initialTopMostItemIndex={{ index: 'LAST', align: 'end' }}
            increaseViewportBy={{ top: 400, bottom: 600 }}
            atBottomThreshold={120}
            atBottomStateChange={setIsNearBottom}
            followOutput={() => (!isFetchingOlderMessages && isNearBottom ? 'smooth' : false)}
            startReached={() => {
              if (hasOlderMessages && !isFetchingOlderMessages) {
                onLoadOlderMessages?.();
              }
            }}
            computeItemKey={(_, item) => item.key}
            context={listContext}
            components={LIST_COMPONENTS}
            itemContent={(_, item) => (
              <div className="mx-auto w-full max-w-4xl px-5 py-2">
                <MessageBubble
                  item={item}
                  isPending={!!pendingTaskId && item.taskId === pendingTaskId}
                  transformContent={transformContent}
                />
              </div>
            )}
          />
        </RichContentScrollRootProvider>
      )}
    </div>
  );
}

export function ChatMessageSkeleton() {
  return (
    <div className="flex-1 overflow-hidden">
      <div className="mx-auto w-full max-w-4xl px-5 py-4 space-y-5">
        <div className="space-y-2">
          <Skeleton className="h-3.5 w-3/4" />
          <Skeleton className="h-3.5 w-1/2" />
        </div>
        <div className="flex justify-end">
          <Skeleton className="h-8 w-48 rounded-2xl" />
        </div>
        <div className="space-y-2">
          <Skeleton className="h-3.5 w-2/3" />
          <Skeleton className="h-3.5 w-5/6" />
          <Skeleton className="h-3.5 w-1/3" />
        </div>
      </div>
    </div>
  );
}

const MessageBubble = memo(function MessageBubble({
  item,
  isPending,
  transformContent,
}: {
  item: ChatRenderItem;
  isPending: boolean;
  transformContent?: (content: string) => string;
}) {
  if (item.kind === 'live') {
    return (
      <AssistantMessage
        taskId={item.taskId}
        isPending={isPending}
        transformContent={transformContent}
      />
    );
  }

  const { message } = item;

  if (message.role === 'user') {
    return (
      <div className="flex justify-end">
        <div className="rounded-2xl bg-muted px-3.5 py-2 text-sm max-w-[80%] break-words">
          {/* User messages are authored as markdown in ContentEditor, so they
           * render through the SAME RichContent as assistant replies and as
           * Issue/Comment — a Mermaid fence a user pastes is a diagram here
           * too. `compact` trims the leading/trailing block margins so a
           * single-line bubble stays as tight as the plain-text version. */}
          <RichContent
            content={message.content}
            attachments={message.attachments}
            density="compact"
            phase="settled"
          />
          <AttachmentList
            attachments={message.attachments}
            content={message.content}
            className="mt-1.5"
          />
        </div>
      </div>
    );
  }

  return (
    <AssistantMessage
      taskId={message.task_id ?? null}
      message={message}
      isPending={isPending}
      transformContent={transformContent}
    />
  );
});

function AssistantMessage({
  taskId,
  message,
  isPending,
  transformContent,
}: {
  taskId: string | null;
  message?: ChatMessage;
  isPending: boolean;
  transformContent?: (content: string) => string;
}) {
  const canFetchTaskMessages = isTaskMessageTaskId(taskId);

  const { data: taskMessages } = useQuery({
    ...taskMessagesOptions(taskId ?? ''),
    enabled: canFetchTaskMessages,
  });

  const timeline: ChatTimelineItem[] = useMemo(
    () => transformTimeline(buildTimeline(taskMessages ?? []), transformContent),
    [taskMessages, transformContent],
  );

  const phase: 'streaming' | 'settled' = message ? 'settled' : 'streaming';

  if (message?.failure_reason) {
    return (
      <FailureBubble
        reason={message.failure_reason}
        rawError={message.content}
        timeline={timeline}
        elapsedMs={message.elapsed_ms}
      />
    );
  }

  const isNoResponse = message?.message_kind === 'no_response';

  return (
    <div className="w-full space-y-1.5">
      {timeline.length > 0 && (
        <TimelineView
          items={timeline}
          attachments={message?.attachments}
          phase={phase}
          isStreaming={!message}
        />
      )}
      {isNoResponse ? (
        <NoResponseNotice />
      ) : message && timeline.length === 0 ? (
        <RichContent
          content={message.content}
          attachments={message.attachments}
          density="compact"
          phase="settled"
          className="leading-relaxed"
        />
      ) : null}
      {message && (
        <>
          <AttachmentList attachments={message.attachments} content={message.content} />
          <MessageFooter message={message} timeline={timeline} isPending={isPending} />
        </>
      )}
    </div>
  );
}

function transformTimeline(
  timeline: ChatTimelineItem[],
  transformContent?: (content: string) => string,
): ChatTimelineItem[] {
  if (!transformContent) return timeline;
  return timeline.map((item) =>
    item.type === 'text' && item.content
      ? { ...item, content: transformContent(item.content) }
      : item,
  );
}

function NoResponseNotice() {
  const { t } = useT('chat');
  return (
    <div className="text-sm italic text-muted-foreground">
      {t(($) => $.message_list.no_response)}
    </div>
  );
}

function MessageFooter({
  message,
  timeline,
  isPending,
}: {
  message: ChatMessage;
  timeline: ChatTimelineItem[];
  isPending: boolean;
}) {
  const isNoResponse = message.message_kind === 'no_response';
  const showCopy = !isPending && !isNoResponse;
  if (message.elapsed_ms == null && !showCopy) return null;
  return (
    <div className="flex items-center gap-1.5">
      {message.elapsed_ms != null && (
        <ElapsedCaption
          variant={isNoResponse ? 'finished' : 'replied'}
          elapsedMs={message.elapsed_ms}
        />
      )}
      {showCopy && <MessageCopyButton message={message} timeline={timeline} />}
    </div>
  );
}

function MessageCopyButton({
  message,
  timeline,
}: {
  message: ChatMessage;
  timeline: ChatTimelineItem[];
}) {
  const { t } = useT('chat');
  const handleCopy = async () => {
    if (await copyText(extractCopyText(message, timeline))) {
      toast.success(t(($) => $.message_list.copied_toast));
    } else {
      toast.error(t(($) => $.message_list.copy_failed_toast));
    }
  };
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground/70 hover:text-foreground"
            onClick={handleCopy}
            aria-label={t(($) => $.message_list.copy_action)}
          />
        }
      >
        <Copy />
      </TooltipTrigger>
      <TooltipContent side="top">{t(($) => $.message_list.copy_action)}</TooltipContent>
    </Tooltip>
  );
}

function ElapsedCaption({
  variant,
  elapsedMs,
  className,
}: {
  variant: 'replied' | 'failed' | 'finished';
  elapsedMs: number;
  className?: string;
}) {
  const { t } = useT('chat');
  const elapsed = formatElapsedMs(elapsedMs);
  const text =
    variant === 'replied'
      ? t(($) => $.message_list.replied_in, { elapsed })
      : variant === 'finished'
        ? t(($) => $.message_list.finished_in, { elapsed })
        : t(($) => $.message_list.failed_after, { elapsed });
  return <div className={cn('text-xs text-muted-foreground/80', className)}>{text}</div>;
}

function FailureBubble({
  reason,
  rawError,
  timeline,
  elapsedMs,
}: {
  reason: string;
  rawError: string;
  timeline: ChatTimelineItem[];
  elapsedMs?: number | null;
}) {
  const { t } = useT('chat');
  const [open, setOpen] = useState(false);
  const chatFailureCopy: Record<string, string> = {
    agent_error: t(($) => $.message_list.failure.agent_error),
    timeout: t(($) => $.message_list.failure.timeout),
    codex_semantic_inactivity: t(($) => $.message_list.failure.runtime_semantic_inactivity),
    runtime_offline: t(($) => $.message_list.failure.runtime_offline),
    runtime_recovery: t(($) => $.message_list.failure.runtime_recovery),
    manual: t(($) => $.message_list.failure.manual),
    cancelled: t(($) => $.message_list.failure.manual),
    skill_bundle_unavailable: t(($) => $.message_list.failure.skill_bundle_unavailable),
    'agent_error.provider_network': t(($) => $.message_list.failure.provider_network),
    'agent_error.provider_auth_or_access': t(($) => $.message_list.failure.provider_auth_or_access),
    'agent_error.provider_quota_limit': t(($) => $.message_list.failure.provider_quota_limit),
    'agent_error.provider_capacity_or_rate_limit': t(
      ($) => $.message_list.failure.provider_capacity_or_rate_limit,
    ),
    'agent_error.context_overflow': t(($) => $.message_list.failure.context_overflow),
    'agent_error.runtime_missing_executable': t(
      ($) => $.message_list.failure.runtime_missing_executable,
    ),
    'agent_error.runtime_version_unsupported': t(
      ($) => $.message_list.failure.runtime_version_unsupported,
    ),
  };
  const copyKey = resolveFailureReasonKey(reason, chatFailureCopy);
  const familyLabel =
    (copyKey && chatFailureCopy[copyKey]) ?? t(($) => $.message_list.failure.fallback);

  const signature = parseRuntimeFailure(rawError);
  const cause = runtimeFailureCause(signature);
  const deferToRuntime = usesRuntimeMessage(cause, signature);
  const causeCopy: Record<RuntimeFailureCause, string> = {
    llm_not_configured: deferToRuntime
      ? t(($) => $.message_list.failure.llm_not_configured_lead)
      : t(($) => $.message_list.failure.llm_not_configured),
    agent_unresolved: t(($) => $.message_list.failure.agent_unresolved),
  };
  const label = cause ? causeCopy[cause] : familyLabel;

  return (
    <div className="w-full space-y-1.5">
      {/* Failure read as an inline, low-key note — not a destructive
       *  alert. Intentionally borderless / no background tint: a chat
       *  failure is informational ("this didn't work"), not a system
       *  error. The icon + muted destructive text are signal enough,
       *  the rest stays in the normal reply rhythm. */}
      <div className="flex items-start gap-1.5 text-sm">
        <AlertTriangle className="size-3.5 shrink-0 text-destructive/80 mt-0.5" />
        <div className="flex-1 min-w-0">
          <div className="text-destructive/90">{label}</div>
          {deferToRuntime && signature && (
            <p className="mt-1 break-words rounded bg-muted/40 p-2 font-mono text-xs text-muted-foreground">
              {signature.message}
            </p>
          )}
          {/* Issue #438: the runtime names a config file, which is exactly the
           *  thing the target user cannot open. The same fix lives in the app,
           *  so name the screen — on both branches, because the verbatim path
           *  is the common one and it is the one that reads as a dead end. */}
          {cause === 'llm_not_configured' && (
            <p className="mt-1 text-xs text-muted-foreground">
              {t(($) => $.message_list.failure.llm_not_configured_action)}
            </p>
          )}
          {rawError.trim() && (
            <Collapsible open={open} onOpenChange={setOpen}>
              <CollapsibleTrigger className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
                {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
                <span>{t(($) => $.message_list.show_details)}</span>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <pre className="mt-1 max-h-40 overflow-auto rounded bg-muted/40 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
                  {rawError}
                </pre>
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>
      </div>
      {timeline.length > 0 && <TimelineView items={timeline} />}
      {elapsedMs != null && <ElapsedCaption variant="failed" elapsedMs={elapsedMs} />}
    </div>
  );
}

function TimelineView({
  items,
  isStreaming,
  attachments,
  phase = 'settled',
}: {
  items: ChatTimelineItem[];
  isStreaming?: boolean;
  attachments?: import('@goosar/core/types').Attachment[];
  phase?: 'streaming' | 'settled';
}) {
  const { preface, middle, final } = splitTimeline(items);

  return (
    <>
      {preface.length > 0 && (
        <RichContent
          content={preface.map((t) => t.content ?? '').join('')}
          attachments={attachments}
          density="compact"
          phase={phase}
          className="leading-relaxed"
        />
      )}
      {middle.length > 0 && (
        <OuterProcessFold
          items={middle}
          isStreaming={!!isStreaming}
          attachments={attachments}
          phase={phase}
        />
      )}
      {final.length > 0 && (
        <RichContent
          content={final.map((t) => t.content ?? '').join('')}
          attachments={attachments}
          density="compact"
          phase={phase}
          className="leading-relaxed"
        />
      )}
    </>
  );
}

function OuterProcessFold({
  items,
  isStreaming,
  attachments,
  phase = 'settled',
}: {
  items: ChatTimelineItem[];
  isStreaming?: boolean;
  attachments?: import('@goosar/core/types').Attachment[];
  phase?: 'streaming' | 'settled';
}) {
  const { t } = useT('chat');
  const [open, setOpen] = useState(!!isStreaming);
  const wasStreaming = useRef(!!isStreaming);
  useEffect(() => {
    if (wasStreaming.current && !isStreaming) setOpen(false);
    wasStreaming.current = !!isStreaming;
  }, [isStreaming]);
  const stepCount = items.length;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
        {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
        <span>{t(($) => $.message_list.process_steps, { count: stepCount })}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 rounded-lg border bg-muted/20 p-2 space-y-0.5">
          {items.map((item) =>
            item.type === 'text' ? (
              <MiddleTextRow key={item.seq} item={item} attachments={attachments} phase={phase} />
            ) : (
              <ItemRow key={item.seq} item={item} />
            ),
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function MiddleTextRow({
  item,
  attachments,
  phase = 'settled',
}: {
  item: ChatTimelineItem;
  attachments?: import('@goosar/core/types').Attachment[];
  phase?: 'streaming' | 'settled';
}) {
  return (
    <div className="py-0.5 text-xs text-muted-foreground">
      <RichContent
        content={item.content ?? ''}
        attachments={attachments}
        density="compact"
        phase={phase}
      />
    </div>
  );
}

function ItemRow({ item }: { item: ChatTimelineItem }) {
  switch (item.type) {
    case 'tool_use':
      return <ToolCallRow item={item} />;
    case 'tool_result':
      return <ToolResultRow item={item} />;
    case 'thinking':
      return <ThinkingRow item={item} />;
    case 'error':
      return <ErrorRow item={item} />;
    default:
      return null;
  }
}

function shortenPath(p: string): string {
  const parts = p.split('/');
  if (parts.length <= 3) return p;
  return '.../' + parts.slice(-2).join('/');
}

function getToolSummary(item: ChatTimelineItem): string {
  if (!item.input) return '';
  const inp = item.input as Record<string, string>;
  if (inp.query) return inp.query;
  if (inp.file_path) return shortenPath(inp.file_path);
  if (inp.path) return shortenPath(inp.path);
  if (inp.pattern) return inp.pattern;
  if (inp.description) return String(inp.description);
  if (inp.command) {
    const cmd = String(inp.command);
    return cmd.length > 100 ? cmd.slice(0, 100) + '...' : cmd;
  }
  if (inp.prompt) {
    const p = String(inp.prompt);
    return p.length > 100 ? p.slice(0, 100) + '...' : p;
  }
  if (inp.skill) return String(inp.skill);
  for (const v of Object.values(inp)) {
    if (typeof v === 'string' && v.length > 0 && v.length < 120) return v;
  }
  return '';
}

function ToolCallRow({ item }: { item: ChatTimelineItem }) {
  const [open, setOpen] = useState(false);
  const summary = getToolSummary(item);
  const hasInput = item.input && Object.keys(item.input).length > 0;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn(
            'h-3 w-3 shrink-0 text-muted-foreground transition-transform',
            open && 'rotate-90',
            !hasInput && 'invisible',
          )}
        />
        <span className="font-medium text-foreground shrink-0">{item.tool}</span>
        {summary && <span className="truncate text-muted-foreground">{summary}</span>}
      </CollapsibleTrigger>
      {hasInput && (
        <CollapsibleContent>
          <pre className="ml-[18px] mt-0.5 max-h-32 overflow-auto rounded bg-muted/50 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
            {JSON.stringify(item.input, null, 2)}
          </pre>
        </CollapsibleContent>
      )}
    </Collapsible>
  );
}

function ToolResultRow({ item }: { item: ChatTimelineItem }) {
  const { t } = useT('chat');
  const [open, setOpen] = useState(false);
  const output = item.output ?? '';
  if (!output) return null;

  const preview = output.length > 120 ? output.slice(0, 120) + '...' : output;
  const labelPrefix = item.tool
    ? t(($) => $.message_list.tool_result_named, { tool: item.tool })
    : t(($) => $.message_list.tool_result_unnamed);

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn(
            'h-3 w-3 shrink-0 text-muted-foreground transition-transform mt-0.5',
            open && 'rotate-90',
          )}
        />
        <span className="text-muted-foreground/70 truncate">
          {labelPrefix}
          {preview}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/50 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
          {output.length > 4000 ? output.slice(0, 4000) + '\n... (truncated)' : output}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ThinkingRow({ item }: { item: ChatTimelineItem }) {
  const [open, setOpen] = useState(false);
  const text = item.content ?? '';
  if (!text) return null;

  const preview = text.length > 150 ? text.slice(0, 150) + '...' : text;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <Brain className="h-3 w-3 shrink-0 text-muted-foreground/60 mt-0.5" />
        <span className="text-muted-foreground italic truncate">{preview}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/30 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-words">
          {text}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ErrorRow({ item }: { item: ChatTimelineItem }) {
  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <AlertCircle className="h-3 w-3 shrink-0 text-destructive mt-0.5" />
      <span className="text-destructive">{item.content}</span>
    </div>
  );
}


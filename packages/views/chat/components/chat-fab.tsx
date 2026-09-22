'use client';

import { MessageCircle } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { cn } from '@goosar/ui/lib/utils';
import { useChatStore } from '@goosar/core/chat';
import {
  chatSessionsOptions,
  countUnreadChatSessions,
  hasPendingChatTasksOptions,
} from '@goosar/core/chat/queries';
import { useRealtimePollingInterval } from '@goosar/core/realtime';
import { useWorkspaceId } from '@goosar/core/hooks';
import { createLogger } from '@goosar/core/logger';
import { Tooltip, TooltipTrigger, TooltipContent } from '@goosar/ui/components/ui/tooltip';
import { useT } from '../../i18n';

const logger = createLogger('chat.ui');

export function ChatFab() {
  const { t } = useT('chat');
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const toggle = useChatStore((s) => s.toggle);
  const pollingInterval = useRealtimePollingInterval();
  const { data: sessions = [] } = useQuery({
    ...chatSessionsOptions(wsId),
    refetchInterval: isOpen ? false : pollingInterval,
  });
  const { data: hasPending } = useQuery({
    ...hasPendingChatTasksOptions(wsId),
    enabled: !isOpen,
    refetchInterval: pollingInterval,
  });

  if (isOpen) return null;

  const unreadSessionCount = countUnreadChatSessions(sessions);
  const isRunning = hasPending?.has_pending ?? false;

  const handleClick = () => {
    logger.info('fab.click (open chat)', { unreadSessionCount, isRunning });
    toggle();
  };

  const tooltip = isRunning
    ? t(($) => $.fab.running)
    : unreadSessionCount > 0
      ? t(($) => $.fab.unread, { count: unreadSessionCount })
      : t(($) => $.fab.default);

  return (
    <Tooltip>
      <TooltipTrigger
        onClick={handleClick}
        aria-label={tooltip}
        className={cn(
          'absolute bottom-2 right-2 z-50 flex size-10 touch-manipulation items-center justify-center rounded-full bg-surface-raised text-muted-foreground shadow-[var(--floating-shadow)] ring-1 ring-surface-border transition-[background-color,color,box-shadow] hover:bg-surface-hover hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-2 focus-visible:ring-offset-page-canvas active:bg-surface-hover',
          isRunning && 'animate-chat-impulse',
        )}
      >
        <MessageCircle className="size-5" />
      </TooltipTrigger>
      <TooltipContent side="top" sideOffset={10}>
        {tooltip}
      </TooltipContent>
    </Tooltip>
  );
}

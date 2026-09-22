// Хуки счётчиков непрочитанного для бейджей вкладок; логика зеркалит веб.
import { useQuery } from '@tanstack/react-query';
import { countUnreadChatMessages } from '@goosar/core/chat/unread';
import { inboxListOptions } from '@/data/queries/inbox';
import { chatSessionsOptions } from '@/data/queries/chat';
import { deduplicateInboxItems } from '@/lib/inbox-display';

export function useInboxUnreadCount(wsId: string | null | undefined): number {
  const { data } = useQuery({
    ...inboxListOptions(wsId ?? null),
    select: (items) => deduplicateInboxItems(items).filter((i) => !i.read).length,
  });
  return data ?? 0;
}

export function useChatUnreadMessageCount(wsId: string | null | undefined): number {
  const { data } = useQuery({
    ...chatSessionsOptions(wsId ?? null),
    select: (sessions) => countUnreadChatMessages(sessions),
  });
  return data ?? 0;
}

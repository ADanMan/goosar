import type { ChatSession } from '../types/chat';

export function countUnreadChatMessages(
  sessions: readonly ChatSession[] | undefined,
  excludeSessionId?: string | null,
): number {
  if (!sessions) return 0;
  return sessions.reduce(
    (sum, s) => (s.id === excludeSessionId ? sum : sum + (s.unread_count ?? 0)),
    0,
  );
}

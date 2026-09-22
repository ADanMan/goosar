'use client';

import { useChatStore } from '@goosar/core/chat';
import { useWorkspacePaths } from '@goosar/core/paths';
import { useNavigation } from '../navigation';
import { ChatFab } from './components/chat-fab';
import { ChatWindow } from './components/chat-window';

export function FloatingChat() {
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const { pathname } = useNavigation();
  const wsPaths = useWorkspacePaths();

  if (!enabled) return null;
  if (pathname === wsPaths.chat() || pathname.startsWith(`${wsPaths.chat()}/`)) {
    return null;
  }

  return (
    <>
      <ChatWindow />
      <ChatFab />
    </>
  );
}

// Действия справа в шапке вкладки чата: меню сессии (при активной сессии)
// и кнопка нового чата.
import { IconButton } from '@/components/ui/icon-button';

interface Props {
  showMore: boolean;
  onMorePress: () => void;
  onNewPress: () => void;
}

export function ChatSessionActions({ showMore, onMorePress, onNewPress }: Props) {
  return (
    <>
      {showMore ? (
        <IconButton
          name="ellipsis-horizontal"
          onPress={onMorePress}
          accessibilityLabel="Session actions"
        />
      ) : null}
      <IconButton name="add" iconSize={24} onPress={onNewPress} accessibilityLabel="New chat" />
    </>
  );
}

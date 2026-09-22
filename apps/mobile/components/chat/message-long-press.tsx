/**
 * Обработчик долгого нажатия на пузырь сообщения в чате: отдаёт
 * onLongPress (показывает нативный ActionSheetIOS) и isPressed
 * (подсветка на время показа шторки).
 *
 * Набор пунктов: Copy · Select Text · Cancel.
 */
import { useCallback, useState } from 'react';
import { ActionSheetIOS } from 'react-native';
import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import type { ChatMessage } from '@goosar/core/types';
import { useChatSelectStore } from '@/data/chat-select-store';

export function useChatMessageLongPress(message: ChatMessage): {
  onLongPress: () => void;
  isPressed: boolean;
} {
  const [isPressed, setIsPressed] = useState(false);

  const onLongPress = useCallback(() => {
    const hasContent = !!message.content;

    Haptics.selectionAsync().catch(() => {});
    setIsPressed(true);

    type Action = { kind: 'copy' } | { kind: 'select' } | { kind: 'cancel' };

    const options: string[] = [];
    const actions: Action[] = [];
    const push = (label: string, action: Action) => {
      options.push(label);
      actions.push(action);
    };

    if (hasContent) {
      push('Copy', { kind: 'copy' });
      push('Select Text', { kind: 'select' });
    }
    push('Cancel', { kind: 'cancel' });

    const cancelButtonIndex = options.length - 1;

    ActionSheetIOS.showActionSheetWithOptions({ options, cancelButtonIndex }, (i) => {
      setIsPressed(false);
      const action = actions[i];
      if (!action || action.kind === 'cancel') return;

      switch (action.kind) {
        case 'copy':
          if (message.content) {
            Clipboard.setStringAsync(message.content);
            Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
          }
          return;
        case 'select':
          useChatSelectStore.getState().setSelecting(message.id);
          return;
      }
    });
  }, [message]);

  return { onLongPress, isPressed };
}

/**
 * Обёртка строки инбокса со свайпом влево для показа кнопки Archive.
 * Свайп только открывает действие — архивирование по полному свайпу
 * убрали как слишком случайное. При пересечении ширины кнопки во время
 * драга один раз срабатывает средний хаптик.
 *
 * Используется ReanimatedSwipeable — работает на UI-потоке и не тормозит
 * на длинных списках, в отличие от старой реализации на Animated.
 */
import { useRef } from 'react';
import { Pressable, View } from 'react-native';
import Animated, { type SharedValue, useAnimatedReaction, runOnJS } from 'react-native-reanimated';
import ReanimatedSwipeable, {
  type SwipeableMethods,
} from 'react-native-gesture-handler/ReanimatedSwipeable';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import type { InboxItem } from '@goosar/core/types';
import { Text } from '@/components/ui/text';
import { InboxRow } from './inbox-row';

const ACTION_WIDTH = 80;

interface Props {
  item: InboxItem;
  onPress: () => void;
  onArchive: () => void;
}

export function SwipeableInboxRow({ item, onPress, onArchive }: Props) {
  const ref = useRef<SwipeableMethods>(null);

  const fireArchive = () => {
    ref.current?.close();
    onArchive();
  };

  return (
    <ReanimatedSwipeable
      ref={ref}
      friction={2}
      rightThreshold={ACTION_WIDTH}
      renderRightActions={(_progress, drag) => <ArchiveAction onPress={fireArchive} drag={drag} />}
    >
      <InboxRow item={item} onPress={onPress} />
    </ReanimatedSwipeable>
  );
}

function ArchiveAction({ onPress, drag }: { onPress: () => void; drag: SharedValue<number> }) {
  useAnimatedReaction(
    () => drag.value <= -ACTION_WIDTH,
    (crossed, prev) => {
      if (crossed && !prev) {
        runOnJS(Haptics.impactAsync)(Haptics.ImpactFeedbackStyle.Medium);
      }
    },
    [],
  );

  return (
    <Animated.View style={{ width: ACTION_WIDTH }}>
      <Pressable
        onPress={onPress}
        accessibilityLabel="Archive"
        className="flex-1 items-center justify-center bg-destructive"
      >
        <View className="items-center gap-0.5">
          <Ionicons name="archive-outline" size={20} color="white" />
          <Text className="text-xs text-white">Archive</Text>
        </View>
      </Pressable>
    </Animated.View>
  );
}

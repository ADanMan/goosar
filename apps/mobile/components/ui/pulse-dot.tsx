/**
 * Медленный пульсирующий индикатор в фирменном цвете — анимация прозрачности
 * на UI-потоке через Reanimated `withRepeat` (2-секундный цикл). Используется
 * для статуса "в работе" агента. Цвет — только `brand`, не `success`: зелёный
 * зарезервирован за состоянием "завершено".
 */
import { useEffect } from 'react';
import Animated, {
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withTiming,
} from 'react-native-reanimated';
import { useColorScheme } from '@/lib/use-color-scheme';
import { THEME } from '@/lib/theme';

interface Props {
  size?: number;
}

export function PulseDot({ size = 8 }: Props) {
  const { colorScheme } = useColorScheme();
  const opacity = useSharedValue(0.3);
  useEffect(() => {
    opacity.value = withRepeat(
      withTiming(1, { duration: 1000 }),
      -1, // infinite
      true, // reverse — yields 0.3 ↔ 1.0 oscillation over 2s
    );
  }, [opacity]);

  const style = useAnimatedStyle(() => ({ opacity: opacity.value }));

  return (
    <Animated.View
      style={[
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor: THEME[colorScheme].brand,
        },
        style,
      ]}
    />
  );
}

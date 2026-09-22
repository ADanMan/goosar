/**
 * Хедер экрана — однострочный, слотовый компонент, рендерится в JSX
 * самого экрана (не через react-navigation), поэтому динамический
 * контент приходит обычными пропсами. Сам обрабатывает верхний
 * safe area; цвета берутся из токенов RNR. Используется только для
 * корней вкладок — экраны с push-навигацией используют нативный Stack.
 */

import type { ReactNode } from 'react';
import { View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Text } from '@/components/ui/text';

interface Props {
  title?: string;
  subtitle?: string;
  center?: ReactNode;
  left?: ReactNode;
  right?: ReactNode;
}

export function Header({ title, subtitle, center, left, right }: Props) {
  return (
    <SafeAreaView edges={['top']} className="bg-background border-b border-border">
      <View className="flex-row items-center h-12 px-2">
        {left ? <View className="flex-row items-center">{left}</View> : null}
        <View className="flex-1 px-2 justify-center">
          {center ??
            (title ? (
              <>
                <Text className="text-lg font-semibold text-foreground" numberOfLines={1}>
                  {title}
                </Text>
                {subtitle ? (
                  <Text className="text-xs text-muted-foreground" numberOfLines={1}>
                    {subtitle}
                  </Text>
                ) : null}
              </>
            ) : null)}
        </View>
        {right ? <View className="flex-row items-center gap-1">{right}</View> : null}
      </View>
    </SafeAreaView>
  );
}

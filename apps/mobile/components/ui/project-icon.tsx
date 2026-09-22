/**
 * Иконка проекта — эмодзи с фолбэком на 📁. Обёрнута в квадратный View
 * с увеличенным lineHeight (× 1.2), потому что на iOS эмодзи визуально
 * крупнее fontSize и обрезается по базовой линии текста без этого запаса.
 */
import { View } from 'react-native';
import { Text } from '@/components/ui/text';

export type ProjectIconSize = 'sm' | 'md' | 'lg';

const SIZE: Record<ProjectIconSize, { box: number; font: number }> = {
  sm: { box: 18, font: 14 },
  md: { box: 22, font: 16 },
  lg: { box: 28, font: 22 },
};

interface Props {
  icon?: string | null;
  size?: ProjectIconSize;
}

export function ProjectIcon({ icon, size = 'sm' }: Props) {
  const { box, font } = SIZE[size];
  return (
    <View
      style={{
        width: box,
        height: box,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Text style={{ fontSize: font, lineHeight: Math.round(font * 1.2) }}>{icon || '📁'}</Text>
    </View>
  );
}

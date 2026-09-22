// Марка Goosar для мобилки: два концентрических диска. Зеркалит
// packages/ui/components/common/goosar-icon.tsx.
import Svg, { Circle } from 'react-native-svg';
import { THEME } from '@/lib/theme';
import { useColorScheme } from '@/lib/use-color-scheme';

interface GoosarLogoProps {
  size?: number;
  color?: string;
}

const MARK_BRAND_YELLOW = '#F0BE32';

export function GoosarLogo({ size = 48, color }: GoosarLogoProps) {
  const { isDarkColorScheme } = useColorScheme();
  const resolvedColor =
    color ?? (isDarkColorScheme ? THEME.dark.foreground : THEME.light.foreground);

  return (
    <Svg width={size} height={size} viewBox="0 0 1024 1024">
      <Circle cx={512} cy={512} r={470} fill={resolvedColor} />
      <Circle cx={512} cy={512} r={272} fill={MARK_BRAND_YELLOW} />
    </Svg>
  );
}

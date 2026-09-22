/**
 * Кнопка закрытия (✕) в шапке модального Stack-экрана — круглая, 28pt,
 * в стиле Linear/Things. Строится поверх `<IconButton variant="secondary">`,
 * поэтому фон, active-состояние и тёмная тема берутся из токенов дизайн-системы.
 */
import { router } from 'expo-router';
import { IconButton } from '@/components/ui/icon-button';

export function ModalCloseButton() {
  return (
    <IconButton
      name="close"
      iconSize={18}
      variant="secondary"
      className="size-7 rounded-full"
      onPress={() => router.back()}
      accessibilityLabel="Close"
    />
  );
}

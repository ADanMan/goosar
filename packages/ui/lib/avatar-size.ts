// Семантическая шкала размеров аватара: потребители выбирают уровень по роли,
// а не пиксели; уровни на сетке 4px.
export type AvatarSize = 'xs' | 'sm' | 'md' | 'lg' | 'xl' | '2xl';

export const AVATAR_SIZE_PX: Record<AvatarSize, number> = {
  xs: 16,
  sm: 20,
  md: 24,
  lg: 32,
  xl: 40,
  '2xl': 56,
};

export const DEFAULT_AVATAR_SIZE: AvatarSize = 'sm';

/**
 * Кнопка-иконка — Button variant=ghost size=icon с Ionicon внутри.
 * Цвет иконки по умолчанию берётся из активной темы навигации, поэтому
 * тёмная тема работает без ручной передачи цвета.
 */

import { type ComponentProps } from 'react';
import { Ionicons } from '@expo/vector-icons';
import { useTheme } from '@react-navigation/native';
import { Button, type ButtonProps } from '@/components/ui/button';

interface Props extends Omit<ButtonProps, 'children' | 'size'> {
  name: ComponentProps<typeof Ionicons>['name'];
  iconSize?: number;
  color?: string;
}

export function IconButton({ name, iconSize = 20, color, ...buttonProps }: Props) {
  const { colors } = useTheme();
  return (
    <Button variant="ghost" size="icon" {...buttonProps}>
      <Ionicons name={name} size={iconSize} color={color ?? colors.text} />
    </Button>
  );
}

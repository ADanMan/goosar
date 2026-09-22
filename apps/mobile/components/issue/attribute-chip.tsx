/**
 * Универсальный чип атрибута в шапке экрана задачи: иконка + текстовый
 * лейбл. Заполненный вариант — значение есть, приглушённый — плейсхолдер.
 * Становится Pressable, если передан onPress.
 */
import { Pressable, View } from 'react-native';
import type { ReactNode } from 'react';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';

interface Props {
  icon: ReactNode;
  label: string;
  variant?: 'filled' | 'dimmed';
  onPress?: () => void;
  className?: string;
}

export function AttributeChip({ icon, label, variant = 'filled', onPress, className }: Props) {
  const containerClass = cn(
    'flex-row items-center gap-1.5 rounded-full border px-2.5 py-1',
    variant === 'filled'
      ? 'border-border bg-secondary/60'
      : 'border-dashed border-muted-foreground/30 bg-transparent',
    className,
  );
  const labelClass = cn(
    'text-xs',
    variant === 'filled' ? 'text-foreground' : 'text-muted-foreground/70',
  );

  const inner = (
    <>
      {icon}
      <Text className={labelClass} numberOfLines={1}>
        {label}
      </Text>
    </>
  );

  if (onPress) {
    return (
      <Pressable
        onPress={onPress}
        className={cn(containerClass, 'active:bg-secondary')}
        hitSlop={4}
      >
        {inner}
      </Pressable>
    );
  }
  return <View className={containerClass}>{inner}</View>;
}

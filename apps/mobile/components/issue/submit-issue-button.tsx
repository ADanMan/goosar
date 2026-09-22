/**
 * Кнопка отправки (↑) в шапке модалки создания задачи, справа. Визуально
 * парная с ModalCloseButton слева: тот же круг, акцентный цвет в активном
 * состоянии и приглушённый в disabled. Во время мутации вместо стрелки
 * показывается спиннер, чтобы исключить повторный тап.
 */
import { ActivityIndicator, Pressable, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { cn } from '@/lib/utils';

interface Props {
  disabled: boolean;
  onPress: () => void;
  loading?: boolean;
}

export function SubmitIssueButton({ disabled, onPress, loading }: Props) {
  const interactive = !disabled && !loading;
  return (
    <Pressable
      onPress={interactive ? onPress : undefined}
      hitSlop={8}
      accessibilityLabel="Create issue"
      accessibilityState={{ disabled: !interactive, busy: loading }}
      className={cn(interactive && 'active:opacity-60')}
    >
      <View
        className={cn(
          'size-7 items-center justify-center rounded-full',
          interactive ? 'bg-brand' : 'bg-secondary',
        )}
      >
        {loading ? (
          <ActivityIndicator size="small" color="#ffffff" />
        ) : (
          <Ionicons name="arrow-up" size={18} color={interactive ? '#ffffff' : '#a1a1aa'} />
        )}
      </View>
    </Pressable>
  );
}

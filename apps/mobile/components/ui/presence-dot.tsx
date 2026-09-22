/**
 * Точка присутствия агента с тремя состояниями (online/unstable/offline).
 * Зеркалирует цветовую схему веб-версии; вместо `ring-*` использует
 * `border-2 border-background`, так как React Native не поддерживает ring.
 * Чисто презентационный компонент — состояние приходит уже вычисленным.
 */
import { View } from 'react-native';
import type { AgentAvailability } from '@goosar/core/agents';
import { cn } from '@/lib/utils';

interface Props {
  availability: AgentAvailability;
  size?: number;
}

const DOT_CLASS: Record<AgentAvailability, string> = {
  online: 'bg-success',
  unstable: 'bg-warning',
  offline: 'bg-muted-foreground/40',
  archived: 'bg-muted-foreground/40',
};

export function PresenceDot({ availability, size = 8 }: Props) {
  return (
    <View
      style={{ width: size, height: size, borderRadius: size / 2 }}
      className={cn('border-2 border-background', DOT_CLASS[availability])}
    />
  );
}

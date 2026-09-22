/**
 * Уведомление над полем ввода чата, когда рантайм активного агента
 * недоступен.
 *
 * Два состояния:
 *   - unstable (рантайм офлайн < 5 мин) — янтарный, «может переподключиться»
 *   - offline  (рантайм офлайн ≥ 5 мин) — приглушённый, «не запустится»
 *
 * В остальных случаях (включая загрузку) баннер ничего не рендерит.
 */
import { Ionicons } from '@expo/vector-icons';
import { View } from 'react-native';
import type { AgentAvailability } from '@goosar/core/agents';
import { Text } from '@/components/ui/text';

interface Props {
  agentName?: string;
  availability: AgentAvailability | undefined;
}

export function OfflineBanner({ agentName, availability }: Props) {
  if (availability !== 'offline' && availability !== 'unstable') return null;
  const name = agentName?.trim() || 'This agent';

  if (availability === 'unstable') {
    return (
      <View className="mx-3 mb-1.5 flex-row items-center gap-1.5 rounded-md bg-warning/15 px-2.5 py-1.5">
        <Ionicons name="alert-circle-outline" size={14} color="#a16207" />
        <Text className="flex-1 text-xs text-warning" numberOfLines={1}>
          {name} may have just disconnected — your message will queue.
        </Text>
      </View>
    );
  }

  return (
    <View className="mx-3 mb-1.5 flex-row items-center gap-1.5 rounded-md bg-muted px-2.5 py-1.5">
      <Ionicons name="cloud-offline-outline" size={14} color="#71717a" />
      <Text className="flex-1 text-xs text-muted-foreground" numberOfLines={1}>
        {name} is offline. Messages will wait until its runtime is back.
      </Text>
    </View>
  );
}

/**
 * Мобильный ActorAvatar — аватар участника или агента (картинка или
 * инициалы), упрощённый аналог web-версии без hover-карточки. Агенты
 * получают отдельное оформление фона. Индикатор присутствия включается
 * опционально через showPresence, так как он тянет собственные запросы.
 */

import { Image, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { useColorScheme } from 'nativewind';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';
import { useActorLookup, getInitials } from '@/data/use-actor-name';
import { useWorkspaceStore } from '@/data/workspace-store';
import { useAgentPresence } from '@/lib/use-agent-presence';
import { PresenceDot } from '@/components/ui/presence-dot';
import { THEME } from '@/lib/theme';

interface Props {
  type: 'member' | 'agent' | 'system' | 'squad' | null | undefined;
  id: string | null | undefined;
  size?: number;
  showPresence?: boolean;
}

export function ActorAvatar({ type, id, size = 32, showPresence }: Props) {
  const avatar = <BareAvatar type={type} id={id} size={size} />;

  if (!showPresence || type !== 'agent' || !id) {
    return avatar;
  }
  return (
    <AgentAvatarWithPresence id={id} size={size}>
      {avatar}
    </AgentAvatarWithPresence>
  );
}

function BareAvatar({ type, id, size }: { type: Props['type']; id: Props['id']; size: number }) {
  const { getName, getAvatarUrl } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const iconColor =
    colorScheme === 'dark' ? THEME.dark.mutedForeground : THEME.light.mutedForeground;

  const radius = type === 'squad' ? Math.round(size * 0.22) : size / 2;

  const rawUrl = type && type !== 'system' ? getAvatarUrl(type, id) : null;
  const emoji = rawUrl?.startsWith('emoji:') ? rawUrl.slice('emoji:'.length).trim() || null : null;
  const url = !emoji && rawUrl && /^(https?:|data:|file:|asset:)/.test(rawUrl) ? rawUrl : null;

  if (emoji) {
    return (
      <View
        style={{ width: size, height: size, borderRadius: radius }}
        className="items-center justify-center bg-muted"
      >
        <Text
          accessibilityLabel={type === 'system' ? '' : getName(type, id)}
          style={{ fontSize: Math.round(size * 0.58), lineHeight: size }}
        >
          {emoji}
        </Text>
      </View>
    );
  }

  if (url) {
    return (
      <Image
        source={{ uri: url }}
        style={{ width: size, height: size, borderRadius: radius }}
        className="bg-muted"
      />
    );
  }

  if (type === 'system') {
    return (
      <View
        style={{ width: size, height: size, borderRadius: radius }}
        className="items-center justify-center bg-muted"
      >
        <Ionicons name="cog" size={Math.round(size * 0.55)} color={iconColor} />
      </View>
    );
  }

  if (type === 'squad') {
    return (
      <View
        style={{ width: size, height: size, borderRadius: radius }}
        className="items-center justify-center bg-muted"
      >
        <Ionicons name="people" size={Math.round(size * 0.55)} color={iconColor} />
      </View>
    );
  }

  const name = getName(type, id);
  const isAgent = type === 'agent';
  return (
    <View
      style={{ width: size, height: size, borderRadius: radius }}
      className={cn('items-center justify-center', isAgent ? 'bg-brand/15' : 'bg-muted')}
    >
      <Text className={cn('text-xs font-medium', isAgent ? 'text-brand' : 'text-muted-foreground')}>
        {getInitials(name)}
      </Text>
    </View>
  );
}

function AgentAvatarWithPresence({
  id,
  size,
  children,
}: {
  id: string;
  size: number;
  children: React.ReactNode;
}) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const detail = useAgentPresence(wsId, id);
  const dotSize = size >= 24 ? 8 : 6;

  return (
    <View style={{ width: size, height: size }} className="relative">
      {children}
      {detail !== 'loading' && (
        <View style={{ position: 'absolute', bottom: -1, right: -1 }} pointerEvents="none">
          <PresenceDot availability={detail.availability} size={dotSize} />
        </View>
      )}
    </View>
  );
}

/**
 * Стопка перекрывающихся аватаров — мобильный аналог web AvatarGroup,
 * построенный поверх ActorAvatar. Входные данные дедуплицируются по
 * паре type:id, чтобы один и тот же актор не показывался дважды.
 */

import { View } from 'react-native';
import { ActorAvatar } from '@/components/ui/actor-avatar';
import { Text } from '@/components/ui/text';

export interface StackActor {
  type: 'member' | 'agent' | null | undefined;
  id: string | null | undefined;
}

interface Props {
  actors: StackActor[];
  max?: number;
  size?: number;
}

export function AvatarStack({ actors, max = 3, size = 24 }: Props) {
  const deduped = dedupe(actors);
  const visible = deduped.slice(0, max);
  const overflow = deduped.length - visible.length;

  return (
    <View className="flex-row">
      {visible.map((actor, i) => (
        <Ring key={`${actor.type}:${actor.id}:${i}`} size={size} offset={i === 0 ? 0 : -size / 3}>
          <ActorAvatar type={actor.type} id={actor.id} size={size} />
        </Ring>
      ))}
      {overflow > 0 ? (
        <Ring size={size} offset={-size / 3}>
          <View
            style={{ width: size, height: size, borderRadius: size / 2 }}
            className="items-center justify-center bg-muted"
          >
            <Text className="text-[10px] font-medium text-muted-foreground">+{overflow}</Text>
          </View>
        </Ring>
      ) : null}
    </View>
  );
}

function Ring({
  size,
  offset,
  children,
}: {
  size: number;
  offset: number;
  children: React.ReactNode;
}) {
  return (
    <View
      style={{
        marginLeft: offset,
        width: size + 4,
        height: size + 4,
        borderRadius: (size + 4) / 2,
      }}
      className="bg-background items-center justify-center"
    >
      {children}
    </View>
  );
}

function dedupe(actors: StackActor[]): StackActor[] {
  const seen = new Set<string>();
  const out: StackActor[] = [];
  for (const a of actors) {
    const key = `${a.type ?? 'none'}:${a.id ?? 'none'}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(a);
  }
  return out;
}

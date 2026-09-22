'use client';

import { ActorAvatar as ActorAvatarBase } from '@goosar/ui/components/common/actor-avatar';
import { AVATAR_SIZE_PX, type AvatarSize } from '@goosar/ui/lib/avatar-size';
import { useActorName } from '@goosar/core/workspace/hooks';
import { cn } from '@goosar/ui/lib/utils';

interface AgentAvatarStackProps {
  agentIds: readonly string[];
  size?: AvatarSize;
  max?: number;
  opacity?: 'full' | 'half';
  className?: string;
}

export function AgentAvatarStack({
  agentIds,
  size = 'sm',
  max = 3,
  opacity = 'full',
  className,
}: AgentAvatarStackProps) {
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
  if (agentIds.length === 0) return null;

  const visible = agentIds.slice(0, max);
  const overflow = agentIds.length - visible.length;
  const px = AVATAR_SIZE_PX[size];
  const overlap = Math.round(px * 0.3);

  return (
    <span
      className={cn('inline-flex items-center', opacity === 'half' && 'opacity-50', className)}
      style={{ paddingLeft: 0 }}
    >
      {visible.map((id, i) => (
        <span
          key={id}
          style={{ marginLeft: i === 0 ? 0 : -overlap }}
          className="ring-2 ring-background rounded-full inline-flex"
        >
          <ActorAvatarBase
            name={getActorName('agent', id)}
            initials={getActorInitials('agent', id)}
            avatarUrl={getActorAvatarUrl('agent', id)}
            isAgent
            size={size}
          />
        </span>
      ))}
      {overflow > 0 && (
        <span
          style={{
            marginLeft: -overlap,
            width: px,
            height: px,
            fontSize: Math.max(9, Math.round(px * 0.45)),
          }}
          className="ring-2 ring-background rounded-full bg-muted text-muted-foreground inline-flex items-center justify-center font-medium tabular-nums"
          aria-label={`${overflow} more`}
        >
          +{overflow}
        </span>
      )}
    </span>
  );
}

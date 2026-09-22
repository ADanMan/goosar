'use client';

import { useState, useEffect } from 'react';
import { Bot, Users } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { AVATAR_SIZE_PX, DEFAULT_AVATAR_SIZE, type AvatarSize } from '@goosar/ui/lib/avatar-size';
import { parseAvatarEmoji } from '@goosar/ui/lib/avatar-emoji';
import { GoosarIcon } from './goosar-icon';

interface ActorAvatarProps {
  name: string;
  initials: string;
  avatarUrl?: string | null;
  isAgent?: boolean;
  isSystem?: boolean;
  isSquad?: boolean;
  size?: AvatarSize;
  className?: string;
}

function ActorAvatar({
  name,
  initials,
  avatarUrl,
  isAgent,
  isSystem,
  isSquad,
  size = DEFAULT_AVATAR_SIZE,
  className,
}: ActorAvatarProps) {
  const [imgError, setImgError] = useState(false);
  const px = AVATAR_SIZE_PX[size];
  const emoji = parseAvatarEmoji(avatarUrl);

  useEffect(() => {
    setImgError(false);
  }, [avatarUrl]);

  return (
    <div
      data-slot="avatar"
      className={cn(
        'inline-flex shrink-0 items-center justify-center font-medium overflow-hidden',
        (!avatarUrl || emoji || imgError) && 'bg-muted text-muted-foreground',
        className,
        'rounded-full',
      )}
      style={{ width: px, height: px, fontSize: px * 0.45 }}
    >
      {emoji ? (
        <span
          role="img"
          aria-label={name}
          className="select-none leading-none"
          style={{ fontSize: px * 0.58 }}
        >
          {emoji}
        </span>
      ) : avatarUrl && !imgError ? (
        <img
          src={avatarUrl}
          alt={name}
          className="h-full w-full object-cover"
          onError={() => setImgError(true)}
        />
      ) : isSystem ? (
        <GoosarIcon noSpin style={{ width: px * 0.55, height: px * 0.55 }} />
      ) : isAgent ? (
        <Bot style={{ width: px * 0.55, height: px * 0.55 }} />
      ) : isSquad ? (
        <Users style={{ width: px * 0.55, height: px * 0.55 }} />
      ) : (
        initials
      )}
    </div>
  );
}

export { ActorAvatar, type ActorAvatarProps };

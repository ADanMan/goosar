'use client';

import { useEffect, useRef, useState } from 'react';
import { ActorAvatar as ActorAvatarBase } from '@goosar/ui/components/common/actor-avatar';
import { AVATAR_SIZE_PX, type AvatarSize } from '@goosar/ui/lib/avatar-size';
import { HoverCard, HoverCardTrigger, HoverCardContent } from '@goosar/ui/components/ui/hover-card';
import { useActorName } from '@goosar/core/workspace/hooks';
import { useAgentPresenceDetail } from '@goosar/core/agents';
import { useCurrentWorkspace, useWorkspacePaths } from '@goosar/core/paths';
import { AgentProfileCard } from '../agents/components/agent-profile-card';
import { AgentLivePeekCard } from '../agents/components/agent-live-peek-card';
import { MemberProfileCard } from '../members/member-profile-card';
import { SquadProfileCard } from '../squads/components/squad-profile-card';
import { availabilityConfig } from '../agents/presence';
import { useNavigation } from '../navigation';

export type AgentHoverCardVariant = 'profile' | 'live';

interface ActorAvatarProps {
  actorType: string;
  actorId: string;
  size?: AvatarSize;
  className?: string;
  enableHoverCard?: boolean;
  showStatusDot?: boolean;
  hoverCardVariant?: AgentHoverCardVariant;
  profileLink?: boolean;
}

const FOCUSABLE_ANCESTOR_SELECTOR =
  'a[href], button:not([disabled]), [role="button"]:not([aria-disabled="true"]), [tabindex]:not([tabindex="-1"])';
const PROFILE_LINK_CONTROL_SELECTOR =
  'button, [role^="menuitem"], [role="option"], [data-slot="dropdown-menu-item"], [data-slot="dropdown-menu-checkbox-item"], [data-slot="popover-trigger"]';

export function ActorAvatar({
  actorType,
  actorId,
  size,
  className,
  enableHoverCard,
  showStatusDot,
  hoverCardVariant = 'profile',
  profileLink,
}: ActorAvatarProps) {
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
  const paths = useWorkspacePaths();
  const avatar = (
    <ActorAvatarBase
      name={getActorName(actorType, actorId)}
      initials={getActorInitials(actorType, actorId)}
      avatarUrl={getActorAvatarUrl(actorType, actorId)}
      isAgent={actorType === 'agent'}
      isSystem={actorType === 'system'}
      isSquad={actorType === 'squad'}
      size={size}
      className={className}
    />
  );

  const wrapDot = showStatusDot && actorType === 'agent';
  const dotted = wrapDot ? (
    <span className="relative inline-flex">
      {avatar}
      <AgentStatusDot agentId={actorId} size={size} />
    </span>
  ) : (
    avatar
  );
  const shouldLinkToProfile =
    profileLink ?? (actorType === 'member' || actorType === 'agent' || actorType === 'squad');
  const profileHref = shouldLinkToProfile
    ? actorType === 'member'
      ? paths.memberDetail(actorId)
      : actorType === 'agent'
        ? paths.agentDetail(actorId)
        : actorType === 'squad'
          ? paths.squadDetail(actorId)
          : null
    : null;
  const content = profileHref ? (
    <ActorAvatarProfileLink href={profileHref}>{dotted}</ActorAvatarProfileLink>
  ) : (
    dotted
  );

  if (!enableHoverCard) {
    return content;
  }
  if (actorType === 'agent') {
    return (
      <AgentAvatarHoverCard agentId={actorId} variant={hoverCardVariant}>
        {content}
      </AgentAvatarHoverCard>
    );
  }
  if (actorType === 'member') {
    return <MemberAvatarHoverCard userId={actorId}>{content}</MemberAvatarHoverCard>;
  }
  if (actorType === 'squad') {
    return <SquadAvatarHoverCard squadId={actorId}>{content}</SquadAvatarHoverCard>;
  }
  return content;
}

function ActorAvatarProfileLink({ href, children }: { href: string; children: React.ReactNode }) {
  const { push, openInNewTab } = useNavigation();

  const navigate = (event: React.MouseEvent | React.KeyboardEvent) => {
    const controlAncestor = event.currentTarget.parentElement?.closest(
      PROFILE_LINK_CONTROL_SELECTOR,
    );
    if (controlAncestor) return;

    event.preventDefault();
    event.stopPropagation();
    if ('metaKey' in event && (event.metaKey || event.ctrlKey || event.shiftKey) && openInNewTab) {
      openInNewTab(href);
      return;
    }
    push(href);
  };

  return (
    <span
      role="link"
      tabIndex={-1}
      className="inline-flex cursor-pointer rounded-full"
      onClick={navigate}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          navigate(event);
        }
      }}
    >
      {children}
    </span>
  );
}

export function AgentStatusDot({ agentId, size }: { agentId: string; size?: AvatarSize }) {
  const ws = useCurrentWorkspace();
  const detail = useAgentPresenceDetail(ws?.id, agentId);
  if (detail === 'loading') return null;

  const { dotClass, label } = availabilityConfig[detail.availability];
  const px = size ? AVATAR_SIZE_PX[size] : 24;
  const dotSize = px >= 24 ? 'h-1.5 w-1.5' : 'h-1 w-1';

  return (
    <span
      aria-label={`Status: ${label}`}
      className={`absolute bottom-0 right-0 rounded-full ring-1 ring-background ${dotClass} ${dotSize}`}
    />
  );
}

function AgentAvatarHoverCard({
  agentId,
  variant,
  children,
}: {
  agentId: string;
  variant: AgentHoverCardVariant;
  children: React.ReactNode;
}) {
  const content =
    variant === 'live' ? (
      <AgentLivePeekCard agentId={agentId} />
    ) : (
      <AgentProfileCard agentId={agentId} />
    );
  return <ActorAvatarHoverCardShell content={content}>{children}</ActorAvatarHoverCardShell>;
}

function MemberAvatarHoverCard({
  userId,
  children,
}: {
  userId: string;
  children: React.ReactNode;
}) {
  return (
    <ActorAvatarHoverCardShell content={<MemberProfileCard userId={userId} />}>
      {children}
    </ActorAvatarHoverCardShell>
  );
}

function SquadAvatarHoverCard({
  squadId,
  children,
}: {
  squadId: string;
  children: React.ReactNode;
}) {
  return (
    <ActorAvatarHoverCardShell content={<SquadProfileCard squadId={squadId} />}>
      {children}
    </ActorAvatarHoverCardShell>
  );
}

function ActorAvatarHoverCardShell({
  content,
  children,
}: {
  content: React.ReactNode;
  children: React.ReactNode;
}) {
  const triggerRef = useRef<HTMLSpanElement>(null);
  const [standalone, setStandalone] = useState(false);

  useEffect(() => {
    const el = triggerRef.current;
    if (!el) return;
    const ancestor = el.parentElement?.closest(FOCUSABLE_ANCESTOR_SELECTOR);
    setStandalone(!ancestor);
  }, []);

  const tabIndex = standalone ? 0 : -1;
  const className = standalone
    ? 'inline-flex cursor-pointer rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
    : 'inline-flex cursor-pointer';

  return (
    <HoverCard>
      <HoverCardTrigger
        render={<span ref={triggerRef} />}
        tabIndex={tabIndex}
        className={className}
      >
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        {content}
      </HoverCardContent>
    </HoverCard>
  );
}

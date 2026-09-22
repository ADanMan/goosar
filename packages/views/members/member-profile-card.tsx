'use client';

import { useQuery } from '@tanstack/react-query';
import type { Agent, MemberRole } from '@goosar/core/types';
import { useWorkspaceId } from '@goosar/core';
import { agentRunCounts30dOptions } from '@goosar/core/agents';
import { agentListOptions, memberListOptions } from '@goosar/core/workspace/queries';
import { resolvePublicFileUrl } from '@goosar/core/workspace/avatar-url';
import { useWorkspacePaths } from '@goosar/core/paths';
import { ActorAvatar as ActorAvatarBase } from '@goosar/ui/components/common/actor-avatar';
import { Skeleton } from '@goosar/ui/components/ui/skeleton';
import { ActorAvatar } from '../common/actor-avatar';
import { AppLink } from '../navigation';
import { useT } from '../i18n';

interface MemberProfileCardProps {
  userId: string;
}

export function MemberProfileCard({ userId }: MemberProfileCardProps) {
  const { t } = useT('members');
  const wsId = useWorkspaceId();
  const { data: members = [], isLoading: membersLoading } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runCounts = [] } = useQuery(agentRunCounts30dOptions(wsId));

  const member = members.find((m) => m.user_id === userId);

  if (membersLoading && !member) {
    return (
      <div className="flex items-center gap-3">
        <Skeleton className="h-10 w-10 rounded-full" />
        <div className="flex-1 space-y-1.5">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-3 w-20" />
        </div>
      </div>
    );
  }

  if (!member) {
    return <div className="text-xs text-muted-foreground">{t(($) => $.card.unavailable)}</div>;
  }

  const initials = member.name
    .split(' ')
    .map((w) => w[0])
    .join('')
    .toUpperCase()
    .slice(0, 2);

  const runCountById = new Map(runCounts.map((r) => [r.agent_id, r.run_count]));
  const ownedAgents = agents
    .filter((a) => a.owner_id === userId && !a.archived_at)
    .sort((a, b) => {
      const ra = runCountById.get(a.id) ?? 0;
      const rb = runCountById.get(b.id) ?? 0;
      if (ra !== rb) return rb - ra;
      return a.name.localeCompare(b.name);
    });

  return (
    <div className="flex flex-col gap-3 text-left">
      {/* Header */}
      <div className="flex items-start gap-3">
        <ActorAvatarBase
          name={member.name}
          initials={initials}
          avatarUrl={resolvePublicFileUrl(member.avatar_url)}
          size="xl"
          className="rounded-full"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <p className="truncate text-sm font-semibold">{member.name}</p>
            <RoleBadge role={member.role} />
          </div>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">{member.email}</p>
        </div>
      </div>

      {/* Owned agents */}
      {ownedAgents.length > 0 && <OwnedAgentsSection agents={ownedAgents} />}
    </div>
  );
}

function RoleBadge({ role }: { role: MemberRole }) {
  const { t } = useT('members');
  return (
    <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
      {role === 'owner'
        ? t(($) => $.role.owner)
        : role === 'admin'
          ? t(($) => $.role.admin)
          : t(($) => $.role.member)}
    </span>
  );
}

function OwnedAgentsSection({ agents }: { agents: Agent[] }) {
  const { t } = useT('members');
  const p = useWorkspacePaths();
  const visible = agents.slice(0, 2);
  const overflow = agents.length - visible.length;

  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <span className="text-muted-foreground">
        {t(($) => $.card.agents_section, { count: agents.length })}
      </span>
      <div className="flex flex-col gap-0.5">
        {visible.map((a) => (
          <AppLink
            key={a.id}
            href={p.agentDetail(a.id)}
            className="group -mx-1 flex cursor-pointer items-start gap-2 rounded-md px-1 py-1 transition-colors hover:bg-accent"
          >
            <ActorAvatar
              actorType="agent"
              actorId={a.id}
              size="sm"
              showStatusDot
              className="mt-0.5 shrink-0"
            />
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{a.name}</div>
              {a.description && (
                <div className="truncate text-muted-foreground">{a.description}</div>
              )}
            </div>
            <span
              aria-hidden
              className="mt-0.5 shrink-0 font-normal text-brand opacity-0 transition-opacity group-hover:opacity-100"
            >
              {t(($) => $.card.detail_link)}
            </span>
          </AppLink>
        ))}
        {overflow > 0 && (
          <span className="text-muted-foreground">
            {t(($) => $.card.more_agents, { count: overflow })}
          </span>
        )}
      </div>
    </div>
  );
}

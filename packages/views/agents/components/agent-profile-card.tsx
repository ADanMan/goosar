'use client';

import { useQuery } from '@tanstack/react-query';
import type { Agent, AgentRuntime } from '@goosar/core/types';
import { useAgentPresenceDetail } from '@goosar/core/agents';
import { useWorkspaceId } from '@goosar/core/hooks';
import {
  deriveRuntimeHealth,
  runtimeDisplayLabel,
  type RuntimeHealth,
} from '@goosar/core/runtimes';
import { agentListOptions, memberListOptions } from '@goosar/core/workspace/queries';
import { resolvePublicFileUrl } from '@goosar/core/workspace/avatar-url';
import { runtimeListOptions } from '@goosar/core/runtimes/queries';
import { useWorkspacePaths } from '@goosar/core/paths';
import { ActorAvatar as ActorAvatarBase } from '@goosar/ui/components/common/actor-avatar';
import { Skeleton } from '@goosar/ui/components/ui/skeleton';
import { AppLink } from '../../navigation';
import { HealthIcon } from '../../runtimes/components/shared';
import { availabilityConfig } from '../presence';
import { VisibilityBadge } from './visibility-badge';
import { useT } from '../../i18n';

interface AgentProfileCardProps {
  agentId: string;
}

export function AgentProfileCard({ agentId }: AgentProfileCardProps) {
  const { t } = useT('agents');
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data: agents = [], isLoading: agentsLoading } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));

  const agent = agents.find((a) => a.id === agentId);

  if (agentsLoading && !agent) {
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

  if (!agent) {
    return (
      <div className="text-xs text-muted-foreground">{t(($) => $.profile_card.unavailable)}</div>
    );
  }

  const owner = agent.owner_id ? (members.find((m) => m.user_id === agent.owner_id) ?? null) : null;
  const runtime = runtimes.find((r) => r.id === agent.runtime_id) ?? null;
  const isArchived = !!agent.archived_at;
  const initials = agent.name
    .split(' ')
    .map((w) => w[0])
    .join('')
    .toUpperCase()
    .slice(0, 2);

  return (
    <div className="group flex flex-col gap-3 text-left">
      {/* Header — avatar + name + availability on the left, "Detail →" link
          on the right (hover-only). Card stays minimal: only the 3-state
          availability dot is surfaced here; last-task state lives in the
          agents list and the agent detail page. */}
      <div className="flex items-start gap-3">
        <ActorAvatarBase
          name={agent.name}
          initials={initials}
          avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
          isAgent
          size="xl"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <p className="truncate text-sm font-semibold">{agent.name}</p>
            {!isArchived && <VisibilityBadge value={agent.visibility} compact />}
            {isArchived && (
              <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                {t(($) => $.row.archived)}
              </span>
            )}
          </div>
          {!isArchived && <AgentAvailabilityLine wsId={wsId} agentId={agent.id} />}
        </div>
        {!isArchived && (
          <AppLink
            href={p.agentDetail(agent.id)}
            className="mr-1 mt-0.5 shrink-0 text-xs font-normal text-brand opacity-0 transition-opacity group-hover:opacity-100"
          >
            {t(($) => $.profile_card.detail_link)}
          </AppLink>
        )}
      </div>

      {/* Description */}
      {agent.description && (
        <p className="line-clamp-2 text-xs text-muted-foreground">{agent.description}</p>
      )}

      {/* Meta rows — runtime (where it lives), model (what powers it),
          skills (what it knows), owner (who manages it). Model + effort
          are surfaced here so a quick hover answers "which model is this
          agent running?" without opening the detail page. */}
      <div className="flex flex-col gap-1.5 text-xs">
        <RuntimeRow agent={agent} runtime={runtime} />
        <ModelRow model={agent.model} thinkingLevel={agent.thinking_level} />
        {agent.skills.length > 0 && <SkillsRow skills={agent.skills.map((s) => s.name)} />}
        {owner && <MetaRow label={t(($) => $.profile_card.owner_label)} value={owner.name} />}
      </div>
    </div>
  );
}

function AgentAvailabilityLine({ wsId, agentId }: { wsId: string | undefined; agentId: string }) {
  const { t } = useT('agents');
  const detail = useAgentPresenceDetail(wsId, agentId);
  if (detail === 'loading') {
    return <Skeleton className="mt-0.5 h-3 w-16" />;
  }
  const av = availabilityConfig[detail.availability];
  return (
    <div className="mt-0.5 inline-flex items-center gap-1.5">
      <span className={`h-1.5 w-1.5 rounded-full ${av.dotClass}`} />
      <span className={`text-xs ${av.textClass}`}>
        {t(($) => $.availability[detail.availability])}
      </span>
    </div>
  );
}

function RuntimeRow({ agent, runtime }: { agent: Agent; runtime: AgentRuntime | null }) {
  const { t } = useT('agents');
  const isCloud = agent.runtime_mode === 'cloud';
  const health: RuntimeHealth = isCloud
    ? 'online'
    : runtime
      ? deriveRuntimeHealth(runtime, Date.now())
      : 'offline';
  const label = runtime
    ? runtimeDisplayLabel(runtime)
    : isCloud
      ? t(($) => $.row.fallback_runtime_cloud)
      : t(($) => $.profile_card.unknown_runtime);
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-12 shrink-0 text-muted-foreground">
        {t(($) => $.profile_card.runtime_label)}
      </span>
      <HealthIcon health={health} className="h-3 w-3 shrink-0" />
      <span className="min-w-0 truncate" title={label}>
        {label}
      </span>
    </div>
  );
}

function ModelRow({ model, thinkingLevel }: { model: string; thinkingLevel?: string }) {
  const { t } = useT('agents');
  const hasModel = model.trim().length > 0;
  const effort = thinkingLevel?.trim();
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-12 shrink-0 text-muted-foreground">
        {t(($) => $.profile_card.model_label)}
      </span>
      {hasModel ? (
        <span className="min-w-0 truncate font-mono text-[11px]" title={model}>
          {model}
        </span>
      ) : (
        <span className="min-w-0 truncate text-muted-foreground">
          {t(($) => $.profile_card.model_unset)}
        </span>
      )}
      {effort && (
        <span className="shrink-0 rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
          {effort}
        </span>
      )}
    </div>
  );
}

function MetaRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-12 shrink-0 text-muted-foreground">{label}</span>
      <span className={`truncate ${mono ? 'font-mono text-[11px]' : ''}`} title={value}>
        {value}
      </span>
    </div>
  );
}

function SkillsRow({ skills }: { skills: string[] }) {
  const { t } = useT('agents');
  const visible = skills.slice(0, 3);
  const overflow = skills.length - visible.length;
  return (
    <div className="flex items-start gap-1.5">
      <span className="w-12 shrink-0 pt-0.5 text-muted-foreground">
        {t(($) => $.profile_card.skills_label)}
      </span>
      <div className="flex min-w-0 flex-wrap gap-1">
        {visible.map((s) => (
          <span
            key={s}
            className="max-w-full truncate rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground"
            title={s}
          >
            {s}
          </span>
        ))}
        {overflow > 0 && (
          <span className="shrink-0 rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
            +{overflow}
          </span>
        )}
      </div>
    </div>
  );
}

'use client';

import { useQuery } from '@tanstack/react-query';
import { ActorAvatar } from '@goosar/ui/components/common/actor-avatar';
import { Button } from '@goosar/ui/components/ui/button';
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@goosar/ui/components/ui/hover-card';
import { workspaceWorkingAgentsOptions } from '@goosar/core/agents';
import { useWorkspaceId } from '@goosar/core/hooks';
import type { WorkspaceWorkingAgent, WorkspaceWorkingAgentMineRelation } from '@goosar/core/types';
import { AgentAvatarStack } from '../../agents/components/agent-avatar-stack';
import { useT } from '../../i18n';

interface WorkspaceAgentWorkingChipProps {
  value: boolean;
  onToggle: () => void;
  mineRelation?: WorkspaceWorkingAgentMineRelation;
}

export function chipAppearance(
  value: boolean,
  hasAgents: boolean,
): { variant: 'brand' | 'brandSubtle' | 'outline'; className: string } {
  const layout = 'h-8 px-2 md:h-7 md:px-2.5';
  if (value) return { variant: 'brand', className: layout };
  if (hasAgents) return { variant: 'brandSubtle', className: layout };
  return { variant: 'outline', className: `${layout} text-muted-foreground` };
}

export function WorkingAgentsHoverContent({
  agents,
}: {
  agents: readonly WorkspaceWorkingAgent[];
}) {
  const { t } = useT('issues');

  if (agents.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">{t(($) => $.agent_activity.empty_hover)}</p>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs font-medium text-muted-foreground">
        {t(($) => $.agent_activity.hover_header, { count: agents.length })}
      </div>
      <div className="flex flex-col gap-1.5">
        {agents.map((agent) => (
          <div key={agent.id} className="flex items-center gap-2 text-xs">
            <ActorAvatar
              name={agent.name}
              initials={agent.name.trim().slice(0, 2).toUpperCase()}
              avatarUrl={agent.avatar_url ?? undefined}
              isAgent
              size="sm"
            />
            <span className="min-w-0 flex-1 truncate font-medium">{agent.name}</span>
            <span className="shrink-0 tabular-nums text-muted-foreground">
              {t(($) => $.agent_activity.tasks_count, {
                count: agent.running_task_count,
              })}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

export function WorkspaceAgentWorkingChip({
  value,
  onToggle,
  mineRelation,
}: WorkspaceAgentWorkingChipProps) {
  const { t } = useT('issues');
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(
    workspaceWorkingAgentsOptions(wsId, 'issue', mineRelation),
  );
  const agentIds = agents.map((agent) => agent.id);
  const agentCount = agents.length;
  const hasAgents = agentCount > 0;
  const label = t(($) => $.agent_activity.chip_agents_working, {
    count: agentCount,
  });
  const appearance = chipAppearance(value, hasAgents);

  const trigger = (
    <Button
      variant={appearance.variant}
      size="sm"
      className={appearance.className}
      onClick={onToggle}
      aria-pressed={value}
      aria-label={label}
    >
      {hasAgents && <AgentAvatarStack agentIds={agentIds} size="sm" max={3} />}
      <span className="tabular-nums md:hidden">{agentCount}</span>
      <span className="hidden tabular-nums md:inline">{label}</span>
    </Button>
  );

  return (
    <HoverCard>
      <HoverCardTrigger render={trigger} />
      <HoverCardContent align="end" className="w-72">
        <WorkingAgentsHoverContent agents={agents} />
      </HoverCardContent>
    </HoverCard>
  );
}

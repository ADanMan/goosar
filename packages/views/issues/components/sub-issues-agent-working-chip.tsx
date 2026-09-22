'use client';

import { memo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { HoverCard, HoverCardTrigger, HoverCardContent } from '@goosar/ui/components/ui/hover-card';
import { useWorkspaceId } from '@goosar/core/hooks';
import { workspaceWorkingAgentsOptions } from '@goosar/core/agents';
import {
  useRealtimePollingInterval,
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS,
} from '@goosar/core/realtime';
import { AgentAvatarStack } from '../../agents/components/agent-avatar-stack';
import { WorkingAgentsHoverContent } from './workspace-agent-working-chip';
import { useT } from '../../i18n';

interface SubIssuesAgentWorkingChipProps {
  parentIssueId: string;
}

export const SubIssuesAgentWorkingChip = memo(function SubIssuesAgentWorkingChip({
  parentIssueId,
}: SubIssuesAgentWorkingChipProps) {
  const { t } = useT('issues');
  const wsId = useWorkspaceId();
  const pollingInterval = useRealtimePollingInterval(BACKGROUND_DEGRADED_POLL_INTERVAL_MS);
  const { data: agents = [] } = useQuery({
    ...workspaceWorkingAgentsOptions(wsId, 'issue', undefined, parentIssueId),
    refetchInterval: pollingInterval,
  });

  if (agents.length === 0) return null;

  const agentIds = agents.map((agent) => agent.id);

  return (
    <HoverCard>
      <HoverCardTrigger
        render={
          <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-muted/60 px-2 py-0.5" />
        }
      >
        <AgentAvatarStack agentIds={agentIds} size="xs" max={3} />
        <span className="animate-chat-text-shimmer text-[11px] font-medium leading-none tabular-nums">
          {t(($) => $.agent_activity.chip_agents_working, {
            count: agentIds.length,
          })}
        </span>
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        <WorkingAgentsHoverContent agents={agents} />
      </HoverCardContent>
    </HoverCard>
  );
});

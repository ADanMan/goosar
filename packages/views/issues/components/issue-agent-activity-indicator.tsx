'use client';

import { memo, useCallback, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { HoverCard, HoverCardTrigger, HoverCardContent } from '@goosar/ui/components/ui/hover-card';
import { useWorkspaceId } from '@goosar/core/hooks';
import { agentTaskSnapshotOptions } from '@goosar/core/agents';
import type { AgentTask } from '@goosar/core/types';
import { cn } from '@goosar/ui/lib/utils';
import type { AvatarSize } from '@goosar/ui/lib/avatar-size';
import { AgentAvatarStack } from '../../agents/components/agent-avatar-stack';
import { AgentActivityHoverContent } from '../../agents/components/agent-activity-hover-content';
import { selectIssueTasks, type IssueTaskGroups } from '../surface/activity';
import { useT } from '../../i18n';

const EMPTY_GROUPS: IssueTaskGroups = { running: [], queued: [] };

const OPEN_DELAY_MS = 900;
const CLOSE_DELAY_MS = 150;

interface IssueAgentActivityIndicatorProps {
  issueId: string;
  size?: AvatarSize;
  hoverCard?: boolean;
}

export const IssueAgentActivityIndicator = memo(function IssueAgentActivityIndicator({
  issueId,
  size = 'xs',
  hoverCard = true,
}: IssueAgentActivityIndicatorProps) {
  const { t } = useT('issues');
  const wsId = useWorkspaceId();
  const select = useCallback(
    (snapshot: AgentTask[]) => selectIssueTasks(snapshot, issueId),
    [issueId],
  );
  const { data: groups = EMPTY_GROUPS } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    select,
  });

  const { agentIds, opacity } = useMemo(() => {
    const primary = groups.running.length > 0 ? groups.running : groups.queued;
    const uniqueAgents = [...new Set(primary.map((t) => t.agent_id))];
    return {
      agentIds: uniqueAgents,
      opacity: (groups.running.length > 0 ? 'full' : 'half') as 'full' | 'half',
    };
  }, [groups]);

  if (agentIds.length === 0) return null;
  const isRunning = opacity === 'full';

  const badge = (
    <>
      <AgentAvatarStack agentIds={agentIds} size={size} opacity={opacity} max={3} />
      <span
        className={cn(
          'text-[10px] leading-none',
          isRunning ? 'animate-chat-text-shimmer' : 'text-muted-foreground',
        )}
      >
        {isRunning
          ? t(($) => $.agent_activity.status_running)
          : t(($) => $.agent_activity.status_queued)}
      </span>
    </>
  );

  if (!hoverCard) {
    return <span className="inline-flex shrink-0 items-center gap-1">{badge}</span>;
  }

  const hoverTasks = [...groups.running, ...groups.queued];

  return (
    <HoverCard>
      <HoverCardTrigger
        delay={OPEN_DELAY_MS}
        closeDelay={CLOSE_DELAY_MS}
        render={<span className="inline-flex shrink-0 items-center gap-1" />}
      >
        {badge}
      </HoverCardTrigger>
      <HoverCardContent align="end" className="w-72">
        <AgentActivityHoverContent tasks={hoverTasks} />
      </HoverCardContent>
    </HoverCard>
  );
});

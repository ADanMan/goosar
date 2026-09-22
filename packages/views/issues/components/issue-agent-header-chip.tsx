'use client';

import { memo, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Popover, PopoverContent, PopoverTrigger } from '@goosar/ui/components/ui/popover';
import { useActorName } from '@goosar/core/workspace/hooks';
import { cn } from '@goosar/ui/lib/utils';
import { api } from '@goosar/core/api';
import { issueKeys } from '@goosar/core/issues/queries';
import { useRealtimePollingInterval } from '@goosar/core/realtime';
import type { AgentTask } from '@goosar/core/types';
import { AgentAvatarStack } from '../../agents/components/agent-avatar-stack';
import { ActiveTaskRow } from './execution-log-section';
import { useT } from '../../i18n';

interface IssueAgentHeaderChipProps {
  issueId: string;
}

export const IssueAgentHeaderChip = memo(function IssueAgentHeaderChip({
  issueId,
}: IssueAgentHeaderChipProps) {
  const pollingInterval = useRealtimePollingInterval();
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    refetchInterval: pollingInterval,
  });

  const { running, queued } = useMemo(() => {
    const running: AgentTask[] = [];
    const queued: AgentTask[] = [];
    for (const task of tasks) {
      if (task.status === 'running') running.push(task);
      else if (
        task.status === 'queued' ||
        task.status === 'dispatched' ||
        task.status === 'waiting_local_directory'
      )
        queued.push(task);
      // Terminal statuses are the execution log's story, not the live chip's.
    }
    return { running, queued };
  }, [tasks]);

  if (running.length === 0 && queued.length === 0) return null;

  return <ActiveChip issueId={issueId} running={running} queued={queued} />;
});

interface ActiveChipProps {
  issueId: string;
  running: AgentTask[];
  queued: AgentTask[];
}

function ActiveChip({ issueId, running, queued }: ActiveChipProps) {
  const { t } = useT('issues');
  const { getActorName } = useActorName();

  const activeTasks = [...running, ...queued];
  const agentIds = [...new Set(activeTasks.map((task) => task.agent_id))];
  const anyRunning = running.length > 0;
  const isSingle = agentIds.length === 1;
  const label = isSingle
    ? t(($) => (anyRunning ? $.agent_live.is_working : $.agent_live.is_queued), {
        name: getActorName('agent', agentIds[0] ?? ''),
      })
    : t(
        ($) => (anyRunning ? $.agent_activity.hover_header : $.agent_activity.hover_header_queued),
        { count: agentIds.length },
      );

  return (
    <div className="flex items-center gap-1">
      <Popover>
        {/* Hover opens the card so the live activity reads as a glanceable
            status surface, not a click target. In Base UI the hover config
            lives on the Trigger (a popover can have multiple triggers), not
            the Root. The trigger stays a real button, so click and keyboard
            (Enter/Space) still toggle it for touch and a11y. A short open
            delay avoids flicker when the pointer merely passes over the chip;
            the close delay keeps it open while the pointer travels across the
            hover bridge into the interactive rows. */}
        <PopoverTrigger
          openOnHover
          delay={150}
          closeDelay={200}
          render={
            <button
              type="button"
              aria-label={label}
              className={cn(
                'flex h-7 max-w-[11rem] items-center gap-1.5 rounded-md px-1.5 text-muted-foreground outline-none transition-colors hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring',
                anyRunning && 'border-beam bg-brand/5',
              )}
            />
          }
        >
          <AgentAvatarStack
            agentIds={agentIds}
            size="sm"
            max={3}
            opacity={anyRunning ? 'full' : 'half'}
          />
          <span
            className={`min-w-0 truncate text-xs ${anyRunning ? 'text-info' : 'text-muted-foreground'}`}
          >
            {label}
          </span>
        </PopoverTrigger>
        <PopoverContent align="end" keepMounted className="w-80">
          <div className="text-xs font-medium text-muted-foreground">
            {t(
              ($) =>
                anyRunning ? $.agent_activity.hover_header : $.agent_activity.hover_header_queued,
              { count: agentIds.length },
            )}
          </div>
          <div className="flex flex-col gap-0.5">
            {activeTasks.map((task) => (
              <ActiveTaskRow key={task.id} task={task} issueId={issueId} />
            ))}
          </div>
        </PopoverContent>
      </Popover>
      {/* Separator from the action buttons — the chip is a status segment,
          not another button, so a hairline keeps the two groups legible. */}
      <span className="h-4 w-px bg-border" aria-hidden="true" />
    </div>
  );
}

'use client';

import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ActorAvatar as ActorAvatarBase } from '@goosar/ui/components/common/actor-avatar';
import { useActorName } from '@goosar/core/workspace/hooks';
import { useWorkspaceId } from '@goosar/core/hooks';
import { runtimeListOptions } from '@goosar/core/runtimes/queries';
import { agentListOptions } from '@goosar/core/workspace/queries';
import { deriveAgentAvailability } from '@goosar/core/agents';
import type { AgentTask, Issue } from '@goosar/core/types';
import { workloadConfig } from '../presence';
import { useT } from '../../i18n';

interface AgentActivityHoverContentProps {
  tasks: readonly AgentTask[];
}

function useActivityNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}

function useActivityLookups() {
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const agentById = new Map(agents.map((a) => [a.id, a] as const));
  const runtimeById = new Map(runtimes.map((r) => [r.id, r] as const));
  return { agentById, runtimeById };
}

type ActivityLookups = ReturnType<typeof useActivityLookups>;

function AgentActivityTaskRow({
  task,
  now,
  agentById,
  runtimeById,
}: {
  task: AgentTask;
  now: number;
} & ActivityLookups) {
  const { t } = useT('issues');
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();

  const agent = agentById.get(task.agent_id);
  const runtime = runtimeFrom(agent?.runtime_id, runtimeById);
  const availability = deriveAgentAvailability(runtime, now);
  const isRunning = task.status === 'running';
  const wl = isRunning ? workloadConfig.working : workloadConfig.queued;
  const dotClass = isRunning
    ? 'bg-brand'
    : availability === 'online'
      ? 'bg-muted-foreground/40'
      : 'bg-warning';
  const labelClass = isRunning
    ? wl.textClass
    : availability === 'online'
      ? 'text-muted-foreground'
      : wl.textClass;
  const startedFrom = isRunning
    ? (task.started_at ?? task.dispatched_at ?? task.created_at)
    : task.created_at;

  return (
    <div className="flex items-center gap-2 text-xs">
      <ActorAvatarBase
        name={getActorName('agent', task.agent_id)}
        initials={getActorInitials('agent', task.agent_id)}
        avatarUrl={getActorAvatarUrl('agent', task.agent_id)}
        isAgent
        size="sm"
      />
      <span className="flex-1 truncate font-medium">{getActorName('agent', task.agent_id)}</span>
      <span className="flex shrink-0 items-center gap-1.5">
        <span className={`h-1.5 w-1.5 rounded-full ${dotClass}`} />
        <span className={labelClass}>
          {isRunning
            ? t(($) => $.agent_activity.status_running)
            : t(($) => $.agent_activity.status_queued)}
        </span>
        <span className="tabular-nums text-muted-foreground">
          {formatDuration(startedFrom, now)}
        </span>
      </span>
    </div>
  );
}

export function AgentActivityHoverContent({ tasks }: AgentActivityHoverContentProps) {
  const { t } = useT('issues');
  const now = useActivityNow();
  const { agentById, runtimeById } = useActivityLookups();

  if (tasks.length === 0) return null;

  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs font-medium text-muted-foreground">
        {/* One row per task, so count tasks — not agents. A single agent can
            run several tasks at once, so an agent-worded header here would
            disagree with the row count below. */}
        {t(($) => $.agent_activity.hover_header_tasks, { count: tasks.length })}
      </div>
      <div className="flex flex-col gap-1.5">
        {tasks.map((task) => (
          <AgentActivityTaskRow
            key={task.id}
            task={task}
            now={now}
            agentById={agentById}
            runtimeById={runtimeById}
          />
        ))}
      </div>
    </div>
  );
}

interface WorkspaceAgentActivityHoverContentProps {
  issues: readonly Issue[];
  tasksByIssueId: ReadonlyMap<string, readonly AgentTask[]>;
  taskCount: number;
}

export function WorkspaceAgentActivityHoverContent({
  issues,
  tasksByIssueId,
  taskCount,
}: WorkspaceAgentActivityHoverContentProps) {
  const { t } = useT('issues');
  const now = useActivityNow();
  const { agentById, runtimeById } = useActivityLookups();

  if (issues.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">{t(($) => $.agent_activity.empty_hover)}</p>
    );
  }

  return (
    <div className="flex flex-col gap-2.5">
      <div className="text-xs font-medium text-muted-foreground">
        {`${t(($) => $.agent_activity.issues_count, {
          count: issues.length,
        })} · ${t(($) => $.agent_activity.tasks_count, { count: taskCount })}`}
      </div>
      <div className="flex flex-col gap-2.5">
        {issues.map((issue) => (
          <div key={issue.id} className="flex flex-col gap-1.5">
            <div className="flex items-baseline gap-1.5 text-xs">
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                {issue.identifier}
              </span>
              <span className="truncate">{issue.title}</span>
            </div>
            <div className="flex flex-col gap-1.5">
              {(tasksByIssueId.get(issue.id) ?? []).map((task) => (
                <AgentActivityTaskRow
                  key={task.id}
                  task={task}
                  now={now}
                  agentById={agentById}
                  runtimeById={runtimeById}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function runtimeFrom<T extends { id: string }>(
  id: string | undefined,
  byId: Map<string, T>,
): T | null {
  if (!id) return null;
  return byId.get(id) ?? null;
}

export function formatDuration(fromIso: string, nowMs: number): string {
  const start = new Date(fromIso).getTime();
  if (!Number.isFinite(start)) return '';
  const sec = Math.max(0, Math.round((nowMs - start) / 1000));
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  const remSec = sec % 60;
  if (min < 60) return `${min}m ${pad2(remSec)}s`;
  const hr = Math.floor(min / 60);
  const remMin = min % 60;
  return `${hr}h ${pad2(remMin)}m`;
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

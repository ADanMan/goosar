'use client';

import { useQuery } from '@tanstack/react-query';
import { ActorAvatar as ActorAvatarBase } from '@goosar/ui/components/common/actor-avatar';
import { Skeleton } from '@goosar/ui/components/ui/skeleton';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useWorkspacePaths } from '@goosar/core/paths';
import { agentListOptions } from '@goosar/core/workspace/queries';
import { resolvePublicFileUrl } from '@goosar/core/workspace/avatar-url';
import { agentTaskSnapshotOptions, useAgentPresenceDetail } from '@goosar/core/agents';
import { issueDetailOptions } from '@goosar/core/issues';
import type { AgentTask } from '@goosar/core/types';
import { AlertTriangle } from 'lucide-react';
import { AppLink } from '../../navigation';
import { useT, useTimeAgo } from '../../i18n';
import { availabilityConfig, workloadConfig } from '../presence';

interface AgentLivePeekCardProps {
  agentId: string;
}

export function AgentLivePeekCard({ agentId }: AgentLivePeekCardProps) {
  const { t } = useT('agents');
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data: agents = [], isLoading: agentsLoading } = useQuery(agentListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const presence = useAgentPresenceDetail(wsId, agentId);

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

  const agentTasks = snapshot.filter((t) => t.agent_id === agentId);
  const runningTask = agentTasks.find((t) => t.status === 'running' && !!t.issue_id);
  const currentIssueId = runningTask?.issue_id ?? null;
  const lastTerminal = pickLatestTerminal(agentTasks);

  const initials = agent.name
    .split(' ')
    .map((w) => w[0])
    .join('')
    .toUpperCase()
    .slice(0, 2);

  const isArchived = presence !== 'loading' && presence.availability === 'archived';
  const workload = presence === 'loading' ? null : presence.workload;
  const workloadVisual = workload ? workloadConfig[workload] : null;
  const archivedVisual = availabilityConfig.archived;

  return (
    <div className="flex flex-col gap-3 text-left">
      {/* Header — avatar + name. */}
      <div className="flex items-start gap-3">
        <ActorAvatarBase
          name={agent.name}
          initials={initials}
          avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
          isAgent
          size="xl"
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold">{agent.name}</p>
          <div className="mt-0.5 inline-flex items-center gap-1.5">
            {isArchived ? (
              <>
                <archivedVisual.icon className={`h-3 w-3 shrink-0 ${archivedVisual.textClass}`} />
                <span className={`text-xs ${archivedVisual.textClass}`}>
                  {t(($) => $.availability.archived)}
                </span>
              </>
            ) : workloadVisual ? (
              <>
                <workloadVisual.icon className={`h-3 w-3 shrink-0 ${workloadVisual.textClass}`} />
                <span className={`text-xs ${workloadVisual.textClass}`}>
                  {t(($) => $.workload[workload!])}
                </span>
              </>
            ) : (
              <Skeleton className="h-3 w-12" />
            )}
          </div>
        </div>
      </div>

      {/* Meta rows. */}
      <div className="flex flex-col gap-1.5 text-xs">
        <CurrentIssueRow
          wsId={wsId}
          issueId={currentIssueId}
          label={t(($) => $.live_peek.current_issue_label)}
          emptyLabel={t(($) => $.live_peek.no_current_issue)}
          issueHref={(id) => p.issueDetail(id)}
        />
        <LastActivityRow
          task={lastTerminal}
          label={t(($) => $.live_peek.last_activity_label)}
          emptyLabel={t(($) => $.live_peek.no_recent_activity)}
          failedLabel={t(($) => $.live_peek.failed_indicator)}
        />
      </div>
    </div>
  );
}

function pickLatestTerminal(tasks: readonly AgentTask[]): AgentTask | null {
  let best: AgentTask | null = null;
  for (const t of tasks) {
    if (t.status !== 'completed' && t.status !== 'failed' && t.status !== 'cancelled') {
      continue;
    }
    if (!t.completed_at) continue;
    if (!best || (best.completed_at && t.completed_at > best.completed_at)) {
      best = t;
    }
  }
  return best;
}

function CurrentIssueRow({
  wsId,
  issueId,
  label,
  emptyLabel,
  issueHref,
}: {
  wsId: string;
  issueId: string | null;
  label: string;
  emptyLabel: string;
  issueHref: (id: string) => string;
}) {
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueId ?? ''),
    enabled: !!issueId,
  });

  return (
    <div className="flex items-center gap-1.5">
      <span className="w-16 shrink-0 text-muted-foreground">{label}</span>
      {issueId ? (
        issue ? (
          <AppLink
            href={issueHref(issueId)}
            className="min-w-0 truncate text-brand hover:underline"
            title={`${issue.identifier} ${issue.title}`}
          >
            <span className="mr-1 font-mono text-[11px]">{issue.identifier}</span>
            <span>{issue.title}</span>
          </AppLink>
        ) : (
          <Skeleton className="h-3 w-24" />
        )
      ) : (
        <span className="text-muted-foreground">{emptyLabel}</span>
      )}
    </div>
  );
}

function LastActivityRow({
  task,
  label,
  emptyLabel,
  failedLabel,
}: {
  task: AgentTask | null;
  label: string;
  emptyLabel: string;
  failedLabel: string;
}) {
  const timeAgo = useTimeAgo();
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-16 shrink-0 text-muted-foreground">{label}</span>
      {task && task.completed_at ? (
        <span className="inline-flex min-w-0 items-center gap-1 truncate">
          <span className="truncate">{timeAgo(task.completed_at)}</span>
          {task.status === 'failed' && (
            <span
              className="inline-flex items-center gap-0.5 rounded bg-warning/10 px-1 py-0.5 text-[10px] font-medium text-warning"
              title={failedLabel}
            >
              <AlertTriangle className="h-2.5 w-2.5" />
              {failedLabel}
            </span>
          )}
        </span>
      ) : (
        <span className="text-muted-foreground">{emptyLabel}</span>
      )}
    </div>
  );
}

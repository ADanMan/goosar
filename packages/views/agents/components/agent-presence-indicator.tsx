'use client';

import { Skeleton } from '@goosar/ui/components/ui/skeleton';
import type { AgentPresenceDetail } from '@goosar/core/agents';
import { availabilityConfig, workloadConfig } from '../presence';
import { useT } from '../../i18n';

interface PresenceIndicatorProps {
  detail: AgentPresenceDetail | null | undefined;
  compact?: boolean;
}

export function AgentPresenceIndicator({ detail, compact }: PresenceIndicatorProps) {
  const { t } = useT('agents');
  if (!detail) {
    return compact ? (
      <Skeleton className="h-1.5 w-1.5 rounded-full" />
    ) : (
      <Skeleton className="h-3 w-24 rounded" />
    );
  }

  const av = availabilityConfig[detail.availability];
  const wl = workloadConfig[detail.workload];
  const availabilityLabel = t(($) => $.availability[detail.availability]);
  const workloadLabel = t(($) => $.workload[detail.workload]);
  const isWorking = detail.workload === 'working';
  const isQueued = detail.workload === 'queued';
  const showQueueBadge = isWorking && detail.queuedCount > 0;
  const queuedTone = detail.availability === 'online' ? 'text-muted-foreground' : wl.textClass;

  if (compact) {
    return (
      <span
        className="inline-flex items-center"
        title={`${availabilityLabel}${detail.workload !== 'idle' ? ` · ${workloadLabel}` : ''}`}
      >
        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${av.dotClass}`} />
      </span>
    );
  }

  return (
    <span className="inline-flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
      {/* Availability — dot + label. Single dimension, single colour. */}
      <span className="inline-flex items-center gap-1.5">
        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${av.dotClass}`} />
        <span className={`text-xs ${av.textClass}`}>{availabilityLabel}</span>
      </span>

      {/* Workload — separator + label, with counts when working/queued.
          All three workload states render here for symmetry: idle gets
          its own "Idle" label so the difference between "no presence
          data" (no chip at all) and "agent is idle" (explicit Idle chip)
          is visible. Archived agents skip the workload chip entirely —
          "Archived" already says everything; "Archived · Idle" is noise. */}
      {detail.availability !== 'archived' && (
        <span className="inline-flex items-center gap-1">
          <span className="text-xs text-muted-foreground">·</span>
          <span className={`text-xs ${isQueued ? queuedTone : wl.textClass}`}>{workloadLabel}</span>
          {isWorking && (
            <span className="font-mono text-xs tabular-nums text-muted-foreground">
              {detail.runningCount} / {detail.capacity}
            </span>
          )}
          {showQueueBadge && (
            <span className="rounded-md bg-muted px-1 py-0 text-xs font-medium text-muted-foreground">
              {t(($) => $.presence.queue_badge, { count: detail.queuedCount })}
            </span>
          )}
          {/* Queued (no running) — show the queued count directly, since
            there's no running/capacity ratio to anchor on. Honestly
            surfaces "stuck" on offline runtimes. */}
          {isQueued && (
            <span className="font-mono text-xs tabular-nums text-muted-foreground">
              {detail.queuedCount}
            </span>
          )}
        </span>
      )}
    </span>
  );
}

'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Agent, AgentActivityBucket } from '../types';
import { agentListOptions } from '../workspace/queries';
import { agentActivity30dOptions } from './queries';

const DAYS = 30;
const DAY_MS = 24 * 60 * 60 * 1000;

export interface ActivityBucket {
  total: number;
  failed: number;
}

export interface AgentActivity {
  buckets: ActivityBucket[];
  daysSinceCreated: number;
}

export interface ActivityWindowSummary {
  buckets: ActivityBucket[];
  totalRuns: number;
  totalFailed: number;
  windowDays: number;
}

const EMPTY: AgentActivity = {
  buckets: Array.from({ length: DAYS }, () => ({ total: 0, failed: 0 })),
  daysSinceCreated: DAYS,
};

const EMPTY_SUMMARY: ActivityWindowSummary = {
  buckets: [],
  totalRuns: 0,
  totalFailed: 0,
  windowDays: 0,
};

export function useWorkspaceActivityMap(wsId: string | undefined): {
  byAgent: Map<string, AgentActivity>;
  loading: boolean;
} {
  const { data: agents, isPending: agentsPending } = useQuery({
    ...agentListOptions(wsId ?? ''),
    enabled: !!wsId,
  });
  const { data: buckets, isPending: bucketsPending } = useQuery({
    ...agentActivity30dOptions(wsId ?? ''),
    enabled: !!wsId,
  });

  const byAgent = useMemo(() => {
    if (!agents || !buckets) return new Map<string, AgentActivity>();
    return buildActivityMap(agents, buckets, Date.now());
  }, [agents, buckets]);

  return { byAgent, loading: agentsPending || bucketsPending };
}

export function buildActivityMap(
  agents: readonly Agent[],
  buckets: readonly AgentActivityBucket[],
  now: number,
): Map<string, AgentActivity> {
  const bucketsByAgent = new Map<string, AgentActivityBucket[]>();
  for (const b of buckets) {
    const list = bucketsByAgent.get(b.agent_id);
    if (list) list.push(b);
    else bucketsByAgent.set(b.agent_id, [b]);
  }

  const out = new Map<string, AgentActivity>();
  for (const agent of agents) {
    out.set(
      agent.id,
      deriveAgentActivity(bucketsByAgent.get(agent.id) ?? [], agent.created_at, now),
    );
  }
  return out;
}

export function deriveAgentActivity(
  buckets: readonly AgentActivityBucket[],
  agentCreatedAt: string,
  now: number,
): AgentActivity {
  const series: ActivityBucket[] = Array.from({ length: DAYS }, () => ({
    total: 0,
    failed: 0,
  }));

  const today = startOfDay(now);

  for (const b of buckets) {
    const ts = new Date(b.bucket_at).getTime();
    if (Number.isNaN(ts)) continue;
    const daysAgo = Math.floor((today - startOfDay(ts)) / DAY_MS);
    if (daysAgo < 0 || daysAgo >= DAYS) continue;
    const slot = DAYS - 1 - daysAgo;
    series[slot]!.total += b.task_count;
    series[slot]!.failed += b.failed_count;
  }

  const createdAt = new Date(agentCreatedAt).getTime();
  const ageMs = Number.isFinite(createdAt) ? now - createdAt : Infinity;
  const daysSinceCreated = Math.min(DAYS, Math.max(0, Math.floor(ageMs / DAY_MS)));

  return {
    buckets: series,
    daysSinceCreated,
  };
}

export function summarizeActivityWindow(
  activity: AgentActivity | undefined,
  windowDays: number,
): ActivityWindowSummary {
  if (!activity) return { ...EMPTY_SUMMARY, windowDays };
  const safeWindow = Math.min(Math.max(0, windowDays), activity.buckets.length);
  const slice = safeWindow === 0 ? [] : activity.buckets.slice(-safeWindow);
  let totalRuns = 0;
  let totalFailed = 0;
  for (const b of slice) {
    totalRuns += b.total;
    totalFailed += b.failed;
  }
  return { buckets: slice, totalRuns, totalFailed, windowDays };
}

function startOfDay(ts: number): number {
  const d = new Date(ts);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}

export const __EMPTY_ACTIVITY = EMPTY;

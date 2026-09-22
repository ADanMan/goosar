'use client';

import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { agentListOptions } from '../workspace/queries';
import { runtimeListOptions } from '../runtimes/queries';
import { agentTaskSnapshotOptions } from './queries';
import { buildPresenceMap, deriveAgentPresenceDetail } from './derive-presence';
import type { AgentPresenceDetail } from './types';

const PRESENCE_TICK_MS = 30_000;

function usePresenceTick(): number {
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), PRESENCE_TICK_MS);
    return () => clearInterval(id);
  }, []);
  return tick;
}

export function useWorkspacePresenceMap(wsId: string | undefined): {
  byAgent: Map<string, AgentPresenceDetail>;
  loading: boolean;
} {
  const {
    data: agents,
    isPending: agentsPending,
    isError: agentsErr,
  } = useQuery({
    ...agentListOptions(wsId ?? ''),
    enabled: !!wsId,
  });
  const {
    data: runtimes,
    isPending: runtimesPending,
    isError: runtimesErr,
  } = useQuery({
    ...runtimeListOptions(wsId ?? ''),
    enabled: !!wsId,
  });
  const {
    data: snapshot,
    isPending: snapshotPending,
    isError: snapshotErr,
  } = useQuery({
    ...agentTaskSnapshotOptions(wsId ?? ''),
    enabled: !!wsId,
  });
  const tick = usePresenceTick();

  const byAgent = useMemo(() => {
    const safeAgents = agents ?? (agentsErr ? [] : null);
    const safeRuntimes = runtimes ?? (runtimesErr ? [] : null);
    const safeSnapshot = snapshot ?? (snapshotErr ? [] : null);
    if (!safeAgents || !safeRuntimes || !safeSnapshot) {
      return new Map<string, AgentPresenceDetail>();
    }
    return buildPresenceMap({
      agents: safeAgents,
      runtimes: safeRuntimes,
      snapshot: safeSnapshot,
      now: Date.now(),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agents, runtimes, snapshot, agentsErr, runtimesErr, snapshotErr, tick]);

  return {
    byAgent,
    loading:
      (agentsPending && !agentsErr) ||
      (runtimesPending && !runtimesErr) ||
      (snapshotPending && !snapshotErr),
  };
}

const MISSING_AGENT_DETAIL: AgentPresenceDetail = {
  availability: 'offline',
  workload: 'idle',
  runningCount: 0,
  queuedCount: 0,
  capacity: 0,
};

export function useAgentPresenceDetail(
  wsId: string | undefined,
  agentId: string | undefined,
): AgentPresenceDetail | 'loading' {
  const { data: agents, isError: agentsErr } = useQuery({
    ...agentListOptions(wsId ?? ''),
    enabled: !!wsId,
  });
  const { data: runtimes, isError: runtimesErr } = useQuery({
    ...runtimeListOptions(wsId ?? ''),
    enabled: !!wsId,
  });
  const { data: snapshot, isError: snapshotErr } = useQuery({
    ...agentTaskSnapshotOptions(wsId ?? ''),
    enabled: !!wsId,
  });
  const tick = usePresenceTick();

  return useMemo<AgentPresenceDetail | 'loading'>(() => {
    if (!wsId || !agentId) return 'loading';

    const safeAgents = agents ?? (agentsErr ? [] : null);
    const safeRuntimes = runtimes ?? (runtimesErr ? [] : null);
    const safeSnapshot = snapshot ?? (snapshotErr ? [] : null);
    if (!safeAgents || !safeRuntimes || !safeSnapshot) return 'loading';

    const agent = safeAgents.find((a) => a.id === agentId);
    if (!agent) return MISSING_AGENT_DETAIL;
    const runtime = safeRuntimes.find((r) => r.id === agent.runtime_id) ?? null;

    const tasks = safeSnapshot.filter((t) => t.agent_id === agentId);
    return deriveAgentPresenceDetail({ agent, runtime, tasks, now: Date.now() });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsId, agentId, agents, runtimes, snapshot, agentsErr, runtimesErr, snapshotErr, tick]);
}

// Мобильное зеркало use-agent-presence: веб-версия читает контекст воркспейса,
// мобилка — свой стор.
import { useEffect, useMemo, useState } from 'react';
import { AppState, type AppStateStatus } from 'react-native';
import { useQuery } from '@tanstack/react-query';
import {
  buildPresenceMap,
  deriveAgentPresenceDetail,
  type AgentPresenceDetail,
} from '@goosar/core/agents';
import { agentListOptions } from '@/data/queries/agents';
import { runtimeListOptions } from '@/data/queries/runtimes';
import { agentTaskSnapshotOptions } from '@/data/queries/agent-task-snapshot';

const PRESENCE_TICK_MS = 30_000;

function usePresenceTick(): number {
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let interval: ReturnType<typeof setInterval> | null = null;

    const start = () => {
      if (interval) return;
      interval = setInterval(() => setTick((t) => t + 1), PRESENCE_TICK_MS);
    };
    const stop = () => {
      if (!interval) return;
      clearInterval(interval);
      interval = null;
    };

    if (AppState.currentState === 'active') start();

    const sub = AppState.addEventListener('change', (next: AppStateStatus) => {
      if (next === 'active') {
        setTick((t) => t + 1);
        start();
      } else {
        stop();
      }
    });

    return () => {
      stop();
      sub.remove();
    };
  }, []);

  return tick;
}

export function useWorkspacePresenceMap(wsId: string | null | undefined): {
  byAgent: Map<string, AgentPresenceDetail>;
  loading: boolean;
} {
  const {
    data: agents,
    isPending: agentsPending,
    isError: agentsErr,
  } = useQuery({ ...agentListOptions(wsId ?? null), enabled: !!wsId });
  const {
    data: runtimes,
    isPending: runtimesPending,
    isError: runtimesErr,
  } = useQuery({ ...runtimeListOptions(wsId ?? null), enabled: !!wsId });
  const {
    data: snapshot,
    isPending: snapshotPending,
    isError: snapshotErr,
  } = useQuery({
    ...agentTaskSnapshotOptions(wsId ?? null),
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- tick is intentional
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

export function useAgentPresence(
  wsId: string | null | undefined,
  agentId: string | null | undefined,
): AgentPresenceDetail | 'loading' {
  const { data: agents, isError: agentsErr } = useQuery({
    ...agentListOptions(wsId ?? null),
    enabled: !!wsId,
  });
  const { data: runtimes, isError: runtimesErr } = useQuery({
    ...runtimeListOptions(wsId ?? null),
    enabled: !!wsId,
  });
  const { data: snapshot, isError: snapshotErr } = useQuery({
    ...agentTaskSnapshotOptions(wsId ?? null),
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
    return deriveAgentPresenceDetail({
      agent,
      runtime,
      tasks,
      now: Date.now(),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- tick is intentional
  }, [wsId, agentId, agents, runtimes, snapshot, agentsErr, runtimesErr, snapshotErr, tick]);
}

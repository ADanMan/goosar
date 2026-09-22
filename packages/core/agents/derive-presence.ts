// Чистое вычисление пользовательского статуса присутствия агента из сырых серверных данных.

import { deriveRuntimeHealth } from '../runtimes/derive-health';
import type { Agent, AgentRuntime, AgentTask } from '../types';
import type { AgentAvailability, AgentPresenceDetail, Workload } from './types';

export function deriveAgentAvailability(
  runtime: AgentRuntime | null,
  now: number,
): AgentAvailability {
  if (!runtime) return 'offline';
  const health = deriveRuntimeHealth(runtime, now);
  if (health === 'online') return 'online';
  if (health === 'recently_lost') return 'unstable';
  return 'offline'; 
}

export function deriveWorkload(counts: { runningCount: number; queuedCount: number }): Workload {
  if (counts.runningCount > 0) return 'working';
  if (counts.queuedCount > 0) return 'queued';
  return 'idle';
}

interface WorkloadDetail {
  workload: Workload;
  runningCount: number;
  queuedCount: number;
}

export function deriveWorkloadDetail(tasks: readonly AgentTask[]): WorkloadDetail {
  let runningCount = 0;
  let queuedCount = 0;
  for (const t of tasks) {
    if (t.status === 'running') {
      runningCount += 1;
    } else if (
      t.status === 'queued' ||
      t.status === 'dispatched' ||
      t.status === 'waiting_local_directory'
    ) {
      queuedCount += 1;
    }
    // Terminal statuses (completed / failed / cancelled) intentionally
    // ignored — workload is "what's on the plate right now", not history.
  }
  return {
    workload: deriveWorkload({ runningCount, queuedCount }),
    runningCount,
    queuedCount,
  };
}

interface DerivePresenceInput {
  agent: Agent;
  runtime: AgentRuntime | null;
  tasks: readonly AgentTask[];
  now: number;
}

export function deriveAgentPresenceDetail(input: DerivePresenceInput): AgentPresenceDetail {
  if (input.agent.archived_at) {
    return {
      availability: 'archived',
      workload: 'idle',
      runningCount: 0,
      queuedCount: 0,
      capacity: input.agent.max_concurrent_tasks,
    };
  }

  const availability = deriveAgentAvailability(input.runtime, input.now);
  const detail = deriveWorkloadDetail(input.tasks);

  return {
    availability,
    workload: detail.workload,
    runningCount: detail.runningCount,
    queuedCount: detail.queuedCount,
    capacity: input.agent.max_concurrent_tasks,
  };
}

export function buildPresenceMap(args: {
  agents: readonly Agent[];
  runtimes: readonly AgentRuntime[];
  snapshot: readonly AgentTask[];
  now: number;
}): Map<string, AgentPresenceDetail> {
  const out = new Map<string, AgentPresenceDetail>();
  const runtimesById = new Map<string, AgentRuntime>();
  for (const r of args.runtimes) runtimesById.set(r.id, r);

  const tasksByAgent = new Map<string, AgentTask[]>();
  for (const t of args.snapshot) {
    const list = tasksByAgent.get(t.agent_id);
    if (list) list.push(t);
    else tasksByAgent.set(t.agent_id, [t]);
  }

  for (const agent of args.agents) {
    const runtime = runtimesById.get(agent.runtime_id) ?? null;
    const tasks = tasksByAgent.get(agent.id) ?? [];
    out.set(agent.id, deriveAgentPresenceDetail({ agent, runtime, tasks, now: args.now }));
  }
  return out;
}

// Чистое вычисление отображаемого пользователю статуса рантайма из сырых данных сервера

import type { AgentRuntime } from '../types';
import type { RuntimeHealth } from './types';

const FIVE_MINUTES_MS = 5 * 60 * 1000;
const ABOUT_TO_GC_THRESHOLD_MS = 6 * 24 * 3600 * 1000; 

export function deriveRuntimeHealth(runtime: AgentRuntime, now: number): RuntimeHealth {
  if (runtime.status === 'online') return 'online';

  const lastSeen = runtime.last_seen_at ? new Date(runtime.last_seen_at).getTime() : 0;
  const offlineFor = now - lastSeen;

  if (offlineFor < FIVE_MINUTES_MS) return 'recently_lost';
  if (offlineFor > ABOUT_TO_GC_THRESHOLD_MS) return 'about_to_gc';
  return 'offline';
}

export type RuntimeAvailability = 'live' | 'registering' | 'disabled' | 'unavailable_here';

interface RuntimeStateFacts {
  lastSeenAt: string | null;
  needsAttention: boolean;
  failureReason: string | null;
  commandName: string | null;
}

export interface LiveRuntimeState extends RuntimeStateFacts {
  availability: 'live';
  health: RuntimeHealth;
}

export interface InactiveRuntimeState extends RuntimeStateFacts {
  availability: Exclude<RuntimeAvailability, 'live'>;
  health: null;
}

export type RuntimeState = LiveRuntimeState | InactiveRuntimeState;

export function deriveRuntimeState(runtime: AgentRuntime, now: number): RuntimeState {
  const commandName = metadataString(runtime, 'command_name');

  if (metadataFlag(runtime, 'runtime_profile_registration_error')) {
    return {
      availability: 'unavailable_here',
      health: null,
      lastSeenAt: null,
      needsAttention: false,
      failureReason: metadataString(runtime, 'runtime_profile_failure_reason'),
      commandName,
    };
  }

  if (metadataFlag(runtime, 'pending_custom_runtime')) {
    return {
      availability:
        runtime.metadata?.runtime_profile_enabled === false ? 'disabled' : 'registering',
      health: null,
      lastSeenAt: null,
      needsAttention: false,
      failureReason: null,
      commandName,
    };
  }

  const health = deriveRuntimeHealth(runtime, now);
  return {
    availability: 'live',
    health,
    lastSeenAt: runtime.last_seen_at ?? null,
    needsAttention: health !== 'online',
    failureReason: null,
    commandName,
  };
}

function metadataFlag(runtime: AgentRuntime, key: string): boolean {
  return runtime.metadata?.[key] === true;
}

function metadataString(runtime: AgentRuntime, key: string): string | null {
  const value = runtime.metadata?.[key];
  return typeof value === 'string' && value.trim() ? value : null;
}

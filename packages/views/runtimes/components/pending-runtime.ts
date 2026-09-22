import type { AgentRuntime, RuntimeProfile } from '@goosar/core/types';

export const PENDING_RUNTIME_WARNING_MS = 45_000;

const PENDING_RUNTIME_ID_PREFIX = 'pending-runtime-profile:';

interface PendingRuntimeMetadata extends Record<string, unknown> {
  pending_custom_runtime: true;
  runtime_profile_id: string;
  runtime_profile_enabled: boolean;
  command_name: string;
  pending_since: string;
}

export interface PendingRuntimeProfile {
  profile: RuntimeProfile;
  createdAt: number;
}

export function pendingRuntimeId(profileId: string): string {
  return `${PENDING_RUNTIME_ID_PREFIX}${profileId}`;
}

export function isPendingCustomRuntimeWarning(runtime: AgentRuntime, now: number): boolean {
  const pendingSince = runtime.metadata?.pending_since;
  if (typeof pendingSince !== 'string') return false;
  const startedAt = new Date(pendingSince).getTime();
  if (!Number.isFinite(startedAt)) return false;
  return now - startedAt >= PENDING_RUNTIME_WARNING_MS;
}

export function pendingRuntimeFromProfile({
  profile,
  createdAt,
  ownerId,
  localDaemonId,
  localMachineName,
  fallbackMachineName,
}: {
  profile: RuntimeProfile;
  createdAt: number;
  ownerId?: string | null;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  fallbackMachineName?: string | null;
}): AgentRuntime {
  const pendingSince = new Date(createdAt).toISOString();
  const machineName =
    localMachineName?.trim() || fallbackMachineName?.trim() || 'Pending custom runtimes';
  const metadata: PendingRuntimeMetadata = {
    pending_custom_runtime: true,
    runtime_profile_id: profile.id,
    runtime_profile_enabled: profile.enabled,
    command_name: profile.command_name,
    pending_since: pendingSince,
  };

  return {
    id: pendingRuntimeId(profile.id),
    workspace_id: profile.workspace_id,
    daemon_id: localDaemonId ?? null,
    name: `${profile.display_name} (${machineName})`,
    runtime_mode: 'local',
    provider: profile.protocol_family,
    launch_header: profile.protocol_family,
    status: 'offline',
    device_info: machineName,
    metadata,
    owner_id: ownerId ?? profile.created_by ?? null,
    visibility: 'private',
    profile_id: profile.id,
    last_seen_at: null,
    created_at: pendingSince,
    updated_at: pendingSince,
  };
}

export function pendingRuntimesForProfiles({
  pendingProfiles,
  runtimes,
  ownerId,
  localDaemonId,
  localMachineName,
  fallbackMachineName,
}: {
  pendingProfiles: PendingRuntimeProfile[];
  runtimes: AgentRuntime[];
  ownerId?: string | null;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  fallbackMachineName?: string | null;
}): AgentRuntime[] {
  if (pendingProfiles.length === 0) return runtimes;
  const registeredProfileIds = new Set(
    runtimes
      .map((runtime) => runtime.profile_id)
      .filter((profileId): profileId is string => !!profileId),
  );
  const pendingRuntimes = pendingProfiles
    .filter(({ profile }) => !registeredProfileIds.has(profile.id))
    .map(({ profile, createdAt }) =>
      pendingRuntimeFromProfile({
        profile,
        createdAt,
        ownerId,
        localDaemonId,
        localMachineName,
        fallbackMachineName,
      }),
    );
  return [...runtimes, ...pendingRuntimes];
}

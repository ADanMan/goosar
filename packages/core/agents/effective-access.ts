import type { AgentInvocationTarget, AgentPermissionMode } from '../types';

export type AccessScope = 'workspace' | 'specific-people' | 'owner-only';

export function effectiveAccessScope(
  permissionMode: AgentPermissionMode | undefined | null,
  invocationTargets: readonly AgentInvocationTarget[] | undefined | null,
): AccessScope {
  if (permissionMode !== 'public_to') {
    return 'owner-only';
  }
  if ((invocationTargets ?? []).some((t) => t.target_type === 'workspace')) {
    return 'workspace';
  }
  return 'specific-people';
}

export const ALL_ACCESS_SCOPES: readonly AccessScope[] = [
  'workspace',
  'specific-people',
  'owner-only',
];

export function isAccessChangeReady(
  change: {
    permission_mode: AgentPermissionMode;
    invocation_targets: readonly { target_type: string }[];
  } | null,
): boolean {
  if (!change) return false;
  if (change.permission_mode === 'private') return true;
  return change.invocation_targets.length > 0;
}

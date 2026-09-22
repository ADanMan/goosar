import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, ApiError } from '../api';
import type {
  DeploymentAdminAddResult,
  DeploymentAdminEntry,
  DeploymentAdminPending,
  DeploymentPolicyDoc,
} from '../api/deployment-admin';
import type { DeploymentUser } from '../api/deployment-user';
import type { UserConfigOverridePatch, WorkspaceConfigPatch } from '../api/workspace-admin';
import { effectiveConfigKeys } from '../workspace/effective-config';

export const deploymentAdminKeys = {
  admins: ['deployment', 'admins'] as const,
  pending: ['deployment', 'admins', 'pending'] as const,
  audit: ['deployment', 'audit'] as const,
  policy: ['deployment', 'policy'] as const,
  fleet: ['deployment', 'fleet'] as const,
  workspaces: ['deployment', 'workspaces'] as const,
  workspaceMembers: (wsId: string) => ['deployment', 'workspaces', wsId, 'members'] as const,
  workspaceConfig: (wsId: string) => ['deployment', 'workspaces', wsId, 'config'] as const,
  workspaceOverrides: (wsId: string) => ['deployment', 'workspaces', wsId, 'overrides'] as const,
  workspaceOverride: (wsId: string, userId: string) =>
    ['deployment', 'workspaces', wsId, 'overrides', userId] as const,
};

export function deploymentAdminsOptions() {
  return queryOptions<DeploymentAdminEntry[] | null>({
    queryKey: deploymentAdminKeys.admins,
    queryFn: async () => {
      try {
        return await api.listDeploymentAdmins();
      } catch (err) {
        if (err instanceof ApiError && err.status === 403) return null;
        throw err;
      }
    },
    retry: false,
    staleTime: 60_000,
  });
}

export function deploymentAdminPendingOptions() {
  return queryOptions({
    queryKey: deploymentAdminKeys.pending,
    queryFn: () => api.listDeploymentAdminPending(),
  });
}

export function deploymentAuditOptions(limit?: number) {
  return queryOptions({
    queryKey: [...deploymentAdminKeys.audit, limit ?? null] as const,
    queryFn: () => api.listDeploymentAudit(limit),
  });
}

export function deploymentPolicyOptions() {
  return queryOptions({
    queryKey: deploymentAdminKeys.policy,
    queryFn: () => api.getDeploymentAdminPolicy(),
  });
}

export function useAddDeploymentAdmin() {
  const qc = useQueryClient();
  return useMutation<DeploymentAdminAddResult, unknown, string>({
    mutationFn: (email: string) => api.addDeploymentAdmin(email),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: deploymentAdminKeys.admins });
    },
  });
}

export function useRemoveDeploymentAdmin() {
  const qc = useQueryClient();
  return useMutation<DeploymentAdminPending, unknown, string>({
    mutationFn: (userId: string) => api.removeDeploymentAdmin(userId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: deploymentAdminKeys.admins });
    },
  });
}

export function useUpdateDeploymentPolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (policy: DeploymentPolicyDoc) => api.putDeploymentAdminPolicy(policy),
    onSuccess: (saved) => {
      qc.setQueryData(deploymentAdminKeys.policy, saved);
    },
  });
}

export function deploymentFleetOptions() {
  return queryOptions({
    queryKey: deploymentAdminKeys.fleet,
    queryFn: () => api.getDeploymentFleet(),
    refetchInterval: DEPLOYMENT_FLEET_REFRESH_MS,
    staleTime: DEPLOYMENT_FLEET_REFRESH_MS,
  });
}

export const DEPLOYMENT_FLEET_REFRESH_MS = 30_000;

export function deploymentWorkspacesOptions() {
  return queryOptions({
    queryKey: deploymentAdminKeys.workspaces,
    queryFn: () => api.listDeploymentWorkspaces(),
  });
}

export function deploymentWorkspaceMembersOptions(wsId: string) {
  return queryOptions({
    queryKey: deploymentAdminKeys.workspaceMembers(wsId),
    queryFn: () => api.listDeploymentWorkspaceMembers(wsId),
  });
}

export function deploymentWorkspaceConfigOptions(wsId: string) {
  return queryOptions({
    queryKey: deploymentAdminKeys.workspaceConfig(wsId),
    queryFn: () => api.getDeploymentWorkspaceConfig(wsId),
    gcTime: 0,
  });
}

export function deploymentWorkspaceOverridesOptions(wsId: string) {
  return queryOptions({
    queryKey: deploymentAdminKeys.workspaceOverrides(wsId),
    queryFn: () => api.listDeploymentWorkspaceConfigOverrides(wsId),
    gcTime: 0,
  });
}

export function deploymentUserConfigOverrideOptions(wsId: string, userId: string) {
  return queryOptions({
    queryKey: deploymentAdminKeys.workspaceOverride(wsId, userId),
    queryFn: () => api.getDeploymentWorkspaceConfigOverride(wsId, userId),
    gcTime: 0,
  });
}

export function useUpdateDeploymentWorkspaceConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: (patch: WorkspaceConfigPatch) => api.putDeploymentWorkspaceConfig(wsId, patch),
    onSuccess: (saved) => {
      qc.setQueryData(deploymentAdminKeys.workspaceConfig(wsId), saved);
      qc.invalidateQueries({ queryKey: effectiveConfigKeys.view(wsId) });
    },
  });
}

export function useSetDeploymentUserConfigOverride(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: ({ userId, patch }: { userId: string; patch: UserConfigOverridePatch }) =>
      api.putDeploymentWorkspaceConfigOverride(wsId, userId, patch),
    onSuccess: (saved, { userId }) => {
      qc.setQueryData(deploymentAdminKeys.workspaceOverride(wsId, userId), saved);
      qc.invalidateQueries({
        queryKey: deploymentAdminKeys.workspaceOverrides(wsId),
      });
      qc.invalidateQueries({ queryKey: effectiveConfigKeys.view(wsId) });
    },
  });
}

export function useDeleteDeploymentUserConfigOverride(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: (userId: string) => api.deleteDeploymentWorkspaceConfigOverride(wsId, userId),
    onSuccess: (_data, userId) => {
      qc.setQueryData(deploymentAdminKeys.workspaceOverride(wsId, userId), null);
      qc.invalidateQueries({
        queryKey: deploymentAdminKeys.workspaceOverrides(wsId),
      });
      qc.invalidateQueries({ queryKey: effectiveConfigKeys.view(wsId) });
    },
  });
}

function useDeploymentUserBlockMutation(call: (userId: string) => Promise<DeploymentUser>) {
  const qc = useQueryClient();
  return useMutation<DeploymentUser, unknown, string>({
    mutationFn: call,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: deploymentAdminKeys.workspaces });
    },
  });
}

export function useDeactivateDeploymentUser() {
  return useDeploymentUserBlockMutation((userId) => api.deactivateDeploymentUser(userId));
}

export function useRevokeDeploymentUserSessions() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api.revokeDeploymentUserSessions(userId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: deploymentAdminKeys.workspaces });
    },
  });
}

export function useReactivateDeploymentUser() {
  return useDeploymentUserBlockMutation((userId) => api.reactivateDeploymentUser(userId));
}

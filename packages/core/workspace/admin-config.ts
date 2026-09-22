import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api';
import type {
  ProvisioningPinInput,
  UserConfigOverridePatch,
  WorkspaceConfigPatch,
} from '../api/workspace-admin';
import { effectiveConfigKeys } from './effective-config';

export const workspaceAdminKeys = {
  config: (wsId: string) => ['workspaces', wsId, 'admin', 'config'] as const,
  overrides: (wsId: string) => ['workspaces', wsId, 'admin', 'overrides'] as const,
  override: (wsId: string, userId: string) =>
    ['workspaces', wsId, 'admin', 'overrides', userId] as const,
  pins: (wsId: string) => ['workspaces', wsId, 'admin', 'provisioning-pins'] as const,
  catalog: (wsId: string) => ['workspaces', wsId, 'admin', 'provisioning-catalog'] as const,
};

export function workspaceConfigOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceAdminKeys.config(wsId),
    queryFn: () => api.getWorkspaceConfig(wsId),
    gcTime: 0,
  });
}

export function userConfigOverrideOptions(wsId: string, userId: string) {
  return queryOptions({
    queryKey: workspaceAdminKeys.override(wsId, userId),
    queryFn: () => api.getWorkspaceConfigOverride(wsId, userId),
    gcTime: 0,
  });
}

export function provisioningPinsOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceAdminKeys.pins(wsId),
    queryFn: () => api.getProvisioningPins(wsId),
  });
}

export function provisioningCatalogOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceAdminKeys.catalog(wsId),
    queryFn: () => api.getProvisioningCatalog(wsId),
  });
}

export function useUpdateWorkspaceConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: (patch: WorkspaceConfigPatch) => api.putWorkspaceConfig(wsId, patch),
    onSuccess: (saved) => {
      qc.setQueryData(workspaceAdminKeys.config(wsId), saved);
      qc.invalidateQueries({ queryKey: effectiveConfigKeys.view(wsId) });
    },
  });
}

export function useSetUserConfigOverride(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: ({ userId, patch }: { userId: string; patch: UserConfigOverridePatch }) =>
      api.putWorkspaceConfigOverride(wsId, userId, patch),
    onSuccess: (saved, { userId }) => {
      qc.setQueryData(workspaceAdminKeys.override(wsId, userId), saved);
      qc.invalidateQueries({ queryKey: effectiveConfigKeys.view(wsId) });
    },
  });
}

export function useDeleteUserConfigOverride(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: (userId: string) => api.deleteWorkspaceConfigOverride(wsId, userId),
    onSuccess: (_data, userId) => {
      qc.setQueryData(workspaceAdminKeys.override(wsId, userId), null);
      qc.invalidateQueries({ queryKey: effectiveConfigKeys.view(wsId) });
    },
  });
}

export function useUpdateProvisioningPins(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: (pins: ProvisioningPinInput[]) => api.putProvisioningPins(wsId, pins),
    onSuccess: (saved) => {
      qc.setQueryData(workspaceAdminKeys.pins(wsId), saved);
    },
  });
}

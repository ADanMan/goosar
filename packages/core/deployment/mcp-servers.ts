import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, ApiError } from '../api';
import type { DeploymentMcpServerInput } from '../api/deployment-mcp';
import { workspaceMcpKeys } from '../workspace/mcp-servers';

export const deploymentMcpKeys = {
  library: ['deployment', 'mcp-servers'] as const,
  workspaceOffers: (wsId: string) => ['workspaces', wsId, 'deployment-mcp-servers'] as const,
};

export function deploymentMcpServersOptions() {
  return queryOptions({
    queryKey: deploymentMcpKeys.library,
    queryFn: async () => {
      try {
        return await api.listDeploymentMcpServers();
      } catch (err) {
        if (err instanceof ApiError && err.status === 403) return null;
        throw err;
      }
    },
    retry: false,
  });
}

export function workspaceDeploymentMcpServersOptions(wsId: string) {
  return queryOptions({
    queryKey: deploymentMcpKeys.workspaceOffers(wsId),
    queryFn: () => api.listWorkspaceDeploymentMcpServers(wsId),
    enabled: wsId !== '',
  });
}

export function useCreateDeploymentMcpServer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: DeploymentMcpServerInput) => api.createDeploymentMcpServer(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: deploymentMcpKeys.library,
      });
    },
  });
}

export function useUpdateDeploymentMcpServer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ serverId, ...input }: DeploymentMcpServerInput & { serverId: string }) =>
      api.updateDeploymentMcpServer(serverId, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: deploymentMcpKeys.library,
      });
    },
  });
}

export function useDeleteDeploymentMcpServer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (serverId: string) => api.deleteDeploymentMcpServer(serverId),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: deploymentMcpKeys.library,
      });
      void queryClient.invalidateQueries({ queryKey: ['workspaces'] });
    },
  });
}

export function useSetWorkspaceDeploymentMcpServerEnabled(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ serverId, enabled }: { serverId: string; enabled: boolean }) =>
      api.setWorkspaceDeploymentMcpServerEnabled(wsId, serverId, enabled),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: deploymentMcpKeys.workspaceOffers(wsId),
      });
      void queryClient.invalidateQueries({
        queryKey: workspaceMcpKeys.library(wsId),
      });
      void queryClient.invalidateQueries({
        queryKey: ['workspaces', wsId, 'agents'],
      });
      void queryClient.invalidateQueries({
        queryKey: deploymentMcpKeys.library,
      });
    },
  });
}

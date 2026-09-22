import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api';
import type { WorkspaceMcpServerInput } from '../api/workspace-mcp';

export const workspaceMcpKeys = {
  library: (wsId: string) => ['workspaces', wsId, 'mcp-servers'] as const,
  agent: (wsId: string, agentId: string) =>
    ['workspaces', wsId, 'agents', agentId, 'mcp-servers'] as const,
};

export function workspaceMcpServersOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceMcpKeys.library(wsId),
    queryFn: () => api.listWorkspaceMcpServers(wsId),
    enabled: wsId !== '',
  });
}

export function agentMcpServersOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: workspaceMcpKeys.agent(wsId, agentId),
    queryFn: () => api.listAgentMcpServers(wsId, agentId),
    enabled: wsId !== '' && agentId !== '',
  });
}

export function useCreateWorkspaceMcpServer(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: WorkspaceMcpServerInput) => api.createWorkspaceMcpServer(wsId, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceMcpKeys.library(wsId),
      });
    },
  });
}

export function useUpdateWorkspaceMcpServer(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ serverId, ...input }: WorkspaceMcpServerInput & { serverId: string }) =>
      api.updateWorkspaceMcpServer(wsId, serverId, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceMcpKeys.library(wsId),
      });
    },
  });
}

export function useDeleteWorkspaceMcpServer(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (serverId: string) => api.deleteWorkspaceMcpServer(wsId, serverId),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceMcpKeys.library(wsId),
      });
      void queryClient.invalidateQueries({
        queryKey: ['workspaces', wsId, 'agents'],
      });
    },
  });
}

export function useAddAgentMcpServer(wsId: string, agentId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (serverId: string) => api.addAgentMcpServer(wsId, agentId, serverId),
    onSuccess: (servers) => {
      queryClient.setQueryData(workspaceMcpKeys.agent(wsId, agentId), servers);
    },
  });
}

export function useSetAgentMcpServerEnabled(wsId: string, agentId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ serverId, enabled }: { serverId: string; enabled: boolean }) =>
      api.setAgentMcpServerEnabled(wsId, agentId, serverId, enabled),
    onSuccess: (servers) => {
      queryClient.setQueryData(workspaceMcpKeys.agent(wsId, agentId), servers);
    },
  });
}

export function useRemoveAgentMcpServer(wsId: string, agentId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (serverId: string) => api.removeAgentMcpServer(wsId, agentId, serverId),
    onSuccess: (servers) => {
      queryClient.setQueryData(workspaceMcpKeys.agent(wsId, agentId), servers);
    },
  });
}

export function useSetWorkspaceMcpCredentials(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ serverId, values }: { serverId: string; values: Record<string, string> }) =>
      api.setWorkspaceMcpCredentials(wsId, serverId, values),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceMcpKeys.library(wsId),
      });
      void queryClient.invalidateQueries({
        queryKey: ['workspaces', wsId, 'agents'],
      });
    },
  });
}

export function useClearWorkspaceMcpCredentials(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (serverId: string) => api.clearWorkspaceMcpCredentials(wsId, serverId),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: workspaceMcpKeys.library(wsId),
      });
      void queryClient.invalidateQueries({
        queryKey: ['workspaces', wsId, 'agents'],
      });
    },
  });
}

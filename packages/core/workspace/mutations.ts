import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { Workspace } from '../types';
import { api } from '../api';
import { defaultStorage } from '../platform/storage';
import { clearWorkspaceStorage } from '../platform/storage-cleanup';
import { workspaceKeys } from './queries';
import { markWorkspaceDeletePending, unmarkWorkspaceDeletePending } from './pending-delete';

export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: {
      name: string;
      slug: string;
      description?: string;
      template_key?: string;
    }) => api.createWorkspace(data),
    onSuccess: (newWs) => {
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] = []) => [...old, newWs]);
    },
    onError: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useJoinWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (wsId: string) => api.joinTarget(wsId),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: workspaceKeys.list() });
      qc.invalidateQueries({ queryKey: workspaceKeys.joinTargets() });
    },
  });
}

export function useLeaveWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.leaveWorkspace(workspaceId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.deleteWorkspace(workspaceId),
    onMutate: (workspaceId) => {
      markWorkspaceDeletePending(workspaceId);
      const slug = qc
        .getQueryData<Workspace[]>(workspaceKeys.list())
        ?.find((w) => w.id === workspaceId)?.slug;
      return { slug };
    },
    onSuccess: (_data, _workspaceId, ctx) => {
      if (ctx?.slug) clearWorkspaceStorage(defaultStorage, ctx.slug);
    },
    onError: (_err, workspaceId) => {
      unmarkWorkspaceDeletePending(workspaceId);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

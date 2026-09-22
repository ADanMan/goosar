import { queryOptions } from '@tanstack/react-query';
import { api } from '../api';

export const composioKeys = {
  all: ['composio'] as const,
  toolkits: () => [...composioKeys.all, 'toolkits'] as const,
  connections: () => [...composioKeys.all, 'connections'] as const,
};

export const composioToolkitsOptions = () =>
  queryOptions({
    queryKey: composioKeys.toolkits(),
    queryFn: () => api.listComposioToolkits(),
    staleTime: 5 * 60 * 1000,
  });

export const composioConnectionsOptions = () =>
  queryOptions({
    queryKey: composioKeys.connections(),
    queryFn: () => api.listComposioConnections(),
  });

import { queryOptions } from '@tanstack/react-query';
import { api } from '../api';

export const effectiveConfigKeys = {
  view: (wsId: string) => ['workspaces', wsId, 'effective-config'] as const,
};

export function effectiveConfigOptions(wsId: string) {
  return queryOptions({
    queryKey: effectiveConfigKeys.view(wsId),
    queryFn: () => api.getEffectiveConfig(wsId),
    staleTime: 60 * 1000,
  });
}

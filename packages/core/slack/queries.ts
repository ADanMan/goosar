import { queryOptions } from '@tanstack/react-query';
import { api } from '../api';

export const slackKeys = {
  all: (wsId: string) => ['slack', wsId] as const,
  installations: (wsId: string) => [...slackKeys.all(wsId), 'installations'] as const,
};

export const slackInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: slackKeys.installations(wsId),
    queryFn: () => api.listSlackInstallations(wsId),
    enabled: !!wsId,
  });

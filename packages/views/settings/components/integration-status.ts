// Единый словарь состояния интеграции, чтобы список интеграций говорил
// одно и то же о шести интеграциях с разными способами отчёта.
export type IntegrationStatus =
  | { kind: 'connected' }
  | { kind: 'no_credentials' }
  | { kind: 'not_configured' }
  | { kind: 'health_error'; at: string | null; code: string | null }
  | { kind: 'unknown' };

export type IntegrationConfigurableBy = 'deployment_admin' | 'workspace_admin' | 'member';

export function installationIntegrationStatus({
  configured,
  connectedCount,
  loaded = true,
}: {
  configured: boolean;
  connectedCount: number;
  loaded?: boolean;
}): IntegrationStatus {
  if (!configured) return { kind: 'not_configured' };
  if (!loaded) return { kind: 'unknown' };
  return connectedCount > 0 ? { kind: 'connected' } : { kind: 'no_credentials' };
}

import { describe, expect, it } from 'vitest';
import { installationIntegrationStatus, type IntegrationStatus } from './integration-status';

describe('installationIntegrationStatus', () => {
  it('reports missing deployment credentials before anything else', () => {
    expect(
      installationIntegrationStatus({ configured: false, connectedCount: 3 }),
    ).toEqual<IntegrationStatus>({ kind: 'not_configured' });
  });

  it('reports connected when the deployment is configured and something is installed', () => {
    expect(
      installationIntegrationStatus({ configured: true, connectedCount: 1 }),
    ).toEqual<IntegrationStatus>({ kind: 'connected' });
  });

  it('reports no credentials when configured but nothing is connected yet', () => {
    expect(
      installationIntegrationStatus({ configured: true, connectedCount: 0 }),
    ).toEqual<IntegrationStatus>({ kind: 'no_credentials' });
  });

  it('reports unknown while the read has not answered', () => {
    expect(
      installationIntegrationStatus({
        configured: true,
        connectedCount: 0,
        loaded: false,
      }),
    ).toEqual<IntegrationStatus>({ kind: 'unknown' });
  });
});

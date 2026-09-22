// Общая форма IPC provisioning:status — зафиксированный контракт, чтобы main
// и renderer не расходились.

export type ProvisioningPackageState = 'ok' | 'pending' | 'fail';

export interface ProvisioningPackageStatus {
  name: string;
  type: string;
  version: string;
  state: ProvisioningPackageState;
  bytesDownloaded?: number;
  bytesTotal?: number;
  reasonCode?: string;
}

export type ProvisioningPackageType = 'skill' | 'mcp-server' | 'runtime';

export interface ProvisioningTypeProgress {
  installed: number;
  total: number;
  failed: number;
}

export type ProvisioningSyncState = 'idle' | 'syncing' | 'ok' | 'fail';

export interface ProvisioningStatus {
  configured: boolean;
  state: ProvisioningSyncState;
  byType: {
    skill: ProvisioningTypeProgress;
    'mcp-server': ProvisioningTypeProgress;
    runtime: ProvisioningTypeProgress;
  };
  summary: ProvisioningTypeProgress;
  reasonCode?: string;
  packages: ProvisioningPackageStatus[];
  restartPending?: boolean;
  preservedLegacyPaths?: string[];
  removedPackages?: RemovedPackageStatus[];
  platform?: string;
  platformsAvailable?: string[];
  totalBeforePlatformFilter?: number;
  unavailablePackages?: { key: string; reason: string }[];
}

export interface RemovedPackageStatus {
  name: string;
  type: string;
  version?: string;
  state: 'removed' | 'pending';
}

export type ProvisioningRestartOutcome = 'restarted' | 'deferred' | 'ok' | 'not_running';

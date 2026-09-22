// Статус provisioning для онбординга.

export interface ProvisioningTypeProgress {
  installed: number;
  total: number;
  failed: number;
}

export type ProvisioningPackageType = 'skill' | 'mcp-server' | 'runtime';

export type ProvisioningState = 'idle' | 'syncing' | 'ok' | 'fail';

export interface ProvisioningPackage {
  name: string;
  type: string;
  version: string;
  state: 'ok' | 'pending' | 'fail';
  reasonCode?: string;
}

export interface ProvisioningStatus {
  configured: boolean;
  state: ProvisioningState;
  byType: Record<ProvisioningPackageType, ProvisioningTypeProgress>;
  summary: ProvisioningTypeProgress;
  reasonCode?: string;
  packages: ProvisioningPackage[];
  restartPending?: boolean;
  preservedLegacyPaths?: string[];
  removedPackages?: {
    name: string;
    type: string;
    version?: string;
    state: 'removed' | 'pending';
  }[];
  platform?: string;
  platformsAvailable?: string[];
  totalBeforePlatformFilter?: number;
  unavailablePackages?: { key: string; reason: string }[];
}

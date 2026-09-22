import type { ProvisioningStatus } from '@goosar/views/onboarding';

interface ProvisioningBridge {
  getProvisioningStatus: () => Promise<ProvisioningStatus>;
  setProvisioningWorkspace: (workspaceId: string | null) => void;
  retryProvisioning: () => Promise<void>;
  notifyProvisioningAccessRevoked: (workspaceId: string) => void;
}

function method<K extends keyof ProvisioningBridge>(name: K): ProvisioningBridge[K] | null {
  const api = window.daemonAPI as unknown as Partial<ProvisioningBridge>;
  return typeof api?.[name] === 'function' ? (api[name] as ProvisioningBridge[K]) : null;
}

export function setProvisioningWorkspace(workspaceId: string | null): void {
  method('setProvisioningWorkspace')?.(workspaceId);
}

export async function getProvisioningStatus(): Promise<ProvisioningStatus | null> {
  const get = method('getProvisioningStatus');
  if (!get) return null;
  try {
    return await get();
  } catch {
    return null;
  }
}

export async function retryProvisioning(): Promise<void> {
  const retry = method('retryProvisioning');
  if (!retry) return;
  await retry();
}

export function notifyProvisioningAccessRevoked(workspaceId: string): void {
  method('notifyProvisioningAccessRevoked')?.(workspaceId);
}

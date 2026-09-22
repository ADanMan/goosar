import { useWorkspaceId } from '@goosar/core/hooks';
import type {
  RuntimeConnectLauncherExtras,
  WorkToolsLauncherExtras,
} from '@goosar/views/onboarding';
import { useLocalRuntimesPending } from './use-local-runtimes-pending';
import { usePerimeterMachine } from './use-perimeter-machine';
import { useExistingLlmConnection } from './use-existing-llm-connection';
import { useProvisioningPoll } from './use-provisioning-status';
import { saveLlmConnection } from './save-llm-connection';
import { useDesktopRuntimeContext } from '../components/use-desktop-runtime-context';
import { NetworkStatusIndicator } from '../components/network-status';

export function useRuntimeConnectExtras(): RuntimeConnectLauncherExtras {
  const runtimesPending = useLocalRuntimesPending();
  const { localDaemonId, localMachineName, daemonState } = useDesktopRuntimeContext();
  const perimeterMachine = usePerimeterMachine();
  const existingLlmConnection = useExistingLlmConnection();

  return {
    onRefresh: async () => {
      await window.daemonAPI?.restart?.();
    },
    runtimesPending,
    onSaveLlmConnection: saveLlmConnection,
    localDaemonId,
    localMachineName,
    perimeterMachine,
    existingLlmConnection,
    daemonState,
    networkStatusSlot: <NetworkStatusIndicator />,
  };
}

export function useWorkToolsExtras(): WorkToolsLauncherExtras {
  const wsId = useWorkspaceId();
  const { status } = useProvisioningPoll(wsId || null);
  const perimeterMachine = usePerimeterMachine();

  return {
    perimeterCaMissing: perimeterMachine?.caBundleMissing === true,
    installedMcpNames: (status?.packages ?? [])
      .filter((pkg) => pkg.type === 'mcp-server' && pkg.state === 'ok')
      .map((pkg) => pkg.name),
  };
}

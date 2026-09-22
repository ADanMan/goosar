import { useCallback, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { NewWorkspacePage } from '@goosar/views/workspace/new-workspace-page';
import { JoinWorkspacePage } from '@goosar/views/workspace/join-workspace-page';
import { InvitePage } from '@goosar/views/invite';
import { InvitationsPage } from '@goosar/views/invitations';
import { OnboardingFlow } from '@goosar/views/onboarding';
import { useNavigation } from '@goosar/views/navigation';
import { paths } from '@goosar/core/paths';
import { workspaceListOptions } from '@goosar/core/workspace/queries';
import { useTabStore } from '@/stores/tab-store';
import { useWindowOverlayStore } from '@/stores/window-overlay-store';
import { useLocalRuntimesPending } from '../platform/use-local-runtimes-pending';
import { usePerimeterMachine } from '../platform/use-perimeter-machine';
import { useExistingLlmConnection } from '../platform/use-existing-llm-connection';
import { useProvisioningStatus } from '../platform/use-provisioning-status';
import { useAgentRuntimeStatus } from '../platform/use-agent-runtime-status';
import { useLlmGatewayStatus } from '../platform/use-llm-gateway-status';
import { usePxProxyStatus } from '../platform/use-px-proxy-status';
import { saveLlmConnection } from '../platform/save-llm-connection';
import { useDesktopRuntimeContext } from './use-desktop-runtime-context';
import { NetworkStatusIndicator } from './network-status';

export function WindowOverlay() {
  const overlay = useWindowOverlayStore((s) => s.overlay);
  if (!overlay) return null;
  return <WindowOverlayInner />;
}

function WindowOverlayInner() {
  const overlay = useWindowOverlayStore((s) => s.overlay);
  const close = useWindowOverlayStore((s) => s.close);
  const { push } = useNavigation();
  const { data: wsList = [] } = useQuery(workspaceListOptions());
  const runtimesPending = useLocalRuntimesPending();
  const { localDaemonId, localMachineName, daemonState } = useDesktopRuntimeContext();
  const perimeterMachine = usePerimeterMachine();
  const existingLlmConnection = useExistingLlmConnection();
  const [provisioningWorkspaceId, setProvisioningWorkspaceId] = useState<string | null>(null);
  const { status: provisioningStatus, retry: retryProvisioning } =
    useProvisioningStatus(provisioningWorkspaceId);
  const { status: agentStatus, retry: retryAgent } = useAgentRuntimeStatus();
  const skipAgent = useCallback(() => {}, []);
  const { status: llmGatewayStatus, retry: retryLlmGateway } = useLlmGatewayStatus();
  const pxProxyStatus = usePxProxyStatus();

  if (!overlay) return null;

  const onBack = wsList.length > 0 ? close : undefined;

  return (
    <div className="fixed inset-0 z-50 flex flex-col overflow-auto bg-background">
      {overlay.type === 'new-workspace' && (
        <NewWorkspacePage
          onSuccess={(ws) => push(paths.workspace(ws.slug).issues())}
          onBack={onBack}
          onJoinInstead={() => useWindowOverlayStore.getState().open({ type: 'join-workspace' })}
        />
      )}
      {overlay.type === 'join-workspace' && (
        <JoinWorkspacePage
          onCreateInstead={() => useWindowOverlayStore.getState().open({ type: 'new-workspace' })}
        />
      )}
      {overlay.type === 'invite' && (
        <InvitePage invitationId={overlay.invitationId} onBack={onBack} />
      )}
      {overlay.type === 'invitations' && <InvitationsPage />}
      {overlay.type === 'onboarding' && (
        <OnboardingFlow
          onComplete={(ws, issueId) => {
            close();
            if (ws && issueId) {
              push(paths.workspace(ws.slug).issueDetail(issueId));
            } else if (ws) {
              push(paths.workspace(ws.slug).issues());
            } else {
              push(paths.root());
            }
            if (ws) {
              useTabStore
                .getState()
                .pinTabOnce(ws.slug, paths.workspace(ws.slug).capabilities(), '');
            }
          }}
          onRuntimeRefresh={async () => {
            await window.daemonAPI?.restart?.();
          }}
          runtimesPending={runtimesPending}
          onSaveLlmConnection={saveLlmConnection}
          localDaemonId={localDaemonId}
          localMachineName={localMachineName}
          perimeterMachine={perimeterMachine}
          existingLlmConnection={existingLlmConnection}
          daemonState={daemonState}
          networkStatusSlot={<NetworkStatusIndicator />}
          provisioningStatus={provisioningStatus}
          onRetryProvisioning={retryProvisioning}
          onWorkspaceProvisioning={setProvisioningWorkspaceId}
          agentStatus={agentStatus}
          onRetryAgent={retryAgent}
          onSkipAgent={skipAgent}
          llmGateway={llmGatewayStatus}
          onRetryLlmGateway={retryLlmGateway}
          pxProxy={pxProxyStatus}
        />
      )}
    </div>
  );
}

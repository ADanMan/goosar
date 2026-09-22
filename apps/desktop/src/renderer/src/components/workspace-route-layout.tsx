import { useEffect } from 'react';
import { Outlet, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { WorkspaceSlugProvider } from '@goosar/core/paths';
import { workspaceBySlugOptions, workspaceListOptions } from '@goosar/core/workspace';
import { setCurrentWorkspace } from '@goosar/core/platform';
import { useAuthStore } from '@goosar/core/auth';
import { useWorkspaceSeen } from '@goosar/views/workspace/use-workspace-seen';
import { WelcomeAfterOnboarding } from '@goosar/views/workspace/welcome-after-onboarding';
import { WorkspacePresencePrefetch } from '@goosar/views/layout';
import { ProvisioningReminder, SourceBackfillModal } from '@goosar/views/onboarding';
import { MissingCredentialsBanner } from '@goosar/views/capabilities';
import { useTabStore } from '@/stores/tab-store';
import { useWindowOverlayStore } from '@/stores/window-overlay-store';
import { setProvisioningWorkspace } from '../platform/provisioning-bridge';
import { useProvisioningPoll } from '../platform/use-provisioning-status';

export function WorkspaceRouteLayout() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>();
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  const overlayActive = useWindowOverlayStore((s) => s.overlay !== null);

  const { data: workspace, isFetched: listFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug ?? ''),
    enabled: !!user && !!workspaceSlug,
  });

  const { data: wsList } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  if (workspace && workspaceSlug) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
    setProvisioningWorkspace(workspace.id);
  }

  const hasBeenSeen = useWorkspaceSeen(workspaceSlug, !!workspace);

  const { status: provisioningStatus, retry: retryProvisioning } = useProvisioningPoll(
    workspace?.id ?? null,
  );

  useEffect(() => {
    if (!user) return;
    if (!listFetched) return;
    if (workspace) return;
    if (hasBeenSeen) return; 
    if (!wsList) return;
    const validSlugs = new Set(wsList.map((w) => w.slug));
    useTabStore.getState().validateWorkspaceSlugs(validSlugs);
  }, [user, listFetched, workspace, hasBeenSeen, wsList]);

  if (isAuthLoading) return null;
  if (!user) return null;
  if (!workspaceSlug) return null;
  if (!listFetched) return null;
  if (!workspace) return null; 

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      <WorkspacePresencePrefetch />
      <Outlet />
      {/* Reads the welcome-store transient signal parked by
       *  OnboardingFlow.handleRuntimeNext. Suppressed while a WindowOverlay
       *  (onboarding / accept-invite / new-workspace) is open so the modal
       *  doesn't portal-jump in front of an active pre-workspace flow.
       *  Once the overlay closes the hook re-evaluates and pops the
       *  Modal — unless the store signal has already been consumed, in
       *  which case the hook renders null. */}
      {!overlayActive && <WelcomeAfterOnboarding />}
      {/* Source-attribution backfill: same Dialog the web shell mounts
       *  inside DashboardLayout. Desktop's WorkspaceRouteLayout doesn't
       *  wrap DashboardLayout, so the modal has to be wired in directly
       *  here. Same overlay-suppression rule as WelcomeAfterOnboarding —
       *  a portal-rendered Dialog at z-50 would otherwise sit above an
       *  active pre-workspace overlay. */}
      {!overlayActive && <SourceBackfillModal />}
      {/* Missing personal credentials (#251). Same reason this file mounts
       *  SourceBackfillModal directly: desktop's WorkspaceRouteLayout is not
       *  wrapped in DashboardLayout, so a cross-cutting prompt has to be
       *  wired in here too. Suppressed under an overlay for the same reason
       *  as the modal — a pre-workspace flow owns the window while it runs. */}
      {!overlayActive && <MissingCredentialsBanner />}
      {/* Undelivered / failed / not-yet-started package sync (#230). Silent
       *  once everything is delivered, and suppressed under an overlay for
       *  the same reason as its neighbours — onboarding shows the same facts
       *  on its own step while it owns the window. */}
      {!overlayActive && (
        <ProvisioningReminder status={provisioningStatus} onRetry={retryProvisioning} />
      )}
    </WorkspaceSlugProvider>
  );
}

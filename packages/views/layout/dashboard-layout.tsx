'use client';

import type { ReactNode } from 'react';
import { SidebarProvider, SidebarInset } from '@goosar/ui/components/ui/sidebar';
import { ModalRegistry } from '../modals/registry';
import { MissingCredentialsBanner } from '../capabilities';
import { SourceBackfillModal } from '../onboarding';
import { OnboardingGuidesAutoClose } from '../workspace/onboarding-guides-autoclose';
import { NavRail, navSectionForPath } from './nav-rail';
import { ContextPanel } from './context-panel';
import { CommandBar } from './command-bar';
import { DashboardGuard } from './dashboard-guard';
import { NavigationProgress } from './navigation-progress';
import { RealtimeStatusIndicator } from './realtime-status';
import { WorkspacePresencePrefetch } from './workspace-presence-prefetch';
import { GlobalShortcuts } from './global-shortcuts';
import { useNavigation } from '../navigation';
import { useWorkspacePaths } from '@goosar/core/paths';

interface DashboardLayoutProps {
  children: ReactNode;
  extra?: ReactNode;
  loadingIndicator?: ReactNode;
}

export function DashboardLayout({ children, extra, loadingIndicator }: DashboardLayoutProps) {
  const { pathname } = useNavigation();
  const p = useWorkspacePaths();
  const activeSection = navSectionForPath(p, pathname);

  return (
    <DashboardGuard
      loadingFallback={
        <div className="flex h-svh items-center justify-center">{loadingIndicator}</div>
      }
    >
      <SidebarProvider className="h-svh bg-app-shell">
        <GlobalShortcuts />
        <WorkspacePresencePrefetch />
        {/* Realtime state (#257). Silent unless the WS transport has given up,
            so it costs nothing in the normal case. Mounted at shell level,
            outside the rail, so it survives the context panel collapsing. It
            renders a `fixed` badge, so it neither reflows the layout nor sits
            on top of any screen's primary actions. */}
        <RealtimeStatusIndicator />
        <NavRail activeSection={activeSection} />
        <div className="flex min-w-0 flex-1 flex-col">
          <CommandBar activeSection={activeSection} />
          <div className="flex min-h-0 flex-1">
            <ContextPanel activeSection={activeSection} />
            <SidebarInset className="relative overflow-hidden">
              <NavigationProgress />
              {children}
              <ModalRegistry />
              <SourceBackfillModal />
              {/* Missing personal credentials (#251). Mounted beside
                  SourceBackfillModal because it is the same kind of thing: a
                  self-gating, cross-cutting prompt with exactly one instance in
                  the tree. It positions itself, so it takes no part in this
                  layout's flow — see the banner's docblock for why that matters
                  given desktop's different shell. */}
              <MissingCredentialsBanner />
              <OnboardingGuidesAutoClose />
              {extra}
            </SidebarInset>
          </div>
        </div>
      </SidebarProvider>
    </DashboardGuard>
  );
}

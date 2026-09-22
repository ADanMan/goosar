'use client';

import type { ReactNode } from 'react';
import { SidebarProvider, SidebarInset } from '@goosar/ui/components/ui/sidebar';
import { ModalRegistry } from '../modals/registry';
import { MissingCredentialsBanner } from '../capabilities';
import { SourceBackfillModal } from '../onboarding';
import { OnboardingGuidesAutoClose } from '../workspace/onboarding-guides-autoclose';
import { AppSidebar } from './app-sidebar';
import { DashboardGuard } from './dashboard-guard';
import { NavigationProgress } from './navigation-progress';
import { RealtimeStatusIndicator } from './realtime-status';
import { WorkspacePresencePrefetch } from './workspace-presence-prefetch';
import { GlobalShortcuts } from './global-shortcuts';

interface DashboardLayoutProps {
  children: ReactNode;
  extra?: ReactNode;
  searchSlot?: ReactNode;
  loadingIndicator?: ReactNode;
}

export function DashboardLayout({
  children,
  extra,
  searchSlot,
  loadingIndicator,
}: DashboardLayoutProps) {
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
            so it costs nothing in the normal case. Mounted at shell level
            rather than in the sidebar footer: the sidebar collapses offcanvas
            (and is a closed Sheet on narrow widths), which would hide the one
            signal a user gets that the screen has stopped updating. It renders
            a `fixed` badge, so it neither reflows the layout nor sits on top of
            any screen's primary actions. */}
        <RealtimeStatusIndicator />
        <AppSidebar searchSlot={searchSlot} />
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
      </SidebarProvider>
    </DashboardGuard>
  );
}

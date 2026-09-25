import { useEffect, useRef, useSyncExternalStore } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { motion } from 'motion/react';
import { cn } from '@goosar/ui/lib/utils';
import { useTabHistory } from '@/hooks/use-tab-history';
import { SidebarProvider, SidebarTrigger, useSidebar } from '@goosar/ui/components/ui/sidebar';
import { ModalRegistry } from '@goosar/views/modals/registry';
import { KerberosPreflightGate } from './kerberos-preflight';
import {
  NavRail,
  ContextPanel,
  GlobalShortcuts,
  RealtimeStatusIndicator,
  navSectionForPath,
} from '@goosar/views/layout';
import { SearchCommand } from '@goosar/views/search';
import { FloatingChat } from '@goosar/views/chat';
import {
  WorkspaceSlugProvider,
  paths,
  useCurrentWorkspace,
  useWorkspacePaths,
} from '@goosar/core/paths';
import { useNavigation } from '@goosar/views/navigation';
import { getCurrentSlug, subscribeToCurrentSlug } from '@goosar/core/platform';
import { useDesktopUnreadBadge } from '@goosar/views/platform';
import { DesktopNavigationProvider, routeContentLinkPath } from '@/platform/navigation';
import { TabBar } from './tab-bar';
import { TabContent } from './tab-content';
import { WindowOverlay } from './window-overlay';
import { KerberosTicketBanner, NetworkStatusIndicator } from './network-status';
import { useT } from '@goosar/views/i18n';

const TOP_BAR_HEIGHT_CLASS = 'h-12';
const WINDOW_TOOLBAR_CLEARANCE = 184;
const toolbarMotion = {
  type: 'spring',
  stiffness: 420,
  damping: 38,
  mass: 0.8,
} as const;

function WindowToolbar() {
  const { t } = useT('layout');
  const { canGoBack, canGoForward, goBack, goForward } = useTabHistory();
  const navButtonClassName =
    'flex size-7 items-center justify-center rounded-md text-muted-foreground/70 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground disabled:pointer-events-none disabled:opacity-30';

  return (
    <div
      className={cn(
        'fixed left-0 top-0 z-30 flex w-[184px] shrink-0 items-center px-3',
        TOP_BAR_HEIGHT_CLASS,
      )}
      style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
    >
      <div
        className="flex items-center gap-1 pl-[70px]"
        style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
      >
        <SidebarTrigger
          className="size-7 text-muted-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
        />
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={goBack}
            disabled={!canGoBack}
            aria-label={t(($) => $.nav.go_back)}
            title={t(($) => $.nav.go_back)}
            className={navButtonClassName}
            style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
          >
            <ChevronLeft className="size-4" />
          </button>
          <button
            type="button"
            onClick={goForward}
            disabled={!canGoForward}
            aria-label={t(($) => $.nav.go_forward)}
            title={t(($) => $.nav.go_forward)}
            className={navButtonClassName}
            style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}
          >
            <ChevronRight className="size-4" />
          </button>
        </div>
      </div>
    </div>
  );
}

function useNativeNavigationGestures() {
  const { goBack, goForward } = useTabHistory();

  useEffect(() => {
    return window.desktopAPI.onNavigationGesture((gesture) => {
      if (gesture === 'back') {
        goBack();
      } else {
        goForward();
      }
    });
  }, [goBack, goForward]);
}

function MainTopBar() {
  const { state, isMobile } = useSidebar();
  const sidebarHidden = state === 'collapsed' || isMobile;

  return (
    <motion.header
      animate={{ paddingLeft: sidebarHidden ? WINDOW_TOOLBAR_CLEARANCE : 0 }}
      className={cn('relative shrink-0 flex items-center gap-2', TOP_BAR_HEIGHT_CLASS)}
      initial={false}
      transition={toolbarMotion}
    >
      <motion.div
        aria-hidden
        animate={{ left: sidebarHidden ? WINDOW_TOOLBAR_CLEARANCE : 0 }}
        className="absolute inset-y-0 right-0"
        initial={false}
        transition={toolbarMotion}
        style={{ WebkitAppRegion: 'drag' } as React.CSSProperties}
      />
      <div className="relative z-10 flex h-full min-w-0 max-w-full flex-1 items-center">
        <TabBar />
      </div>
    </motion.header>
  );
}

function MainCanvas({ children }: { children: React.ReactNode }) {
  const { state, isMobile } = useSidebar();
  const sidebarHidden = state === 'collapsed' || isMobile;

  return (
    <motion.div
      animate={{ marginLeft: sidebarHidden ? 8 : 2 }}
      className="relative flex flex-1 min-h-0 flex-col overflow-hidden mr-2 mb-2 rounded-xl bg-page-canvas ring-1 ring-surface-border shadow-[var(--surface-shadow)]"
      initial={false}
      transition={toolbarMotion}
    >
      {children}
    </motion.div>
  );
}

function useInternalLinkHandler() {
  useEffect(() => {
    const handler = (e: Event) => {
      const path = (e as CustomEvent).detail?.path;
      if (!path) return;
      routeContentLinkPath(path);
    };
    window.addEventListener('goosar:navigate', handler);
    return () => window.removeEventListener('goosar:navigate', handler);
  }, []);
}

function DesktopInboxBridge() {
  const workspace = useCurrentWorkspace();
  useDesktopUnreadBadge(workspace?.id ?? null);
  const { push } = useNavigation();
  const pushRef = useRef(push);
  useEffect(() => {
    pushRef.current = push;
  }, [push]);

  useEffect(() => {
    return window.desktopAPI.onInboxOpen(({ slug, issueKey }) => {
      if (!slug) return;
      const inboxPath = `${paths.workspace(slug).inbox()}?issue=${encodeURIComponent(issueKey)}`;
      pushRef.current(inboxPath);
    });
  }, []);

  return null;
}

function DesktopNavRailAndPanel() {
  const { pathname } = useNavigation();
  const p = useWorkspacePaths();
  const activeSection = navSectionForPath(p, pathname);
  return (
    <>
      <NavRail activeSection={activeSection} footerExtra={<NetworkStatusIndicator />} />
      <ContextPanel activeSection={activeSection} />
    </>
  );
}

export function DesktopShell() {
  useInternalLinkHandler();
  useNativeNavigationGestures();

  const slug = useSyncExternalStore(subscribeToCurrentSlug, getCurrentSlug, () => null);

  return (
    <DesktopNavigationProvider>
      {/* WorkspaceSlugProvider accepts null — components that need slug
          use useWorkspaceSlug() (nullable) or useRequiredWorkspaceSlug()
          (throws). TabContent MUST always render so the tab router can
          mount WorkspaceRouteLayout, which calls setCurrentWorkspace()
          to populate the slug. The sidebar gates on slug being present
          to avoid the useRequiredWorkspaceSlug throw. Zero-workspace
          users see the window-level overlay (new-workspace flow)
          triggered by IndexRedirect, not a route. */}
      <WorkspaceSlugProvider slug={slug}>
        <DesktopInboxBridge />
        <div className="flex h-screen bg-app-shell">
          <SidebarProvider className="flex-1 bg-app-shell">
            {slug && <GlobalShortcuts />}
            {/* Realtime state (#257) — the desktop half of the shared mount.
                It cannot live in the sidebar footer next to NetworkStatus:
                the sidebar collapses offcanvas behind a global shortcut, and
                the degraded signal has to outlive that. Fixed to the window's
                bottom-left corner, outside the sidebar and the canvas flow. */}
            {slug && <RealtimeStatusIndicator />}
            {slug && <WindowToolbar />}
            {slug && <DesktopNavRailAndPanel />}
            {/* Right side: header + content container */}
            <div className="flex flex-1 min-w-0 flex-col">
              <MainTopBar />
              {/* Missing/expiring Kerberos ticket (#297/#298): a noticeable
                  call-to-action, not only the sidebar popover. */}
              {slug && <KerberosTicketBanner />}
              {/* Pre-flight ticket check before a task/chat starts (#466):
                  renders nothing until it has to ask. */}
              {slug && <KerberosPreflightGate />}
              <MainCanvas>
                <TabContent />
                {slug && <FloatingChat />}
              </MainCanvas>
            </div>
          </SidebarProvider>
        </div>
        {slug && <ModalRegistry />}
        {slug && <SearchCommand />}
        <WindowOverlay />
      </WorkspaceSlugProvider>
    </DesktopNavigationProvider>
  );
}

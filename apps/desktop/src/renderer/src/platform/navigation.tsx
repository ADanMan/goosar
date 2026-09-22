import { useMemo } from 'react';
import { NavigationProvider, type NavigationAdapter } from '@goosar/views/navigation';
import { useAuthStore } from '@goosar/core/auth';
import { isReservedSlug } from '@goosar/core/paths';
import { useTabStore, getActiveTab, splitTabUrl, useActiveTabUrl } from '@/stores/tab-store';
import { useWindowOverlayStore } from '@/stores/window-overlay-store';

function requireRuntimeAppUrl(scope: string): string {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(`Invariant violated: ${scope} rendered before App accepted runtime config`);
  }
  return runtimeConfig.config.appUrl;
}

function extractWorkspaceSlug(path: string): string | null {
  const first = path.split('/').filter(Boolean)[0] ?? '';
  if (!first) return null;
  if (isReservedSlug(first)) return null;
  return first;
}

function tryRouteToOverlay(path: string): boolean {
  const overlay = useWindowOverlayStore.getState();
  if (path === '/workspaces/new') {
    overlay.open({ type: 'new-workspace' });
    return true;
  }
  if (path === '/workspaces/join') {
    overlay.open({ type: 'join-workspace' });
    return true;
  }
  if (path === '/onboarding' || path.startsWith('/onboarding?')) {
    overlay.open({ type: 'onboarding' });
    return true;
  }
  if (path === '/invitations') {
    overlay.open({ type: 'invitations' });
    return true;
  }
  if (path.startsWith('/invite/')) {
    let id = '';
    try {
      id = decodeURIComponent(path.slice('/invite/'.length));
    } catch {
      return true;
    }
    if (id) {
      overlay.open({ type: 'invite', invitationId: id });
      return true;
    }
  }
  if (overlay.overlay) overlay.close();
  return false;
}

function tryRouteToOtherWorkspace(path: string): boolean {
  const targetSlug = extractWorkspaceSlug(path);
  if (!targetSlug) return false;
  const { activeWorkspaceSlug, switchWorkspace } = useTabStore.getState();
  if (targetSlug === activeWorkspaceSlug) return false;
  switchWorkspace(targetSlug, path);
  return true;
}

export function routeContentLinkPath(path: string): void {
  const store = useTabStore.getState();
  const slug = extractWorkspaceSlug(path);
  if (slug && slug !== store.activeWorkspaceSlug) {
    store.switchWorkspace(slug, path);
    return;
  }
  const tabId = store.openTab(path, '');
  store.setActiveTab(tabId);
}

function tryRouteToPinnedNewTab(path: string): boolean {
  const store = useTabStore.getState();
  const active = getActiveTab(store);
  if (!active?.pinned) return false;

  const currentPathname = splitTabUrl(active.url).pathname;
  const newPathname = splitTabUrl(path).pathname;
  if (currentPathname === newPathname) return false;

  store.openTab(path, '', { activate: true });
  return true;
}

export function DesktopNavigationProvider({ children }: { children: React.ReactNode }) {
  const appUrl = requireRuntimeAppUrl('DesktopNavigationProvider');
  const activeUrl = useActiveTabUrl();
  const location = useMemo(() => {
    const url = activeUrl ?? '/';
    const { pathname, suffix } = splitTabUrl(url);
    const hashIdx = suffix.indexOf('#');
    const search = hashIdx === -1 ? suffix : suffix.slice(0, hashIdx);
    return { pathname, search };
  }, [activeUrl]);

  const adapter: NavigationAdapter = useMemo(
    () => ({
      push: (path: string) => {
        if (path === '/login') {
          useAuthStore.getState().logout();
          return;
        }
        if (tryRouteToOverlay(path)) return;
        const store = useTabStore.getState();
        const active = getActiveTab(store);
        if (active && active.url === path) return;
        if (tryRouteToOtherWorkspace(path)) return;
        if (tryRouteToPinnedNewTab(path)) return;
        store.navigateActiveSession(path);
      },
      replace: (path: string) => {
        if (tryRouteToOverlay(path)) return;
        if (tryRouteToOtherWorkspace(path)) return;
        useTabStore.getState().navigateActiveSession(path, { replace: true });
      },
      back: () => {
        useTabStore.getState().goBack();
      },
      canGoBack: () => {
        const active = getActiveTab(useTabStore.getState());
        return (active?.history.index ?? 0) > 0;
      },
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path: string, title?: string, opts?: { activate?: boolean }) => {
        const slug = extractWorkspaceSlug(path);
        const store = useTabStore.getState();
        if (slug && slug !== store.activeWorkspaceSlug) {
          store.switchWorkspace(slug, path);
          return;
        }
        store.openTab(path, title ?? '', { activate: opts?.activate });
      },
      getShareableUrl: (path: string) => `${appUrl}${path}`,
    }),
    [appUrl, location],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

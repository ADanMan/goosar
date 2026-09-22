import type { DataRouter } from 'react-router-dom';
import type { QueryClient } from '@tanstack/react-query';
import type { ScrollRestorationAdapter } from '@goosar/views/platform';
import { createAppRouter } from '@/routes';
import { useTabStore, getActiveTab, splitTabUrl, scrollMementoKey } from '@/stores/tab-store';

let router: DataRouter | null = null;
let initialized = false;

let pendingTokens = 0;

let recoveryAttempts = 0;
const MAX_RECOVERY_ATTEMPTS = 5;

let activeHostElement: HTMLElement | null = null;

let queryClient: QueryClient | null = null;

let lastIdentity: string | null = null;
let lastActiveTabId: string | null = null;
let lastActiveUrl: string | null = null;

export function getAppRouter(): DataRouter {
  if (!router) router = createAppRouter();
  return router;
}

export function registerActiveHostElement(el: HTMLElement | null): void {
  activeHostElement = el;
}

export function registerCoordinatorQueryClient(qc: QueryClient): void {
  queryClient = qc;
}

export function createScrollRestorationAdapter(tabId: string): ScrollRestorationAdapter {
  return {
    get(containerKey) {
      const state = useTabStore.getState();
      const active = getActiveTab(state);
      if (!active || active.id !== tabId) return undefined;
      const routeKey = splitTabUrl(active.url).pathname;
      return active.memento.scroll[scrollMementoKey(routeKey, containerKey)];
    },
  };
}

function currentRouterUrl(r: DataRouter): string {
  const { pathname, search, hash } = r.state.location;
  return `${pathname}${search ?? ''}${hash ?? ''}`;
}

function activeSessionUrl(): string | null {
  return getActiveTab(useTabStore.getState())?.url ?? null;
}

function reconcile(): void {
  const r = getAppRouter();
  const target = activeSessionUrl() ?? '/';
  if (currentRouterUrl(r) === target) {
    recoveryAttempts = 0;
    return;
  }
  pendingTokens++;
  r.navigate(target, { replace: true });
}

function captureScrollEntries(): Record<string, { top: number; height: number }> {
  const entries: Record<string, { top: number; height: number }> = {};
  if (!activeHostElement) return entries;
  const els = activeHostElement.querySelectorAll<HTMLElement>('[data-tab-scroll-root]');
  els.forEach((el) => {
    if (el.scrollTop <= 0) return;
    const key = el.getAttribute('data-tab-scroll-root') || 'main';
    entries[key] = { top: el.scrollTop, height: el.scrollHeight };
  });
  return entries;
}

function handleStoreChange(): void {
  const state = useTabStore.getState();
  const active = getActiveTab(state);
  const identity = active
    ? `${state.activeWorkspaceSlug}:${active.id}:${state.mountGeneration}`
    : null;
  const activeUrl = active?.url ?? null;

  const hostSwitching = identity !== lastIdentity;
  const inTabNavigation = !hostSwitching && activeUrl !== lastActiveUrl;
  if (hostSwitching || inTabNavigation) {
    const outgoingTabId = lastActiveTabId;
    const outgoingUrl = lastActiveUrl;
    lastIdentity = identity;
    lastActiveTabId = active?.id ?? null;
    lastActiveUrl = activeUrl;
    if (outgoingTabId && outgoingUrl) {
      const routeKey = splitTabUrl(outgoingUrl).pathname;
      useTabStore.getState().commitScrollMemento(outgoingTabId, routeKey, captureScrollEntries());
    }
  }

  reconcile();
}

function handleReloadGenerationChange(generation: number): void {
  void generation;
  queryClient?.invalidateQueries({ type: 'active', refetchType: 'none' });
}

export function initTabCoordinator(): void {
  if (initialized) return;
  initialized = true;

  const r = getAppRouter();

  r.subscribe(() => {
    if (pendingTokens > 0) {
      pendingTokens--;
      recoveryAttempts = 0;
      return;
    }
    const url = currentRouterUrl(r);
    const sessionUrl = activeSessionUrl() ?? '/';
    if (url === sessionUrl) return;
    if (recoveryAttempts >= MAX_RECOVERY_ATTEMPTS) {
      console.error(
        `[tab-coordinator] giving up recovery after ${MAX_RECOVERY_ATTEMPTS} attempts ` +
          `(router at "${url}", session at "${sessionUrl}")`,
      );
      return;
    }
    recoveryAttempts++;
    console.error(
      `[tab-coordinator] protocol error: router moved to "${url}" without a ` +
        `Coordinator token (session at "${sessionUrl}") — recovering ` +
        `(${recoveryAttempts}/${MAX_RECOVERY_ATTEMPTS})`,
    );
    reconcile();
  });

  let prevGeneration = useTabStore.getState().mountGeneration;
  useTabStore.subscribe((state) => {
    if (state.mountGeneration !== prevGeneration) {
      prevGeneration = state.mountGeneration;
      handleReloadGenerationChange(state.mountGeneration);
    }
    handleStoreChange();
  });

  const state = useTabStore.getState();
  const active = getActiveTab(state);
  lastIdentity = active
    ? `${state.activeWorkspaceSlug}:${active.id}:${state.mountGeneration}`
    : null;
  lastActiveTabId = active?.id ?? null;
  lastActiveUrl = active?.url ?? null;
  reconcile();
}

export function __resetTabCoordinatorForTests(): void {
  router = null;
  initialized = false;
  pendingTokens = 0;
  recoveryAttempts = 0;
  activeHostElement = null;
  queryClient = null;
  lastIdentity = null;
  lastActiveTabId = null;
  lastActiveUrl = null;
}

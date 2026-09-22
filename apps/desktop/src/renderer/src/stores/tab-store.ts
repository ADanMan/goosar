import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { arrayMove } from '@dnd-kit/sortable';
import { createPersistStorage, defaultStorage } from '@goosar/core/platform';
import { createSafeId } from '@goosar/core/utils';
import { isReservedSlug } from '@goosar/core/paths';

export interface TabMemento {
  scroll: Record<string, { top: number; height: number }>;
}

export function emptyMemento(): TabMemento {
  return { scroll: {} };
}

export function scrollMementoKey(routeKey: string, containerKey: string): string {
  return `${routeKey}::${containerKey}`;
}

export interface TabSession {
  id: string;
  url: string;
  resourceKey: string;
  title: string;
  pinned: boolean;
  history: { stack: string[]; index: number };
  memento: TabMemento;
}

export type Tab = TabSession;

export interface WorkspaceTabGroup {
  tabs: TabSession[];
  activeTabId: string;
}

interface TabStore {
  activeWorkspaceSlug: string | null;

  byWorkspace: Record<string, WorkspaceTabGroup>;

  autoPinned: Record<string, true>;

  mountGeneration: number;

  switchWorkspace: (slug: string, openPath?: string) => void;
  openTab: (path: string, title: string, opts?: { activate?: boolean }) => string;
  addTab: (path: string, title: string) => string;
  pinTabOnce: (slug: string, path: string, title: string) => string;
  closeTab: (tabId: string) => void;
  closeOtherTabs: (tabId: string) => void;
  setActiveTab: (tabId: string) => void;
  updateTab: (tabId: string, patch: Partial<Pick<TabSession, 'title'>>) => void;
  navigateActiveSession: (url: string, opts?: { replace?: boolean }) => void;
  goBack: () => void;
  goForward: () => void;
  commitScrollMemento: (
    tabId: string,
    routeKey: string,
    entries: Record<string, { top: number; height: number }>,
  ) => void;
  reloadActiveTab: () => void;
  closeActiveTab: () => void;
  moveTab: (fromIndex: number, toIndex: number) => void;
  togglePin: (tabId: string) => void;
  validateWorkspaceSlugs: (validSlugs: Set<string>) => void;
  reset: () => void;
}

function extractWorkspaceSlug(path: string): string | null {
  const first = path.split('/').filter(Boolean)[0] ?? '';
  if (!first) return null;
  if (isReservedSlug(first)) return null;
  return first;
}

export function splitTabUrl(url: string): { pathname: string; suffix: string } {
  const searchIdx = url.indexOf('?');
  const hashIdx = url.indexOf('#');
  const cut =
    searchIdx === -1 ? hashIdx : hashIdx === -1 ? searchIdx : Math.min(searchIdx, hashIdx);
  if (cut === -1) return { pathname: url, suffix: '' };
  return { pathname: url.slice(0, cut), suffix: url.slice(cut) };
}

export function resourceKeyForUrl(url: string): string {
  return splitTabUrl(url).pathname;
}

export function sanitizeTabPath(path: string): string | null {
  const { pathname, suffix } = splitTabUrl(path);
  const segments = pathname.split('/').filter(Boolean);
  const firstSegment = segments[0] ?? '';
  if (!firstSegment) return null;
  if (isReservedSlug(firstSegment)) {
    const isTransition = path === '/workspaces/new' || path.startsWith('/invite/');
    if (!isTransition) {
      console.warn(
        `[tab-store] tab path "${path}" starts with reserved slug "${firstSegment}" — ` +
          `caller likely forgot the workspace prefix. Dropping.`,
      );
    }
    return null;
  }
  if (segments.length === 1) {
    return `/${firstSegment}/issues${suffix}`;
  }
  return path;
}

function createId(): string {
  return createSafeId();
}

function makeSession(url: string, title: string): TabSession {
  return {
    id: createId(),
    url,
    resourceKey: resourceKeyForUrl(url),
    title,
    pinned: false,
    history: { stack: [url], index: 0 },
    memento: emptyMemento(),
  };
}

function pinnedBoundary(tabs: TabSession[]): number {
  let i = 0;
  while (i < tabs.length && tabs[i].pinned) i++;
  return i;
}

function defaultPathFor(slug: string): string {
  return `/${slug}/issues`;
}

function defaultTabFor(slug: string): TabSession {
  const path = defaultPathFor(slug);
  return makeSession(path, 'Issues');
}

function findTabLocation(
  byWorkspace: Record<string, WorkspaceTabGroup>,
  tabId: string,
): { slug: string; group: WorkspaceTabGroup; index: number } | null {
  for (const slug of Object.keys(byWorkspace)) {
    const group = byWorkspace[slug];
    const index = group.tabs.findIndex((t) => t.id === tabId);
    if (index >= 0) return { slug, group, index };
  }
  return null;
}

function buildCloseOtherTabsResult(
  byWorkspace: Record<string, WorkspaceTabGroup>,
  tabId: string,
): Record<string, WorkspaceTabGroup> | null {
  const hit = findTabLocation(byWorkspace, tabId);
  if (!hit) return null;
  const { slug, group } = hit;
  const closingTabs = group.tabs.filter((tab) => !tab.pinned && tab.id !== tabId);
  if (closingTabs.length === 0) return null;

  const closingIds = new Set(closingTabs.map((tab) => tab.id));
  const nextTabs = group.tabs.filter((tab) => !closingIds.has(tab.id));
  const nextActiveTabId = closingIds.has(group.activeTabId) ? tabId : group.activeTabId;

  return {
    ...byWorkspace,
    [slug]: { tabs: nextTabs, activeTabId: nextActiveTabId },
  };
}

export const useTabStore = create<TabStore>()(
  persist(
    (set, get) => ({
      activeWorkspaceSlug: null,
      byWorkspace: {},
      autoPinned: {},
      mountGeneration: 0,

      switchWorkspace(slug, openPath) {
        if (!slug) return;
        const { byWorkspace } = get();
        const existing = byWorkspace[slug];

        const desiredPath = openPath ?? (existing ? null : defaultPathFor(slug));

        if (!existing) {
          const cleanDesired = desiredPath ? sanitizeTabPath(desiredPath) : null;
          const seedPath = cleanDesired ?? defaultPathFor(slug);
          const tab = makeSession(seedPath, 'Issues');
          set({
            activeWorkspaceSlug: slug,
            byWorkspace: {
              ...byWorkspace,
              [slug]: { tabs: [tab], activeTabId: tab.id },
            },
          });
          return;
        }

        if (desiredPath) {
          const clean = sanitizeTabPath(desiredPath);
          if (clean) {
            const key = resourceKeyForUrl(clean);
            const match = existing.tabs.find((t) => t.resourceKey === key);
            if (match) {
              set({
                activeWorkspaceSlug: slug,
                byWorkspace: {
                  ...byWorkspace,
                  [slug]: { ...existing, activeTabId: match.id },
                },
              });
              return;
            }
            const tab = makeSession(clean, 'Issues');
            set({
              activeWorkspaceSlug: slug,
              byWorkspace: {
                ...byWorkspace,
                [slug]: {
                  tabs: [...existing.tabs, tab],
                  activeTabId: tab.id,
                },
              },
            });
            return;
          }
        }

        set({ activeWorkspaceSlug: slug });
      },

      openTab(path, title, opts) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        const clean = sanitizeTabPath(path);
        if (!activeWorkspaceSlug || !clean) return '';
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return '';

        const key = resourceKeyForUrl(clean);
        const existing = group.tabs.find((t) => t.resourceKey === key);
        if (existing) {
          set({
            byWorkspace: {
              ...byWorkspace,
              [activeWorkspaceSlug]: { ...group, activeTabId: existing.id },
            },
          });
          return existing.id;
        }

        const tab = makeSession(clean, title);
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: {
              tabs: [...group.tabs, tab],
              activeTabId: opts?.activate === true ? tab.id : group.activeTabId,
            },
          },
        });
        return tab.id;
      },

      addTab(path, title) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        const clean = sanitizeTabPath(path);
        if (!activeWorkspaceSlug || !clean) return '';
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return '';

        const tab = makeSession(clean, title);
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: {
              tabs: [...group.tabs, tab],
              activeTabId: group.activeTabId,
            },
          },
        });
        return tab.id;
      },

      pinTabOnce(slug, path, title) {
        const clean = sanitizeTabPath(path);
        if (!slug || !clean) return '';
        const { byWorkspace, autoPinned } = get();
        const marker = `${slug}::${clean}`;
        if (autoPinned[marker]) return '';

        const group = byWorkspace[slug];
        const baseTabs = group ? group.tabs : [defaultTabFor(slug)];
        const baseActiveId = group ? group.activeTabId : baseTabs[0].id;

        const key = resourceKeyForUrl(clean);
        const existingIndex = baseTabs.findIndex((t) => t.resourceKey === key);
        let tabs: TabSession[];
        let tabId: string;
        if (existingIndex >= 0) {
          const existing = baseTabs[existingIndex];
          tabId = existing.id;
          const rest = baseTabs.filter((_, i) => i !== existingIndex);
          tabs = [
            ...rest.slice(0, pinnedBoundary(rest)),
            { ...existing, pinned: true },
            ...rest.slice(pinnedBoundary(rest)),
          ];
        } else {
          const tab: TabSession = { ...makeSession(clean, title), pinned: true };
          tabId = tab.id;
          tabs = [
            ...baseTabs.slice(0, pinnedBoundary(baseTabs)),
            tab,
            ...baseTabs.slice(pinnedBoundary(baseTabs)),
          ];
        }

        set({
          autoPinned: { ...autoPinned, [marker]: true },
          byWorkspace: {
            ...byWorkspace,
            [slug]: { tabs, activeTabId: tabId },
          },
        });
        void baseActiveId;
        return tabId;
      },

      closeTab(tabId) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;

        if (group.tabs.length === 1) {
          const fresh = defaultTabFor(slug);
          set({
            byWorkspace: {
              ...byWorkspace,
              [slug]: { tabs: [fresh], activeTabId: fresh.id },
            },
          });
          return;
        }

        const nextTabs = group.tabs.filter((t) => t.id !== tabId);
        const nextActiveTabId =
          group.activeTabId === tabId
            ? nextTabs[Math.min(index, nextTabs.length - 1)].id
            : group.activeTabId;

        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { tabs: nextTabs, activeTabId: nextActiveTabId },
          },
        });
      },

      closeOtherTabs(tabId) {
        const { byWorkspace } = get();
        const next = buildCloseOtherTabsResult(byWorkspace, tabId);
        if (!next) return;
        set({ byWorkspace: next });
      },

      setActiveTab(tabId) {
        const { byWorkspace, activeWorkspaceSlug } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group } = hit;
        if (slug === activeWorkspaceSlug && group.activeTabId === tabId) return;
        set({
          activeWorkspaceSlug: slug,
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, activeTabId: tabId },
          },
        });
      },

      updateTab(tabId, patch) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];
        const next: TabSession = { ...current, ...patch };
        if (next.title === current.title) {
          return;
        }
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      navigateActiveSession(url, opts) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        const index = group.tabs.findIndex((t) => t.id === group.activeTabId);
        if (index < 0) return;
        const clean = sanitizeTabPath(url);
        if (!clean) return;
        if (extractWorkspaceSlug(clean) !== activeWorkspaceSlug) return;

        const current = group.tabs[index];
        if (current.url === clean) return;

        const replace = opts?.replace === true;
        const stack = replace
          ? [
              ...current.history.stack.slice(0, current.history.index),
              clean,
              ...current.history.stack.slice(current.history.index + 1),
            ]
          : [...current.history.stack.slice(0, current.history.index + 1), clean];
        const historyIndex = replace ? current.history.index : current.history.index + 1;

        const next: TabSession = {
          ...current,
          url: clean,
          resourceKey: resourceKeyForUrl(clean),
          history: { stack, index: historyIndex },
        };
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: { ...group, tabs: nextTabs },
          },
        });
      },

      goBack() {
        stepHistory(get, set, -1);
      },

      goForward() {
        stepHistory(get, set, +1);
      },

      commitScrollMemento(tabId, routeKey, entries) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];

        const prefix = `${routeKey}::`;
        const nextScroll: TabMemento['scroll'] = {};
        for (const [key, value] of Object.entries(current.memento.scroll)) {
          if (!key.startsWith(prefix)) nextScroll[key] = value;
        }
        for (const [containerKey, value] of Object.entries(entries)) {
          nextScroll[scrollMementoKey(routeKey, containerKey)] = value;
        }

        const prevKeys = Object.keys(current.memento.scroll);
        const nextKeys = Object.keys(nextScroll);
        const unchanged =
          prevKeys.length === nextKeys.length &&
          nextKeys.every((k) => {
            const prev = current.memento.scroll[k];
            const next = nextScroll[k];
            return prev !== undefined && prev.top === next.top && prev.height === next.height;
          });
        if (unchanged) return;

        const nextTabs = [...group.tabs];
        nextTabs[index] = { ...current, memento: { scroll: nextScroll } };
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      reloadActiveTab() {
        const { activeWorkspaceSlug, byWorkspace, mountGeneration } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        if (!group.tabs.some((t) => t.id === group.activeTabId)) return;
        set({ mountGeneration: mountGeneration + 1 });
      },

      closeActiveTab() {
        const { activeWorkspaceSlug, byWorkspace, closeTab } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        closeTab(group.activeTabId);
      },

      moveTab(fromIndex, toIndex) {
        if (fromIndex === toIndex) return;
        const { activeWorkspaceSlug, byWorkspace } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        if (fromIndex < 0 || fromIndex >= group.tabs.length) return;

        const boundary = pinnedBoundary(group.tabs);
        const source = group.tabs[fromIndex];
        let clampedTo: number;
        if (source.pinned) {
          clampedTo = Math.max(0, Math.min(toIndex, boundary - 1));
        } else {
          clampedTo = Math.max(boundary, Math.min(toIndex, group.tabs.length - 1));
        }
        if (clampedTo === fromIndex) return;
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: {
              ...group,
              tabs: arrayMove(group.tabs, fromIndex, clampedTo),
            },
          },
        });
      },

      togglePin(tabId) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];
        const nextTab: TabSession = { ...current, pinned: !current.pinned };

        const withoutCurrent = [...group.tabs.slice(0, index), ...group.tabs.slice(index + 1)];
        const newBoundary = pinnedBoundary(withoutCurrent);
        const insertAt = newBoundary;
        const nextTabs = [
          ...withoutCurrent.slice(0, insertAt),
          nextTab,
          ...withoutCurrent.slice(insertAt),
        ];

        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      validateWorkspaceSlugs(validSlugs) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        let changed = false;
        const nextByWorkspace: Record<string, WorkspaceTabGroup> = {};
        for (const slug of Object.keys(byWorkspace)) {
          if (validSlugs.has(slug)) {
            nextByWorkspace[slug] = byWorkspace[slug];
          } else {
            changed = true;
          }
        }

        let nextActive = activeWorkspaceSlug;
        if (nextActive && !validSlugs.has(nextActive)) {
          nextActive = Object.keys(nextByWorkspace)[0] ?? null;
          changed = true;
        }

        if (!nextActive) {
          nextActive = Object.keys(nextByWorkspace)[0] ?? null;
          if (nextActive) changed = true;
        }

        if (!nextActive) {
          const fallbackSlug = validSlugs.values().next().value;
          if (fallbackSlug) {
            const fresh = defaultTabFor(fallbackSlug);
            nextByWorkspace[fallbackSlug] = {
              tabs: [fresh],
              activeTabId: fresh.id,
            };
            nextActive = fallbackSlug;
            changed = true;
          }
        }

        if (!changed) return;
        set({ byWorkspace: nextByWorkspace, activeWorkspaceSlug: nextActive });
      },

      reset() {
        set({
          activeWorkspaceSlug: null,
          byWorkspace: {},
          autoPinned: {},
          mountGeneration: 0,
        });
      },
    }),
    {
      name: 'goosar_tabs',
      version: 4,
      storage: createJSONStorage(() => createPersistStorage(defaultStorage)),
      migrate: (persistedState, version) => {
        let state = persistedState;
        if (version < 2 && state && typeof state === 'object') {
          state = migrateV1ToV2(state as Partial<V1Persisted>);
        }
        if (version < 3 && state && typeof state === 'object') {
          state = migrateV2ToV3(state as V2Persisted);
        }
        if (version < 4 && state && typeof state === 'object') {
          state = migrateV3ToV4(state as V3Persisted);
        }
        return state as V4Persisted;
      },
      partialize: (state) => ({
        activeWorkspaceSlug: state.activeWorkspaceSlug,
        autoPinned: state.autoPinned,
        byWorkspace: Object.fromEntries(
          Object.entries(state.byWorkspace).map(([slug, group]) => [
            slug,
            {
              activeTabId: group.activeTabId,
              tabs: group.tabs.map((t) => ({
                id: t.id,
                url: t.url,
                title: t.title,
                pinned: t.pinned,
                history: t.history,
                memento: t.memento,
              })),
            },
          ]),
        ),
      }),
      merge: (persistedState, currentState) => mergePersistedTabs(persistedState, currentState),
    },
  ),
);

interface PersistedTabState {
  activeWorkspaceSlug: string | null;
  byWorkspace: Record<string, WorkspaceTabGroup>;
  autoPinned: Record<string, true>;
}

export function mergePersistedTabs<T extends PersistedTabState>(
  persistedState: unknown,
  currentState: T,
): T {
  const persisted = persistedState as Partial<V4Persisted> | undefined;
  if (!persisted?.byWorkspace) return currentState;

  const byWorkspace: Record<string, WorkspaceTabGroup> = {};
  for (const [slug, pGroup] of Object.entries(persisted.byWorkspace)) {
    const tabs: TabSession[] = [];
    for (const pTab of pGroup.tabs) {
      const clean = sanitizeTabPath(pTab.url);
      if (!clean || extractWorkspaceSlug(clean) !== slug) {
        console.warn(
          `[tab-store] dropping persisted tab "${pTab.url}" from ` +
            `group "${slug}" — url/slug mismatch`,
        );
        continue;
      }
      const stack =
        Array.isArray(pTab.history?.stack) && pTab.history.stack.length > 0
          ? pTab.history.stack
          : [clean];
      const index = Math.min(
        Math.max(pTab.history?.index ?? stack.length - 1, 0),
        stack.length - 1,
      );
      tabs.push({
        id: pTab.id,
        url: clean,
        resourceKey: resourceKeyForUrl(clean),
        title: pTab.title,
        pinned: pTab.pinned === true,
        history: { stack, index },
        memento:
          pTab.memento && typeof pTab.memento.scroll === 'object' ? pTab.memento : emptyMemento(),
      });
    }
    if (tabs.length === 0) continue;
    tabs.sort((a, b) => (a.pinned === b.pinned ? 0 : a.pinned ? -1 : 1));
    const activeTabId = tabs.some((t) => t.id === pGroup.activeTabId)
      ? pGroup.activeTabId
      : tabs[0].id;
    byWorkspace[slug] = { tabs, activeTabId };
  }

  const activeWorkspaceSlug =
    persisted.activeWorkspaceSlug && byWorkspace[persisted.activeWorkspaceSlug]
      ? persisted.activeWorkspaceSlug
      : (Object.keys(byWorkspace)[0] ?? null);

  const autoPinned =
    persisted.autoPinned && typeof persisted.autoPinned === 'object'
      ? Object.fromEntries(
          Object.entries(persisted.autoPinned)
            .filter(([, v]) => v === true)
            .map(([k]) => [k, true as const]),
        )
      : {};

  return { ...currentState, byWorkspace, activeWorkspaceSlug, autoPinned };
}

function stepHistory(
  get: () => TabStore,
  set: (partial: Partial<TabStore>) => void,
  delta: -1 | 1,
) {
  const { activeWorkspaceSlug, byWorkspace } = get();
  if (!activeWorkspaceSlug) return;
  const group = byWorkspace[activeWorkspaceSlug];
  if (!group) return;
  const index = group.tabs.findIndex((t) => t.id === group.activeTabId);
  if (index < 0) return;
  const current = group.tabs[index];
  const nextIndex = current.history.index + delta;
  if (nextIndex < 0 || nextIndex >= current.history.stack.length) return;
  const url = current.history.stack[nextIndex];
  const next: TabSession = {
    ...current,
    url,
    resourceKey: resourceKeyForUrl(url),
    history: { ...current.history, index: nextIndex },
  };
  const nextTabs = [...group.tabs];
  nextTabs[index] = next;
  set({
    byWorkspace: {
      ...byWorkspace,
      [activeWorkspaceSlug]: { ...group, tabs: nextTabs },
    },
  });
}

interface V1Tab {
  id: string;
  path: string;
  title: string;
  icon: string;
}

interface V1Persisted {
  tabs: V1Tab[];
  activeTabId: string;
}

interface V2PersistedTab {
  id: string;
  path: string;
  title: string;
  icon: string;
}

interface V2PersistedGroup {
  tabs: V2PersistedTab[];
  activeTabId: string;
}

interface V2Persisted {
  activeWorkspaceSlug: string | null;
  byWorkspace: Record<string, V2PersistedGroup>;
}

interface V3PersistedTab {
  id: string;
  path: string;
  title: string;
  icon: string;
  pinned: boolean;
}

interface V3PersistedGroup {
  tabs: V3PersistedTab[];
  activeTabId: string;
}

interface V3Persisted {
  activeWorkspaceSlug: string | null;
  byWorkspace: Record<string, V3PersistedGroup>;
}

interface V4PersistedTab {
  id: string;
  url: string;
  title: string;
  icon?: string;
  pinned: boolean;
  history: { stack: string[]; index: number };
  memento: TabMemento;
}

interface V4PersistedGroup {
  tabs: V4PersistedTab[];
  activeTabId: string;
}

interface V4Persisted {
  activeWorkspaceSlug: string | null;
  byWorkspace: Record<string, V4PersistedGroup>;
  autoPinned?: Record<string, true>;
}

export function migrateV3ToV4(v3: V3Persisted): V4Persisted {
  const byWorkspace: Record<string, V4PersistedGroup> = {};
  for (const [slug, group] of Object.entries(v3.byWorkspace ?? {})) {
    byWorkspace[slug] = {
      activeTabId: group.activeTabId,
      tabs: group.tabs.map((t) => ({
        id: t.id,
        url: t.path,
        title: t.title,
        pinned: t.pinned,
        history: { stack: [t.path], index: 0 },
        memento: emptyMemento(),
      })),
    };
  }
  return {
    activeWorkspaceSlug: v3.activeWorkspaceSlug ?? null,
    byWorkspace,
  };
}

export function migrateV2ToV3(v2: V2Persisted): V3Persisted {
  const byWorkspace: Record<string, V3PersistedGroup> = {};
  for (const [slug, group] of Object.entries(v2.byWorkspace ?? {})) {
    byWorkspace[slug] = {
      activeTabId: group.activeTabId,
      tabs: group.tabs.map((t) => ({ ...t, pinned: false })),
    };
  }
  return {
    activeWorkspaceSlug: v2.activeWorkspaceSlug ?? null,
    byWorkspace,
  };
}

export function migrateV1ToV2(v1: Partial<V1Persisted>): V2Persisted {
  const byWorkspace: Record<string, V2PersistedGroup> = {};
  const oldTabs = v1.tabs ?? [];
  for (const tab of oldTabs) {
    const slug = extractWorkspaceSlug(tab.path);
    if (!slug) continue; 
    if (!byWorkspace[slug]) byWorkspace[slug] = { tabs: [], activeTabId: '' };
    byWorkspace[slug].tabs.push({
      id: tab.id,
      path: tab.path,
      title: tab.title,
      icon: tab.icon,
    });
  }

  for (const slug of Object.keys(byWorkspace)) {
    const group = byWorkspace[slug];
    const hasOldActive = group.tabs.some((t) => t.id === v1.activeTabId);
    group.activeTabId = hasOldActive ? (v1.activeTabId as string) : group.tabs[0].id;
  }

  let activeWorkspaceSlug: string | null = null;
  for (const slug of Object.keys(byWorkspace)) {
    if (byWorkspace[slug].activeTabId === v1.activeTabId) {
      activeWorkspaceSlug = slug;
      break;
    }
  }
  if (!activeWorkspaceSlug) {
    activeWorkspaceSlug = Object.keys(byWorkspace)[0] ?? null;
  }

  return { activeWorkspaceSlug, byWorkspace };
}

export function getActiveTab(s: TabStore): TabSession | null {
  if (!s.activeWorkspaceSlug) return null;
  const group = s.byWorkspace[s.activeWorkspaceSlug];
  if (!group) return null;
  return group.tabs.find((t) => t.id === group.activeTabId) ?? null;
}

export function useActiveGroup(): WorkspaceTabGroup | null {
  return useTabStore((s) =>
    s.activeWorkspaceSlug ? (s.byWorkspace[s.activeWorkspaceSlug] ?? null) : null,
  );
}

export function useActiveTabIdentity(): { slug: string | null; tabId: string | null } {
  const slug = useTabStore((s) => s.activeWorkspaceSlug);
  const tabId = useTabStore((s) =>
    s.activeWorkspaceSlug ? (s.byWorkspace[s.activeWorkspaceSlug]?.activeTabId ?? null) : null,
  );
  return { slug, tabId };
}

export function useActiveTabUrl(): string | null {
  return useTabStore((s) => getActiveTab(s)?.url ?? null);
}

export function useActiveTabHistory(): {
  historyIndex: number;
  historyLength: number;
} {
  const historyIndex = useTabStore((s) => getActiveTab(s)?.history.index ?? 0);
  const historyLength = useTabStore((s) => getActiveTab(s)?.history.stack.length ?? 1);
  return { historyIndex, historyLength };
}

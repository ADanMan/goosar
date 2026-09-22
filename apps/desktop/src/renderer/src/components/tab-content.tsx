import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { RouterProvider } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { ScrollRestorationProvider } from '@goosar/views/platform';
import { useActiveGroup, useTabStore } from '@/stores/tab-store';
import {
  createScrollRestorationAdapter,
  getAppRouter,
  initTabCoordinator,
  registerActiveHostElement,
  registerCoordinatorQueryClient,
} from '@/platform/tab-coordinator';

export function TabContent() {
  const group = useActiveGroup();
  const generation = useTabStore((s) => s.mountGeneration);
  const qc = useQueryClient();

  useState(() => {
    initTabCoordinator();
    return true;
  });

  useEffect(() => {
    registerCoordinatorQueryClient(qc);
  }, [qc]);

  useEffect(() => {
    if (!group) return;
    const tab = group.tabs.find((t) => t.id === group.activeTabId);
    if (tab) document.title = tab.title;
  }, [group?.activeTabId, group?.tabs]);

  if (!group) return null;

  return <ActiveTabHost key={`${group.activeTabId}:${generation}`} tabId={group.activeTabId} />;
}

function ActiveTabHost({ tabId }: { tabId: string }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const router = getAppRouter();
  const scrollAdapter = useMemo(() => createScrollRestorationAdapter(tabId), [tabId]);

  useLayoutEffect(() => {
    registerActiveHostElement(hostRef.current);
    return () => registerActiveHostElement(null);
  }, []);

  return (
    <div ref={hostRef} style={{ display: 'contents' }}>
      <ScrollRestorationProvider adapter={scrollAdapter}>
        <RouterProvider router={router} />
      </ScrollRestorationProvider>
    </div>
  );
}

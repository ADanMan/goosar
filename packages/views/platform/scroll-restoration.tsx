'use client';

import { createContext, useCallback, useContext, type ReactNode } from 'react';

export interface ScrollRestorationAdapter {
  get(containerKey: string): { top: number; height: number } | undefined;
}

const ScrollRestorationContext = createContext<ScrollRestorationAdapter | null>(null);

export function ScrollRestorationProvider({
  adapter,
  children,
}: {
  adapter: ScrollRestorationAdapter;
  children: ReactNode;
}) {
  return (
    <ScrollRestorationContext.Provider value={adapter}>
      {children}
    </ScrollRestorationContext.Provider>
  );
}

export function useRestoredScrollOffset(containerKey: string): number | undefined {
  const adapter = useContext(ScrollRestorationContext);
  return adapter?.get(containerKey)?.top;
}

export function useRestoredScrollRef(containerKey: string): (el: HTMLElement | null) => void {
  const adapter = useContext(ScrollRestorationContext);
  return useCallback(
    (el: HTMLElement | null) => {
      if (!el) return;
      const saved = adapter?.get(containerKey);
      if (!saved || saved.top <= 0) return;
      el.scrollTop = saved.top;
    },
    [adapter, containerKey],
  );
}

'use client';

import { createContext, useContext, type ReactNode } from 'react';

const RichContentScrollRootContext = createContext<HTMLElement | null>(null);

export function RichContentScrollRootProvider({
  scrollRoot,
  children,
}: {
  scrollRoot: HTMLElement | null;
  children: ReactNode;
}) {
  return (
    <RichContentScrollRootContext.Provider value={scrollRoot}>
      {children}
    </RichContentScrollRootContext.Provider>
  );
}

export function useRichContentScrollRoot(): HTMLElement | null {
  return useContext(RichContentScrollRootContext);
}

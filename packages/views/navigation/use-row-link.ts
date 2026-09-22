'use client';

import { useCallback } from 'react';
import { useNavigation } from './context';

export function useRowLink() {
  const { push, openInNewTab, prefetch } = useNavigation();

  return useCallback(
    (href: string) => {
      const open = (newTab: boolean) => {
        if (newTab && openInNewTab) openInNewTab(href);
        else push(href);
      };
      return {
        onClick: (e: React.MouseEvent) => {
          if (e.defaultPrevented || e.button !== 0) return;
          open(e.metaKey || e.ctrlKey);
        },
        onAuxClick: (e: React.MouseEvent) => {
          if (e.defaultPrevented || e.button !== 1) return; 
          e.preventDefault();
          open(true);
        },
        onMouseEnter: () => prefetch?.(href),
      };
    },
    [push, openInNewTab, prefetch],
  );
}

'use client';

import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useRichContentScrollRoot } from './scroll-root';
import { hasBlockMounted, markBlockMounted, mountedBlockKey } from './mounted-block-registry';

const useIsomorphicLayoutEffect = typeof window !== 'undefined' ? useLayoutEffect : useEffect;

const NEAR_VIEWPORT_ROOT_MARGIN = '800px 0px';

function supportsIntersectionObserver(): boolean {
  return typeof window !== 'undefined' && typeof window.IntersectionObserver === 'function';
}

export function LazyRichBlock({
  reservedHeightPx,
  sourceKey,
  children,
}: {
  reservedHeightPx: number;
  sourceKey?: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const scrollRoot = useRichContentScrollRoot();
  const registryKey = sourceKey == null ? null : mountedBlockKey('rich', sourceKey);
  const [mounted, setMounted] = useState(false);

  useIsomorphicLayoutEffect(() => {
    if (mounted || registryKey == null) return;
    if (hasBlockMounted(registryKey)) setMounted(true);
  }, [mounted, registryKey]);

  useEffect(() => {
    if (mounted) return;

    if (!supportsIntersectionObserver()) {
      setMounted(true);
      return;
    }

    const el = ref.current;
    if (!el) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setMounted(true);
          observer.disconnect();
        }
      },
      {
        root: scrollRoot,
        rootMargin: NEAR_VIEWPORT_ROOT_MARGIN,
      },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [mounted, scrollRoot]);

  useEffect(() => {
    if (mounted && registryKey != null) markBlockMounted(registryKey);
  }, [mounted, registryKey]);

  return (
    <div
      ref={ref}
      data-rich-block-shell=""
      data-mounted={mounted ? '' : undefined}
      style={{ minHeight: reservedHeightPx }}
    >
      {mounted ? children : <RichBlockPlaceholder />}
    </div>
  );
}

function RichBlockPlaceholder() {
  return (
    <div
      className="my-3 h-full w-full rounded-md border border-border/50 bg-muted/20"
      aria-hidden="true"
    />
  );
}

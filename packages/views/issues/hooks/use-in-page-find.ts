'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { getShortcut, shortcutMatchesEvent } from '@goosar/core/shortcuts';
import { isImeComposing } from '@goosar/core/utils';

const HIGHLIGHT_NAME = 'goosar-find';
const ACTIVE_HIGHLIGHT_NAME = 'goosar-find-active';

function highlightApiSupported(): boolean {
  return typeof CSS !== 'undefined' && 'highlights' in CSS && typeof Highlight !== 'undefined';
}

const SKIP_TAGS = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT']);

export interface TextMatch {
  node: Text;
  start: number;
  end: number;
}

export function collectTextMatches(root: HTMLElement, query: string): TextMatch[] {
  const matches: TextMatch[] = [];
  const needle = query.toLowerCase();
  if (!needle) return matches;

  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const parent = node.parentElement;
      if (!parent) return NodeFilter.FILTER_REJECT;
      if (SKIP_TAGS.has(parent.tagName)) return NodeFilter.FILTER_REJECT;
      if (parent.closest('[data-find-ignore]')) return NodeFilter.FILTER_REJECT;
      const value = node.nodeValue;
      if (!value || value.trim().length === 0) return NodeFilter.FILTER_REJECT;
      return NodeFilter.FILTER_ACCEPT;
    },
  });

  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const textNode = node as Text;
    const haystack = (textNode.nodeValue ?? '').toLowerCase();
    let index = haystack.indexOf(needle);
    while (index !== -1) {
      matches.push({ node: textNode, start: index, end: index + needle.length });
      index = haystack.indexOf(needle, index + needle.length);
    }
  }

  return matches;
}

function isElementVisible(el: HTMLElement | null): boolean {
  return !!el && el.getClientRects().length > 0;
}

function scrollRangeIntoView(container: HTMLElement | null, range: Range): void {
  if (!container) return;
  const rects = range.getClientRects();
  const rect = rects.length > 0 ? rects[0]! : range.getBoundingClientRect();
  if (!rect || (rect.width === 0 && rect.height === 0)) return;

  const containerRect = container.getBoundingClientRect();
  const pad = 80;
  const above = rect.top < containerRect.top + pad;
  const below = rect.bottom > containerRect.bottom - pad;
  if (!above && !below) return;

  const offsetWithin = rect.top - containerRect.top + container.scrollTop;
  const target = offsetWithin - container.clientHeight / 2 + rect.height / 2;
  container.scrollTop = Math.max(0, target);
}

export interface UseInPageFindResult {
  open: boolean;
  query: string;
  matchCount: number;
  activeIndex: number;
  supported: boolean;
  inputRef: React.RefObject<HTMLInputElement | null>;
  setQuery: (value: string) => void;
  openFind: () => void;
  closeFind: () => void;
  goNext: () => void;
  goPrev: () => void;
}

export function useInPageFind(options: {
  container: HTMLElement | null;
  contentKey: unknown;
  enabled?: boolean;
}): UseInPageFindResult {
  const { container, contentKey, enabled = true } = options;

  const [open, setOpen] = useState(false);
  const [query, setQueryState] = useState('');
  const [matchCount, setMatchCount] = useState(0);
  const [activeIndex, setActiveIndex] = useState(-1);

  const inputRef = useRef<HTMLInputElement | null>(null);
  const containerRef = useRef<HTMLElement | null>(container);
  containerRef.current = container;
  const rangesRef = useRef<Range[]>([]);
  const activeIndexRef = useRef(activeIndex);
  activeIndexRef.current = activeIndex;
  const supported = highlightApiSupported();

  const clearHighlights = useCallback(() => {
    if (!supported) return;
    CSS.highlights.delete(HIGHLIGHT_NAME);
    CSS.highlights.delete(ACTIVE_HIGHLIGHT_NAME);
  }, [supported]);

  const applyActive = useCallback(
    (ranges: Range[], index: number, scroll: boolean) => {
      const range = index >= 0 ? ranges[index] : undefined;
      if (supported) {
        if (range) {
          const active = new Highlight(range);
          active.priority = 1;
          CSS.highlights.set(ACTIVE_HIGHLIGHT_NAME, active);
        } else {
          CSS.highlights.delete(ACTIVE_HIGHLIGHT_NAME);
        }
      }
      if (range && scroll) scrollRangeIntoView(containerRef.current, range);
    },
    [supported],
  );

  const recompute = useCallback(
    (resetActive: boolean) => {
      const root = containerRef.current;
      if (!open || !root || query.trim().length === 0) {
        rangesRef.current = [];
        clearHighlights();
        setMatchCount(0);
        setActiveIndex(-1);
        return;
      }

      const matches = collectTextMatches(root, query);
      const ranges = matches.map((m) => {
        const range = new Range();
        range.setStart(m.node, m.start);
        range.setEnd(m.node, m.end);
        return range;
      });
      rangesRef.current = ranges;

      if (ranges.length === 0) {
        clearHighlights();
        setMatchCount(0);
        setActiveIndex(-1);
        return;
      }

      if (supported) {
        CSS.highlights.set(HIGHLIGHT_NAME, new Highlight(...ranges));
      }
      setMatchCount(ranges.length);
      const prev = activeIndexRef.current;
      const nextActive = resetActive || prev < 0 ? 0 : Math.min(prev, ranges.length - 1);
      setActiveIndex(nextActive);
      applyActive(ranges, nextActive, resetActive);
    },
    [open, query, supported, clearHighlights, applyActive],
  );

  const recomputeRef = useRef(recompute);
  recomputeRef.current = recompute;

  useEffect(() => {
    const raf = requestAnimationFrame(() => recomputeRef.current(true));
    return () => cancelAnimationFrame(raf);
  }, [open, query, container]);

  useEffect(() => {
    if (!open) return;
    const raf = requestAnimationFrame(() => recomputeRef.current(false));
    return () => cancelAnimationFrame(raf);
  }, [contentKey, open]);

  useEffect(() => {
    if (!open || !container) return;
    let raf = 0;
    const observer = new MutationObserver(() => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => recomputeRef.current(false));
    });
    observer.observe(container, {
      subtree: true,
      childList: true,
      characterData: true,
    });
    return () => {
      observer.disconnect();
      cancelAnimationFrame(raf);
    };
  }, [open, container]);

  useEffect(() => {
    if (!open) return;
    applyActive(rangesRef.current, activeIndex, true);
  }, [activeIndex, open, applyActive]);

  useEffect(() => {
    if (!enabled) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.repeat || isImeComposing(e)) return;
      if (!shortcutMatchesEvent(getShortcut('findInIssue'), e)) return;
      if (!isElementVisible(containerRef.current)) return;
      e.preventDefault();
      setOpen(true);
      requestAnimationFrame(() => {
        const input = inputRef.current;
        if (input) {
          input.focus();
          input.select();
        }
      });
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [enabled]);

  useEffect(() => clearHighlights, [clearHighlights]);

  const setQuery = useCallback((value: string) => setQueryState(value), []);
  const openFind = useCallback(() => setOpen(true), []);
  const closeFind = useCallback(() => setOpen(false), []);

  const goNext = useCallback(() => {
    setActiveIndex((prev) => {
      const n = rangesRef.current.length;
      if (n === 0) return -1;
      return prev < 0 ? 0 : (prev + 1) % n;
    });
  }, []);

  const goPrev = useCallback(() => {
    setActiveIndex((prev) => {
      const n = rangesRef.current.length;
      if (n === 0) return -1;
      return prev < 0 ? n - 1 : (prev - 1 + n) % n;
    });
  }, []);

  return {
    open,
    query,
    matchCount,
    activeIndex,
    supported,
    inputRef,
    setQuery,
    openFind,
    closeFind,
    goNext,
    goPrev,
  };
}

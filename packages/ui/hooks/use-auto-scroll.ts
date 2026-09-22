import { type RefObject, useEffect, useRef, useCallback } from 'react';

export function useAutoScroll(ref: RefObject<HTMLElement | null>) {
  const stickRef = useRef(true);
  const lockRef = useRef(false);
  const didInitialScrollRef = useRef(false);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const scrollToBottom = () => {
      el.scrollTo({ top: el.scrollHeight });
    };

    const onScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el;
      stickRef.current = scrollHeight - scrollTop - clientHeight < 50;
    };

    const onContentChange = () => {
      if (lockRef.current) return;
      if (stickRef.current) {
        scrollToBottom();
      }
    };

    const ro = new ResizeObserver(onContentChange);
    for (const child of el.children) {
      ro.observe(child);
    }

    const mo = new MutationObserver((mutations) => {
      for (const mutation of mutations) {
        for (const node of mutation.addedNodes) {
          if (node instanceof Element) {
            ro.observe(node);
          }
        }
      }
      onContentChange();
    });
    mo.observe(el, { childList: true, subtree: true });

    el.addEventListener('scroll', onScroll, { passive: true });

    if (!didInitialScrollRef.current) {
      didInitialScrollRef.current = true;
      scrollToBottom();
    }

    return () => {
      el.removeEventListener('scroll', onScroll);
      ro.disconnect();
      mo.disconnect();
    };
  }, [ref]);

  const suppressAutoScroll = useCallback(() => {
    lockRef.current = true;
    return () => {
      lockRef.current = false;
    };
  }, []);

  return { suppressAutoScroll };
}

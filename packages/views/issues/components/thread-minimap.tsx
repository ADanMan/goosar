import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { TimelineEntry } from '@goosar/core/types';
import { useActorName } from '@goosar/core/workspace/hooks';
import { cn } from '@goosar/ui/lib/utils';
import { useT } from '../../i18n';

const MIN_THREADS = 2;

const PREVIEW_OPEN_DELAY_MS = 150;
const PREVIEW_CLOSE_DELAY_MS = 150;

const WAVE_RADIUS_PX = 56;
const WAVE_MAX_SCALE = 1.7;

export function waveScale(distancePx: number): number {
  const d = Math.abs(distancePx);
  if (d >= WAVE_RADIUS_PX) return 1;
  const t = Math.cos(((d / WAVE_RADIUS_PX) * Math.PI) / 2);
  return 1 + (WAVE_MAX_SCALE - 1) * t * t;
}

const PREVIEW_TITLE_MAX = 200;
const PREVIEW_BODY_MAX = 300;

export function commentPreview(markdown: string): { title: string; body: string } {
  const lines = markdown
    .replace(/```[\s\S]*?```/g, ' ')
    .split(/\r?\n/)
    .map((line) =>
      line
        .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
        .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
        .replace(/^\s*(?:[-+*]|\d+[.)])\s+/, '')
        .replace(/[#*`>~]/g, '')
        .replace(/\s+/g, ' ')
        .trim(),
    )
    .filter(Boolean);
  return {
    title: (lines[0] ?? '').slice(0, PREVIEW_TITLE_MAX),
    body: lines.slice(1).join(' ').slice(0, PREVIEW_BODY_MAX),
  };
}

export interface ThreadMinimapThread {
  id: string;
  entry: TimelineEntry;
}

interface ThreadMinimapProps {
  threads: ThreadMinimapThread[];
  scrollContainerEl: HTMLElement | null;
  onJump: (threadId: string) => void;
  className?: string;
}

function sameIdSet(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}

function useVisibleThreadIds(
  threads: ThreadMinimapThread[],
  scrollContainerEl: HTMLElement | null,
): Set<string> {
  const [visibleIds, setVisibleIds] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    const container = scrollContainerEl;
    if (!container) return;

    let raf = 0;
    const compute = () => {
      raf = 0;
      const rect = container.getBoundingClientRect();
      const next = new Set<string>();
      for (const t of threads) {
        const el = document.getElementById(`comment-${t.id}`);
        if (!el) continue;
        const r = el.getBoundingClientRect();
        if (r.bottom > rect.top && r.top < rect.bottom) next.add(t.id);
      }
      setVisibleIds((prev) => (sameIdSet(prev, next) ? prev : next));
    };
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(compute);
    };

    compute();
    container.addEventListener('scroll', schedule, { passive: true });
    const ro = new ResizeObserver(schedule);
    ro.observe(container);
    if (container.firstElementChild) ro.observe(container.firstElementChild);
    return () => {
      container.removeEventListener('scroll', schedule);
      ro.disconnect();
      if (raf) cancelAnimationFrame(raf);
    };
  }, [threads, scrollContainerEl]);

  return visibleIds;
}

interface PreviewAnchor {
  index: number;
  y: number;
}

function MinimapTick({
  label,
  inViewport,
  isPreviewOpen,
  onClick,
}: {
  label: string;
  inViewport: boolean;
  isPreviewOpen: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      className="group/tick flex min-h-[5px] w-5 flex-[0_1_0.875rem] cursor-pointer items-center justify-end focus-visible:outline-none"
    >
      <span
        className={cn(
          'h-0.5 w-3 origin-right rounded-full transition-[scale,background-color] duration-100 ease-out',
          inViewport ? 'bg-foreground/70' : 'bg-muted-foreground/30',
          'group-hover/tick:bg-foreground',
          isPreviewOpen && 'scale-x-[1.7] bg-foreground',
          'group-focus-visible/tick:scale-x-[1.7] group-focus-visible/tick:bg-foreground',
          'motion-reduce:group-hover/tick:scale-x-[1.7]',
        )}
      />
    </button>
  );
}

export function ThreadMinimap({
  threads,
  scrollContainerEl,
  onJump,
  className,
}: ThreadMinimapProps) {
  const { t } = useT('issues');
  const { getActorName } = useActorName();
  const visibleIds = useVisibleThreadIds(threads, scrollContainerEl);

  const prevPreviewsRef = useRef<
    Map<string, { content: string | undefined; preview: { title: string; body: string } }>
  >(new Map());
  const previews = useMemo(() => {
    const next = new Map<
      string,
      { content: string | undefined; preview: { title: string; body: string } }
    >();
    const arr = threads.map((th) => {
      const cached = prevPreviewsRef.current.get(th.id);
      const preview =
        cached && cached.content === th.entry.content
          ? cached.preview
          : commentPreview(th.entry.content ?? '');
      next.set(th.id, { content: th.entry.content, preview });
      return preview;
    });
    prevPreviewsRef.current = next;
    return arr;
  }, [threads]);

  const shimRef = useRef<HTMLDivElement | null>(null);
  const navRef = useRef<HTMLElement | null>(null);
  const cardRef = useRef<HTMLDivElement | null>(null);

  const waveRafRef = useRef(0);
  const pointerYRef = useRef<number | null>(null);
  const reducedMotionRef = useRef(false);

  const [preview, setPreview] = useState<PreviewAnchor | null>(null);
  const previewRef = useRef<PreviewAnchor | null>(null);
  const pendingAnchorRef = useRef<PreviewAnchor | null>(null);
  const openTimerRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const showPreview = useCallback((anchor: PreviewAnchor | null) => {
    previewRef.current = anchor;
    setPreview((prev) => (prev?.index === anchor?.index && prev?.y === anchor?.y ? prev : anchor));
  }, []);

  useEffect(() => {
    reducedMotionRef.current = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    return () => {
      if (waveRafRef.current) cancelAnimationFrame(waveRafRef.current);
      if (openTimerRef.current !== null) window.clearTimeout(openTimerRef.current);
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    };
  }, []);

  const cancelClose = useCallback(() => {
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  }, []);
  const scheduleClose = useCallback(() => {
    cancelClose();
    if (openTimerRef.current !== null) {
      window.clearTimeout(openTimerRef.current);
      openTimerRef.current = null;
    }
    closeTimerRef.current = window.setTimeout(() => {
      closeTimerRef.current = null;
      showPreview(null);
    }, PREVIEW_CLOSE_DELAY_MS);
  }, [cancelClose, showPreview]);

  const runWave = useCallback(() => {
    waveRafRef.current = 0;
    const nav = navRef.current;
    const shim = shimRef.current;
    if (!nav || !shim) return;
    const y = pointerYRef.current;
    const buttons = nav.querySelectorAll<HTMLButtonElement>('button');
    const shimTop = shim.getBoundingClientRect().top;
    const shimHeight = shim.clientHeight;
    const cardHalf = (cardRef.current?.offsetHeight ?? 96) / 2;
    const scales: string[] = [];
    let nearest: { index: number; centerY: number; dist: number } | null = null;
    buttons.forEach((b, i) => {
      if (y === null) {
        scales.push('');
        return;
      }
      const r = b.getBoundingClientRect();
      const centerY = r.top + r.height / 2;
      const dist = Math.abs(y - centerY);
      const s = reducedMotionRef.current ? 1 : waveScale(y - centerY);
      scales.push(s > 1.001 ? `${s.toFixed(3)} 1` : '');
      if (!nearest || dist < nearest.dist) nearest = { index: i, centerY, dist };
    });
    buttons.forEach((b, i) => {
      const tick = b.firstElementChild as HTMLElement | null;
      if (!tick) return;
      const s = scales[i]!;
      if (s) tick.style.setProperty('scale', s);
      else tick.style.removeProperty('scale');
    });

    if (y === null || !nearest) return;
    const { index, centerY } = nearest as { index: number; centerY: number };
    const anchor: PreviewAnchor = {
      index,
      y: Math.min(Math.max(centerY - shimTop, cardHalf + 6), shimHeight - cardHalf - 6),
    };
    pendingAnchorRef.current = anchor;
    if (previewRef.current) {
      showPreview(anchor);
    } else if (openTimerRef.current === null) {
      openTimerRef.current = window.setTimeout(() => {
        openTimerRef.current = null;
        if (pointerYRef.current !== null) showPreview(pendingAnchorRef.current);
      }, PREVIEW_OPEN_DELAY_MS);
    }
  }, [showPreview]);
  const scheduleWave = useCallback(() => {
    if (!waveRafRef.current) waveRafRef.current = requestAnimationFrame(runWave);
  }, [runWave]);
  const handleWaveMove = useCallback(
    (e: React.PointerEvent) => {
      cancelClose();
      pointerYRef.current = e.clientY;
      scheduleWave();
    },
    [cancelClose, scheduleWave],
  );
  const handleWaveLeave = useCallback(() => {
    pointerYRef.current = null;
    scheduleWave();
    scheduleClose();
  }, [scheduleWave, scheduleClose]);

  const handleFocus = useCallback(
    (e: React.FocusEvent) => {
      const nav = navRef.current;
      const shim = shimRef.current;
      const btn = (e.target as HTMLElement).closest('button');
      if (!nav || !shim || !btn) return;
      cancelClose();
      const buttons = [...nav.querySelectorAll<HTMLButtonElement>('button')];
      const index = buttons.indexOf(btn as HTMLButtonElement);
      if (index < 0) return;
      const r = btn.getBoundingClientRect();
      const cardHalf = (cardRef.current?.offsetHeight ?? 96) / 2;
      const y = r.top + r.height / 2 - shim.getBoundingClientRect().top;
      showPreview({
        index,
        y: Math.min(Math.max(y, cardHalf + 6), shim.clientHeight - cardHalf - 6),
      });
    },
    [cancelClose, showPreview],
  );

  if (threads.length < MIN_THREADS) return null;

  const activeThread = preview ? threads[preview.index] : undefined;
  const activePreview = preview ? previews[preview.index] : undefined;
  const activeTitle =
    activeThread && activePreview
      ? activePreview.title ||
        getActorName(activeThread.entry.actor_type, activeThread.entry.actor_id)
      : undefined;

  return (
    <div
      ref={shimRef}
      className={cn('pointer-events-none z-10 flex flex-col justify-center py-6', className)}
    >
      <nav
        ref={navRef}
        aria-label={t(($) => $.detail.thread_nav_label)}
        onPointerMove={handleWaveMove}
        onPointerLeave={handleWaveLeave}
        onFocusCapture={handleFocus}
        onBlurCapture={scheduleClose}
        className="pointer-events-auto flex max-h-full flex-col overflow-hidden"
      >
        {threads.map((thread, i) => (
          <MinimapTick
            key={thread.id}
            label={
              previews[i]!.title || getActorName(thread.entry.actor_type, thread.entry.actor_id)
            }
            inViewport={visibleIds.has(thread.id)}
            isPreviewOpen={preview?.index === i}
            onClick={() => onJump(thread.id)}
          />
        ))}
      </nav>

      {/* The rail's single preview card. Mounted without an enter animation
          (the open-intent delay already gates accidental flashes; once the
          user waited, showing content instantly is the responsive choice)
          and slid between ticks with a short transform transition. Hovering
          the card keeps it open so its text stays selectable. It opens
          inward (leftward, over the content) — the only direction with room
          next to the scrollbar. */}
      {preview && activeThread && activePreview && (
        <div
          ref={cardRef}
          onPointerEnter={cancelClose}
          onPointerLeave={scheduleClose}
          className="pointer-events-auto absolute right-8 top-0 w-72 rounded-lg bg-popover p-2.5 text-sm text-popover-foreground shadow-md ring-1 ring-foreground/10 transition-transform duration-150 ease-out motion-reduce:transition-none"
          style={{ transform: `translateY(${preview.y}px) translateY(-50%)` }}
        >
          <p className="truncate text-sm font-semibold text-foreground">{activeTitle}</p>
          {activePreview.body && (
            <p className="mt-1 line-clamp-3 text-sm text-muted-foreground">{activePreview.body}</p>
          )}
        </div>
      )}
    </div>
  );
}

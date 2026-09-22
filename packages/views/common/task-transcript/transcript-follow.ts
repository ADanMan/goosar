// Защёлка живого следования за новейшими записями транскрипта.

export const FOLLOW_EDGE_THRESHOLD = 120;

const INPUT_INTENT_WINDOW_MS = 300;

export const LINE_SCROLL_PX = 40;

export interface NewestFirstFollow {
  setActive(active: boolean): void;
  reset(): void;
  isFollowing(): boolean;
  disengage(): void;
  input(delta: number): void;
  pointerDown(onScroller: boolean): void;
  pointerUp(): void;
  onAtTopChange(atTop: boolean): void;
  onScroll(scrollTop: number): boolean;
}

export function createNewestFirstFollow(now: () => number = () => Date.now()): NewestFirstFollow {
  let active = false;
  let following = true;
  let pendingAway = 0;
  let lastInputAt = -INPUT_INTENT_WINDOW_MS;
  let mouseHeld = false;
  let scrollbarDrag = false;

  const userControlsViewport = () =>
    mouseHeld || scrollbarDrag || now() - lastInputAt < INPUT_INTENT_WINDOW_MS;

  return {
    setActive(a: boolean) {
      active = a;
    },
    reset() {
      following = true;
      pendingAway = 0;
      mouseHeld = false;
      scrollbarDrag = false;
    },
    isFollowing: () => active && following,
    disengage() {
      if (active) following = false;
    },
    input(delta: number) {
      if (!active) return;
      lastInputAt = now();
      pendingAway = Math.max(0, pendingAway + delta);
      if (following && pendingAway > FOLLOW_EDGE_THRESHOLD) following = false;
    },
    pointerDown(onScroller: boolean) {
      if (!active) return;
      mouseHeld = true;
      if (onScroller) scrollbarDrag = true;
    },
    pointerUp() {
      mouseHeld = false;
      scrollbarDrag = false;
    },
    onAtTopChange(atTop: boolean) {
      if (!active || !atTop) return;
      following = true;
      pendingAway = 0;
    },
    onScroll(scrollTop: number): boolean {
      if (!active) return false;
      if (scrollbarDrag && scrollTop > FOLLOW_EDGE_THRESHOLD) {
        following = false;
        return false;
      }
      if (following && !userControlsViewport() && scrollTop > 0) {
        pendingAway = 0;
        return true;
      }
      return false;
    },
  };
}

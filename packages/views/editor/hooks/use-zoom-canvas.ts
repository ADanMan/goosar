'use client';

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
} from 'react';
import {
  MAX_SCALE,
  PAN_STEP_PX,
  ZOOM_STEP,
  centerTransform,
  clampTransform,
  computeFitScale,
  computeFitTransform,
  computeMinScale,
  distanceBetween,
  midpointOf,
  panBy,
  wheelZoomFactor,
  zoomByAtCenter,
  zoomToAt,
  type Point,
  type Size,
  type ZoomTransform,
} from '../utils/zoom-transform';

interface UseZoomCanvasOptions {
  content: Size | null;
}

export interface ZoomCanvasApi {
  setViewportNode: (node: HTMLDivElement | null) => void;
  transform: ZoomTransform;
  zoomPercent: number;
  canZoomIn: boolean;
  canZoomOut: boolean;
  isPanning: boolean;
  isAnimated: boolean;
  zoomIn: () => void;
  zoomOut: () => void;
  zoomToActualSize: () => void;
  fit: () => void;
  handlePointerDown: (event: ReactPointerEvent<HTMLElement>) => void;
  handlePointerMove: (event: ReactPointerEvent<HTMLElement>) => void;
  handlePointerUp: (event: ReactPointerEvent<HTMLElement>) => void;
  handleKeyDown: (event: ReactKeyboardEvent<HTMLElement>) => void;
  handleDoubleClick: (event: ReactMouseEvent<HTMLElement>) => void;
}

const IDENTITY: ZoomTransform = { scale: 1, x: 0, y: 0 };
const EMPTY_SIZE: Size = { width: 0, height: 0 };

function nearlyEqual(a: number, b: number): boolean {
  return Math.abs(a - b) < 0.5;
}

export function useZoomCanvas({ content }: UseZoomCanvasOptions): ZoomCanvasApi {
  const [viewportNode, setViewportNode] = useState<HTMLDivElement | null>(null);
  const [viewport, setViewport] = useState<Size>(EMPTY_SIZE);
  const [transform, setTransform] = useState<ZoomTransform>(IDENTITY);
  const [isPanning, setIsPanning] = useState(false);
  const [isAnimated, setIsAnimated] = useState(false);

  const hasFittedRef = useRef(false);
  const activePointersRef = useRef(new Map<number, Point>());
  const panOriginRef = useRef<{ pointer: Point; transform: ZoomTransform } | null>(null);
  const pinchOriginRef = useRef<{ distance: number; scale: number } | null>(null);

  const contentSize = content ?? EMPTY_SIZE;
  const hasContent = contentSize.width > 0 && contentSize.height > 0;
  const hasViewport = viewport.width > 0 && viewport.height > 0;
  const ready = hasContent && hasViewport;

  const stateRef = useRef({ transform, contentSize, viewport, ready });
  stateRef.current = { transform, contentSize, viewport, ready };

  useLayoutEffect(() => {
    const element = viewportNode;
    if (!element) return;

    const measure = () => {
      const width = element.offsetWidth;
      const height = element.offsetHeight;
      setViewport((previous) =>
        nearlyEqual(previous.width, width) && nearlyEqual(previous.height, height)
          ? previous
          : { width, height },
      );
    };

    measure();

    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);

    return () => observer.disconnect();
  }, [viewportNode]);

  useEffect(() => {
    if (!ready || hasFittedRef.current) return;
    hasFittedRef.current = true;
    setTransform(computeFitTransform(contentSize, viewport));
  }, [ready, contentSize, viewport]);

  const contentKey = `${contentSize.width}x${contentSize.height}`;
  const previousContentKeyRef = useRef(contentKey);
  useEffect(() => {
    if (previousContentKeyRef.current === contentKey) return;
    previousContentKeyRef.current = contentKey;
    if (!ready) return;
    setTransform(computeFitTransform(contentSize, viewport));
  }, [contentKey, ready, contentSize, viewport]);

  useEffect(() => {
    if (!ready) return;
    setTransform((current) => clampTransform(current, contentSize, viewport));
  }, [ready, contentSize, viewport]);

  const fit = useCallback(() => {
    const { contentSize: c, viewport: v, ready: r } = stateRef.current;
    if (!r) return;
    setIsAnimated(true);
    setTransform(computeFitTransform(c, v));
  }, []);

  const zoomToActualSize = useCallback(() => {
    const { contentSize: c, viewport: v, ready: r } = stateRef.current;
    if (!r) return;
    setIsAnimated(true);
    setTransform(clampTransform(centerTransform(c, v, 1), c, v));
  }, []);

  const zoomBy = useCallback((factor: number) => {
    const { transform: t, contentSize: c, viewport: v, ready: r } = stateRef.current;
    if (!r) return;
    setIsAnimated(true);
    setTransform(zoomByAtCenter(t, factor, c, v));
  }, []);

  const zoomIn = useCallback(() => zoomBy(ZOOM_STEP), [zoomBy]);
  const zoomOut = useCallback(() => zoomBy(1 / ZOOM_STEP), [zoomBy]);

  useEffect(() => {
    const element = viewportNode;
    if (!element) return;

    const onWheel = (event: WheelEvent) => {
      const { transform: t, contentSize: c, viewport: v, ready: r } = stateRef.current;
      if (!r) return;
      event.preventDefault();
      setIsAnimated(false);

      const rect = element.getBoundingClientRect();
      const anchor: Point = {
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      };
      const factor = wheelZoomFactor(event.deltaY, event.deltaMode);
      setTransform(zoomToAt(t, t.scale * factor, anchor, c, v));
    };

    element.addEventListener('wheel', onWheel, { passive: false });
    return () => element.removeEventListener('wheel', onWheel);
  }, [viewportNode]);

  const localPoint = useCallback(
    (event: { clientX: number; clientY: number }): Point => {
      const rect = viewportNode?.getBoundingClientRect();
      return {
        x: event.clientX - (rect?.left ?? 0),
        y: event.clientY - (rect?.top ?? 0),
      };
    },
    [viewportNode],
  );

  const handlePointerDown = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      if (!stateRef.current.ready) return;
      if (event.pointerType === 'mouse' && event.button !== 0) return;

      setIsAnimated(false);
      const point = localPoint(event);
      activePointersRef.current.set(event.pointerId, point);
      event.currentTarget.setPointerCapture?.(event.pointerId);

      const pointers = [...activePointersRef.current.values()];
      if (pointers.length === 1) {
        panOriginRef.current = { pointer: point, transform: stateRef.current.transform };
        pinchOriginRef.current = null;
        setIsPanning(true);
        return;
      }
      if (pointers.length === 2) {
        panOriginRef.current = null;
        pinchOriginRef.current = {
          distance: distanceBetween(pointers[0]!, pointers[1]!),
          scale: stateRef.current.transform.scale,
        };
        setIsPanning(false);
      }
    },
    [localPoint],
  );

  const handlePointerMove = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      if (!activePointersRef.current.has(event.pointerId)) return;
      const { contentSize: c, viewport: v, ready: r } = stateRef.current;
      if (!r) return;

      const point = localPoint(event);
      activePointersRef.current.set(event.pointerId, point);
      const pointers = [...activePointersRef.current.values()];

      if (pointers.length >= 2) {
        const pinchOrigin = pinchOriginRef.current;
        if (!pinchOrigin || pinchOrigin.distance <= 0) return;
        const [first, second] = pointers as [Point, Point];
        const nextScale =
          pinchOrigin.scale * (distanceBetween(first, second) / pinchOrigin.distance);
        setTransform((current) => zoomToAt(current, nextScale, midpointOf(first, second), c, v));
        return;
      }

      const panOrigin = panOriginRef.current;
      if (!panOrigin) return;
      setTransform(
        clampTransform(
          {
            scale: panOrigin.transform.scale,
            x: panOrigin.transform.x + (point.x - panOrigin.pointer.x),
            y: panOrigin.transform.y + (point.y - panOrigin.pointer.y),
          },
          c,
          v,
        ),
      );
    },
    [localPoint],
  );

  const handlePointerUp = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    activePointersRef.current.delete(event.pointerId);
    event.currentTarget.releasePointerCapture?.(event.pointerId);

    const pointers = [...activePointersRef.current.values()];
    if (pointers.length === 1) {
      panOriginRef.current = {
        pointer: pointers[0]!,
        transform: stateRef.current.transform,
      };
      pinchOriginRef.current = null;
      setIsPanning(true);
      return;
    }
    if (pointers.length === 0) {
      panOriginRef.current = null;
      pinchOriginRef.current = null;
      setIsPanning(false);
    }
  }, []);

  const handleKeyDown = useCallback(
    (event: ReactKeyboardEvent<HTMLElement>) => {
      const { contentSize: c, viewport: v, ready: r } = stateRef.current;
      if (!r) return;

      switch (event.key) {
        case '+':
        case '=':
          event.preventDefault();
          zoomIn();
          return;
        case '-':
        case '_':
          event.preventDefault();
          zoomOut();
          return;
        case '0':
          event.preventDefault();
          fit();
          return;
        case 'ArrowLeft':
        case 'ArrowRight':
        case 'ArrowUp':
        case 'ArrowDown': {
          event.preventDefault();
          setIsAnimated(true);
          const step = event.shiftKey ? PAN_STEP_PX * 3 : PAN_STEP_PX;
          const deltaX = event.key === 'ArrowLeft' ? step : event.key === 'ArrowRight' ? -step : 0;
          const deltaY = event.key === 'ArrowUp' ? step : event.key === 'ArrowDown' ? -step : 0;
          setTransform((current) => panBy(current, deltaX, deltaY, c, v));
          return;
        }
        default:
      }
    },
    [zoomIn, zoomOut, fit],
  );

  const handleDoubleClick = useCallback(
    (event: ReactMouseEvent<HTMLElement>) => {
      const { transform: t, contentSize: c, viewport: v, ready: r } = stateRef.current;
      if (!r) return;

      setIsAnimated(true);
      const fitScale = computeFitScale(c, v);
      if (Math.abs(t.scale - fitScale) >= 0.001) {
        setTransform(computeFitTransform(c, v));
        return;
      }
      setTransform(zoomToAt(t, fitScale < 0.999 ? 1 : 2, localPoint(event), c, v));
    },
    [localPoint],
  );

  return {
    setViewportNode,
    transform,
    zoomPercent: Math.round(transform.scale * 100),
    canZoomIn: transform.scale < MAX_SCALE - 0.001,
    canZoomOut: transform.scale > computeMinScale(contentSize, viewport) + 0.001,
    isPanning,
    isAnimated,
    zoomIn,
    zoomOut,
    zoomToActualSize,
    fit,
    handlePointerDown,
    handlePointerMove,
    handlePointerUp,
    handleKeyDown,
    handleDoubleClick,
  };
}

export type { Size, ZoomTransform };
export { MAX_SCALE };

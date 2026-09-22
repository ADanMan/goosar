// Математика пан/зум для общего холста: диаграммы Mermaid и превью изображений.

export interface Size {
  width: number;
  height: number;
}

export interface Point {
  x: number;
  y: number;
}

export interface ZoomTransform {
  scale: number;
  x: number;
  y: number;
}

export const MIN_SCALE = 0.25;
export const MAX_SCALE = 4;

const MIN_VISIBLE_PX = 48;

export const ZOOM_STEP = 1.2;

export const PAN_STEP_PX = 48;

export function clampScale(scale: number, minScale: number = MIN_SCALE): number {
  if (!Number.isFinite(scale)) return 1;
  return Math.min(MAX_SCALE, Math.max(minScale, scale));
}

function hasArea(size: Size): boolean {
  return (
    Number.isFinite(size.width) && Number.isFinite(size.height) && size.width > 0 && size.height > 0
  );
}

export function computeFitScale(content: Size, viewport: Size): number {
  if (!hasArea(content) || !hasArea(viewport)) return 1;

  return Math.min(
    MAX_SCALE,
    Math.min(1, viewport.width / content.width, viewport.height / content.height),
  );
}

export function computeMinScale(content: Size, viewport: Size): number {
  return Math.min(MIN_SCALE, computeFitScale(content, viewport));
}

export function centerTransform(content: Size, viewport: Size, scale: number): ZoomTransform {
  return {
    scale,
    x: (viewport.width - content.width * scale) / 2,
    y: (viewport.height - content.height * scale) / 2,
  };
}

export function computeFitTransform(content: Size, viewport: Size): ZoomTransform {
  return centerTransform(content, viewport, computeFitScale(content, viewport));
}

export function clampTransform(
  transform: ZoomTransform,
  content: Size,
  viewport: Size,
): ZoomTransform {
  if (!hasArea(content) || !hasArea(viewport)) return transform;

  const scale = clampScale(transform.scale, computeMinScale(content, viewport));
  const scaledWidth = content.width * scale;
  const scaledHeight = content.height * scale;
  const marginX = Math.min(MIN_VISIBLE_PX, scaledWidth);
  const marginY = Math.min(MIN_VISIBLE_PX, scaledHeight);

  return {
    scale,
    x: Math.min(Math.max(transform.x, marginX - scaledWidth), viewport.width - marginX),
    y: Math.min(Math.max(transform.y, marginY - scaledHeight), viewport.height - marginY),
  };
}

export function zoomToAt(
  transform: ZoomTransform,
  nextScale: number,
  anchor: Point,
  content: Size,
  viewport: Size,
): ZoomTransform {
  const scale = clampScale(nextScale, computeMinScale(content, viewport));
  if (scale === transform.scale) return transform;

  const ratio = scale / transform.scale;

  return clampTransform(
    {
      scale,
      x: anchor.x - (anchor.x - transform.x) * ratio,
      y: anchor.y - (anchor.y - transform.y) * ratio,
    },
    content,
    viewport,
  );
}

export function zoomByAt(
  transform: ZoomTransform,
  factor: number,
  anchor: Point,
  content: Size,
  viewport: Size,
): ZoomTransform {
  return zoomToAt(transform, transform.scale * factor, anchor, content, viewport);
}

export function zoomByAtCenter(
  transform: ZoomTransform,
  factor: number,
  content: Size,
  viewport: Size,
): ZoomTransform {
  return zoomByAt(
    transform,
    factor,
    { x: viewport.width / 2, y: viewport.height / 2 },
    content,
    viewport,
  );
}

export function panBy(
  transform: ZoomTransform,
  deltaX: number,
  deltaY: number,
  content: Size,
  viewport: Size,
): ZoomTransform {
  return clampTransform(
    { scale: transform.scale, x: transform.x + deltaX, y: transform.y + deltaY },
    content,
    viewport,
  );
}

export function distanceBetween(a: Point, b: Point): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

export function midpointOf(a: Point, b: Point): Point {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
}

export function wheelZoomFactor(deltaY: number, deltaMode: number): number {
  const pixels = deltaMode === 1 ? deltaY * 16 : deltaMode === 2 ? deltaY * 100 : deltaY;
  return Math.exp(-pixels / 400);
}

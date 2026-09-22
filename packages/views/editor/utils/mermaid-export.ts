// Экспорт диаграмм Mermaid (.mmd, .svg, .png) из SVG-разметки в хост-документе,
// не через preview-iframe.

const SVG_NAMESPACE = 'http://www.w3.org/2000/svg';

const PNG_PIXEL_RATIO = 2;

export interface ExportSvgOptions {
  background: string;
  fontFamily: string;
  width: number;
  height: number;
}

interface ViewBox {
  minX: number;
  minY: number;
  width: number;
  height: number;
}

function readViewBox(root: SVGElement, fallback: Size): ViewBox {
  const parts = (root.getAttribute('viewBox') ?? '')
    .split(/[\s,]+/)
    .map((value) => Number.parseFloat(value));

  if (parts.length === 4 && parts.every((value) => Number.isFinite(value))) {
    const [minX, minY, width, height] = parts as [number, number, number, number];
    if (width > 0 && height > 0) return { minX, minY, width, height };
  }

  return { minX: 0, minY: 0, width: fallback.width, height: fallback.height };
}

interface Size {
  width: number;
  height: number;
}

function mergeStyleAttribute(
  existing: string | null,
  overrides: Record<string, string>,
  remove: string[],
): string {
  const declarations = new Map<string, string>();

  for (const part of (existing ?? '').split(';')) {
    const separator = part.indexOf(':');
    if (separator === -1) continue;
    const property = part.slice(0, separator).trim().toLowerCase();
    if (property) declarations.set(property, part.slice(separator + 1).trim());
  }

  for (const property of remove) declarations.delete(property);
  for (const [property, value] of Object.entries(overrides)) {
    declarations.set(property, value);
  }

  return [...declarations].map(([property, value]) => `${property}: ${value}`).join('; ');
}

export function buildExportSvg(
  svgMarkup: string,
  { background, fontFamily, width, height }: ExportSvgOptions,
): string | null {
  const parsed = new DOMParser().parseFromString(svgMarkup, 'image/svg+xml');
  if (parsed.querySelector('parsererror')) return null;

  const root = parsed.documentElement as unknown as SVGElement;
  if (root.nodeName.toLowerCase() !== 'svg') return null;

  root.setAttribute('xmlns', SVG_NAMESPACE);
  root.setAttribute('width', String(width));
  root.setAttribute('height', String(height));
  root.setAttribute(
    'style',
    mergeStyleAttribute(
      root.getAttribute('style'),
      { 'background-color': background, 'font-family': fontFamily },
      ['max-width'],
    ),
  );

  const viewBox = readViewBox(root, { width, height });
  const backgroundRect = parsed.createElementNS(SVG_NAMESPACE, 'rect');
  backgroundRect.setAttribute('x', String(viewBox.minX));
  backgroundRect.setAttribute('y', String(viewBox.minY));
  backgroundRect.setAttribute('width', String(viewBox.width));
  backgroundRect.setAttribute('height', String(viewBox.height));
  backgroundRect.setAttribute('fill', background);
  root.insertBefore(backgroundRect, root.firstChild);

  return new XMLSerializer().serializeToString(root);
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error('Failed to load SVG for PNG export'));
    image.src = url;
  });
}

export async function renderSvgToPngBlob(
  standaloneSvg: string,
  { width, height }: Size,
  pixelRatio: number = PNG_PIXEL_RATIO,
): Promise<Blob> {
  const svgBlob = new Blob([standaloneSvg], { type: 'image/svg+xml;charset=utf-8' });
  const url = URL.createObjectURL(svgBlob);

  try {
    const image = await loadImage(url);
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(width * pixelRatio));
    canvas.height = Math.max(1, Math.round(height * pixelRatio));

    const context = canvas.getContext('2d');
    if (!context) throw new Error('Canvas 2D context unavailable for PNG export');
    context.drawImage(image, 0, 0, canvas.width, canvas.height);

    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'));
    if (!blob) throw new Error('Failed to encode PNG');

    return blob;
  } finally {
    URL.revokeObjectURL(url);
  }
}

export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.rel = 'noopener';
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

export function diagramFilenameStem(chart: string): string {
  const firstLine = chart
    .split('\n')
    .map((line) => line.trim())
    .find((line) => line.length > 0 && !line.startsWith('%%'));

  const slug = (firstLine ?? '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
    .replace(/-+$/g, '');

  return slug || 'diagram';
}

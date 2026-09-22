'use client';

import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react';
import { Check, Copy, Maximize2 } from 'lucide-react';
import { copyText } from '@goosar/ui/lib/clipboard';
import { useT } from '../i18n';
import { useDragToScroll } from './hooks/use-drag-to-scroll';
import { MermaidViewer } from './mermaid-viewer';
import type { Size } from './utils/zoom-transform';

type MermaidAPI = typeof import('mermaid').default;

let mermaidPromise: Promise<MermaidAPI> | null = null;

function getMermaid(): Promise<MermaidAPI> {
  mermaidPromise ??= import('mermaid').then(({ default: mermaid }) => mermaid);

  return mermaidPromise;
}

function toLegacyColor(color: string, fallback: string, ownerDocument: Document): string {
  const canvas = ownerDocument.createElement('canvas');
  canvas.width = 1;
  canvas.height = 1;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (!context) return fallback;

  context.fillStyle = '#000';
  context.fillStyle = color || fallback;
  context.fillRect(0, 0, 1, 1);
  const [red, green, blue] = context.getImageData(0, 0, 1, 1).data;

  return `rgb(${red}, ${green}, ${blue})`;
}

function resolveCssColor(host: HTMLElement, variableName: string, fallback: string): string {
  const probe = host.ownerDocument.createElement('span');
  probe.style.color = `var(${variableName})`;
  probe.style.display = 'none';
  host.appendChild(probe);
  const color = getComputedStyle(probe).color;
  probe.remove();

  return toLegacyColor(color || fallback, fallback, host.ownerDocument);
}

const FALLBACK_BACKGROUND = 'rgb(255, 255, 255)';

function getMermaidThemeVariables(host: HTMLElement | null) {
  if (!host) {
    return {
      primaryColor: 'rgb(245, 245, 245)',
      primaryBorderColor: 'rgb(59, 130, 246)',
      primaryTextColor: 'rgb(17, 24, 39)',
      lineColor: 'rgb(107, 114, 128)',
      fontFamily: 'inherit',
    };
  }

  return {
    primaryColor: resolveCssColor(host, '--muted', 'rgb(245, 245, 245)'),
    primaryBorderColor: resolveCssColor(host, '--primary', 'rgb(59, 130, 246)'),
    primaryTextColor: resolveCssColor(host, '--foreground', 'rgb(17, 24, 39)'),
    lineColor: resolveCssColor(host, '--muted-foreground', 'rgb(107, 114, 128)'),
    fontFamily: 'inherit',
  };
}

function getSandboxCssVariables(host: HTMLElement | null): string {
  const styles = host ? getComputedStyle(host) : null;
  return ['--muted', '--primary', '--foreground', '--muted-foreground']
    .map((name) => `${name}: ${styles?.getPropertyValue(name).trim() || 'initial'};`)
    .join(' ');
}

function getExportStyle(host: HTMLElement | null): {
  background: string;
  fontFamily: string;
} {
  if (!host) {
    return { background: FALLBACK_BACKGROUND, fontFamily: 'sans-serif' };
  }

  return {
    background: resolveCssColor(host, '--muted', FALLBACK_BACKGROUND),
    fontFamily: getComputedStyle(host).fontFamily || 'sans-serif',
  };
}

function getMermaidLayout(svg: string): Size | null {
  const viewBoxMatch = svg.match(
    /viewBox=["']\s*([\d.-]+)\s+([\d.-]+)\s+([\d.-]+)\s+([\d.-]+)\s*["']/i,
  );
  const [, , , widthValue, heightValue] = viewBoxMatch ?? [];
  const width = widthValue ? Number.parseFloat(widthValue) : undefined;
  const height = heightValue ? Number.parseFloat(heightValue) : undefined;

  if (width && height && width > 0 && height > 0) {
    return {
      width: Math.ceil(width),
      height: Math.ceil(height),
    };
  }

  return null;
}

export const MERMAID_SKELETON_HEIGHT_PX = 280;
const MERMAID_LAYOUT_CACHE_PREFIX = 'goosar:mermaid:layout:';

function hashChart(chart: string): string {
  let hash = 5381;
  for (let i = 0; i < chart.length; i++) {
    hash = ((hash << 5) + hash) ^ chart.charCodeAt(i);
  }
  return (hash >>> 0).toString(36);
}

export function reservedMermaidHeightPx(chart: string): number {
  return readCachedLayout(chart)?.height ?? MERMAID_SKELETON_HEIGHT_PX;
}

function readCachedLayout(chart: string): Size | null {
  if (typeof window === 'undefined') return null;
  try {
    const raw = window.sessionStorage.getItem(MERMAID_LAYOUT_CACHE_PREFIX + hashChart(chart));
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (
      typeof parsed?.width === 'number' &&
      typeof parsed?.height === 'number' &&
      parsed.width > 0 &&
      parsed.height > 0
    ) {
      return { width: parsed.width, height: parsed.height };
    }
    return null;
  } catch {
    return null;
  }
}

function writeCachedLayout(chart: string, layout: Size | null): void {
  if (typeof window === 'undefined') return;
  if (!layout) return;
  try {
    window.sessionStorage.setItem(
      MERMAID_LAYOUT_CACHE_PREFIX + hashChart(chart),
      JSON.stringify({ width: layout.width, height: layout.height }),
    );
  } catch {
    // Quota exceeded or storage disabled — degrade silently; we still
    // render correctly, just without the zero-shift optimisation.
  }
}

function buildSandboxedMermaidDocument(svg: string, host: HTMLElement | null): string {
  const cssVariables = getSandboxCssVariables(host);

  return `<!doctype html><html><head><style>:root { ${cssVariables} } body { margin: 0; display: flex; justify-content: center; background: transparent; } svg { max-width: 100%; height: auto; }</style></head><body>${svg}</body></html>`;
}

function buildViewerMermaidDocument(svg: string, host: HTMLElement | null, layout: Size): string {
  const cssVariables = getSandboxCssVariables(host);

  return `<!doctype html><html><head><style>:root { ${cssVariables} } html, body { margin: 0; width: 100%; height: 100%; overflow: hidden; background: transparent; } svg { display: block; width: ${layout.width}px; height: ${layout.height}px; max-width: none; }</style></head><body>${svg}</body></html>`;
}

function useThemeVersion() {
  const [themeVersion, setThemeVersion] = useState(0);

  useEffect(() => {
    const bumpThemeVersion = () => setThemeVersion((version) => version + 1);
    const observer = new MutationObserver(bumpThemeVersion);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class', 'style', 'data-theme'],
    });
    if (document.body) {
      observer.observe(document.body, {
        attributes: true,
        attributeFilter: ['class', 'style', 'data-theme'],
      });
    }

    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    mediaQuery.addEventListener('change', bumpThemeVersion);

    return () => {
      observer.disconnect();
      mediaQuery.removeEventListener('change', bumpThemeVersion);
    };
  }, []);

  return themeVersion;
}

function useHorizontalOverflow(ref: React.RefObject<HTMLElement | null>, deps: unknown[]) {
  const [edges, setEdges] = useState<{ start: boolean; end: boolean }>({
    start: false,
    end: false,
  });

  useEffect(() => {
    const element = ref.current;
    if (!element) return;

    const measure = () => {
      const { scrollLeft, scrollWidth, clientWidth } = element;
      const maxScroll = scrollWidth - clientWidth;
      setEdges((previous) => {
        const start = scrollLeft > 1;
        const end = scrollLeft < maxScroll - 1;
        return previous.start === start && previous.end === end ? previous : { start, end };
      });
    };

    measure();
    element.addEventListener('scroll', measure, { passive: true });

    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
    observer?.observe(element);

    return () => {
      element.removeEventListener('scroll', measure);
      observer?.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ref, ...deps]);

  return edges;
}

const VIEWER_FALLBACK_LAYOUT: Size = { width: 800, height: MERMAID_SKELETON_HEIGHT_PX };

interface RenderedDiagram {
  svg: string;
  inlineDocument: string;
  viewerDocument: string;
  layout: Size | null;
  viewerLayout: Size;
  exportBackground: string;
  exportFontFamily: string;
}

export function MermaidDiagram({ chart }: { chart: string }) {
  const { t } = useT('editor');
  const reactId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const expandButtonRef = useRef<HTMLButtonElement>(null);
  const diagramId = useMemo(() => `mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, '')}`, [reactId]);
  const themeVersion = useThemeVersion();
  const [rendered, setRendered] = useState<RenderedDiagram | null>(null);
  const [skeletonLayout, setSkeletonLayout] = useState<Size | null>(() => readCachedLayout(chart));
  const [error, setError] = useState<string | null>(null);
  const [viewerOpen, setViewerOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function renderDiagram() {
      try {
        setError(null);
        setSkeletonLayout(readCachedLayout(chart));
        const mermaid = await getMermaid();
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: 'strict',
          theme: 'base',
          htmlLabels: false,
          themeVariables: getMermaidThemeVariables(containerRef.current),
          suppressErrorRendering: true,
        });
        const { svg: renderedSvg } = await mermaid.render(diagramId, chart);
        if (cancelled) return;

        const measured = getMermaidLayout(renderedSvg);
        const viewerLayout = measured ?? VIEWER_FALLBACK_LAYOUT;
        const exportStyle = getExportStyle(containerRef.current);
        writeCachedLayout(chart, measured);
        setSkeletonLayout(measured);
        setRendered({
          svg: renderedSvg,
          inlineDocument: buildSandboxedMermaidDocument(renderedSvg, containerRef.current),
          viewerDocument: buildViewerMermaidDocument(
            renderedSvg,
            containerRef.current,
            viewerLayout,
          ),
          layout: measured,
          viewerLayout,
          exportBackground: exportStyle.background,
          exportFontFamily: exportStyle.fontFamily,
        });
      } catch (err) {
        if (!cancelled) {
          setRendered(null);
          setError(err instanceof Error ? err.message : 'Failed to render Mermaid diagram');
        }
      }
    }

    void renderDiagram();

    return () => {
      cancelled = true;
    };
  }, [chart, diagramId, themeVersion]);

  const overflow = useHorizontalOverflow(scrollRef, [rendered?.inlineDocument]);
  const openViewer = useCallback(() => setViewerOpen(true), []);
  const dragToScroll = useDragToScroll({ onTap: openViewer });

  const handleCopySource = useCallback(async () => {
    if (await copyText(chart)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }, [chart]);

  if (error) {
    return (
      <div ref={containerRef} className="mermaid-diagram mermaid-diagram-error">
        <div className="mermaid-diagram-error-head">
          <p>{t(($) => $.mermaid.render_error)}</p>
          <button
            type="button"
            onClick={handleCopySource}
            title={t(($) => $.mermaid.copy_source)}
            aria-label={t(($) => $.mermaid.copy_source)}
          >
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
          </button>
        </div>
        {/* The parser message is the only clue about which line is wrong;
            without it the fallback is just an unexplained code block. */}
        <p className="mermaid-diagram-error-detail">{error}</p>
        <pre>
          <code>{chart}</code>
        </pre>
      </div>
    );
  }

  const containerStyle: CSSProperties | undefined = rendered
    ? undefined
    : { minHeight: skeletonLayout?.height ?? MERMAID_SKELETON_HEIGHT_PX };

  return (
    <div
      ref={containerRef}
      className="mermaid-diagram"
      aria-label="Mermaid diagram"
      style={containerStyle}
      data-overflow-start={overflow.start ? '' : undefined}
      data-overflow-end={overflow.end ? '' : undefined}
    >
      {rendered ? (
        <>
          {/* The scroll container is a sibling of the toolbar, not its parent:
              as an absolutely-positioned child of the scroller the toolbar used
              to slide out of view with the diagram on wide charts. */}
          {/* Tap opens the viewer, but a drag must not — see useDragToScroll.
              The gesture drives this instead of `onClick`, because a click
              fires at the end of a drag too and would reopen the viewer over
              a user who was only trying to look at the rest of a wide chart. */}
          <div ref={scrollRef} className="mermaid-diagram-scroll" {...dragToScroll}>
            <iframe
              className="mermaid-diagram-frame"
              sandbox=""
              srcDoc={rendered.inlineDocument}
              style={{
                height: rendered.layout ? `${rendered.layout.height}px` : undefined,
                width: rendered.layout ? `${rendered.layout.width}px` : undefined,
              }}
              title={t(($) => $.mermaid.frame_title)}
            />
          </div>
          <div className="mermaid-diagram-toolbar">
            <button
              type="button"
              onClick={handleCopySource}
              title={t(($) => $.mermaid.copy_source)}
              aria-label={t(($) => $.mermaid.copy_source)}
            >
              {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            </button>
            <button
              ref={expandButtonRef}
              type="button"
              onClick={() => setViewerOpen(true)}
              title={t(($) => $.mermaid.open_viewer)}
              aria-label={t(($) => $.mermaid.open_viewer)}
            >
              <Maximize2 className="size-3.5" />
            </button>
          </div>
          <MermaidViewer
            open={viewerOpen}
            onOpenChange={setViewerOpen}
            chart={chart}
            svg={rendered.svg}
            viewerDocument={rendered.viewerDocument}
            layout={rendered.viewerLayout}
            exportBackground={rendered.exportBackground}
            exportFontFamily={rendered.exportFontFamily}
            finalFocusRef={expandButtonRef}
          />
        </>
      ) : (
        <div className="mermaid-diagram-loading">{t(($) => $.mermaid.rendering)}</div>
      )}
    </div>
  );
}

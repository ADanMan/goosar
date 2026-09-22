import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { readFileSync } from 'node:fs';

vi.mock('../i18n', async () => {
  const editor = (await import('../locales/en/editor.json')).default;
  return {
    useT: () => ({ t: (select: (bundle: typeof editor) => string) => select(editor) }),
  };
});

vi.mock('./code-block-static', () => ({
  CodeBlockStatic: ({ body }: { body: string }) => (
    <pre data-testid="code-block-static">{body}</pre>
  ),
}));

const copyTextMock = vi.hoisted(() => vi.fn().mockResolvedValue(true));
vi.mock('@goosar/ui/lib/clipboard', () => ({ copyText: copyTextMock }));

const mermaidRenderMock = vi.hoisted(() => vi.fn());
const mermaidInitializeMock = vi.hoisted(() => vi.fn());
vi.mock('mermaid', () => ({
  default: { initialize: mermaidInitializeMock, render: mermaidRenderMock },
}));

const MOCK_SVG = '<svg viewBox="0 0 1000 500"><g><text>mock diagram</text></g></svg>';

import { MermaidDiagram } from './mermaid-diagram';

const CHART = 'graph LR\n  A[Start] --> B[Done]';
const VIEWPORT = { width: 800, height: 400 };

function stubViewportSize() {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    bottom: VIEWPORT.height,
    height: VIEWPORT.height,
    left: 0,
    right: VIEWPORT.width,
    top: 0,
    width: VIEWPORT.width,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  });
}

beforeEach(() => {
  stubViewportSize();
  mermaidRenderMock.mockReset();
  mermaidRenderMock.mockResolvedValue({ svg: MOCK_SVG });
  mermaidInitializeMock.mockClear();
  copyTextMock.mockClear();
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
    configurable: true,
    value: () => ({
      fillStyle: '#000',
      fillRect: vi.fn(),
      getImageData: () => ({ data: new Uint8ClampedArray([12, 34, 56, 255]) }),
    }),
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  document.documentElement.className = '';
});

function currentScale(): number {
  const element = document.querySelector<HTMLElement>('.zoom-canvas-content')!;
  return Number.parseFloat(/scale\(([\d.]+)\)/.exec(element.style.transform)![1]!);
}

async function openViewer() {
  const expand = await screen.findByRole('button', { name: 'Open diagram viewer' });
  fireEvent.click(expand);
  await screen.findByRole('application');
}

async function findScroller(): Promise<HTMLElement> {
  return waitFor(() => {
    const found = document.querySelector<HTMLElement>('.mermaid-diagram-scroll');
    expect(found).not.toBeNull();
    return found!;
  });
}

function tap(element: HTMLElement, { x, y }: { x: number; y: number }) {
  fireEvent.pointerDown(element, { pointerId: 1, clientX: x, clientY: y });
  fireEvent.pointerUp(element, { pointerId: 1, clientX: x, clientY: y });
  fireEvent.click(element, { clientX: x, clientY: y });
}

function drag(
  element: HTMLElement,
  from: { x: number; y: number },
  to: { x: number; y: number },
  { pointerType = 'mouse', button = 0 }: { pointerType?: string; button?: number } = {},
) {
  const id = { pointerId: 1, pointerType, button };
  fireEvent.pointerDown(element, { ...id, clientX: from.x, clientY: from.y });
  fireEvent.pointerMove(element, { ...id, clientX: to.x, clientY: to.y });
  fireEvent.pointerUp(element, { ...id, clientX: to.x, clientY: to.y });
  fireEvent.click(element, { clientX: to.x, clientY: to.y });
}

async function expectViewerStaysClosed() {
  await waitFor(() => {
    expect(mermaidRenderMock).toHaveBeenCalled();
  });
  expect(screen.queryByRole('application')).toBeNull();
}

describe('MermaidDiagram theme changes', () => {
  it('keeps the viewer open and preserves zoom when the theme flips', async () => {
    render(<MermaidDiagram chart={CHART} />);
    await openViewer();

    fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }));
    const zoomed = currentScale();
    expect(zoomed).toBeGreaterThan(0.8);

    await act(async () => {
      document.documentElement.classList.add('dark');
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(mermaidRenderMock.mock.calls.length).toBeGreaterThan(1);
    });

    expect(screen.getByRole('application')).toBeInTheDocument();
    expect(currentScale()).toBeCloseTo(zoomed, 5);
    // Dialog portal setup and the MutationObserver callback can be starved while
    // the full views suite shares a saturated CI runner. Keep the larger budget
    // local to this integration-style test instead of weakening the suite default.
  }, 15_000);

  it('never blanks the diagram while the themed re-render is still in flight', async () => {
    render(<MermaidDiagram chart={CHART} />);
    await waitFor(() => {
      expect(document.querySelector('.mermaid-diagram-frame')).not.toBeNull();
    });

    let releaseRender!: (value: { svg: string }) => void;
    mermaidRenderMock.mockImplementationOnce(
      () =>
        new Promise<{ svg: string }>((resolve) => {
          releaseRender = resolve;
        }),
    );

    await act(async () => {
      document.documentElement.classList.add('dark');
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(mermaidRenderMock.mock.calls.length).toBeGreaterThan(1);
    });

    expect(document.querySelector('.mermaid-diagram-frame')).not.toBeNull();
    expect(screen.queryByText('Rendering diagram…')).toBeNull();

    await act(async () => {
      releaseRender({ svg: '<svg viewBox="0 0 1000 500"><text>themed</text></svg>' });
    });
    expect(document.querySelector('.mermaid-diagram-frame')).not.toBeNull();
  });
});

describe('Mermaid selection suppression', () => {
  function blockFor(css: string, selector: string): string {
    const start = css.indexOf(selector);
    expect(start, `${selector} missing from stylesheet`).toBeGreaterThan(-1);
    return css.slice(start, css.indexOf('}', start));
  }

  it('stops a drag on the inline diagram from selecting text', () => {
    const mermaidCss = readFileSync('editor/styles/mermaid.css', 'utf8');

    expect(blockFor(mermaidCss, '.mermaid-diagram-scroll {')).toContain('user-select: none');
  });

  it('stops a pan that leaves the viewer canvas from selecting text', () => {
    const zoomCss = readFileSync('editor/styles/zoom-canvas.css', 'utf8');

    expect(blockFor(zoomCss, '.zoom-canvas {')).toContain('user-select: none');
  });
});

describe('MermaidDiagram rendering config', () => {
  it('renders labels as SVG text, without which PNG export silently produces nothing', async () => {
    render(<MermaidDiagram chart={CHART} />);

    await waitFor(() => {
      expect(mermaidInitializeMock).toHaveBeenCalled();
    });

    expect(mermaidInitializeMock).toHaveBeenCalledWith(
      expect.objectContaining({ htmlLabels: false }),
    );
    expect(mermaidInitializeMock).toHaveBeenCalledWith(
      expect.objectContaining({ securityLevel: 'strict' }),
    );
  });
});

describe('MermaidDiagram inline presentation', () => {
  it('renders the diagram in an empty sandbox at its natural size', async () => {
    render(<MermaidDiagram chart={CHART} />);

    const frame = await waitFor(() => {
      const found = document.querySelector<HTMLIFrameElement>('.mermaid-diagram-frame');
      expect(found).not.toBeNull();
      return found!;
    });

    expect(frame.getAttribute('sandbox')).toBe('');
    expect(frame.style.width).toBe('1000px');
    expect(frame.style.height).toBe('500px');
  });

  it('copies the source straight from the inline toolbar', async () => {
    render(<MermaidDiagram chart={CHART} />);

    fireEvent.click(await screen.findByRole('button', { name: 'Copy diagram source' }));

    await waitFor(() => {
      expect(copyTextMock).toHaveBeenCalledWith(CHART);
    });
  });

  it('opens the viewer when the diagram itself is tapped, not just the button', async () => {
    render(<MermaidDiagram chart={CHART} />);
    const scroll = await findScroller();

    tap(scroll, { x: 100, y: 100 });

    expect(await screen.findByRole('application')).toBeInTheDocument();
  });
});

describe('MermaidDiagram inline tap vs drag', () => {
  async function setupScroller({ scrollable = true }: { scrollable?: boolean } = {}) {
    render(<MermaidDiagram chart={CHART} />);
    const scroll = await findScroller();
    let scrollLeft = 0;
    Object.defineProperty(scroll, 'scrollLeft', {
      configurable: true,
      get: () => scrollLeft,
      set: (value: number) => {
        const max = scrollable ? 2000 : 0;
        scrollLeft = Math.min(Math.max(value, 0), max);
      },
    });
    return scroll;
  }

  it('opens the viewer on a still click', async () => {
    const scroll = await setupScroller();

    tap(scroll, { x: 100, y: 100 });

    expect(await screen.findByRole('application')).toBeInTheDocument();
  });

  it('still opens the viewer when a click jitters below the threshold', async () => {
    const scroll = await setupScroller();

    drag(scroll, { x: 100, y: 100 }, { x: 102, y: 102 });

    expect(await screen.findByRole('application')).toBeInTheDocument();
  });

  it('does not open the viewer after a horizontal drag', async () => {
    const scroll = await setupScroller();

    drag(scroll, { x: 300, y: 100 }, { x: 200, y: 100 });

    await expectViewerStaysClosed();
  });

  it('pans the scroll container while dragging horizontally', async () => {
    const scroll = await setupScroller();

    fireEvent.pointerDown(scroll, { pointerId: 1, clientX: 300, clientY: 100 });
    fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 200, clientY: 100 });

    expect(scroll.scrollLeft).toBe(100);

    fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 260, clientY: 100 });
    expect(scroll.scrollLeft).toBe(40);
  });

  it('does not open the viewer after dragging a diagram that cannot scroll', async () => {
    const scroll = await setupScroller({ scrollable: false });

    drag(scroll, { x: 300, y: 100 }, { x: 200, y: 100 });

    expect(scroll.scrollLeft).toBe(0);
    await expectViewerStaysClosed();
  });

  it('does not open the viewer when the browser takes over the gesture', async () => {
    const scroll = await setupScroller();

    fireEvent.pointerDown(scroll, {
      pointerId: 1,
      pointerType: 'touch',
      clientX: 100,
      clientY: 300,
    });
    fireEvent.pointerCancel(scroll, { pointerId: 1, pointerType: 'touch' });

    await expectViewerStaysClosed();
  });

  it('leaves touch panning to the browser instead of driving scrollLeft itself', async () => {
    const scroll = await setupScroller();

    fireEvent.pointerDown(scroll, {
      pointerId: 1,
      pointerType: 'touch',
      clientX: 300,
      clientY: 100,
    });
    fireEvent.pointerMove(scroll, {
      pointerId: 1,
      pointerType: 'touch',
      clientX: 200,
      clientY: 100,
    });

    expect(scroll.scrollLeft).toBe(0);
  });

  it('pans for a pen drag, which has no native drag-to-scroll either', async () => {
    const scroll = await setupScroller();

    fireEvent.pointerDown(scroll, { pointerId: 1, pointerType: 'pen', clientX: 300, clientY: 100 });
    fireEvent.pointerMove(scroll, { pointerId: 1, pointerType: 'pen', clientX: 200, clientY: 100 });

    expect(scroll.scrollLeft).toBe(100);
  });

  it('ignores right-button drags', async () => {
    const scroll = await setupScroller();

    drag(scroll, { x: 300, y: 100 }, { x: 200, y: 100 }, { button: 2 });

    expect(scroll.scrollLeft).toBe(0);
    await expectViewerStaysClosed();
  });

  it('keeps the expand button opening the viewer regardless of the gesture rule', async () => {
    await setupScroller();

    fireEvent.click(screen.getByRole('button', { name: 'Open diagram viewer' }));

    expect(await screen.findByRole('application')).toBeInTheDocument();
  });
});

describe('MermaidDiagram error state', () => {
  it('surfaces the parser message and a copy affordance alongside the source fallback', async () => {
    mermaidRenderMock.mockRejectedValueOnce(new Error('Parse error on line 3'));

    render(<MermaidDiagram chart={CHART} />);

    await waitFor(() => {
      expect(document.querySelector('.mermaid-diagram-error')).not.toBeNull();
    });
    expect(screen.getByText('Parse error on line 3')).toBeInTheDocument();
    expect(screen.getByText('Unable to render Mermaid diagram.')).toBeInTheDocument();
    expect(document.querySelector('.mermaid-diagram-error code')?.textContent).toBe(CHART);

    fireEvent.click(screen.getByRole('button', { name: 'Copy diagram source' }));
    await waitFor(() => {
      expect(copyTextMock).toHaveBeenCalledWith(CHART);
    });
  });
});

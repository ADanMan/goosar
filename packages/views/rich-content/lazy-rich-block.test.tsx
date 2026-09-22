/**
 * Ленивая оболочка блока рядом с viewport.
 *
 * В jsdom нет IntersectionObserver, поэтому тесты подключают управляемый
 * фейковый наблюдатель: он запоминает наблюдаемые элементы и позволяет тесту
 * решать, когда блок становится видимым. Это делает отложенный рендер, защёлку
 * и резервирование размера проверяемыми, а не предполагаемыми.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { renderToString } from 'react-dom/server';
import { createRoot, hydrateRoot } from 'react-dom/client';
import { flushSync } from 'react-dom';
import { LazyRichBlock } from './lazy-rich-block';
import { RichContentScrollRootProvider } from './scroll-root';
import { resetMountedBlocks } from './mounted-block-registry';

type IOCallback = (entries: { isIntersecting: boolean }[]) => void;

interface FakeObserver {
  callback: IOCallback;
  options?: IntersectionObserverInit;
  observed: Element[];
  disconnected: boolean;
}

let observers: FakeObserver[] = [];

beforeEach(() => {
  observers = [];
  resetMountedBlocks();
  class FakeIntersectionObserver {
    private readonly self: FakeObserver;
    constructor(callback: IOCallback, options?: IntersectionObserverInit) {
      this.self = { callback, options, observed: [], disconnected: false };
      observers.push(this.self);
    }
    observe(el: Element) {
      this.self.observed.push(el);
    }
    disconnect() {
      this.self.disconnected = true;
    }
    unobserve() {}
    takeRecords() {
      return [];
    }
  }
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function enterViewport() {
  act(() => {
    for (const o of observers) o.callback([{ isIntersecting: true }]);
  });
}

const Expensive = () => <div data-testid="expensive">diagram</div>;

describe('LazyRichBlock', () => {
  it('does not mount its child until the block is near the viewport', () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    expect(screen.queryByTestId('expensive')).toBeNull();
    expect(observers).toHaveLength(1);
    expect(observers[0]?.observed).toHaveLength(1);
  });

  it('mounts the child once it becomes near-viewport', () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );
    expect(screen.queryByTestId('expensive')).toBeNull();

    enterViewport();

    expect(screen.getByTestId('expensive')).toBeInTheDocument();
  });

  it('stops observing after mounting so the block never unmounts', () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );
    enterViewport();

    expect(observers[0]?.disconnected).toBe(true);

    act(() => {
      observers[0]?.callback([{ isIntersecting: false }]);
    });
    expect(screen.getByTestId('expensive')).toBeInTheDocument();
  });

  it('reserves the same height before and after mount', () => {
    const { container } = render(
      <LazyRichBlock reservedHeightPx={480}>
        <Expensive />
      </LazyRichBlock>,
    );
    const shell = container.querySelector('[data-rich-block-shell]') as HTMLElement;

    expect(shell.style.minHeight).toBe('480px');
    expect(shell.hasAttribute('data-mounted')).toBe(false);

    enterViewport();

    expect(shell.style.minHeight).toBe('480px');
    expect(shell.hasAttribute('data-mounted')).toBe(true);
  });

  it('watches an area larger than the viewport so blocks are ready on arrival', () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    const margin = observers[0]?.options?.rootMargin ?? '';
    const topPx = Number.parseInt(margin.split(' ')[0] ?? '0', 10);
    expect(topPx).toBeGreaterThan(600);
  });

  it("observes against the surface's scroll root when one is provided", () => {
    const scrollRoot = document.createElement('div');
    document.body.appendChild(scrollRoot);

    render(
      <RichContentScrollRootProvider scrollRoot={scrollRoot}>
        <LazyRichBlock reservedHeightPx={280}>
          <Expensive />
        </LazyRichBlock>
      </RichContentScrollRootProvider>,
    );

    expect(observers[0]?.options?.root).toBe(scrollRoot);
    scrollRoot.remove();
  });

  it('falls back to the viewport root for page-scrolled surfaces', () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    expect(observers[0]?.options?.root ?? null).toBeNull();
  });

  it('mounts via an effect when IntersectionObserver is unavailable', () => {
    vi.unstubAllGlobals();
    vi.stubGlobal('IntersectionObserver', undefined);

    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    expect(screen.getByTestId('expensive')).toBeInTheDocument();
  });
});

describe('LazyRichBlock across row recycling', () => {
  const SOURCE = 'flowchart LR\n  A --> B';

  beforeEach(() => {
    resetMountedBlocks();
  });

  it('re-mounts a recycled block immediately, without waiting to be seen again', () => {
    const first = render(
      <LazyRichBlock reservedHeightPx={280} sourceKey={SOURCE}>
        <Expensive />
      </LazyRichBlock>,
    );
    enterViewport();
    expect(first.getByTestId('expensive')).toBeInTheDocument();

    first.unmount();
    observers = [];

    const second = render(
      <LazyRichBlock reservedHeightPx={280} sourceKey={SOURCE}>
        <Expensive />
      </LazyRichBlock>,
    );

    expect(second.getByTestId('expensive')).toBeInTheDocument();
  });

  it('does not resurrect a different block that was never mounted', () => {
    const first = render(
      <LazyRichBlock reservedHeightPx={280} sourceKey={SOURCE}>
        <Expensive />
      </LazyRichBlock>,
    );
    enterViewport();
    first.unmount();
    observers = [];

    const other = render(
      <LazyRichBlock reservedHeightPx={280} sourceKey="graph TD\n  X --> Y">
        <Expensive />
      </LazyRichBlock>,
    );

    expect(other.queryByTestId('expensive')).toBeNull();
  });

  it('keeps deferring when no sourceKey is supplied', () => {
    const first = render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );
    enterViewport();
    first.unmount();
    observers = [];

    const second = render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    expect(second.queryByTestId('expensive')).toBeNull();
  });
});

describe('LazyRichBlock SSR', () => {
  function serverRender(ui: Parameters<typeof renderToString>[0]): string {
    const browserObserver = globalThis.IntersectionObserver;
    vi.stubGlobal('IntersectionObserver', undefined);
    try {
      return renderToString(ui);
    } finally {
      vi.stubGlobal('IntersectionObserver', browserObserver);
    }
  }

  it('renders the placeholder, not the block, on the server', () => {
    const html = serverRender(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    expect(html).toContain('data-rich-block-shell');
    expect(html).not.toContain('expensive');
    expect(html).not.toContain('data-mounted');
  });

  it('hydrates the server markup without a mismatch', async () => {
    const html = serverRender(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    const container = document.createElement('div');
    container.innerHTML = html;
    document.body.appendChild(container);

    const errors: string[] = [];
    const errorSpy = vi.spyOn(console, 'error').mockImplementation((...args) => {
      errors.push(args.map(String).join(' '));
    });

    let root: ReturnType<typeof hydrateRoot> | undefined;
    await act(async () => {
      root = hydrateRoot(
        container,
        <LazyRichBlock reservedHeightPx={280}>
          <Expensive />
        </LazyRichBlock>,
      );
    });

    const hydrationErrors = errors.filter((e) => /hydrat|did not match|mismatch/i.test(e));
    expect(hydrationErrors).toEqual([]);

    errorSpy.mockRestore();
    await act(async () => {
      root?.unmount();
    });
    container.remove();
  });

  it('produces the same first frame on server and client', () => {
    const serverHtml = serverRender(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    flushSync(() => {
      root.render(
        <LazyRichBlock reservedHeightPx={280}>
          <Expensive />
        </LazyRichBlock>,
      );
    });
    const clientFirstFrame = container.innerHTML;

    const normalize = (html: string) =>
      html.replace(
        /style="([^"]*)"/g,
        (_, css: string) => `style="${css.replace(/\s*;\s*$/, '').replace(/:\s+/g, ':')}"`,
      );

    expect(normalize(clientFirstFrame)).toBe(normalize(serverHtml));

    act(() => root.unmount());
    container.remove();
  });
});

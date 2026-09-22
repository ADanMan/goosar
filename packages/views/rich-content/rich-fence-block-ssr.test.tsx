/**
 * Закешированная зарезервированная высота не должна попадать в первый кадр.
 *
 * Кеш раскладки Mermaid лежит в sessionStorage, которого нет на сервере.
 * Чтение его при рендере даёт на сервере скелетон по умолчанию, а в браузере
 * с тёплым кешем — реальную закешированную высоту: разный
 * `style="min-height:…"` на том же кадре, который React гидрирует. React
 * считает это несовпадением атрибутов и НЕ чинит его сам.
 *
 * Тесты гоняют настоящий RichFenceBlock с реально предзаполненной записью в
 * sessionStorage.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act } from '@testing-library/react';
import { renderToString } from 'react-dom/server';
import { createRoot, hydrateRoot } from 'react-dom/client';
import { flushSync } from 'react-dom';

vi.mock('../i18n', async () => {
  const editor = (await import('../locales/en/editor.json')).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string) => select(editor),
    }),
    useTimeAgo: () => 'just now',
  };
});

vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn().mockResolvedValue({
      svg: '<svg viewBox="0 0 800 412"><g><text>diagram</text></g></svg>',
    }),
  },
}));

import { RichFenceBlock } from './rich-code-block';
import { resetMountedBlocks } from './mounted-block-registry';

const CHART = 'flowchart LR\n  A --> B';
const CACHED_HEIGHT = 412;
const SKELETON_HEIGHT = 280;

function cacheKey(chart: string): string {
  let hash = 5381;
  for (let i = 0; i < chart.length; i++) {
    hash = ((hash << 5) + hash) ^ chart.charCodeAt(i);
  }
  return `goosar:mermaid:layout:${(hash >>> 0).toString(36)}`;
}

beforeEach(() => {
  resetMountedBlocks();
  window.sessionStorage.clear();
  window.sessionStorage.setItem(
    cacheKey(CHART),
    JSON.stringify({ width: 800, height: CACHED_HEIGHT }),
  );
  vi.stubGlobal('IntersectionObserver', undefined);
});

afterEach(() => {
  vi.unstubAllGlobals();
  window.sessionStorage.clear();
});

function serverRender(ui: Parameters<typeof renderToString>[0]): string {
  const storage = window.sessionStorage;
  vi.stubGlobal('sessionStorage', undefined);
  try {
    return renderToString(ui);
  } finally {
    vi.stubGlobal('sessionStorage', storage);
  }
}

const block = () => <RichFenceBlock language="mermaid" body={CHART} />;

describe('RichFenceBlock reserved height', () => {
  it('reserves the skeleton height on the server, not the cached height', () => {
    const html = serverRender(block());

    expect(html).toContain(`min-height:${SKELETON_HEIGHT}px`);
    expect(html).not.toContain(`min-height:${CACHED_HEIGHT}px`);
  });

  it("uses the skeleton height on the client's first frame despite a warm cache", () => {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);

    flushSync(() => root.render(block()));

    const shell = container.querySelector('[data-rich-block-shell]') as HTMLElement;
    expect(shell.style.minHeight).toBe(`${SKELETON_HEIGHT}px`);

    act(() => root.unmount());
    container.remove();
  });

  it('hydrates a warm-cache client against server markup without a mismatch', async () => {
    const html = serverRender(block());

    const container = document.createElement('div');
    container.innerHTML = html;
    document.body.appendChild(container);

    const errors: string[] = [];
    const errorSpy = vi.spyOn(console, 'error').mockImplementation((...args) => {
      errors.push(args.map(String).join(' '));
    });

    let root: ReturnType<typeof hydrateRoot> | undefined;
    await act(async () => {
      root = hydrateRoot(container, block());
    });

    expect(errors.filter((e) => /hydrat|did not match|mismatch/i.test(e))).toEqual([]);

    errorSpy.mockRestore();
    await act(async () => root?.unmount());
    container.remove();
  });

  it('adopts the cached height after mount, so the zero-shift benefit survives', async () => {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(block());
    });

    const shell = container.querySelector('[data-rich-block-shell]') as HTMLElement;
    expect(shell.style.minHeight).toBe(`${CACHED_HEIGHT}px`);

    await act(async () => root.unmount());
    container.remove();
  });

  it('falls back to the skeleton height when the cache is cold', async () => {
    window.sessionStorage.clear();

    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(block());
    });

    const shell = container.querySelector('[data-rich-block-shell]') as HTMLElement;
    expect(shell.style.minHeight).toBe(`${SKELETON_HEIGHT}px`);

    await act(async () => root.unmount());
    container.remove();
  });
});

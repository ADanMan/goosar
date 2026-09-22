// Тесты веб-половины моста goosar:navigate: общий контент шлёт это событие,
// когда ссылка ведёт внутрь приложения.
import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest';
import { render } from '@testing-library/react';

const router = vi.hoisted(() => ({
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  prefetch: vi.fn(),
}));

vi.mock('next/navigation', () => ({
  useRouter: () => router,
  usePathname: () => '/acme/issues',
  useSearchParams: () => new URLSearchParams(),
}));

import { WebNavigationProvider } from './navigation';
import { useNavigation, type NavigationAdapter } from '@goosar/views/navigation';

function navigate(path: string) {
  window.dispatchEvent(new CustomEvent('goosar:navigate', { detail: { path } }));
}

function renderAdapter(): () => NavigationAdapter {
  let adapter: NavigationAdapter | null = null;
  function Probe() {
    adapter = useNavigation();
    return null;
  }
  render(
    <WebNavigationProvider>
      <Probe />
    </WebNavigationProvider>,
  );
  return () => adapter!;
}

beforeEach(() => {
  router.push.mockReset();
});

describe('WebNavigationProvider internal link bridge', () => {
  it('pushes the path a content link resolved to', () => {
    render(<WebNavigationProvider>{null}</WebNavigationProvider>);

    navigate('/acme/issues/MUL-1');

    expect(router.push).toHaveBeenCalledWith('/acme/issues/MUL-1');
  });

  it('ignores an event without a path', () => {
    render(<WebNavigationProvider>{null}</WebNavigationProvider>);

    window.dispatchEvent(new CustomEvent('goosar:navigate', { detail: {} }));

    expect(router.push).not.toHaveBeenCalled();
  });

  it('stops listening once unmounted', () => {
    const { unmount } = render(<WebNavigationProvider>{null}</WebNavigationProvider>);

    unmount();
    navigate('/acme/issues/MUL-1');

    expect(router.push).not.toHaveBeenCalled();
  });
});

describe('WebNavigationProvider canGoBack', () => {
  const win = window as unknown as { navigation?: unknown };

  afterEach(() => {
    delete win.navigation;
  });

  it("passes through the Navigation API's answer", () => {
    win.navigation = { canGoBack: true };

    expect(renderAdapter()().canGoBack!()).toBe(true);
  });

  it('reads the answer live rather than freezing it at render', () => {
    win.navigation = { canGoBack: true };
    const adapter = renderAdapter();

    win.navigation = { canGoBack: false };

    expect(adapter().canGoBack!()).toBe(false);
  });

  it('is unmoved by a push that committed no history entry', () => {
    win.navigation = { canGoBack: false };
    const adapter = renderAdapter();

    adapter().push('/acme/issues');

    expect(router.push).toHaveBeenCalledWith('/acme/issues');
    expect(adapter().canGoBack!()).toBe(false);
  });

  it('reports false where the browser cannot answer, so callers use the fallback', () => {
    expect(renderAdapter()().canGoBack!()).toBe(false);
  });
});

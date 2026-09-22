// @vitest-environment jsdom

import type { ReactNode } from 'react';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import enLayout from '../locales/en/layout.json';
import ruLayout from '../locales/ru/layout.json';
import jaLayout from '../locales/ja/layout.json';
import koLayout from '../locales/ko/layout.json';
import zhHansLayout from '../locales/zh-Hans/layout.json';

const h = vi.hoisted(() => ({ state: 'connected' as string }));

vi.mock('@goosar/core/realtime', () => ({
  useRealtimeConnectionState: () => h.state,
}));

vi.mock('../i18n', () => ({
  useT: () => ({
    t: (sel: (r: typeof enLayout) => string) => sel(enLayout),
  }),
}));

vi.mock('@goosar/ui/components/ui/popover', () => ({
  Popover: ({ children }: { children: ReactNode }) => <>{children}</>,
  PopoverContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PopoverTrigger: ({
    children,
    ...rest
  }: {
    children: ReactNode;
    'aria-label'?: string;
    title?: string;
  }) => (
    <button type="button" {...rest}>
      {children}
    </button>
  ),
}));

import { Sidebar, SidebarFooter, SidebarProvider } from '@goosar/ui/components/ui/sidebar';
import { RealtimeStatusIndicator } from './realtime-status';

beforeEach(() => {
  h.state = 'connected';
});

afterEach(() => {
  cleanup();
});

describe('RealtimeStatusIndicator (#257)', () => {
  it('renders nothing while the realtime connection is connected', () => {
    h.state = 'connected';
    const { container } = render(<RealtimeStatusIndicator />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing while connecting — an ordinary reconnect is not a problem to report', () => {
    h.state = 'connecting';
    const { container } = render(<RealtimeStatusIndicator />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing while unauthorized — that is an auth failure, not a delayed-updates state', () => {
    h.state = 'unauthorized';
    const { container } = render(<RealtimeStatusIndicator />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders the indicator once the realtime connection is degraded', () => {
    h.state = 'degraded';
    render(<RealtimeStatusIndicator />);
    expect(
      screen.getByRole('button', { name: enLayout.realtime.degraded.title }),
    ).toBeInTheDocument();
  });

  it('does not shadow the popover heading with a native tooltip', () => {
    h.state = 'degraded';
    render(<RealtimeStatusIndicator />);
    const trigger = screen.getByRole('button', {
      name: enLayout.realtime.degraded.title,
    });
    expect(trigger).not.toHaveAttribute('title');
  });

  it('names the consequence first and the cause second', () => {
    h.state = 'degraded';
    render(<RealtimeStatusIndicator />);
    expect(screen.getByText(enLayout.realtime.degraded.title)).toBeInTheDocument();
    expect(screen.getByText(enLayout.realtime.degraded.detail)).toBeInTheDocument();
    expect(screen.getByText(enLayout.realtime.degraded.cause)).toBeInTheDocument();
  });
});

describe('RealtimeStatusIndicator survives a hidden sidebar (#257)', () => {
  const originalInnerWidth = window.innerWidth;

  function setViewportWidth(width: number) {
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      writable: true,
      value: width,
    });
  }

  afterEach(() => {
    setViewportWidth(originalInnerWidth);
  });

  function Shell() {
    return (
      <SidebarProvider defaultOpen={false}>
        <Sidebar variant="inset">
          <SidebarFooter>
            <span>footer-only-content</span>
          </SidebarFooter>
        </Sidebar>
        <RealtimeStatusIndicator />
      </SidebarProvider>
    );
  }

  it('renders while the sidebar is a closed mobile sheet that drops its own children', () => {
    h.state = 'degraded';
    setViewportWidth(375);
    render(<Shell />);

    expect(screen.queryByText('footer-only-content')).toBeNull();
    expect(screen.getByTestId('realtime-status-indicator')).toBeInTheDocument();
  });

  it('renders outside the collapsed offcanvas sidebar element on desktop widths', () => {
    h.state = 'degraded';
    setViewportWidth(1440);
    const { container } = render(<Shell />);

    const sidebar = container.querySelector('[data-state="collapsed"]');
    expect(sidebar).not.toBeNull();
    expect(sidebar).toHaveAttribute('data-collapsible', 'offcanvas');
    const indicator = screen.getByTestId('realtime-status-indicator');
    expect(sidebar?.contains(indicator)).toBe(false);
  });
});

describe('degraded copy does not promise a working poll (#257)', () => {
  const locales = {
    en: { layout: enLayout, qualifier: 'not at all while the connection is down' },
    ru: { layout: ruLayout, qualifier: 'пока связи нет' },
    ja: { layout: jaLayout, qualifier: '接続が途絶えている間' },
    ko: { layout: koLayout, qualifier: '연결이 끊긴 동안' },
    'zh-Hans': { layout: zhHansLayout, qualifier: '连接中断期间' },
  };

  for (const [locale, { layout, qualifier }] of Object.entries(locales)) {
    it(`qualifies the consequence in ${locale}`, () => {
      expect(layout.realtime.degraded.detail).toContain(qualifier);
    });
  }

  it('renders the qualified consequence, not a bare promise', () => {
    h.state = 'degraded';
    render(<RealtimeStatusIndicator />);
    expect(screen.getByText(enLayout.realtime.degraded.detail)).toHaveTextContent(
      locales.en.qualifier,
    );
  });
});

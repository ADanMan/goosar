// @vitest-environment jsdom

import type { ReactNode } from 'react';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import enLayout from '../locales/en/layout.json';

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
  PopoverTrigger: ({ children, ...rest }: { children: ReactNode; 'aria-label'?: string }) => (
    <button type="button" {...rest}>
      {children}
    </button>
  ),
}));

vi.mock('./dashboard-guard', () => ({
  DashboardGuard: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock('./app-sidebar', () => ({ AppSidebar: () => <nav /> }));
vi.mock('./navigation-progress', () => ({ NavigationProgress: () => null }));
vi.mock('./workspace-presence-prefetch', () => ({
  WorkspacePresencePrefetch: () => null,
}));
vi.mock('./global-shortcuts', () => ({ GlobalShortcuts: () => null }));
vi.mock('../modals/registry', () => ({ ModalRegistry: () => null }));
vi.mock('../onboarding', () => ({ SourceBackfillModal: () => null }));
vi.mock('../capabilities', () => ({ MissingCredentialsBanner: () => null }));
vi.mock('../workspace/onboarding-guides-autoclose', () => ({
  OnboardingGuidesAutoClose: () => null,
}));

import { DashboardLayout } from './dashboard-layout';

beforeEach(() => {
  h.state = 'connected';
});

afterEach(() => {
  cleanup();
});

describe('DashboardLayout mounts the realtime indicator (#257)', () => {
  it('shows nothing extra while the realtime connection is healthy', () => {
    h.state = 'connected';
    render(<DashboardLayout>content</DashboardLayout>);
    expect(screen.queryByTestId('realtime-status-indicator')).toBeNull();
  });

  it('renders the degraded indicator at shell level, outside the sidebar', () => {
    h.state = 'degraded';
    render(<DashboardLayout>content</DashboardLayout>);
    const indicator = screen.getByTestId('realtime-status-indicator');
    expect(indicator).toBeInTheDocument();
    expect(indicator.closest('nav')).toBeNull();
  });
});

import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { configStore } from '@goosar/core/config';
import { NavigationProvider } from '../navigation';
import type { NavigationAdapter } from '../navigation';
import enLayout from '../locales/en/layout.json';
import { HelpLauncher } from './help-launcher';

vi.mock('../i18n', () => ({
  useT: () => ({
    t: (sel: (r: typeof enLayout) => string, vars?: Record<string, string>) => {
      const template = sel(enLayout);
      return vars
        ? template.replace(/\{\{(\w+)\}\}/g, (_, key) => String(vars[key] ?? ''))
        : template;
    },
    i18n: { language: 'en' },
  }),
}));

vi.mock('@goosar/ui/components/ui/dropdown-menu', async () => {
  const { createContext, useContext } = await import('react');
  const GroupContext = createContext(false);
  return {
    DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
    DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
    DropdownMenuItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
      <div role="menuitem" onClick={onClick}>
        {children}
      </div>
    ),
    DropdownMenuGroup: ({ children }: { children: ReactNode }) => (
      <GroupContext.Provider value={true}>{children}</GroupContext.Provider>
    ),
    DropdownMenuLabel: ({ children }: { children: ReactNode }) => {
      if (!useContext(GroupContext)) {
        throw new Error(
          'Base UI: MenuGroupRootContext is missing. Menu group parts must be used within <Menu.Group>.',
        );
      }
      return <div>{children}</div>;
    },
    DropdownMenuSeparator: () => null,
    DropdownMenuTrigger: ({ children }: { children: ReactNode }) => <>{children}</>,
  };
});

afterEach(() => {
  configStore.getState().setServerVersion('');
});

function renderHelpLauncher() {
  const push = vi.fn();
  const adapter: NavigationAdapter = {
    push,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: '/',
    searchParams: new URLSearchParams(),
    getShareableUrl: (path: string) => path,
  };
  const view = render(
    <NavigationProvider value={adapter}>
      <HelpLauncher />
    </NavigationProvider>,
  );
  return { push, view };
}

describe('HelpLauncher', () => {
  it('does not show a version row when the server omits it', () => {
    renderHelpLauncher();
    expect(screen.queryByText(/Server version/)).not.toBeInTheDocument();
  });

  it('shows the server version once /api/config resolves it', () => {
    configStore.getState().setServerVersion('1.2.3');
    renderHelpLauncher();
    expect(screen.getByText('Server version 1.2.3')).toBeInTheDocument();
  });

  it('renders the version row without a missing-group crash', () => {
    configStore.getState().setServerVersion('9.9.9');
    expect(() => renderHelpLauncher()).not.toThrow();
    expect(screen.getByText('Server version 9.9.9')).toBeInTheDocument();
  });

  it('renders the replay onboarding item', () => {
    renderHelpLauncher();
    expect(screen.getByText('Replay onboarding')).toBeInTheDocument();
  });

  it('pushes /onboarding?replay=1 when the replay item is activated', () => {
    const { push } = renderHelpLauncher();
    fireEvent.click(screen.getByText('Replay onboarding'));
    expect(push).toHaveBeenCalledTimes(1);
    expect(push).toHaveBeenCalledWith('/onboarding?replay=1');
  });
});

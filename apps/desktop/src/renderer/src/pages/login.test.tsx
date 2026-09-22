import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import enAuth from '@goosar/views/locales/en/auth.json';
import enSettings from '@goosar/views/locales/en/settings.json';
import type { PerimeterStateView } from '../../../shared/perimeter-config';

const mocks = vi.hoisted(() => {
  class MockApiError extends Error {
    readonly status: number;
    constructor(message: string, status: number) {
      super(message);
      this.name = 'ApiError';
      this.status = status;
    }
  }
  return {
    MockApiError,
    loginPageProps: null as Record<string, unknown> | null,
    openExternal: vi.fn(),
    setRuntimeConfig: vi.fn(),
    probeRuntimeServer: vi.fn(),
    onAuthError: vi.fn(() => () => {}),
    onAuthMfaToken: vi.fn((_cb: (ticket: string) => void) => () => {}),
    setPendingMfaToken: vi.fn(),
    pendingMfaToken: null as string | null,
    networkState: null as PerimeterStateView | null,
    networkStatusOptions: null as { subscribe?: boolean } | null,
  };
});

vi.mock('@goosar/views/auth', () => ({
  LoginPage: (props: Record<string, unknown>) => {
    mocks.loginPageProps = props;
    return <div data-testid="login-form">{props.extra as ReactNode}</div>;
  },
}));

vi.mock('@goosar/views/platform', () => ({
  DragStrip: () => <div data-testid="drag-strip" />,
}));

vi.mock('@goosar/ui/components/common/goosar-icon', () => ({
  GoosarIcon: () => <div data-testid="goosar-icon" />,
}));

vi.mock('../components/network-status', () => ({
  NetworkStatusPanel: () => <div data-testid="network-panel" />,
  useNetworkStatus: (options?: { subscribe?: boolean }) => ({
    state: mocks.networkState,
    busy: false,
    refresh: vi.fn(),
    recheck: vi.fn(),
    setState: vi.fn(),
    __options: (mocks.networkStatusOptions = options ?? {}),
  }),
}));

vi.mock('@goosar/views/i18n', () => ({
  useT: (namespace: string) => ({
    t: (
      selector: (resources: typeof enAuth & typeof enSettings) => string,
      values?: Record<string, string>,
    ) =>
      Object.entries(values ?? {}).reduce(
        (result, [key, value]) => result.split(`{{${key}}}`).join(value),
        selector(
          (namespace === 'settings' ? enSettings : enAuth) as typeof enAuth & typeof enSettings,
        ),
      ),
  }),
}));

vi.mock('@goosar/core/api', () => ({ ApiError: mocks.MockApiError }));

vi.mock('@goosar/core/auth', () => {
  const state = {
    get pendingMfaToken() {
      return mocks.pendingMfaToken;
    },
    setPendingMfaToken: mocks.setPendingMfaToken,
  };
  const useAuthStore = (selector?: (s: typeof state) => unknown) =>
    selector ? selector(state) : state;
  useAuthStore.getState = () => state;
  return { useAuthStore };
});

import { DesktopLoginPage } from './login';

const CONFIGURED = {
  schemaVersion: 1 as const,
  apiUrl: 'https://goosar.acme.test',
  wsUrl: 'wss://goosar.acme.test/ws',
  appUrl: 'https://goosar.acme.test',
};

function setRuntimeConfig(config = CONFIGURED) {
  Object.defineProperty(window, 'desktopAPI', {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config },
      openExternal: mocks.openExternal,
      setRuntimeConfig: mocks.setRuntimeConfig,
      probeRuntimeServer: mocks.probeRuntimeServer,
      onAuthError: mocks.onAuthError,
      onAuthMfaToken: mocks.onAuthMfaToken,
    },
  });
}

function describeError(err: unknown): string | undefined {
  const fn = mocks.loginPageProps?.describeError as
    ((e: unknown) => string | undefined) | undefined;
  return fn?.(err);
}

function reportLoginFailure(err: unknown) {
  const fn = mocks.loginPageProps?.onError as ((e: unknown) => void) | undefined;
  act(() => fn?.(err));
}

function networkStateWithGoosarRoute(route: {
  state: 'ok' | 'fail';
  reasonCode: string;
  detail?: string;
}): PerimeterStateView {
  return {
    localProxyUrl: 'http://127.0.0.1:3128',
    caBundlePresent: true,
    corpCaPresent: true,
    kerberosTicket: 'valid',
    kerberosExpiresAt: null,
    kerberosSupported: true,
    realm: null,
    live: {
      overall: 'fail',
      checkedAt: 0,
      checks: [{ id: 'route_server', ...route }],
    },
  };
}

const VIA_DEAD_PROXY = networkStateWithGoosarRoute({
  state: 'ok',
  reasonCode: 'route_proxy',
  detail: 'http://isa.corp:8080',
});
const ROUTE_UNRESOLVED = networkStateWithGoosarRoute({
  state: 'fail',
  reasonCode: 'route_unknown',
  detail: 'goosar.acme.test',
});
const ROUTE_DIRECT = networkStateWithGoosarRoute({
  state: 'ok',
  reasonCode: 'route_direct',
  detail: 'goosar.acme.test',
});

const VIA_OFFERED_PATHS = networkStateWithGoosarRoute({
  state: 'ok',
  reasonCode: 'route_proxy_list',
  detail: 'http://isa.corp:8080, http://backup.corp:8080, DIRECT',
});

const TUNNEL_DEAD = new TypeError('Failed to fetch: net::ERR_TUNNEL_CONNECTION_FAILED');

async function withNoNetwork(body: () => Promise<void>) {
  const original = Object.getOwnPropertyDescriptor(window.navigator, 'onLine');
  Object.defineProperty(window.navigator, 'onLine', {
    configurable: true,
    get: () => false,
  });
  try {
    await body();
  } finally {
    if (original) {
      Object.defineProperty(window.navigator, 'onLine', original);
    } else {
      delete (window.navigator as { onLine?: boolean }).onLine;
    }
  }
}

async function openServerEditor(address: string) {
  fireEvent.click(await screen.findByRole('button', { name: /change/i }));
  fireEvent.change(screen.getByLabelText(/server/i), {
    target: { value: address },
  });
}

describe('DesktopLoginPage', () => {
  beforeEach(() => {
    mocks.loginPageProps = null;
    mocks.networkState = null;
    mocks.networkStatusOptions = null;
    mocks.openExternal.mockReset();
    mocks.setRuntimeConfig.mockReset();
    mocks.probeRuntimeServer
      .mockReset()
      .mockResolvedValue({ ok: true, address: CONFIGURED.apiUrl });
    mocks.onAuthMfaToken.mockReset().mockReturnValue(() => {});
    mocks.setPendingMfaToken.mockReset();
    mocks.pendingMfaToken = null;
    setRuntimeConfig();
  });

  it('shows the configured server address on the login screen', async () => {
    render(<DesktopLoginPage />);

    expect(await screen.findByText(CONFIGURED.apiUrl)).toBeInTheDocument();
  });

  it('hands the shared form a second-factor ticket delivered by deep link', async () => {
    render(<DesktopLoginPage />);
    const deliver = mocks.onAuthMfaToken.mock.calls[0]![0];

    act(() => deliver('ticket-1'));

    expect(mocks.setPendingMfaToken).toHaveBeenCalledWith('ticket-1');
    mocks.pendingMfaToken = 'ticket-1';
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps?.corporateMfaToken).toBe('ticket-1'));
  });

  it('keeps the drag strip as the first child of the window', () => {
    const { container } = render(<DesktopLoginPage />);

    expect(container.firstElementChild?.firstElementChild).toHaveAttribute(
      'data-testid',
      'drag-strip',
    );
  });

  it('names the configured address when the server does not answer at load', async () => {
    mocks.probeRuntimeServer.mockResolvedValue({
      ok: false,
      address: CONFIGURED.apiUrl,
      message: 'getaddrinfo ENOTFOUND goosar.acme.test',
    });

    render(<DesktopLoginPage />);

    expect(
      await screen.findByText(new RegExp(`cannot reach.*${CONFIGURED.apiUrl}`, 'i')),
    ).toBeInTheDocument();
    expect(screen.getByText(/getaddrinfo ENOTFOUND goosar\.acme\.test/)).toBeInTheDocument();
  });

  it('describeError names the configured address for a transport failure', async () => {
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message = describeError(new TypeError('Failed to fetch'));

    expect(message).toContain(CONFIGURED.apiUrl);
    expect(message).toMatch(/did not get through/i);
  });

  it('describeError defers to the shared message when the server answered', async () => {
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    expect(describeError(new mocks.MockApiError('Rate limited', 429))).toBeUndefined();
  });

  it('names the proxy, not the server, when the route runs through one', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message = describeError(TUNNEL_DEAD);

    expect(message).toContain('http://isa.corp:8080');
    expect(message).toMatch(/more likely there than at the server/i);
  });

  it('names every offered path when the system offered more than one', async () => {
    mocks.networkState = VIA_OFFERED_PATHS;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message = describeError(TUNNEL_DEAD) ?? '';

    expect(message).toContain('http://isa.corp:8080');
    expect(message).toContain('http://backup.corp:8080');
    expect(message).toContain(enAuth.desktop.server.route_path_direct);
    expect(message).not.toMatch(/routes that address through the proxy/i);
    expect(message).toMatch(/not recorded here/i);
  });

  it('names the proxy as a suspect without convicting it', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message = describeError(TUNNEL_DEAD) ?? '';

    expect(message).not.toMatch(/did not answer|stopped there|failed at a proxy/i);
  });

  it('does not promise network details that may not be rendered', async () => {
    const failures = [
      TUNNEL_DEAD,
      new TypeError('Failed to fetch'),
      new TypeError('Failed to fetch: net::ERR_NETWORK_CHANGED'),
      new TypeError('Failed to fetch: net::ERR_INTERNET_DISCONNECTED'),
      new TypeError('Failed to fetch: net::ERR_NAME_NOT_RESOLVED'),
      new TypeError('Failed to fetch: net::ERR_PAC_STATUS_NOT_OK'),
    ];
    for (const state of [null, VIA_DEAD_PROXY, ROUTE_UNRESOLVED, ROUTE_DIRECT]) {
      mocks.networkState = state;
      mocks.loginPageProps = null;
      const view = render(<DesktopLoginPage />);
      await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

      for (const failure of failures) {
        expect(describeError(failure) ?? '').not.toMatch(/below/i);
      }
      view.unmount();
    }
  });

  it('says the route is unresolved when the system did not report one', async () => {
    mocks.networkState = ROUTE_UNRESOLVED;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message = describeError(new TypeError('Failed to fetch'));

    expect(message).toMatch(/route .* could not be determined/i);
    expect(message).toContain(CONFIGURED.apiUrl);
  });

  it('does not blame the server before the route has arrived', async () => {
    mocks.networkState = null;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message = describeError(new TypeError('Failed to fetch')) ?? '';

    expect(message).toMatch(/route .* could not be determined/i);
    expect(message).not.toMatch(/server is running/i);
  });

  it('does not name a remembered proxy once the machine is offline', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message =
      describeError(new TypeError('Failed to fetch: net::ERR_INTERNET_DISCONNECTED')) ?? '';

    expect(message).not.toContain('isa.corp');
    expect(message).toMatch(/no network connection/i);
  });

  it('does not name a remembered proxy when the machine reports no network', async () => {
    const failure = new TypeError('Failed to fetch');
    await withNoNetwork(async () => {
      mocks.networkState = VIA_DEAD_PROXY;
      render(<DesktopLoginPage />);
      await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

      reportLoginFailure(failure);
      const message = describeError(failure) ?? '';

      expect(message).not.toContain('isa.corp');
      expect(message).toMatch(/no network connection/i);
    });
  });

  it('reads the machine as online again once the override is gone', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    expect(describeError(new TypeError('Failed to fetch')) ?? '').toContain('http://isa.corp:8080');
  });

  it('tells a network that changed mid-request from a machine with no network', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message = describeError(new TypeError('Failed to fetch: net::ERR_NETWORK_CHANGED')) ?? '';

    expect(message).not.toMatch(/no network connection/i);
    expect(message).toMatch(/changed while/i);
    expect(message).not.toContain('isa.corp');
  });

  it('keeps naming the server on a resolved direct route', async () => {
    mocks.networkState = ROUTE_DIRECT;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const message = describeError(new TypeError('Failed to fetch'));

    expect(message).toMatch(/cannot reach/i);
    expect(message).toContain(CONFIGURED.apiUrl);
    expect(message).not.toMatch(/proxy/i);
  });

  it('stays out of the way when the server answered, whatever the route', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    expect(describeError(new mocks.MockApiError('Server error', 500))).toBeUndefined();
  });

  it('keeps listening for route changes while the screen is open', async () => {
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    expect(mocks.networkStatusOptions?.subscribe).not.toBe(false);
  });

  it('opens the network diagnostics itself when the break is on the route', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());
    expect(screen.queryByTestId('network-panel')).toBeNull();

    reportLoginFailure(TUNNEL_DEAD);

    expect(screen.getByTestId('network-panel')).toBeInTheDocument();
  });

  it('leaves them collapsed when it is simply a dead server', async () => {
    mocks.networkState = ROUTE_DIRECT;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    reportLoginFailure(new TypeError('Failed to fetch'));

    expect(screen.queryByTestId('network-panel')).toBeNull();
  });

  it('keeps an offline verdict after the network comes back', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    const failure = new TypeError('Failed to fetch');
    await withNoNetwork(async () => {
      reportLoginFailure(failure);
    });
    const message = describeError(failure) ?? '';
    expect(message).toMatch(/no network|нет сети/i);
    expect(message).not.toMatch(/isa\.corp/);
  });

  it('keeps the diagnostics open when a later failure is not route-shaped', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    reportLoginFailure(TUNNEL_DEAD);
    expect(screen.getByTestId('network-panel')).toBeInTheDocument();

    reportLoginFailure(new mocks.MockApiError('Too many requests', 429));

    expect(screen.getByTestId('network-panel')).toBeInTheDocument();
  });

  it('does not reopen the diagnostics the user closed', async () => {
    mocks.networkState = VIA_DEAD_PROXY;
    render(<DesktopLoginPage />);
    await waitFor(() => expect(mocks.loginPageProps).not.toBeNull());

    reportLoginFailure(TUNNEL_DEAD);
    fireEvent.click(screen.getByRole('button', { name: /network/i }));
    expect(screen.queryByTestId('network-panel')).toBeNull();

    reportLoginFailure(TUNNEL_DEAD);

    expect(screen.queryByTestId('network-panel')).toBeNull();
  });

  it('refuses to apply an address the server does not answer, and names it', async () => {
    render(<DesktopLoginPage />);
    await openServerEditor('https://typo.acme.test');

    mocks.probeRuntimeServer.mockResolvedValue({
      ok: false,
      address: 'https://typo.acme.test',
      message: 'getaddrinfo ENOTFOUND typo.acme.test',
    });
    fireEvent.click(screen.getByRole('button', { name: /connect/i }));

    expect(
      await screen.findByText(/cannot reach.*https:\/\/typo\.acme\.test/i),
    ).toBeInTheDocument();
    expect(mocks.setRuntimeConfig).not.toHaveBeenCalled();
  });

  it('applies a reachable address and reports that it is taking effect', async () => {
    render(<DesktopLoginPage />);
    await openServerEditor('https://onprem.acme.test');

    mocks.probeRuntimeServer.mockResolvedValue({
      ok: true,
      address: 'https://onprem.acme.test',
    });
    mocks.setRuntimeConfig.mockResolvedValue({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://onprem.acme.test',
        wsUrl: 'wss://onprem.acme.test/ws',
        appUrl: 'https://onprem.acme.test',
      },
    });
    fireEvent.click(screen.getByRole('button', { name: /connect/i }));

    await waitFor(() => {
      expect(mocks.setRuntimeConfig).toHaveBeenCalledWith({
        apiUrl: 'https://onprem.acme.test',
      });
    });
    expect(await screen.findByText(/reloading to apply/i)).toBeInTheDocument();
  });

  it('names pinned endpoints it would replace, and does not write until told to', async () => {
    setRuntimeConfig({
      ...CONFIGURED,
      wsUrl: 'wss://sockets.acme.test/gateway',
    });
    render(<DesktopLoginPage />);
    await openServerEditor('https://onprem.acme.test');

    mocks.probeRuntimeServer.mockResolvedValue({
      ok: true,
      address: 'https://onprem.acme.test',
    });
    fireEvent.click(screen.getByRole('button', { name: /connect/i }));

    expect(
      await screen.findByText(/replaces endpoints pinned in desktop\.json/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /wsUrl: wss:\/\/sockets\.acme\.test\/gateway → wss:\/\/onprem\.acme\.test\/ws/,
      ),
    ).toBeInTheDocument();
    expect(mocks.setRuntimeConfig).not.toHaveBeenCalled();
  });

  it('applies the change once the replacement is accepted', async () => {
    setRuntimeConfig({
      ...CONFIGURED,
      wsUrl: 'wss://sockets.acme.test/gateway',
    });
    render(<DesktopLoginPage />);
    await openServerEditor('https://onprem.acme.test');

    mocks.probeRuntimeServer.mockResolvedValue({
      ok: true,
      address: 'https://onprem.acme.test',
    });
    mocks.setRuntimeConfig.mockResolvedValue({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://onprem.acme.test',
        wsUrl: 'wss://onprem.acme.test/ws',
        appUrl: 'https://onprem.acme.test',
      },
    });
    fireEvent.click(screen.getByRole('button', { name: /connect/i }));

    fireEvent.click(await screen.findByRole('button', { name: /change anyway/i }));

    await waitFor(() =>
      expect(mocks.setRuntimeConfig).toHaveBeenCalledWith({
        apiUrl: 'https://onprem.acme.test',
      }),
    );
  });

  it('disarms the confirmation when the address is edited past it', async () => {
    setRuntimeConfig({
      ...CONFIGURED,
      wsUrl: 'wss://sockets.acme.test/gateway',
    });
    render(<DesktopLoginPage />);
    await openServerEditor('https://onprem.acme.test');

    mocks.probeRuntimeServer.mockResolvedValue({
      ok: true,
      address: 'https://onprem.acme.test',
    });
    fireEvent.click(screen.getByRole('button', { name: /connect/i }));
    await screen.findByRole('button', { name: /change anyway/i });

    fireEvent.change(screen.getByLabelText(/server/i), {
      target: { value: 'https://elsewhere.acme.test' },
    });

    expect(screen.queryByRole('button', { name: /change anyway/i })).toBeNull();
    expect(screen.queryByText(/replaces endpoints pinned in desktop\.json/i)).toBeNull();
  });

  it('drops the pin warning when the edit is cancelled', async () => {
    setRuntimeConfig({
      ...CONFIGURED,
      wsUrl: 'wss://sockets.acme.test/gateway',
    });
    render(<DesktopLoginPage />);
    await openServerEditor('https://onprem.acme.test');

    mocks.probeRuntimeServer.mockResolvedValue({
      ok: true,
      address: 'https://onprem.acme.test',
    });
    fireEvent.click(screen.getByRole('button', { name: /connect/i }));
    await screen.findByRole('button', { name: /change anyway/i });

    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

    expect(screen.queryByText(/replaces endpoints pinned in desktop\.json/i)).toBeNull();
    expect(mocks.setRuntimeConfig).not.toHaveBeenCalled();
  });

  it('says nothing about pins when the endpoints were derived, not pinned', async () => {
    render(<DesktopLoginPage />);
    await openServerEditor('https://onprem.acme.test');

    mocks.probeRuntimeServer.mockResolvedValue({
      ok: true,
      address: 'https://onprem.acme.test',
    });
    mocks.setRuntimeConfig.mockResolvedValue({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://onprem.acme.test',
        wsUrl: 'wss://onprem.acme.test/ws',
        appUrl: 'https://onprem.acme.test',
      },
    });
    fireEvent.click(screen.getByRole('button', { name: /connect/i }));

    await waitFor(() => expect(mocks.setRuntimeConfig).toHaveBeenCalled());
    expect(screen.queryByText(/replaces endpoints pinned in desktop\.json/i)).toBeNull();
  });

  it('surfaces a rejected address instead of silently keeping the old one', async () => {
    render(<DesktopLoginPage />);
    await openServerEditor('https://onprem.acme.test');

    mocks.probeRuntimeServer.mockResolvedValue({
      ok: true,
      address: 'https://onprem.acme.test',
    });
    mocks.setRuntimeConfig.mockResolvedValue({
      ok: false,
      error: { message: 'EACCES: permission denied' },
    });
    fireEvent.click(screen.getByRole('button', { name: /connect/i }));

    expect(await screen.findByText(/EACCES: permission denied/)).toBeInTheDocument();
  });
});

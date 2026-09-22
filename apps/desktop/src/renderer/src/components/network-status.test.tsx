import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';

import enSettings from '@goosar/views/locales/en/settings.json';
import type { PerimeterStateView } from '../../../shared/perimeter-config';

vi.mock('@goosar/views/i18n', () => ({
  useT: () => ({
    t: (selector: (resources: typeof enSettings) => string, vars?: Record<string, string>) => {
      const template = selector(enSettings);
      return vars ? template.replace(/{{(\w+)}}/g, (_, key: string) => vars[key] ?? '') : template;
    },
  }),
  useUiLocale: () => 'en-US',
}));

const mocks = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  toastWarning: vi.fn(),
}));

vi.mock('sonner', () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warning: mocks.toastWarning,
  },
}));

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (s: { user: { email: string } }) => unknown) =>
    selector({ user: { email: 'user@example.test' } }),
}));

import {
  KerberosTicketBanner,
  KinitDialog,
  NetworkStatusIndicator,
  NetworkStatusPanel,
} from './network-status';

const HEALTHY: PerimeterStateView = {
  localProxyUrl: 'http://127.0.0.1:3128',
  caBundlePresent: true,
  corpCaPresent: true,
  kerberosTicket: 'valid',
  kerberosExpiresAt: null,
  kerberosSupported: true,
  realm: 'EXAMPLE.TEST',
  live: {
    overall: 'ok',
    checkedAt: 1_000,
    checks: [
      {
        id: 'route_server',
        state: 'ok',
        reasonCode: 'route_proxy',
        detail: 'http://isa.corp:8080',
      },
      { id: 'route_llm', state: 'ok', reasonCode: 'route_direct', detail: 'gw.internal' },
      {
        id: 'entry',
        state: 'ok',
        reasonCode: 'entry_system_proxy',
        detail: 'http://isa.corp:8080',
      },
      { id: 'px', state: 'ok', reasonCode: 'live_px_ok' },
      { id: 'ca', state: 'ok', reasonCode: 'ca_ok' },
    ],
  },
};

function stateWith(checks: PerimeterStateView['live']): PerimeterStateView {
  return { ...HEALTHY, live: checks };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('NetworkStatusPanel', () => {
  it('names the proxy the system resolved for each address', () => {
    const { container } = render(
      <NetworkStatusPanel state={HEALTHY} busy={false} onRecheck={vi.fn()} />,
    );

    expect(screen.getAllByText('http://isa.corp:8080').length).toBeGreaterThan(0);
    expect(screen.getByText('gw.internal')).toBeTruthy();
    expect(
      container.querySelector('[data-check-id="route_server"]')?.getAttribute('data-check-reason'),
    ).toBe('route_proxy');
  });

  it('offers nothing that reconfigures the machine', () => {
    const { container } = render(
      <NetworkStatusPanel state={HEALTHY} busy={false} onRecheck={vi.fn()} />,
    );

    expect(container.querySelector('[role="switch"]')).toBeNull();
    expect(container.querySelector('input')).toBeNull();
  });

  it('shows an unresolved route as a failure, not as a blank row', () => {
    const { container } = render(
      <NetworkStatusPanel
        state={stateWith({
          overall: 'fail',
          checkedAt: 1,
          checks: [
            {
              id: 'route_server',
              state: 'fail',
              reasonCode: 'route_unknown',
              detail: 'goosar.ru',
            },
          ],
        })}
        busy={false}
        onRecheck={vi.fn()}
      />,
    );

    const row = container.querySelector('[data-check-id="route_server"]');
    expect(row?.getAttribute('data-check-state')).toBe('fail');
    expect(row?.getAttribute('data-check-reason')).toBe('route_unknown');
  });

  it('never prints proxy credentials a PAC directive carried', () => {
    const { container } = render(
      <NetworkStatusPanel
        state={stateWith({
          overall: 'fail',
          checkedAt: 1,
          checks: [
            {
              id: 'route_server',
              state: 'ok',
              reasonCode: 'route_proxy',
              detail: 'http://alice:s3cret@isa.corp:8080',
            },
          ],
        })}
        busy={false}
        onRecheck={vi.fn()}
      />,
    );

    const text = container.textContent ?? '';
    expect(text).not.toContain('s3cret');
    expect(text).not.toContain('alice');
    expect(text).toContain('isa.corp:8080');
  });

  it('spells out the two-proxy conflict', () => {
    const { container } = render(
      <NetworkStatusPanel
        state={stateWith({
          overall: 'fail',
          checkedAt: 1,
          checks: [
            {
              id: 'entry',
              state: 'fail',
              reasonCode: 'entry_conflict',
              detail: 'http://isa.corp:8080, http://other.corp:3128',
            },
          ],
        })}
        busy={false}
        onRecheck={vi.fn()}
      />,
    );

    const row = container.querySelector('[data-check-id="entry"]');
    expect(row?.getAttribute('data-check-state')).toBe('fail');
    expect(row?.getAttribute('data-check-reason')).toBe('entry_conflict');
    expect(screen.getByText('http://isa.corp:8080, http://other.corp:3128')).toBeTruthy();
  });

  it('separates a local proxy that only listens from one that is absent', () => {
    const { container } = render(
      <NetworkStatusPanel
        state={stateWith({
          overall: 'fail',
          checkedAt: 1,
          checks: [{ id: 'px', state: 'fail', reasonCode: 'live_px_upstream_dead' }],
        })}
        busy={false}
        onRecheck={vi.fn()}
      />,
    );

    expect(container.querySelector('[data-check-id="px"]')?.getAttribute('data-check-reason')).toBe(
      'live_px_upstream_dead',
    );
  });

  it('shows the cache principal, expiry and ticket source on the Kerberos row', () => {
    const { container } = render(
      <NetworkStatusPanel
        state={{
          ...HEALTHY,
          kerberosCachePrincipal: 'alice@CORP.EXAMPLE',
          kerberosExpiresAt: 1_700_000_000_000,
          ticketSource: 'kinit_app',
          live: {
            overall: 'ok',
            checkedAt: 1,
            checks: [{ id: 'kerberos', state: 'ok', reasonCode: 'ticket_valid' }],
          },
        }}
        busy={false}
        onRecheck={vi.fn()}
      />,
    );

    const row = container.querySelector('[data-check-id="kerberos"]');
    expect(row?.textContent).toContain('alice@CORP.EXAMPLE');
    expect(row?.textContent).toContain(enSettings.desktop.perimeter.ticket_source_kinit_app);
  });

  it('shows nothing extra for a subsystem with no principal/expiry/source', () => {
    const { container } = render(
      <NetworkStatusPanel
        state={stateWith({
          overall: 'ok',
          checkedAt: 1,
          checks: [{ id: 'ca', state: 'ok', reasonCode: 'ca_ok' }],
        })}
        busy={false}
        onRecheck={vi.fn()}
      />,
    );

    expect(container.querySelector('[data-check-id="ca"] .ml-5')).toBeNull();
  });
});

describe('NetworkStatusIndicator', () => {
  function installApi(over: Record<string, unknown> = {}) {
    const api = {
      getPerimeter: vi.fn().mockResolvedValue(HEALTHY),
      recheckPerimeter: vi.fn().mockResolvedValue(HEALTHY),
      onPerimeterState: vi.fn().mockReturnValue(() => {}),
      restartProvisioningNow: vi.fn(),
      perimeterKinit: vi.fn(),
      perimeterKinitRenew: vi.fn(),
      ...over,
    };
    Object.defineProperty(window, 'daemonAPI', {
      configurable: true,
      value: api,
    });
    return api;
  }

  it('reads the status and offers no way to change it', async () => {
    const api = installApi();
    const { container } = render(<NetworkStatusIndicator />);

    await waitFor(() => expect(api.getPerimeter).toHaveBeenCalled());
    expect('setPerimeter' in api).toBe(false);
    expect(container.querySelector('[role="switch"]')).toBeNull();
  });

  it('re-resolves the routes when asked to look again', async () => {
    const api = installApi();
    render(
      <NetworkStatusPanelHarness
        onRecheck={async () => {
          await window.daemonAPI.recheckPerimeter();
        }}
      />,
    );

    fireEvent.click(screen.getByRole('button'));
    await waitFor(() => expect(api.recheckPerimeter).toHaveBeenCalled());
  });

  it('renders nothing when the preload predates the API', async () => {
    Object.defineProperty(window, 'daemonAPI', {
      configurable: true,
      value: {},
    });
    const { container } = render(<NetworkStatusIndicator />);

    await waitFor(() => expect(container.firstChild).toBeNull());
  });

  describe('background ticket lifecycle (#466)', () => {
    const settle = () =>
      act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 10));
      });

    const expiredState: PerimeterStateView = {
      ...HEALTHY,
      kerberosTicket: 'expired',
      kerberosExpiresAt: Date.now() - 60_000,
      kerberosRenewUntil: null,
    };

    it('says nothing at all when klist could not be read', async () => {
      const api = installApi({
        getPerimeter: vi.fn().mockResolvedValue({
          ...HEALTHY,
          kerberosTicket: 'unknown',
          kerberosExpiresAt: null,
        }),
        perimeterKinitRenew: vi.fn(),
      });
      render(<NetworkStatusIndicator />);

      await waitFor(() => expect(api.getPerimeter).toHaveBeenCalled());
      await settle();
      expect(document.getElementById('kinit-password')).toBeNull();
      expect(api.perimeterKinitRenew).not.toHaveBeenCalled();
    });

    it('renews an expired-but-renewable ticket silently, without a dialog', async () => {
      const api = installApi({
        getPerimeter: vi.fn().mockResolvedValue(expiredState),
        perimeterKinitRenew: vi.fn().mockResolvedValue({ ok: true, state: HEALTHY }),
      });
      render(<NetworkStatusIndicator />);

      await waitFor(() => expect(api.perimeterKinitRenew).toHaveBeenCalled());
      await settle();
      expect(document.getElementById('kinit-password')).toBeNull();
      expect(mocks.toastSuccess).not.toHaveBeenCalled();
    });

    it('asks for a password once the renewal is refused', async () => {
      const api = installApi({
        getPerimeter: vi.fn().mockResolvedValue(expiredState),
        perimeterKinitRenew: vi
          .fn()
          .mockResolvedValue({ ok: false, reason: 'renew_rejected', message: 'no' }),
      });
      render(<NetworkStatusIndicator />);

      await waitFor(() => expect(api.perimeterKinitRenew).toHaveBeenCalled());
      await waitFor(() => expect(document.getElementById('kinit-password')).toBeTruthy());
    });
  });
});

describe('KerberosTicketBanner (#297/#298)', () => {
  function installApi(state: PerimeterStateView, over: Record<string, unknown> = {}) {
    const api = {
      getPerimeter: vi.fn().mockResolvedValue(state),
      recheckPerimeter: vi.fn().mockResolvedValue(state),
      onPerimeterState: vi.fn().mockReturnValue(() => {}),
      perimeterKinit: vi.fn(),
      perimeterKinitRenew: vi.fn(),
      ...over,
    };
    Object.defineProperty(window, 'daemonAPI', {
      configurable: true,
      value: api,
    });
    return api;
  }

  function ticketState(
    ticket: PerimeterStateView['kerberosTicket'],
    expiresAt: number | null = null,
  ): PerimeterStateView {
    return { ...HEALTHY, kerberosTicket: ticket, kerberosExpiresAt: expiresAt };
  }

  it('renders nothing while the ticket is comfortably valid', async () => {
    const api = installApi(ticketState('valid'));
    const { container } = render(<KerberosTicketBanner />);
    await waitFor(() => expect(api.getPerimeter).toHaveBeenCalled());
    expect(container.querySelector('[data-testid="kerberos-ticket-banner"]')).toBeNull();
  });

  it('renders nothing on platforms without in-app kinit', async () => {
    const api = installApi({
      ...ticketState('none'),
      kerberosSupported: false,
    });
    const { container } = render(<KerberosTicketBanner />);
    await waitFor(() => expect(api.getPerimeter).toHaveBeenCalled());
    expect(container.querySelector('[data-testid="kerberos-ticket-banner"]')).toBeNull();
  });

  it('renders nothing on a Mac not provisioned for the perimeter (no CA files)', async () => {
    const api = installApi({
      ...ticketState('none'),
      caBundlePresent: false,
      corpCaPresent: false,
    });
    const { container } = render(<KerberosTicketBanner />);
    await waitFor(() => expect(api.getPerimeter).toHaveBeenCalled());
    expect(container.querySelector('[data-testid="kerberos-ticket-banner"]')).toBeNull();
  });

  it('calls out a missing ticket, offering sign-in but NOT renewal', async () => {
    installApi(ticketState('none'));
    const { container } = render(<KerberosTicketBanner />);
    await waitFor(() =>
      expect(container.querySelector('[data-testid="kerberos-ticket-banner"]')).toBeTruthy(),
    );
    const banner = container.querySelector('[data-testid="kerberos-ticket-banner"]');
    expect(banner?.getAttribute('data-ticket-state')).toBe('none');
    expect(banner?.querySelectorAll('button').length).toBe(1);
  });

  it('renews an expiring ticket via kinit -R and applies the fresh state', async () => {
    const renewed = ticketState('valid');
    const api = installApi(ticketState('expiring_soon', Date.now() + 10 * 60_000), {
      perimeterKinitRenew: vi.fn().mockResolvedValue({ ok: true, state: renewed }),
    });
    const { container } = render(<KerberosTicketBanner />);
    await waitFor(() =>
      expect(container.querySelector('[data-testid="kerberos-ticket-banner"]')).toBeTruthy(),
    );

    const [renewButton] = Array.from(
      container.querySelectorAll('[data-testid="kerberos-ticket-banner"] button'),
    );
    fireEvent.click(renewButton);

    await waitFor(() => expect(api.perimeterKinitRenew).toHaveBeenCalled());
    expect(mocks.toastSuccess).toHaveBeenCalled();
    await waitFor(() =>
      expect(container.querySelector('[data-testid="kerberos-ticket-banner"]')).toBeNull(),
    );
  });

  it('falls back to the password dialog when renewal is rejected', async () => {
    const api = installApi(ticketState('expired'), {
      perimeterKinitRenew: vi.fn().mockResolvedValue({
        ok: false,
        reason: 'renew_rejected',
        message: 'not renewable',
      }),
    });
    const { container } = render(<KerberosTicketBanner />);
    await waitFor(() =>
      expect(container.querySelector('[data-testid="kerberos-ticket-banner"]')).toBeTruthy(),
    );

    const [renewButton] = Array.from(
      container.querySelectorAll('[data-testid="kerberos-ticket-banner"] button'),
    );
    fireEvent.click(renewButton);

    await waitFor(() => expect(api.perimeterKinitRenew).toHaveBeenCalled());
    expect(mocks.toastWarning).toHaveBeenCalled();
    await waitFor(() => expect(document.getElementById('kinit-password')).toBeTruthy());
  });
});

function NetworkStatusPanelHarness({ onRecheck }: { onRecheck: () => void }) {
  return <NetworkStatusPanel state={HEALTHY} busy={false} onRecheck={onRecheck} />;
}

describe('KinitDialog principal prompt (#531)', () => {
  const DERIVED = 'ivanov@EXAMPLE.COM';
  const REAL = 'ivanov@EXAMPLE.NET';

  function installApi(over: Record<string, unknown> = {}) {
    const api = {
      perimeterKinit: vi.fn(),
      kerberosGetPreferences: vi
        .fn()
        .mockResolvedValue({ principal: null, thresholdMs: 1_800_000, snoozedUntil: null }),
      kerberosSetPreferences: vi.fn(async (next: unknown) => next),
      ...over,
    };
    Object.defineProperty(window, 'daemonAPI', { configurable: true, value: api });
    return api;
  }

  async function typePassword(value: string) {
    const input = document.getElementById('kinit-password') as HTMLInputElement;
    fireEvent.change(input, { target: { value } });
  }

  it('asks for a principal, saves it as the override and retries', async () => {
    const api = installApi({
      perimeterKinit: vi
        .fn()
        .mockResolvedValueOnce({
          ok: false,
          reason: 'principal_unknown',
          message: 'unknown principal',
        })
        .mockResolvedValueOnce({ ok: true, state: HEALTHY }),
    });
    const onSuccess = vi.fn();

    render(<KinitDialog open onOpenChange={vi.fn()} principal={DERIVED} onSuccess={onSuccess} />);

    await typePassword('pw');
    fireEvent.click(document.getElementById('kinit-submit')!);

    const field = await waitFor(() => {
      const el = document.getElementById('kinit-principal') as HTMLInputElement;
      if (!el) throw new Error('no principal field');
      return el;
    });
    expect(field.value).toBe(DERIVED);

    fireEvent.change(field, { target: { value: REAL } });
    await typePassword('pw');
    fireEvent.click(document.getElementById('kinit-submit')!);

    await waitFor(
      () => {
        expect(onSuccess).toHaveBeenCalled();
        expect(api.kerberosSetPreferences).toHaveBeenCalledWith(
          expect.objectContaining({ principal: REAL }),
        );
      },
      { timeout: 5_000 },
    );
    expect(api.perimeterKinit).toHaveBeenLastCalledWith(REAL, 'pw');
  });

  it('keeps the typed password so only the domain has to be corrected', async () => {
    const api = installApi({
      perimeterKinit: vi
        .fn()
        .mockResolvedValueOnce({
          ok: false,
          reason: 'principal_unknown',
          message: 'unknown principal',
        })
        .mockResolvedValueOnce({ ok: true, state: HEALTHY }),
    });

    render(<KinitDialog open onOpenChange={vi.fn()} principal={DERIVED} onSuccess={vi.fn()} />);

    await typePassword('pw');
    fireEvent.click(document.getElementById('kinit-submit')!);
    const field = await waitFor(() => {
      const el = document.getElementById('kinit-principal') as HTMLInputElement;
      if (!el) throw new Error('no principal field');
      return el;
    });

    const password = document.getElementById('kinit-password') as HTMLInputElement;
    expect(password.value).toBe('pw');
    expect((document.getElementById('kinit-submit') as HTMLButtonElement).disabled).toBe(false);

    fireEvent.change(field, { target: { value: REAL } });
    fireEvent.click(document.getElementById('kinit-submit')!);
    await waitFor(() => expect(api.perimeterKinit).toHaveBeenCalledTimes(2));
    expect(api.perimeterKinit).toHaveBeenLastCalledWith(REAL, 'pw');
  });

  it('says so when the override could not be stored', async () => {
    installApi({
      perimeterKinit: vi
        .fn()
        .mockResolvedValueOnce({
          ok: false,
          reason: 'principal_unknown',
          message: 'unknown principal',
        })
        .mockResolvedValueOnce({ ok: true, state: HEALTHY }),
      kerberosSetPreferences: vi.fn(async () => {
        throw new Error('EACCES');
      }),
    });

    render(<KinitDialog open onOpenChange={vi.fn()} principal={DERIVED} onSuccess={vi.fn()} />);

    await typePassword('pw');
    fireEvent.click(document.getElementById('kinit-submit')!);
    const field = await waitFor(() => {
      const el = document.getElementById('kinit-principal') as HTMLInputElement;
      if (!el) throw new Error('no principal field');
      return el;
    });
    fireEvent.change(field, { target: { value: REAL } });
    fireEvent.click(document.getElementById('kinit-submit')!);

    await waitFor(() => expect(mocks.toastWarning).toHaveBeenCalled(), {
      timeout: 5_000,
    });
  });

  it('does not submit an empty principal and takes Enter in the field', async () => {
    const api = installApi({
      perimeterKinit: vi
        .fn()
        .mockResolvedValueOnce({
          ok: false,
          reason: 'principal_unknown',
          message: 'unknown principal',
        })
        .mockResolvedValueOnce({ ok: true, state: HEALTHY }),
    });

    render(<KinitDialog open onOpenChange={vi.fn()} principal={DERIVED} onSuccess={vi.fn()} />);

    await typePassword('pw');
    fireEvent.click(document.getElementById('kinit-submit')!);
    const field = await waitFor(() => {
      const el = document.getElementById('kinit-principal') as HTMLInputElement;
      if (!el) throw new Error('no principal field');
      return el;
    });

    fireEvent.change(field, { target: { value: '   ' } });
    expect((document.getElementById('kinit-submit') as HTMLButtonElement).disabled).toBe(true);
    expect(mocks.toastError).toHaveBeenCalledTimes(1);

    fireEvent.change(field, { target: { value: REAL } });
    fireEvent.keyDown(field, { key: 'Enter' });
    await waitFor(() => expect(api.perimeterKinit).toHaveBeenCalledTimes(2));
    expect(api.perimeterKinit).toHaveBeenLastCalledWith(REAL, 'pw');
  });

  it('refuses an ill-formed principal without spawning kinit again', async () => {
    const api = installApi({
      perimeterKinit: vi.fn().mockResolvedValue({
        ok: false,
        reason: 'principal_unknown',
        message: 'unknown principal',
      }),
    });

    render(<KinitDialog open onOpenChange={vi.fn()} principal={DERIVED} onSuccess={vi.fn()} />);

    await typePassword('pw');
    fireEvent.click(document.getElementById('kinit-submit')!);
    const field = await waitFor(() => {
      const el = document.getElementById('kinit-principal') as HTMLInputElement;
      if (!el) throw new Error('no principal field');
      return el;
    });

    fireEvent.change(field, { target: { value: 'ivanov nowhere' } });
    await typePassword('pw');
    fireEvent.click(document.getElementById('kinit-submit')!);

    await waitFor(() => expect(mocks.toastError).toHaveBeenCalled());
    expect(api.perimeterKinit).toHaveBeenCalledTimes(1);
    expect(api.kerberosSetPreferences).not.toHaveBeenCalled();
  });
});

describe('KinitDialog bare kinit (#707)', () => {
  function installApi(over: Record<string, unknown> = {}) {
    const api = {
      perimeterKinit: vi.fn().mockResolvedValue({ ok: true, state: HEALTHY }),
      kerberosGetPreferences: vi
        .fn()
        .mockResolvedValue({ principal: null, thresholdMs: 1_800_000, snoozedUntil: null }),
      kerberosSetPreferences: vi.fn(async (next: unknown) => next),
      ...over,
    };
    Object.defineProperty(window, 'daemonAPI', { configurable: true, value: api });
    return api;
  }

  it('shows an optional principal field with a placeholder when no principal resolves', () => {
    installApi();
    render(<KinitDialog open onOpenChange={vi.fn()} principal={null} onSuccess={vi.fn()} />);
    const field = document.getElementById('kinit-principal') as HTMLInputElement;
    expect(field).toBeTruthy();
    expect(field.placeholder.length).toBeGreaterThan(0);
    expect(field.value).toBe('');
  });

  it('submits a bare kinit (no principal argument) when the field is left empty', async () => {
    const api = installApi();
    const onSuccess = vi.fn();
    render(<KinitDialog open onOpenChange={vi.fn()} principal={null} onSuccess={onSuccess} />);

    const password = document.getElementById('kinit-password') as HTMLInputElement;
    fireEvent.change(password, { target: { value: 'pw' } });
    expect((document.getElementById('kinit-submit') as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(document.getElementById('kinit-submit')!);

    await waitFor(() => expect(onSuccess).toHaveBeenCalled());
    expect(api.perimeterKinit).toHaveBeenCalledWith(null, 'pw');
  });

  it('still uses a typed principal when the user fills the optional field', async () => {
    const api = installApi();
    render(<KinitDialog open onOpenChange={vi.fn()} principal={null} onSuccess={vi.fn()} />);

    const field = document.getElementById('kinit-principal') as HTMLInputElement;
    fireEvent.change(field, { target: { value: 'ivanov@EXAMPLE.TEST' } });
    const password = document.getElementById('kinit-password') as HTMLInputElement;
    fireEvent.change(password, { target: { value: 'pw' } });
    fireEvent.click(document.getElementById('kinit-submit')!);

    await waitFor(() =>
      expect(api.perimeterKinit).toHaveBeenCalledWith('ivanov@EXAMPLE.TEST', 'pw'),
    );
  });
});

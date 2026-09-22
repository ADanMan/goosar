import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';

import { runTaskPreflight, setTaskPreflight } from '@goosar/core/platform';

import type { KerberosPreferences } from '../../../shared/kerberos-preferences-types';
import type { PerimeterStateView } from '../../../shared/perimeter-config';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

vi.mock('@goosar/views/i18n', async () => {
  const { settingsI18nMock } = await import('../../../../test/i18n-settings-mock');
  return { ...(await settingsI18nMock()), useUiLocale: () => 'en' };
});

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (s: { user: { email: string } }) => unknown) =>
    selector({ user: { email: 'worker@goosar.example' } }),
}));

import { KerberosPreflightGate } from './kerberos-preflight';

const HOUR = 60 * 60_000;
const NOW = Date.UTC(2026, 0, 10, 12, 0, 0);

function stateWith(over: Partial<PerimeterStateView> = {}): PerimeterStateView {
  return {
    localProxyUrl: 'http://127.0.0.1:3128',
    caBundlePresent: true,
    corpCaPresent: true,
    kerberosTicket: 'valid',
    kerberosExpiresAt: NOW + 8 * HOUR,
    kerberosRenewUntil: NOW + 5 * 24 * HOUR,
    kerberosCachePrincipal: 'worker@CORP.EXAMPLE',
    lastKinit: null,
    kerberosSupported: true,
    realm: 'CORP.EXAMPLE',
    live: { overall: 'ok', checkedAt: NOW, checks: [] },
    ...over,
  };
}

const PREFS: KerberosPreferences = {
  principal: null,
  thresholdMs: 30 * 60_000,
  snoozedUntil: null,
};

function installApi({
  state = stateWith(),
  packages = [{ name: 'ews-mcp', type: 'mcp-server', version: '1', state: 'ok' }],
  preferences = PREFS,
  renewOk = true,
}: {
  state?: PerimeterStateView;
  packages?: { name: string; type: string; version: string; state: string }[];
  preferences?: KerberosPreferences;
  renewOk?: boolean;
} = {}) {
  const api = {
    getPerimeter: vi.fn().mockResolvedValue(state),
    getProvisioningStatus: vi.fn().mockResolvedValue({ packages }),
    kerberosGetPreferences: vi.fn().mockResolvedValue(preferences),
    kerberosSetPreferences: vi.fn(),
    perimeterKinit: vi.fn(),
    perimeterKinitRenew: vi
      .fn()
      .mockResolvedValue(renewOk ? { ok: true, state } : { ok: false, reason: 'renew_rejected' }),
  };
  Object.defineProperty(window, 'daemonAPI', { configurable: true, value: api });
  return api;
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.setSystemTime(new Date(NOW));
});
afterEach(() => {
  setTaskPreflight(null);
  vi.useRealTimers();
});

describe('KerberosPreflightGate', () => {
  it('stays out of the way for a comfortably valid ticket', async () => {
    installApi();
    render(<KerberosPreflightGate />);

    await expect(runTaskPreflight()).resolves.toBe(true);
  });

  it('never gates a machine with no Kerberos-backed server', async () => {
    const api = installApi({
      state: stateWith({ kerberosTicket: 'none', kerberosExpiresAt: null }),
      packages: [{ name: 'b24-agent', type: 'mcp-server', version: '1', state: 'ok' }],
    });
    render(<KerberosPreflightGate />);

    await expect(runTaskPreflight()).resolves.toBe(true);
    expect(api.kerberosGetPreferences).not.toHaveBeenCalled();
  });

  it('never gates a platform without in-app kinit', async () => {
    const api = installApi({ state: stateWith({ kerberosSupported: false }) });
    render(<KerberosPreflightGate />);

    await expect(runTaskPreflight()).resolves.toBe(true);
    expect(api.getProvisioningStatus).not.toHaveBeenCalled();
  });

  it('renews silently rather than showing a dialog', async () => {
    const api = installApi({
      state: stateWith({ kerberosExpiresAt: NOW + 5 * 60_000 }),
    });
    render(<KerberosPreflightGate />);

    await expect(runTaskPreflight()).resolves.toBe(true);
    expect(api.perimeterKinitRenew).toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('renews the principalDomain cache, not the realm-derived one', async () => {
    const api = installApi({
      state: stateWith({
        kerberosExpiresAt: NOW + 5 * 60_000,
        realm: 'EXAMPLE.COM',
        principalDomain: 'EXAMPLE.NET',
      }),
    });
    render(<KerberosPreflightGate />);

    await expect(runTaskPreflight()).resolves.toBe(true);
    expect(api.perimeterKinitRenew).toHaveBeenCalledWith('worker@EXAMPLE.NET');
  });

  it('blocks the send and asks for a password when renewal fails', async () => {
    installApi({
      state: stateWith({ kerberosTicket: 'none', kerberosExpiresAt: null }),
      renewOk: false,
    });
    render(<KerberosPreflightGate />);

    const decision = runTaskPreflight();
    const dialog = await screen.findByRole('dialog');
    expect(dialog.textContent).toMatch(/Kerberos ticket/i);

    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await expect(decision).resolves.toBe(false);
  });

  it('ignores a snooze — a pre-flight cannot be postponed', async () => {
    installApi({
      state: stateWith({ kerberosTicket: 'none', kerberosExpiresAt: null }),
      preferences: { ...PREFS, snoozedUntil: NOW + HOUR },
      renewOk: false,
    });
    render(<KerberosPreflightGate />);

    const decision = runTaskPreflight();
    const dialog = await screen.findByRole('dialog');
    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await expect(decision).resolves.toBe(false);
  });

  it('allows the send when the bridge cannot answer at all', async () => {
    Object.defineProperty(window, 'daemonAPI', {
      configurable: true,
      value: undefined,
    });
    render(<KerberosPreflightGate />);

    await expect(runTaskPreflight()).resolves.toBe(true);
  });

  it('unregisters itself on unmount', async () => {
    installApi({
      state: stateWith({ kerberosTicket: 'none', kerberosExpiresAt: null }),
    });
    const { unmount } = render(<KerberosPreflightGate />);
    unmount();

    await waitFor(async () => expect(await runTaskPreflight()).toBe(true));
  });
});

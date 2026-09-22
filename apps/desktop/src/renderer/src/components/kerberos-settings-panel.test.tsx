import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import type { KerberosPreferences } from '../../../shared/kerberos-preferences-types';
import type { PerimeterStateView } from '../../../shared/perimeter-config';
import { parseKerberosPreferences } from '../../../main/kerberos-preferences';

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

vi.mock('@goosar/views/i18n', async () => {
  const { settingsI18nMock } = await import('../../../../test/i18n-settings-mock');
  return { ...(await settingsI18nMock()), useUiLocale: () => 'en' };
});

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (s: { user: { email: string } }) => unknown) =>
    selector({ user: { email: 'worker@goosar.example' } }),
}));

import { KerberosSignInPanel } from './kerberos-settings-panel';

const STATE: PerimeterStateView = {
  localProxyUrl: 'http://127.0.0.1:3128',
  caBundlePresent: true,
  corpCaPresent: true,
  kerberosTicket: 'valid',
  kerberosExpiresAt: Date.UTC(2026, 0, 10, 19, 0, 0),
  kerberosRenewUntil: Date.UTC(2026, 0, 17, 9, 0, 0),
  kerberosCachePrincipal: 'worker@CORP.EXAMPLE',
  lastKinit: { at: Date.UTC(2026, 0, 10, 9, 0, 0), kind: 'kinit', ok: true },
  kerberosSupported: true,
  realm: 'REALM.EXAMPLE',
  live: { overall: 'ok', checkedAt: 1_000, checks: [] },
};

function installApi(stored: KerberosPreferences | null = null, state: PerimeterStateView = STATE) {
  let preferences: KerberosPreferences = stored ?? {
    principal: null,
    thresholdMs: 30 * 60_000,
    snoozedUntil: null,
  };
  const api = {
    getPerimeter: vi.fn().mockResolvedValue(state),
    recheckPerimeter: vi.fn().mockResolvedValue(state),
    onPerimeterState: vi.fn().mockReturnValue(() => {}),
    perimeterKinit: vi.fn(),
    perimeterKinitRenew: vi.fn().mockResolvedValue({ ok: true, state }),
    kerberosGetPreferences: vi.fn(async () => preferences),
    kerberosSetPreferences: vi.fn(async (next: unknown) => {
      preferences = parseKerberosPreferences(next);
      return preferences;
    }),
  };
  Object.defineProperty(window, 'daemonAPI', { configurable: true, value: api });
  return api;
}

describe('KerberosSignInPanel principal override (#466)', () => {
  beforeEach(() => vi.clearAllMocks());

  it('prefills the field with nothing and offers the derived principal as the placeholder', async () => {
    installApi();
    render(<KerberosSignInPanel />);

    const field = await screen.findByLabelText(/UPN/i);
    expect((field as HTMLInputElement).value).toBe('');
    expect((field as HTMLInputElement).placeholder).toBe('worker@REALM.EXAMPLE');
  });

  it('shows a stored override in the field and as the principal in use', async () => {
    installApi({
      principal: 'other.name@CORP.EXAMPLE',
      thresholdMs: 30 * 60_000,
      snoozedUntil: null,
    });
    render(<KerberosSignInPanel />);

    const field = await screen.findByLabelText(/UPN/i);
    await waitFor(() => expect((field as HTMLInputElement).value).toBe('other.name@CORP.EXAMPLE'));
    expect(await screen.findByText(/other\.name@CORP\.EXAMPLE/)).toBeTruthy();
  });

  it('saves a valid override through the bridge', async () => {
    const api = installApi();
    render(<KerberosSignInPanel />);

    const field = await screen.findByLabelText(/UPN/i);
    fireEvent.change(field, { target: { value: ' other.name@CORP.EXAMPLE ' } });
    fireEvent.click(screen.getByRole('button', { name: /^Save$/i }));

    await waitFor(() => expect(api.kerberosSetPreferences).toHaveBeenCalled());
    expect(api.kerberosSetPreferences.mock.calls[0][0]).toMatchObject({
      principal: 'other.name@CORP.EXAMPLE',
    });
    expect(await api.kerberosSetPreferences.mock.results[0].value).toMatchObject({
      principal: 'other.name@CORP.EXAMPLE',
    });
  });

  it('refuses to save an ill-formed principal and says why', async () => {
    const api = installApi();
    render(<KerberosSignInPanel />);

    const field = await screen.findByLabelText(/UPN/i);
    fireEvent.change(field, { target: { value: 'no-realm-here' } });

    expect(await screen.findByRole('alert')).toBeTruthy();
    const saveButton = screen.getByRole('button', { name: /^Save$/i });
    expect((saveButton as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(saveButton);
    fireEvent.keyDown(field, { key: 'Enter' });

    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy());
    expect(api.kerberosSetPreferences).not.toHaveBeenCalled();
  });

  it('warns about a lower-case realm without refusing it', async () => {
    installApi();
    render(<KerberosSignInPanel />);

    const field = await screen.findByLabelText(/UPN/i);
    fireEvent.change(field, { target: { value: 'worker@corp.example' } });

    expect(await screen.findByTestId('kerberos-realm-case')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('resets to the derived principal', async () => {
    const api = installApi({
      principal: 'other.name@CORP.EXAMPLE',
      thresholdMs: 30 * 60_000,
      snoozedUntil: null,
    });
    render(<KerberosSignInPanel />);

    await screen.findByLabelText(/UPN/i);
    fireEvent.click(screen.getByRole('button', { name: /Use default/i }));

    await waitFor(() => expect(api.kerberosSetPreferences).toHaveBeenCalled());
    expect(await api.kerberosSetPreferences.mock.results[0].value).toMatchObject({
      principal: null,
    });
  });
});

describe('KerberosSignInPanel observability (#466)', () => {
  beforeEach(() => vi.clearAllMocks());

  it("reports the cache's own principal, the renewable deadline and the last attempt", async () => {
    installApi();
    render(<KerberosSignInPanel />);

    const block = await screen.findByTestId('kerberos-observability');
    expect(block.textContent).toContain('worker@CORP.EXAMPLE');
    expect(block.textContent).toMatch(/\d{2}:\d{2}/);
  });

  it('re-checks on demand', async () => {
    const api = installApi();
    render(<KerberosSignInPanel />);

    await screen.findByLabelText(/UPN/i);
    fireEvent.click(screen.getByRole('button', { name: /Check now/i }));
    await waitFor(() => expect(api.recheckPerimeter).toHaveBeenCalled());
  });

  it("renews the OVERRIDE's cache, not merely the default one", async () => {
    const api = installApi({
      principal: 'other.name@CORP.EXAMPLE',
      thresholdMs: 30 * 60_000,
      snoozedUntil: null,
    });
    render(<KerberosSignInPanel />);

    await screen.findByLabelText(/UPN/i);
    fireEvent.click(screen.getByRole('button', { name: /Renew ticket/i }));

    await waitFor(() => expect(api.perimeterKinitRenew).toHaveBeenCalled());
    expect(api.perimeterKinitRenew).toHaveBeenCalledWith('other.name@CORP.EXAMPLE');
  });
});

describe('KerberosSignInPanel honours perimeter.principalDomain (#531)', () => {
  beforeEach(() => vi.clearAllMocks());

  const SPLIT_DOMAIN: PerimeterStateView = {
    ...STATE,
    realm: 'EXAMPLE.COM',
    principalDomain: 'EXAMPLE.NET',
  };

  it('derives, displays and renews against principalDomain, not the realm', async () => {
    const api = installApi(null, SPLIT_DOMAIN);
    render(<KerberosSignInPanel />);

    const field = await screen.findByLabelText(/UPN/i);
    expect((field as HTMLInputElement).placeholder).toBe('worker@EXAMPLE.NET');
    expect(await screen.findByText(/worker@EXAMPLE\.NET/)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Renew ticket/i }));
    await waitFor(() => expect(api.perimeterKinitRenew).toHaveBeenCalled());
    expect(api.perimeterKinitRenew).toHaveBeenCalledWith('worker@EXAMPLE.NET');
  });

  it('still lets a per-user override win over principalDomain', async () => {
    const api = installApi(
      {
        principal: 'other.name@EXAMPLE.NET',
        thresholdMs: 30 * 60_000,
        snoozedUntil: null,
      },
      SPLIT_DOMAIN,
    );
    render(<KerberosSignInPanel />);

    await screen.findByLabelText(/UPN/i);
    fireEvent.click(screen.getByRole('button', { name: /Renew ticket/i }));
    await waitFor(() => expect(api.perimeterKinitRenew).toHaveBeenCalled());
    expect(api.perimeterKinitRenew).toHaveBeenCalledWith('other.name@EXAMPLE.NET');
  });
});

describe('KerberosSignInPanel and the kinit dialog agree on the override (#531)', () => {
  beforeEach(() => vi.clearAllMocks());

  it('adopts a principal the dialog stored instead of offering to wipe it', async () => {
    const api = installApi();
    api.perimeterKinit
      .mockResolvedValueOnce({
        ok: false,
        reason: 'principal_unknown',
        message: 'unknown principal',
      })
      .mockResolvedValueOnce({ ok: true, state: STATE });

    render(<KerberosSignInPanel />);
    const field = (await screen.findByLabelText(/UPN/i)) as HTMLInputElement;

    fireEvent.click(screen.getByRole('button', { name: /Get a Kerberos ticket/i }));
    fireEvent.change(document.getElementById('kinit-password')!, {
      target: { value: 'pw' },
    });
    fireEvent.click(document.getElementById('kinit-submit')!);

    const dialogField = await waitFor(() => {
      const el = document.getElementById('kinit-principal');
      if (!el) throw new Error('no principal field');
      return el as HTMLInputElement;
    });
    fireEvent.change(dialogField, { target: { value: 'worker@EXAMPLE.NET' } });
    fireEvent.click(document.getElementById('kinit-submit')!);

    await waitFor(
      () =>
        expect(api.kerberosSetPreferences).toHaveBeenCalledWith(
          expect.objectContaining({ principal: 'worker@EXAMPLE.NET' }),
        ),
      { timeout: 5_000 },
    );
    await waitFor(() => expect(field.value).toBe('worker@EXAMPLE.NET'), {
      timeout: 5_000,
    });
    expect((screen.getByRole('button', { name: /^Save$/i }) as HTMLButtonElement).disabled).toBe(
      true,
    );
  });
});

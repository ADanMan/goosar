import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

import type { KerberosPreferences } from '../../../shared/kerberos-preferences-types';

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (s: { user: { email: string } }) => unknown) =>
    selector({ user: { email: 'worker@goosar.example' } }),
}));

import { useKerberosPreferences } from './use-kerberos-principal';

const STORED: KerberosPreferences = {
  principal: null,
  thresholdMs: 30 * 60_000,
  snoozedUntil: null,
};

function installApi() {
  let preferences = { ...STORED };
  const api = {
    kerberosGetPreferences: vi.fn(async () => preferences),
    kerberosSetPreferences: vi.fn(async (next: KerberosPreferences) => {
      preferences = next;
      return preferences;
    }),
  };
  Object.defineProperty(window, 'daemonAPI', { configurable: true, value: api });
  return api;
}

beforeEach(() => vi.clearAllMocks());

describe('useKerberosPreferences', () => {
  it('shows a save made by one surface to every other one', async () => {
    installApi();
    const settings = renderHook(() => useKerberosPreferences());
    const sidebar = renderHook(() => useKerberosPreferences());
    await waitFor(() => expect(settings.result.current.loaded).toBe(true));

    await act(async () => {
      await settings.result.current.save({
        principal: 'other.name@CORP.EXAMPLE',
      });
    });

    expect(sidebar.result.current.preferences.principal).toBe('other.name@CORP.EXAMPLE');
  });

  it('keeps the defaults when the bridge has no such method', async () => {
    Object.defineProperty(window, 'daemonAPI', {
      configurable: true,
      value: {},
    });
    const { result } = renderHook(() => useKerberosPreferences());

    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.preferences.thresholdMs).toBe(30 * 60_000);
  });
});

describe('saveKerberosPrincipalOverride', () => {
  it('reports failure when main stored something other than what was sent', async () => {
    Object.defineProperty(window, 'daemonAPI', {
      configurable: true,
      value: {
        kerberosGetPreferences: vi.fn(async () => ({ ...STORED })),
        kerberosSetPreferences: vi.fn(async (next: KerberosPreferences) => ({
          ...next,
          principal: null,
        })),
      },
    });
    const { saveKerberosPrincipalOverride } = await import('./use-kerberos-principal');
    expect(await saveKerberosPrincipalOverride('worker@EXAMPLE.NET')).toBe(false);
  });
});

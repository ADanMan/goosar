import { describe, expect, it, vi } from 'vitest';
import { render } from '@testing-library/react';
import enSettings from '@goosar/views/locales/en/settings.json';
import {
  evaluateLocalProxyCheck,
  evaluateReachabilityCheck,
  evaluateRouteCheck,
  type PerimeterCheck,
  type PerimeterStateView,
} from '../../../shared/perimeter-config';
import type { ProxyRoute } from '../../../shared/system-proxy';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));
vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (s: { user: null }) => unknown) => selector({ user: null }),
}));

vi.mock('@goosar/views/i18n', () => ({
  useT: () => ({
    t: (selector: (resources: typeof enSettings) => string) => selector(enSettings),
  }),
  useUiLocale: () => 'en',
}));

import { NetworkStatusPanel } from './network-status';

function stateWith(checks: PerimeterCheck[]): PerimeterStateView {
  return {
    localProxyUrl: 'http://127.0.0.1:3128',
    caBundlePresent: true,
    corpCaPresent: true,
    kerberosTicket: 'valid',
    kerberosExpiresAt: null,
    kerberosSupported: false,
    realm: null,
    live: { overall: 'fail', checkedAt: 1, checks },
  };
}

function addressTextOf(check: PerimeterCheck): string {
  const { container, unmount } = render(
    <NetworkStatusPanel state={stateWith([check])} busy={false} onRecheck={vi.fn()} />,
  );
  const row = container.querySelector(`[data-check-id="${check.id}"]`);
  const text = row?.querySelectorAll('span')[2]?.textContent ?? '';
  unmount();
  return text;
}

function reasonTextOf(check: PerimeterCheck): string {
  const { container, unmount } = render(
    <NetworkStatusPanel state={stateWith([check])} busy={false} onRecheck={vi.fn()} />,
  );
  const row = container.querySelector(`[data-check-id="${check.id}"]`);
  const text = row?.querySelectorAll('span')[1]?.textContent ?? '';
  unmount();
  return text;
}

const GENERIC_FALLBACK = enSettings.desktop.perimeter.live_overall_unknown;

describe('network status reasons', () => {
  it('distinguishes a silent local proxy, a dead upstream, and a dead server', () => {
    const notListening = evaluateLocalProxyCheck({
      host: '127.0.0.1',
      port: 3128,
      listening: false,
      upstreamWorks: false,
    });
    const upstreamDead = evaluateLocalProxyCheck({
      host: '127.0.0.1',
      port: 3128,
      listening: true,
      upstreamWorks: false,
    });
    const serverDead = evaluateReachabilityCheck('server', {
      kind: 'unreachable',
    });

    const texts = [notListening, upstreamDead, serverDead].map(reasonTextOf);

    expect(new Set(texts).size).toBe(3);
    for (const text of texts) {
      expect(text).not.toBe(GENERIC_FALLBACK);
      expect(text.length).toBeGreaterThan(0);
    }
  });

  it('says the system offered several routes rather than resolving one', () => {
    const proxyRoute: ProxyRoute = {
      kind: 'proxy',
      host: 'isa.corp',
      port: 8080,
      scheme: 'http',
    };
    const listed = evaluateRouteCheck('route_server', {
      host: 'goosar.ru',
      route: proxyRoute,
      candidates: [
        proxyRoute,
        { kind: 'proxy', host: 'backup.corp', port: 8080, scheme: 'http' },
        { kind: 'direct' },
      ],
    });
    const single = evaluateRouteCheck('route_server', {
      host: 'goosar.ru',
      route: proxyRoute,
      candidates: [proxyRoute],
    });

    expect(reasonTextOf(listed)).toBe(enSettings.desktop.perimeter.route_proxy_list);
    expect(reasonTextOf(listed)).not.toBe(reasonTextOf(single));
    expect(reasonTextOf(listed)).not.toBe(GENERIC_FALLBACK);
    expect(addressTextOf(listed)).toBe('http://isa.corp:8080, http://backup.corp:8080, DIRECT');
  });

  it('says a missing local proxy is missing, not merely unavailable', () => {
    const executableMissing = evaluateLocalProxyCheck(
      { host: '127.0.0.1', port: 3128, listening: false, upstreamWorks: false },
      true,
    );

    expect(executableMissing.reasonCode).toBe('live_px_executable_missing');
    expect(reasonTextOf(executableMissing)).toBe(
      enSettings.desktop.perimeter.live_px_executable_missing,
    );
  });

  it('names principal_mismatch and the outside-perimeter unconfirmed state', () => {
    const mismatch: PerimeterCheck = {
      id: 'kerberos',
      state: 'fail',
      reasonCode: 'principal_mismatch',
    };
    const outsideUnconfirmed: PerimeterCheck = {
      id: 'kerberos',
      state: 'unknown',
      reasonCode: 'ticket_valid_unconfirmed_outside_perimeter',
    };

    expect(reasonTextOf(mismatch)).toBe(enSettings.desktop.perimeter.principal_mismatch);
    expect(reasonTextOf(outsideUnconfirmed)).toBe(
      enSettings.desktop.perimeter.ticket_valid_unconfirmed_outside_perimeter,
    );
    for (const text of [reasonTextOf(mismatch), reasonTextOf(outsideUnconfirmed)]) {
      expect(text).not.toBe(GENERIC_FALLBACK);
      expect(text.length).toBeGreaterThan(0);
    }
  });
});

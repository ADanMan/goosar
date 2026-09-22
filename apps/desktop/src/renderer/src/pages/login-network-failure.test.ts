import { describe, expect, it, vi } from 'vitest';
import type { PerimeterCheck, PerimeterStateView } from '../../../shared/perimeter-config';

const mocks = vi.hoisted(() => {
  class MockApiError extends Error {
    readonly status: number;
    constructor(message: string, status: number) {
      super(message);
      this.name = 'ApiError';
      this.status = status;
    }
  }
  return { MockApiError };
});

vi.mock('@goosar/core/api', () => ({ ApiError: mocks.MockApiError }));

import {
  classifyLoginTransportFailure,
  shouldRevealDiagnostics,
  goosarRouteCheck,
} from './login-network-failure';

function stateWith(checks: PerimeterCheck[]): PerimeterStateView {
  return {
    localProxyUrl: 'http://127.0.0.1:3128',
    caBundlePresent: true,
    corpCaPresent: true,
    kerberosTicket: 'valid',
    kerberosExpiresAt: null,
    kerberosSupported: true,
    realm: null,
    live: { overall: 'fail', checks, checkedAt: 0 },
  };
}

const VIA_PROXY: PerimeterCheck = {
  id: 'route_server',
  state: 'ok',
  reasonCode: 'route_proxy',
  detail: 'http://isa.corp:8080',
};
const DIRECT: PerimeterCheck = {
  id: 'route_server',
  state: 'ok',
  reasonCode: 'route_direct',
  detail: 'goosar.acme.test',
};
const UNRESOLVED: PerimeterCheck = {
  id: 'route_server',
  state: 'fail',
  reasonCode: 'route_unknown',
  detail: 'goosar.acme.test',
};

const UNCONFIGURED: PerimeterCheck = {
  id: 'route_server',
  state: 'unknown',
  reasonCode: 'route_unconfigured',
};
const VIA_PROXY_WITH_CREDENTIALS: PerimeterCheck = {
  id: 'route_server',
  state: 'ok',
  reasonCode: 'route_proxy',
  detail: 'http://alice:s3cret@isa.corp:8080',
};

const TUNNEL_DEAD = new TypeError('Failed to fetch: net::ERR_TUNNEL_CONNECTION_FAILED');
const PLAIN_TRANSPORT = new TypeError('Failed to fetch');
const OFFLINE = new TypeError('Failed to fetch: net::ERR_INTERNET_DISCONNECTED');
const NETWORK_CHANGED = new TypeError('Failed to fetch: net::ERR_NETWORK_CHANGED');
const NAME_DEAD = new TypeError('Failed to fetch: net::ERR_NAME_NOT_RESOLVED');
const NETWORK_IO_SUSPENDED = new TypeError('Failed to fetch: net::ERR_NETWORK_IO_SUSPENDED');
const PAC_DEAD = new TypeError('Failed to fetch: net::ERR_PAC_STATUS_NOT_OK');
const PROGRAM_BUG = new TypeError("Cannot read properties of undefined (reading 'id')");

describe('goosarRouteCheck', () => {
  it("reads the goosar route, not the model gateway's", () => {
    const llm: PerimeterCheck = {
      id: 'route_llm',
      state: 'ok',
      reasonCode: 'route_direct',
    };

    expect(goosarRouteCheck(stateWith([llm, VIA_PROXY]))).toEqual(VIA_PROXY);
  });

  it('has nothing to read before the first status arrives', () => {
    expect(goosarRouteCheck(null)).toBeNull();
  });

  it('survives a live status that carries no checks', () => {
    const malformed = {
      ...stateWith([]),
      live: { overall: 'fail', checkedAt: 0 },
    } as unknown as PerimeterStateView;

    expect(goosarRouteCheck(malformed)).toBeNull();
  });
});

describe('classifyLoginTransportFailure', () => {
  it('says nothing when the server answered with a status', () => {
    expect(
      classifyLoginTransportFailure(new mocks.MockApiError('Rate limited', 429), VIA_PROXY),
    ).toBeNull();
    expect(
      classifyLoginTransportFailure(new mocks.MockApiError('Boom', 500), UNRESOLVED),
    ).toBeNull();
  });

  it('names the proxy when the system routes this address through one', () => {
    expect(classifyLoginTransportFailure(TUNNEL_DEAD, VIA_PROXY)).toEqual({
      kind: 'proxy',
      proxy: 'http://isa.corp:8080',
    });
  });

  it('names the proxy for a bare transport failure too', () => {
    expect(classifyLoginTransportFailure(PLAIN_TRANSPORT, VIA_PROXY)).toEqual({
      kind: 'proxy',
      proxy: 'http://isa.corp:8080',
    });
  });

  it('reports an unresolved route as unresolved', () => {
    expect(classifyLoginTransportFailure(PLAIN_TRANSPORT, UNRESOLVED)).toEqual({
      kind: 'route_unknown',
    });
  });

  it('keeps the plain server verdict on a resolved direct route', () => {
    expect(classifyLoginTransportFailure(PLAIN_TRANSPORT, DIRECT)).toEqual({
      kind: 'direct',
    });
  });

  it('does not claim a direct route while the route is still unknown', () => {
    expect(classifyLoginTransportFailure(PLAIN_TRANSPORT, null)).toEqual({
      kind: 'route_unknown',
    });
    expect(classifyLoginTransportFailure(PLAIN_TRANSPORT, UNCONFIGURED)).toEqual({
      kind: 'route_unknown',
    });
  });

  it('does not blame a remembered proxy when the machine is offline', () => {
    expect(classifyLoginTransportFailure(OFFLINE, VIA_PROXY)).toEqual({
      kind: 'offline',
    });
  });

  it('does not blame a remembered proxy when this machine reports no network', () => {
    const verdict = classifyLoginTransportFailure(PLAIN_TRANSPORT, VIA_PROXY, true);

    expect(verdict).toEqual({ kind: 'offline' });
    expect(JSON.stringify(verdict)).not.toContain('isa.corp');
  });

  it('tells a network that changed mid-request from a network that is gone', () => {
    for (const err of [NETWORK_CHANGED, NETWORK_IO_SUSPENDED]) {
      const verdict = classifyLoginTransportFailure(err, VIA_PROXY);

      expect(verdict).toEqual({ kind: 'network_changed' });
      expect(JSON.stringify(verdict)).not.toContain('isa.corp');
    }
  });

  it('reads a PAC failure as an unresolved route, not as a proxy hop', () => {
    expect(classifyLoginTransportFailure(PAC_DEAD, DIRECT)).toEqual({
      kind: 'route_unknown',
    });
    expect(
      classifyLoginTransportFailure(
        new TypeError('net::ERR_MANDATORY_PROXY_CONFIGURATION_FAILED'),
        null,
      ),
    ).toEqual({ kind: 'route_unknown' });
  });

  it('does not read a bug in our own code as a dead route', () => {
    expect(classifyLoginTransportFailure(PROGRAM_BUG, VIA_PROXY)).toBeNull();
    expect(
      classifyLoginTransportFailure(
        new TypeError("undefined is not an object (evaluating 'user.id')"),
        VIA_PROXY,
      ),
    ).toBeNull();
  });

  it('does not blame a proxy when a host name would not resolve', () => {
    expect(classifyLoginTransportFailure(NAME_DEAD, VIA_PROXY)).toEqual({
      kind: 'name_unresolved',
    });
  });

  it('refuses to print a proxy address that carries credentials', () => {
    const verdict = classifyLoginTransportFailure(TUNNEL_DEAD, VIA_PROXY_WITH_CREDENTIALS);

    expect(verdict).toEqual({ kind: 'proxy_unnamed' });
    expect(JSON.stringify(verdict)).not.toContain('s3cret');
  });

  it('names a proxy whose credential has already been replaced by the marker', () => {
    expect(
      classifyLoginTransportFailure(TUNNEL_DEAD, {
        id: 'route_server',
        state: 'ok',
        reasonCode: 'route_proxy',
        detail: 'http://***@isa.corp:8080',
      }),
    ).toEqual({ kind: 'proxy', proxy: 'http://***@isa.corp:8080' });
  });

  it('classifies transport failures that arrive as flattened strings', () => {
    for (const message of [
      'connect ECONNREFUSED 10.0.0.5:443',
      'socket hang up',
      'connect ETIMEDOUT 10.0.0.5:443',
      'NetworkError when attempting to fetch resource.',
    ]) {
      expect(classifyLoginTransportFailure(new Error(message), VIA_PROXY)).toEqual({
        kind: 'proxy',
        proxy: 'http://isa.corp:8080',
      });
    }
    expect(
      classifyLoginTransportFailure(new Error('getaddrinfo ENOTFOUND goosar.acme.test'), VIA_PROXY),
    ).toEqual({ kind: 'name_unresolved' });
  });

  it('trusts a proxy error code over a route snapshot that says direct', () => {
    expect(classifyLoginTransportFailure(TUNNEL_DEAD, DIRECT)).toEqual({
      kind: 'proxy_unnamed',
    });
    expect(
      classifyLoginTransportFailure(new TypeError('net::ERR_PROXY_CONNECTION_FAILED'), null),
    ).toEqual({ kind: 'proxy_unnamed' });
  });

  it('falls back to an unnamed proxy when the route has no address', () => {
    expect(
      classifyLoginTransportFailure(PLAIN_TRANSPORT, {
        id: 'route_server',
        state: 'ok',
        reasonCode: 'route_proxy',
      }),
    ).toEqual({ kind: 'proxy_unnamed' });
  });
});

describe('classifyLoginTransportFailure — several offered paths', () => {
  const VIA_LIST: PerimeterCheck = {
    id: 'route_server',
    state: 'ok',
    reasonCode: 'route_proxy_list',
    detail: 'http://isa.corp:8080, http://backup.corp:8080, DIRECT',
  };

  it('reports every path the system offered instead of convicting the first', () => {
    expect(classifyLoginTransportFailure(TUNNEL_DEAD, VIA_LIST)).toEqual({
      kind: 'proxy_candidates',
      proxies: ['http://isa.corp:8080', 'http://backup.corp:8080'],
      direct: true,
    });
  });

  it('reports a list with no direct fallback as such', () => {
    expect(
      classifyLoginTransportFailure(PLAIN_TRANSPORT, {
        ...VIA_LIST,
        detail: 'http://isa.corp:8080, http://backup.corp:8080',
      }),
    ).toEqual({
      kind: 'proxy_candidates',
      proxies: ['http://isa.corp:8080', 'http://backup.corp:8080'],
      direct: false,
    });
  });

  it('prints a redacted address and never a credential', () => {
    const verdict = classifyLoginTransportFailure(PLAIN_TRANSPORT, {
      ...VIA_LIST,
      detail: 'http://***@isa.corp:8080, DIRECT',
    });

    expect(verdict).toEqual({
      kind: 'proxy_candidates',
      proxies: ['http://***@isa.corp:8080'],
      direct: true,
    });
    expect(
      classifyLoginTransportFailure(PLAIN_TRANSPORT, {
        ...VIA_LIST,
        detail: 'http://alice:s3cret@isa.corp:8080, DIRECT',
      }),
    ).toEqual({ kind: 'route_unknown' });
  });

  it('names no hop when the offered list cannot be read', () => {
    expect(
      classifyLoginTransportFailure(PLAIN_TRANSPORT, {
        ...VIA_LIST,
        detail: 'something, else',
      }),
    ).toEqual({ kind: 'route_unknown' });
  });

  it('opens the diagnostics panel for an offered list, like any route break', () => {
    expect(
      shouldRevealDiagnostics({
        kind: 'proxy_candidates',
        proxies: ['http://isa.corp:8080'],
        direct: true,
      }),
    ).toBe(true);
  });
});

describe('shouldRevealDiagnostics', () => {
  it('opens the panel for every route-shaped break', () => {
    expect(shouldRevealDiagnostics({ kind: 'proxy', proxy: 'http://p:8080' })).toBe(true);
    expect(shouldRevealDiagnostics({ kind: 'proxy_unnamed' })).toBe(true);
    expect(shouldRevealDiagnostics({ kind: 'route_unknown' })).toBe(true);
  });

  it('opens the panel when the network moved under the request', () => {
    expect(shouldRevealDiagnostics({ kind: 'network_changed' })).toBe(true);
  });

  it('leaves it collapsed for a plain dead server or a served error', () => {
    expect(shouldRevealDiagnostics({ kind: 'direct' })).toBe(false);
    expect(shouldRevealDiagnostics(null)).toBe(false);
  });

  it('leaves it collapsed when the break is not about the route', () => {
    expect(shouldRevealDiagnostics({ kind: 'offline' })).toBe(false);
    expect(shouldRevealDiagnostics({ kind: 'name_unresolved' })).toBe(false);
  });
});

import { describe, it, expect } from 'vitest';
import {
  evaluateRouteCheck,
  evaluateLocalProxyCheck,
  evaluateEntryCheck,
} from './perimeter-config';
import type { ProxyRoute } from './system-proxy';

const proxyRoute: ProxyRoute = {
  kind: 'proxy',
  host: 'isa.corp',
  port: 8080,
  scheme: 'http',
};

describe('evaluateRouteCheck', () => {
  it('names the proxy the system resolved', () => {
    const check = evaluateRouteCheck('route_server', {
      host: 'goosar.ru',
      route: proxyRoute,
    });

    expect(check).toMatchObject({
      id: 'route_server',
      state: 'ok',
      reasonCode: 'route_proxy',
      detail: 'http://isa.corp:8080',
    });
  });

  it('reports a resolved direct route as a resolved answer, not as a blank', () => {
    expect(
      evaluateRouteCheck('route_llm', {
        host: 'gw.internal',
        route: { kind: 'direct' },
      }),
    ).toMatchObject({ state: 'ok', reasonCode: 'route_direct' });
  });

  it('fails loudly when the route is unknown', () => {
    expect(
      evaluateRouteCheck('route_server', {
        host: 'goosar.ru',
        route: { kind: 'unknown' },
      }),
    ).toMatchObject({ state: 'fail', reasonCode: 'route_unknown' });
  });

  it('stays unknown, not failed, when there is no address to resolve', () => {
    expect(
      evaluateRouteCheck('route_llm', { host: null, route: { kind: 'unknown' } }),
    ).toMatchObject({ state: 'unknown', reasonCode: 'route_unconfigured' });
  });

  it('names every route the system offered, not only the one it forwards', () => {
    const check = evaluateRouteCheck('route_server', {
      host: 'goosar.ru',
      route: proxyRoute,
      candidates: [
        proxyRoute,
        { kind: 'proxy', host: 'backup.corp', port: 8080, scheme: 'http' },
        { kind: 'direct' },
      ],
    });

    expect(check).toMatchObject({
      id: 'route_server',
      state: 'ok',
      reasonCode: 'route_proxy_list',
      detail: 'http://isa.corp:8080, http://backup.corp:8080, DIRECT',
    });
  });

  it('keeps the single-route wording when the system offered exactly one', () => {
    expect(
      evaluateRouteCheck('route_server', {
        host: 'goosar.ru',
        route: proxyRoute,
        candidates: [proxyRoute],
      }),
    ).toMatchObject({ reasonCode: 'route_proxy', detail: 'http://isa.corp:8080' });
  });

  it('still fails when the route it would forward is unknown', () => {
    expect(
      evaluateRouteCheck('route_server', {
        host: 'goosar.ru',
        route: { kind: 'unknown' },
        candidates: [{ kind: 'unknown' }, { kind: 'direct' }],
      }),
    ).toMatchObject({ state: 'fail', reasonCode: 'route_unknown' });
  });

  it('prints the offered addresses without their credentials', () => {
    const check = evaluateRouteCheck('route_server', {
      host: 'goosar.ru',
      route: { ...proxyRoute, userinfo: 'alice:s3cret' },
      candidates: [{ ...proxyRoute, userinfo: 'alice:s3cret' }, { kind: 'direct' }],
    });

    expect(check.detail).toBe('http://***@isa.corp:8080, DIRECT');
    expect(JSON.stringify(check)).not.toContain('s3cret');
  });
});

describe('evaluateLocalProxyCheck', () => {
  const px = { host: '127.0.0.1', port: 3128 };

  it('passes only when a request actually got through', () => {
    expect(evaluateLocalProxyCheck({ ...px, listening: true, upstreamWorks: true })).toMatchObject({
      state: 'ok',
      reasonCode: 'live_px_ok',
    });
  });

  it('separates a dead upstream from a proxy that is not there at all', () => {
    expect(evaluateLocalProxyCheck({ ...px, listening: true, upstreamWorks: false })).toMatchObject(
      { state: 'fail', reasonCode: 'live_px_upstream_dead' },
    );
    expect(
      evaluateLocalProxyCheck({ ...px, listening: false, upstreamWorks: false }),
    ).toMatchObject({ state: 'fail', reasonCode: 'live_px_unreachable' });
  });

  it('calls out a local proxy that is not bound to the loopback', () => {
    expect(
      evaluateLocalProxyCheck({
        host: '0.0.0.0',
        port: 3128,
        listening: true,
        upstreamWorks: true,
      }),
    ).toMatchObject({ state: 'fail', reasonCode: 'live_px_not_loopback' });
  });

  it('reports no local proxy as unconfigured rather than broken', () => {
    expect(evaluateLocalProxyCheck(null)).toMatchObject({
      state: 'unknown',
      reasonCode: 'live_px_unconfigured',
    });
  });
});

describe('evaluateEntryCheck', () => {
  it('names where subprocesses enter the network', () => {
    expect(evaluateEntryCheck({ kind: 'system', route: proxyRoute }, null)).toMatchObject({
      id: 'entry',
      state: 'ok',
      reasonCode: 'entry_system_proxy',
      detail: 'http://isa.corp:8080',
    });
    expect(
      evaluateEntryCheck({ kind: 'loopback_proxy', host: '127.0.0.1', port: 3128 }, null),
    ).toMatchObject({ reasonCode: 'entry_local_proxy' });
  });

  it('reports the two-proxy conflict as its own state', () => {
    expect(
      evaluateEntryCheck(
        { kind: 'system', route: proxyRoute },
        {
          routes: ['http://isa.corp:8080', 'http://other.corp:3128'],
        },
      ),
    ).toMatchObject({ state: 'fail', reasonCode: 'entry_conflict' });
  });

  it('fails when no route is known at all', () => {
    expect(evaluateEntryCheck({ kind: 'system', route: { kind: 'unknown' } }, null)).toMatchObject({
      state: 'fail',
      reasonCode: 'entry_unknown',
    });
  });
});

import { describe, it, expect } from 'vitest';
import {
  parseResolvedProxy,
  parseResolvedProxyList,
  routeAddress,
  isLoopbackHost,
  buildSubprocessProxyEnv,
  chooseSubprocessEntry,
  buildEntryProxyEnv,
  redactAddressCredentials,
  type ProxyRoute,
  type ProxyScheme,
  proxyProbeCarriedRequest,
  classifyProxyProbe,
} from './system-proxy';

const proxy = (host: string, port: number, scheme: ProxyScheme = 'http'): ProxyRoute => ({
  kind: 'proxy',
  host,
  port,
  scheme,
});

describe('parseResolvedProxy', () => {
  it('reads DIRECT as a direct route', () => {
    expect(parseResolvedProxy('DIRECT')).toEqual({ kind: 'direct' });
  });

  it('reads the first entry of a PAC list', () => {
    expect(parseResolvedProxy('PROXY isa.corp:8080;DIRECT')).toEqual(proxy('isa.corp', 8080));
  });

  it('keeps the scheme of a SOCKS route', () => {
    expect(routeAddress(parseResolvedProxy('SOCKS5 gw.corp:1080'))).toBe('socks5://gw.corp:1080');
  });

  it('reads an empty or unparseable answer as unknown, never as direct', () => {
    expect(parseResolvedProxy('')).toEqual({ kind: 'unknown' });
    expect(parseResolvedProxy(null)).toEqual({ kind: 'unknown' });
    expect(parseResolvedProxy('PROXY no-port-here')).toEqual({
      kind: 'unknown',
    });
    expect(parseResolvedProxy('PROXY host:99999')).toEqual({ kind: 'unknown' });
  });
});

describe('buildSubprocessProxyEnv', () => {
  it('hands one resolved proxy to the subprocess in both env cases', () => {
    const built = buildSubprocessProxyEnv(
      [
        { host: 'goosar.ru', route: proxy('isa.corp', 8080) },
        { host: 'gw.internal', route: proxy('isa.corp', 8080) },
      ],
      [],
    );

    expect(built.env.HTTP_PROXY).toBe('http://isa.corp:8080');
    expect(built.env.https_proxy).toBe('http://isa.corp:8080');
    expect(built.conflict).toBeNull();
  });

  it('bypasses only the hosts the system resolved as direct', () => {
    const built = buildSubprocessProxyEnv(
      [
        { host: 'goosar.ru', route: proxy('isa.corp', 8080) },
        { host: 'gw.internal', route: { kind: 'direct' } },
      ],
      ['localhost'],
    );

    const bypass = built.env.NO_PROXY.split(',');
    expect(bypass).toContain('localhost');
    expect(bypass).toContain('gw.internal');
    expect(bypass).not.toContain('goosar.ru');
  });

  it('never bypasses a host whose route is unknown', () => {
    const built = buildSubprocessProxyEnv(
      [
        { host: 'goosar.ru', route: proxy('isa.corp', 8080) },
        { host: 'gw.internal', route: { kind: 'unknown' } },
      ],
      ['localhost'],
    );

    expect(built.env.NO_PROXY.split(',')).not.toContain('gw.internal');
    expect(built.unresolvedHosts).toEqual(['gw.internal']);
  });

  it('refuses to guess when two hosts need two different proxies', () => {
    const built = buildSubprocessProxyEnv(
      [
        { host: 'goosar.ru', route: proxy('isa.corp', 8080) },
        { host: 'gw.internal', route: proxy('other.corp', 3128) },
      ],
      [],
    );

    expect(built.env.HTTP_PROXY).toBeUndefined();
    expect(built.env.http_proxy).toBeUndefined();
    expect(built.conflict?.routes).toEqual(['http://isa.corp:8080', 'http://other.corp:3128']);
  });

  it('sets no proxy variables at all when the system resolved everything direct', () => {
    const built = buildSubprocessProxyEnv(
      [{ host: 'goosar.ru', route: { kind: 'direct' } }],
      ['localhost'],
    );

    expect(built.env.HTTP_PROXY).toBeUndefined();
    expect(built.env.NO_PROXY.split(',')).toContain('goosar.ru');
  });
});

describe('chooseSubprocessEntry', () => {
  const px = { host: '127.0.0.1', port: 3128 };

  it('prefers a working loopback proxy — subprocesses cannot do Negotiate', () => {
    expect(
      chooseSubprocessEntry({
        systemRoute: proxy('isa.corp', 8080),
        px: { ...px, listening: true, upstreamWorks: true },
      }),
    ).toEqual({ kind: 'loopback_proxy', host: '127.0.0.1', port: 3128 });
  });

  it('falls back to the system route when the local proxy listens but does not work', () => {
    expect(
      chooseSubprocessEntry({
        systemRoute: proxy('isa.corp', 8080),
        px: { ...px, listening: true, upstreamWorks: false },
      }),
    ).toEqual({ kind: 'system', route: proxy('isa.corp', 8080) });
  });

  it('refuses a local proxy that is not on the loopback', () => {
    expect(
      chooseSubprocessEntry({
        systemRoute: proxy('isa.corp', 8080),
        px: { host: '0.0.0.0', port: 3128, listening: true, upstreamWorks: true },
      }),
    ).toEqual({ kind: 'system', route: proxy('isa.corp', 8080) });
  });

  it('stays unknown when neither the local proxy nor the system offers a route', () => {
    expect(
      chooseSubprocessEntry({
        systemRoute: { kind: 'unknown' },
        px: null,
      }),
    ).toEqual({ kind: 'system', route: { kind: 'unknown' } });
  });
});

describe('proxyProbeCarriedRequest (#266)', () => {
  it('counts any answer the destination produced, including a refusal', () => {
    expect(proxyProbeCarriedRequest(200)).toBe(true);
    expect(proxyProbeCarriedRequest(401)).toBe(true);
    expect(proxyProbeCarriedRequest(404)).toBe(true);
  });

  it('rejects the statuses a proxy emits about a request it did not carry', () => {
    expect(proxyProbeCarriedRequest(407)).toBe(false);
    expect(proxyProbeCarriedRequest(502)).toBe(false);
    expect(proxyProbeCarriedRequest(503)).toBe(false);
    expect(proxyProbeCarriedRequest(504)).toBe(false);
  });

  it('rejects a missing status', () => {
    expect(proxyProbeCarriedRequest(0)).toBe(false);
  });
});

describe('classifyProxyProbe (#283)', () => {
  it('reads any answer from the destination as carried, refusals included', () => {
    expect(classifyProxyProbe(200)).toBe('carried');
    expect(classifyProxyProbe(401)).toBe('carried');
    expect(classifyProxyProbe(404)).toBe('carried');
  });

  it('reads 407 as the proxy rejecting the authentication', () => {
    expect(classifyProxyProbe(407)).toBe('auth_rejected');
  });

  it('reads the gateway statuses as a blocked upstream, not an auth problem', () => {
    expect(classifyProxyProbe(502)).toBe('blocked');
    expect(classifyProxyProbe(503)).toBe('blocked');
    expect(classifyProxyProbe(504)).toBe('blocked');
  });

  it('reads a missing status as no response', () => {
    expect(classifyProxyProbe(0)).toBe('no_response');
    expect(classifyProxyProbe(-1)).toBe('no_response');
  });

  it('keeps proxyProbeCarriedRequest as exactly the carried case', () => {
    for (const status of [200, 401, 404, 407, 502, 503, 504, 0]) {
      expect(proxyProbeCarriedRequest(status)).toBe(classifyProxyProbe(status) === 'carried');
    }
  });
});

describe('isLoopbackHost', () => {
  it('accepts the loopback forms and rejects everything else', () => {
    expect(isLoopbackHost('127.0.0.1')).toBe(true);
    expect(isLoopbackHost('localhost')).toBe(true);
    expect(isLoopbackHost('::1')).toBe(true);
    expect(isLoopbackHost('127.9.9.9')).toBe(true);
    expect(isLoopbackHost('0.0.0.0')).toBe(false);
    expect(isLoopbackHost('10.0.0.1')).toBe(false);
  });
});

describe('buildEntryProxyEnv', () => {
  const hostRoutes = [
    { host: 'goosar.ru', route: proxy('isa.corp', 8080) },
    { host: 'gw.internal', route: { kind: 'direct' as const } },
  ];

  it('sends everything but the direct hosts through the working local proxy', () => {
    const built = buildEntryProxyEnv(
      { kind: 'loopback_proxy', host: '127.0.0.1', port: 3128 },
      hostRoutes,
      ['localhost'],
    );

    expect(built.env.HTTP_PROXY).toBe('http://127.0.0.1:3128');
    expect(built.env.NO_PROXY.split(',')).toContain('gw.internal');
    expect(built.conflict).toBeNull();
  });

  it('dissolves a two-proxy conflict when the local proxy carries the traffic', () => {
    const built = buildEntryProxyEnv(
      { kind: 'loopback_proxy', host: '127.0.0.1', port: 3128 },
      [
        { host: 'goosar.ru', route: proxy('isa.corp', 8080) },
        { host: 'gw.internal', route: proxy('other.corp', 3128) },
      ],
      [],
    );

    expect(built.conflict).toBeNull();
    expect(built.env.HTTP_PROXY).toBe('http://127.0.0.1:3128');
  });

  it('still refuses to bypass an unresolved host when the local proxy carries the traffic', () => {
    const built = buildEntryProxyEnv(
      { kind: 'loopback_proxy', host: '127.0.0.1', port: 3128 },
      [{ host: 'gw.internal', route: { kind: 'unknown' } }],
      ['localhost'],
    );

    expect(built.env.NO_PROXY.split(',')).not.toContain('gw.internal');
    expect(built.unresolvedHosts).toEqual(['gw.internal']);
  });

  it('falls back to the system-resolved answer, conflicts included', () => {
    const built = buildEntryProxyEnv(
      { kind: 'system', route: proxy('isa.corp', 8080) },
      hostRoutes,
      ['localhost'],
    );

    expect(built.env.HTTP_PROXY).toBe('http://isa.corp:8080');
    expect(built.env.NO_PROXY.split(',')).toContain('gw.internal');
  });
});

describe('parseResolvedProxy — a PAC credential', () => {
  const CREDENTIALED = 'PROXY alice:s3cret@isa.corp:8080';

  it('keeps the credential out of the host and in a field of its own', () => {
    expect(parseResolvedProxy(CREDENTIALED)).toEqual({
      kind: 'proxy',
      host: 'isa.corp',
      port: 8080,
      scheme: 'http',
      userinfo: 'alice:s3cret',
    });
  });

  it('prints the address with a marker instead of the credential', () => {
    expect(routeAddress(parseResolvedProxy(CREDENTIALED))).toBe('http://***@isa.corp:8080');
  });

  it('prints an address that carries no credential exactly as it is', () => {
    expect(routeAddress(parseResolvedProxy('PROXY isa.corp:8080'))).toBe('http://isa.corp:8080');
  });

  it('still hands the credential to the subprocess that has to authenticate', () => {
    const built = buildSubprocessProxyEnv(
      [{ host: 'goosar.ru', route: parseResolvedProxy(CREDENTIALED) }],
      [],
    );

    expect(built.env.HTTP_PROXY).toBe('http://alice:s3cret@isa.corp:8080');
    expect(built.env.https_proxy).toBe('http://alice:s3cret@isa.corp:8080');
  });

  it('keeps the credential out of the conflict list the panel prints', () => {
    const built = buildSubprocessProxyEnv(
      [
        { host: 'goosar.ru', route: parseResolvedProxy(CREDENTIALED) },
        { host: 'gw.internal', route: proxy('other.corp', 3128) },
      ],
      [],
    );

    expect(built.conflict?.routes).toEqual(['http://***@isa.corp:8080', 'http://other.corp:3128']);
    expect(JSON.stringify(built.conflict)).not.toContain('s3cret');
  });

  it('reads a directive whose host is nothing but userinfo as unknown', () => {
    expect(parseResolvedProxy('PROXY alice:s3cret@:8080')).toEqual({
      kind: 'unknown',
    });
  });
});

describe('parseResolvedProxyList', () => {
  it('reads every entry the answer offers, in the order it offers them', () => {
    expect(parseResolvedProxyList('PROXY a.corp:8080; PROXY b.corp:8080; DIRECT')).toEqual([
      proxy('a.corp', 8080),
      proxy('b.corp', 8080),
      { kind: 'direct' },
    ]);
  });

  it('agrees with parseResolvedProxy about the first entry', () => {
    for (const answer of [
      'DIRECT',
      'PROXY isa.corp:8080;DIRECT',
      'PROXY no-port-here;DIRECT',
      '',
    ]) {
      expect(parseResolvedProxyList(answer)[0] ?? { kind: 'unknown' }).toEqual(
        parseResolvedProxy(answer),
      );
    }
  });

  it('is empty when the answer offers nothing at all', () => {
    expect(parseResolvedProxyList('')).toEqual([]);
    expect(parseResolvedProxyList(null)).toEqual([]);
    expect(parseResolvedProxyList(';;  ;')).toEqual([]);
  });

  it('stops reading a pathological answer', () => {
    const answer = Array.from({ length: 40 }, (_, index) => `PROXY p${index}.corp:8080`).join(';');

    expect(parseResolvedProxyList(answer)).toHaveLength(8);
  });
});

describe('redactAddressCredentials', () => {
  it('replaces userinfo with a marker and keeps the address', () => {
    expect(redactAddressCredentials('http://alice:s3cret@isa.corp:8080')).toBe(
      'http://***@isa.corp:8080',
    );
  });

  it('leaves an address that carries no credential exactly as it was', () => {
    expect(redactAddressCredentials('http://isa.corp:8080')).toBe('http://isa.corp:8080');
  });

  it('redacts a credential that contains a sub-delimiter', () => {
    expect(redactAddressCredentials('http://alice:pa,ss@isa.corp:8080')).toBe(
      'http://***@isa.corp:8080',
    );
  });

  it('redacts a credential that contains characters no one expected', () => {
    for (const password of [
      'pa/ss',
      'pa//ss',
      'pa?ss',
      'pa#ss',
      'pa[ss]',
      'pa;ss',
      'p@ss',
      'pa\\ss',
    ]) {
      const redacted = redactAddressCredentials(`http://alice:${password}@isa.corp:8080`);

      expect(redacted).toBe('http://***@isa.corp:8080');
      expect(redacted).not.toContain('ss');
    }
  });

  it('redacts each address in a joined list on its own', () => {
    expect(redactAddressCredentials('http://isa.corp:8080, http://bob:pw@gw.internal:3128')).toBe(
      'http://isa.corp:8080, http://***@gw.internal:3128',
    );
  });
});

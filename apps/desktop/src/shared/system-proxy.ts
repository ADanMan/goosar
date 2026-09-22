// Маршрут, который операционная система выбрала для адреса. Заменяет прежний
// переключатель «Контур» с жёстко заданным локальным прокси: теперь демон, CLI
// агентов и рендерер следуют системным настройкам прокси.

export type ProxyScheme = 'http' | 'https' | 'socks4' | 'socks5';

export type ProxyRoute =
  | { kind: 'direct' }
  | {
      kind: 'proxy';
      host: string;
      port: number;
      scheme: ProxyScheme;
      userinfo?: string;
    }
  | { kind: 'unknown' };

const PAC_SCHEMES: Readonly<Record<string, ProxyScheme>> = Object.freeze({
  PROXY: 'http',
  HTTP: 'http',
  HTTPS: 'https',
  SOCKS: 'socks4',
  SOCKS4: 'socks4',
  SOCKS5: 'socks5',
});

const MAX_PORT = 65535;

const MAX_ROUTE_CANDIDATES = 8;

function parsePacEntry(entry: string): ProxyRoute {
  const [verbRaw, ...rest] = entry.split(/\s+/);
  const verb = (verbRaw ?? '').toUpperCase();
  if (verb === 'DIRECT') return { kind: 'direct' };

  const scheme = PAC_SCHEMES[verb];
  const endpoint = rest.join('');
  if (!scheme || endpoint.length === 0) return { kind: 'unknown' };

  const colon = endpoint.lastIndexOf(':');
  if (colon <= 0) return { kind: 'unknown' };
  const authority = endpoint.slice(0, colon);
  const port = Number(endpoint.slice(colon + 1));
  if (!Number.isInteger(port) || port <= 0 || port > MAX_PORT) {
    return { kind: 'unknown' };
  }

  const at = authority.lastIndexOf('@');
  const userinfo = at >= 0 ? authority.slice(0, at) : '';
  const host = at >= 0 ? authority.slice(at + 1) : authority;
  if (host.length === 0) return { kind: 'unknown' };

  return userinfo.length > 0
    ? { kind: 'proxy', host, port, scheme, userinfo }
    : { kind: 'proxy', host, port, scheme };
}

export function parseResolvedProxyList(result: string | null | undefined): ProxyRoute[] {
  if (typeof result !== 'string') return [];
  return result
    .split(';')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0)
    .slice(0, MAX_ROUTE_CANDIDATES)
    .map(parsePacEntry);
}

export function parseResolvedProxy(result: string | null | undefined): ProxyRoute {
  return parseResolvedProxyList(result)[0] ?? { kind: 'unknown' };
}

const CREDENTIAL_MARKER = '***';

function routeTransportUrl(route: ProxyRoute): string | null {
  if (route.kind !== 'proxy') return null;
  const credential = route.userinfo ? `${route.userinfo}@` : '';
  return `${route.scheme}://${credential}${route.host}:${route.port}`;
}

export function routeAddress(route: ProxyRoute): string | null {
  if (route.kind !== 'proxy') return null;
  const marker = route.userinfo ? `${CREDENTIAL_MARKER}@` : '';
  return `${route.scheme}://${marker}${route.host}:${route.port}`;
}

const USERINFO_IN_AUTHORITY = /([a-z][a-z0-9+.-]*:\/\/)(?:(?!:\/\/)\S)*@/gi;

export function redactAddressCredentials(detail: string): string {
  return detail.replace(USERINFO_IN_AUTHORITY, '$1***@');
}

export type ProxyProbeVerdict = 'carried' | 'auth_rejected' | 'blocked' | 'no_response' | 'not_run';

export function classifyProxyProbe(
  status: number,
): 'carried' | 'auth_rejected' | 'blocked' | 'no_response' {
  if (status === 407) return 'auth_rejected';
  if (status === 502 || status === 503 || status === 504) return 'blocked';
  return status > 0 ? 'carried' : 'no_response';
}

export function proxyProbeCarriedRequest(status: number): boolean {
  return classifyProxyProbe(status) === 'carried';
}

export function isLoopbackHost(host: string): boolean {
  const trimmed = host
    .trim()
    .toLowerCase()
    .replace(/^\[|\]$/g, '');
  if (trimmed === 'localhost' || trimmed === '::1') return true;
  return /^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(trimmed);
}

export interface HostRoute {
  host: string;
  route: ProxyRoute;
}

export interface SubprocessProxyEnv {
  env: Record<string, string>;
  unresolvedHosts: string[];
  conflict: { routes: string[] } | null;
}

export function buildSubprocessProxyEnv(
  hostRoutes: readonly HostRoute[],
  noProxyBase: readonly string[],
): SubprocessProxyEnv {
  const bypass = [...noProxyBase];
  const unresolvedHosts: string[] = [];
  const proxyUrls: string[] = [];
  const proxyAddresses: string[] = [];

  for (const { host, route } of hostRoutes) {
    if (route.kind === 'direct') {
      if (host.length > 0 && !bypass.includes(host)) bypass.push(host);
      continue;
    }
    if (route.kind === 'unknown') {
      if (host.length > 0 && !unresolvedHosts.includes(host)) {
        unresolvedHosts.push(host);
      }
      continue;
    }
    const url = routeTransportUrl(route);
    if (url && !proxyUrls.includes(url)) {
      proxyUrls.push(url);
      const address = routeAddress(route);
      if (address && !proxyAddresses.includes(address)) {
        proxyAddresses.push(address);
      }
    }
  }

  const env: Record<string, string> = {};
  const noProxy = bypass.join(',');
  if (noProxy.length > 0) {
    env.NO_PROXY = noProxy;
    env.no_proxy = noProxy;
  }

  if (proxyUrls.length > 1) {
    return {
      env,
      unresolvedHosts,
      conflict: { routes: [...proxyAddresses].sort() },
    };
  }
  const proxyUrl = proxyUrls[0];
  if (proxyUrl) {
    env.HTTP_PROXY = proxyUrl;
    env.HTTPS_PROXY = proxyUrl;
    env.http_proxy = proxyUrl;
    env.https_proxy = proxyUrl;
  }
  return { env, unresolvedHosts, conflict: null };
}

export interface LocalProxyFacts {
  host: string;
  port: number;
  listening: boolean;
  upstreamWorks: boolean;
  probe?: ProxyProbeVerdict;
}

export type SubprocessEntry =
  { kind: 'loopback_proxy'; host: string; port: number } | { kind: 'system'; route: ProxyRoute };

export function chooseSubprocessEntry(input: {
  systemRoute: ProxyRoute;
  px: LocalProxyFacts | null;
}): SubprocessEntry {
  const { px } = input;
  if (
    px &&
    px.listening &&
    px.upstreamWorks &&
    isLoopbackHost(px.host) &&
    Number.isInteger(px.port) &&
    px.port > 0 &&
    px.port <= MAX_PORT
  ) {
    return { kind: 'loopback_proxy', host: px.host, port: px.port };
  }
  return { kind: 'system', route: input.systemRoute };
}

export function buildEntryProxyEnv(
  entry: SubprocessEntry,
  hostRoutes: readonly HostRoute[],
  noProxyBase: readonly string[],
): SubprocessProxyEnv {
  const resolved = buildSubprocessProxyEnv(hostRoutes, noProxyBase);
  if (entry.kind !== 'loopback_proxy') return resolved;

  const proxyUrl = `http://${entry.host}:${entry.port}`;
  return {
    env: {
      ...resolved.env,
      HTTP_PROXY: proxyUrl,
      HTTPS_PROXY: proxyUrl,
      http_proxy: proxyUrl,
      https_proxy: proxyUrl,
    },
    unresolvedHosts: resolved.unresolvedHosts,
    conflict: null,
  };
}

import {
  buildEntryProxyEnv,
  chooseSubprocessEntry,
  isLoopbackHost,
  parseResolvedProxy,
  parseResolvedProxyList,
  routeAddress,
  type HostRoute,
  type LocalProxyFacts,
  type ProxyProbeVerdict,
  type ProxyRoute,
  type SubprocessEntry,
} from '../shared/system-proxy';

export const LOCAL_NO_PROXY: readonly string[] = ['localhost', '127.0.0.1'];

export interface RouteTargets {
  goosarUrl: string | null;
  llmApiBase: string | null;
}

export interface RouteResolutionDeps {
  resolveProxy: (url: string) => Promise<string>;
  probeListening: (host: string, port: number) => Promise<boolean>;
  probeThroughProxy: (proxyUrl: string, targetUrl: string) => Promise<ProxyProbeVerdict>;
}

export interface ResolvedTarget {
  url: string | null;
  host: string | null;
  route: ProxyRoute;
  candidates: ProxyRoute[];
}

export interface NetworkRouteSnapshot {
  goosar: ResolvedTarget;
  llm: ResolvedTarget;
  localProxy: LocalProxyFacts | null;
  entry: SubprocessEntry;
  env: Record<string, string>;
  unresolvedHosts: string[];
  conflict: { routes: string[] } | null;
}

function hostOf(url: string | null): string | null {
  if (!url) return null;
  try {
    const host = new URL(url).hostname;
    return host.length > 0 ? host : null;
  } catch {
    return null;
  }
}

async function resolveOne(url: string | null, deps: RouteResolutionDeps): Promise<ResolvedTarget> {
  const host = hostOf(url);
  if (!url || !host) {
    return { url, host, route: { kind: 'unknown' }, candidates: [] };
  }
  try {
    const answer = await deps.resolveProxy(url);
    const candidates = parseResolvedProxyList(answer);
    return { url, host, route: parseResolvedProxy(answer), candidates };
  } catch {
    return { url, host, route: { kind: 'unknown' }, candidates: [] };
  }
}

function chooseProbeTarget(
  ...targets: readonly { url: string | null; host: string | null }[]
): string | null {
  for (const target of targets) {
    if (!target.url || !target.host) continue;
    if (isLoopbackHost(target.host)) continue;
    return target.url;
  }
  return null;
}

export async function resolveNetworkRoutes(
  targets: RouteTargets,
  localProxy: { host: string; port: number } | null,
  deps: RouteResolutionDeps,
  extraNoProxy: readonly string[] = [],
): Promise<NetworkRouteSnapshot> {
  const [goosar, llm] = await Promise.all([
    resolveOne(targets.goosarUrl, deps),
    resolveOne(targets.llmApiBase, deps),
  ]);

  let localProxyFacts: LocalProxyFacts | null = null;
  if (localProxy) {
    const listening = await deps.probeListening(localProxy.host, localProxy.port);
    const probeTarget = chooseProbeTarget(llm, goosar);
    const probe: ProxyProbeVerdict =
      listening && probeTarget
        ? await deps.probeThroughProxy(`http://${localProxy.host}:${localProxy.port}`, probeTarget)
        : 'not_run';
    localProxyFacts = {
      ...localProxy,
      listening,
      upstreamWorks: probe === 'carried',
      probe,
    };
  }

  const entry = chooseSubprocessEntry({
    systemRoute: goosar.route,
    px: localProxyFacts,
  });

  const hostRoutes: HostRoute[] = [];
  for (const target of [goosar, llm]) {
    if (target.host) hostRoutes.push({ host: target.host, route: target.route });
  }
  const bypassBase = [...LOCAL_NO_PROXY];
  for (const entryHost of extraNoProxy) {
    const trimmed = entryHost.trim();
    if (trimmed.length > 0 && !bypassBase.includes(trimmed)) {
      bypassBase.push(trimmed);
    }
  }
  const built = buildEntryProxyEnv(entry, hostRoutes, bypassBase);

  return {
    goosar,
    llm,
    localProxy: localProxyFacts,
    entry,
    env: built.env,
    unresolvedHosts: built.unresolvedHosts,
    conflict: built.conflict,
  };
}

export function describeEntry(entry: SubprocessEntry): string {
  if (entry.kind === 'loopback_proxy') {
    return `http://${entry.host}:${entry.port}`;
  }
  return routeAddress(entry.route) ?? entry.route.kind;
}

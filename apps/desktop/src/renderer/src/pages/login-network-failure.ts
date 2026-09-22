import { ApiError } from '@goosar/core/api';
import type { PerimeterCheck, PerimeterStateView } from '../../../shared/perimeter-config';

const TRANSPORT_FAILURE =
  /failed to fetch|fetch failed|network ?error|load failed|networkerror|err_[a-z_]+|econnrefused|enotfound|etimedout|socket hang up/i;

const PROGRAMMING_ERROR =
  /cannot read propert|is not a function|is not iterable|(?:undefined|null) is not an object|cannot destructure/i;

export function isServerUnreachableError(err: unknown): boolean {
  if (err instanceof ApiError) return false;
  const text = errorText(err);
  if (err instanceof TypeError) return !PROGRAMMING_ERROR.test(text);
  return TRANSPORT_FAILURE.test(text);
}

const PROXY_FAILURE_CODE =
  /\bERR_(TUNNEL_CONNECTION_FAILED|PROXY_[A-Z_]+|UNEXPECTED_PROXY_AUTH)\b/i;

export function hasProxyFailureCode(err: unknown): boolean {
  return PROXY_FAILURE_CODE.test(errorText(err));
}

const ROUTE_RESOLUTION_FAILURE_CODE = /\bERR_(PAC_[A-Z_]+|MANDATORY_PROXY_CONFIGURATION_FAILED)\b/i;

const OFFLINE_CODE = /\bERR_INTERNET_DISCONNECTED\b/i;

const NETWORK_CHANGED_CODE = /\bERR_(NETWORK_CHANGED|NETWORK_IO_SUSPENDED)\b/i;

const NAME_FAILURE_CODE = /\bERR_NAME_(NOT_RESOLVED|RESOLUTION_FAILED)\b|\bENOTFOUND\b/i;

const PRINTABLE_PROXY = /^[a-z][a-z0-9+.-]*:\/\/(?:\*\*\*@)?[^@/\\?#\s:]+:\d{1,5}$/i;

export function displayableProxyAddress(detail: string | null | undefined): string | null {
  const address = (detail ?? '').trim();
  return PRINTABLE_PROXY.test(address) ? address : null;
}

export function parseRouteCandidates(
  detail: string | null | undefined,
): { proxies: string[]; direct: boolean } | null {
  const entries = (detail ?? '')
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0);
  const proxies: string[] = [];
  let direct = false;
  let readable = 0;
  for (const entry of entries) {
    if (entry.toUpperCase() === 'DIRECT') {
      direct = true;
      readable += 1;
      continue;
    }
    const address = displayableProxyAddress(entry);
    if (address) {
      proxies.push(address);
      readable += 1;
    }
  }
  return readable > 1 ? { proxies, direct } : null;
}

export function goosarRouteCheck(
  state: PerimeterStateView | null | undefined,
): PerimeterCheck | null {
  return state?.live?.checks?.find((check) => check.id === 'route_server') ?? null;
}

export type LoginTransportFailure =
  | { kind: 'proxy'; proxy: string }
  /** A proxy is on the route, but no address for it can be shown. */
  | { kind: 'proxy_unnamed' }
  /**
   * The system offered SEVERAL paths for this address and does not report
   * which one this request took. Every path it named is carried, so the copy
   * can list them without pretending to know which one broke.
   */
  | { kind: 'proxy_candidates'; proxies: string[]; direct: boolean }
  /** Which route this address takes is not known here. */
  | { kind: 'route_unknown' }
  /** This machine reports no usable network at all. */
  | { kind: 'offline' }
  /** The network configuration moved while the request was in flight. */
  | { kind: 'network_changed' }
  /** A host name could not be resolved on this machine. */
  | { kind: 'name_unresolved' }
  /**
   * A route the system RESOLVED as direct: the server is the hop that did not
   * answer. Only a resolved direct route lands here. An absent snapshot (the
   * status arrives over IPC a second or two after the screen mounts, and that
   * race is the 2026-08-28 case) or an unconfigured one is `route_unknown`,
   * because "we were not told" is not the same fact as "the system chose
   * direct" — and only the second one licenses blaming the server.
   */
  | { kind: 'direct' };

export function machineReportsNoNetwork(): boolean {
  return typeof navigator !== 'undefined' && navigator.onLine === false;
}

export function classifyLoginTransportFailure(
  err: unknown,
  routeCheck: PerimeterCheck | null,
  offline: boolean = machineReportsNoNetwork(),
): LoginTransportFailure | null {
  if (!isServerUnreachableError(err)) return null;

  const text = errorText(err);
  if (offline || OFFLINE_CODE.test(text)) return { kind: 'offline' };
  if (NETWORK_CHANGED_CODE.test(text)) return { kind: 'network_changed' };
  if (NAME_FAILURE_CODE.test(text)) return { kind: 'name_unresolved' };
  if (ROUTE_RESOLUTION_FAILURE_CODE.test(text)) return { kind: 'route_unknown' };

  if (routeCheck?.reasonCode === 'route_proxy_list') {
    const candidates = parseRouteCandidates(routeCheck.detail);
    return candidates === null
      ? { kind: 'route_unknown' }
      : { kind: 'proxy_candidates', ...candidates };
  }

  const viaProxy = routeCheck?.reasonCode === 'route_proxy' || hasProxyFailureCode(err);
  if (viaProxy) {
    const proxy =
      routeCheck?.reasonCode === 'route_proxy' ? displayableProxyAddress(routeCheck.detail) : null;
    return proxy !== null ? { kind: 'proxy', proxy } : { kind: 'proxy_unnamed' };
  }

  if (routeCheck?.reasonCode === 'route_direct') return { kind: 'direct' };
  return { kind: 'route_unknown' };
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err ?? '');
}

export function shouldRevealDiagnostics(failure: LoginTransportFailure | null): boolean {
  if (failure === null) return false;
  return (
    failure.kind === 'proxy' ||
    failure.kind === 'proxy_unnamed' ||
    failure.kind === 'proxy_candidates' ||
    failure.kind === 'route_unknown' ||
    failure.kind === 'network_changed'
  );
}

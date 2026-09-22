import { DEFAULT_RUNTIME_CONFIG } from './runtime-config';
import {
  CURRENT_DESKTOP_SCHEMA_VERSION,
  futureDesktopSchemaRefusal,
} from './desktop-config-schema';
import type { RemovedPackageStatus } from './provisioning-status';
import {
  isLoopbackHost,
  routeAddress,
  type LocalProxyFacts,
  type ProxyProbeVerdict,
  type ProxyRoute,
  type SubprocessEntry,
} from './system-proxy';

export interface PerimeterSettings {
  proxyHost: string;
  proxyPort: number;
  noProxy: string[];
}

export const DEFAULT_PERIMETER_NO_PROXY: readonly string[] = ['localhost', '127.0.0.1'];

export function llmApiBaseHost(apiBase: string | null | undefined): string | null {
  if (typeof apiBase !== 'string') return null;
  const trimmed = apiBase.trim();
  if (trimmed.length === 0) return null;
  try {
    const host = new URL(trimmed).hostname;
    return host.length > 0 ? host : null;
  } catch {
    return null;
  }
}

export function mergeNoProxy(base: readonly string[], extra: readonly string[]): string[] {
  const merged = [...base];
  for (const entry of extra) {
    const trimmed = entry.trim();
    if (trimmed.length > 0 && !merged.includes(trimmed)) merged.push(trimmed);
  }
  return merged;
}

export const DEFAULT_PERIMETER_SETTINGS: PerimeterSettings = Object.freeze({
  proxyHost: '127.0.0.1',
  proxyPort: 3128,
  noProxy: [...DEFAULT_PERIMETER_NO_PROXY],
});

export interface PerimeterStateView {
  localProxyUrl: string;
  caBundlePresent: boolean;
  corpCaPresent: boolean;
  kerberosTicket: KerberosTicketState;
  kerberosExpiresAt: number | null;
  kerberosRenewUntil?: number | null;
  kerberosCachePrincipal?: string | null;
  lastKinit?: LastKinitOutcome | null;
  ticketSource?: TicketSource;
  kerberosSupported: boolean;
  realm: string | null;
  principalDomain?: string | null;
  live?: PerimeterLiveStatus | null;
}

export type PerimeterCheckState = 'ok' | 'degraded' | 'fail' | 'unknown';

export type PerimeterCheckId =
  | 'ca'
  // The route the SYSTEM resolved for each address the app depends on (#249).
  // These two rows are what turns "it does not work" into a diagnosis.
  | 'route_server'
  | 'route_llm'
  // Where child processes enter the network, and whether that is expressible.
  | 'entry'
  | 'px'
  | 'kerberos'
  | 'server'
  | 'llm'
  | 'daemon'
  | 'provisioning';

export interface PerimeterCheck {
  id: PerimeterCheckId;
  state: PerimeterCheckState;
  reasonCode: string;
  detail?: string;
  counts?: { installed: number; total: number };
  restartPending?: boolean;
  preservedLegacyPaths?: string[];
  removedPackages?: string[];
}

export interface PerimeterLiveStatus {
  overall: PerimeterCheckState;
  checks: PerimeterCheck[];
  checkedAt: number;
}

export type HttpReachability =
  | { kind: 'response'; status: number }
  | { kind: 'unreachable' }
  | { kind: 'unconfigured' }
  | { kind: 'skipped' };

export type LlmAuthReachability =
  | { kind: 'response'; status: number }
  | { kind: 'unreachable' }
  | { kind: 'no_key' }
  | { kind: 'unconfigured' }
  | { kind: 'skipped' };

export function evaluateLlmAuthCheck(fact: LlmAuthReachability): PerimeterCheck {
  switch (fact.kind) {
    case 'response':
      if (fact.status >= 200 && fact.status < 300) {
        return { id: 'llm', state: 'ok', reasonCode: 'live_llm_ok' };
      }
      if (fact.status === 401 || fact.status === 403) {
        return { id: 'llm', state: 'fail', reasonCode: 'llm_auth_rejected' };
      }
      return { id: 'llm', state: 'degraded', reasonCode: 'live_llm_degraded' };
    case 'unreachable':
      return { id: 'llm', state: 'fail', reasonCode: 'live_llm_unreachable' };
    case 'no_key':
      return {
        id: 'llm',
        state: 'unknown',
        reasonCode: 'llm_key_not_configured',
      };
    case 'unconfigured':
    case 'skipped':
      return { id: 'llm', state: 'unknown', reasonCode: 'live_llm_unconfigured' };
  }
}

export type LlmGatewayVerdict =
  'ok' | 'auth_rejected' | 'degraded' | 'unreachable' | 'unconfigured';

export function llmGatewayRowVerdict(fact: LlmAuthReachability): LlmGatewayVerdict {
  switch (fact.kind) {
    case 'response':
      if (fact.status >= 200 && fact.status < 300) return 'ok';
      if (fact.status === 401 || fact.status === 403) return 'auth_rejected';
      return 'degraded';
    case 'unreachable':
      return 'unreachable';
    case 'no_key':
    case 'unconfigured':
    case 'skipped':
      return 'unconfigured';
  }
}

export type DaemonReachability =
  { kind: 'running' } | { kind: 'stopped' } | { kind: 'unknown' } | { kind: 'skipped' };

export type ProvisioningReachability =
  | {
      kind: 'status';
      state: 'syncing' | 'ok' | 'fail';
      installed: number;
      total: number;
      failed: number;
      restartPending?: boolean;
      preservedLegacyPaths?: readonly string[];
      removedPackages?: readonly RemovedPackageStatus[];
    }
  | {
      kind: 'unconfigured';
      removedPackages?: readonly RemovedPackageStatus[];
    }
  | { kind: 'unknown' }
  | { kind: 'skipped' };

export function evaluateCaCheck(present: boolean): PerimeterCheck {
  return present
    ? { id: 'ca', state: 'ok', reasonCode: 'ca_ok' }
    : { id: 'ca', state: 'fail', reasonCode: 'ca_absent' };
}

const DIRECT_PATH = 'DIRECT';

function describeRouteCandidates(candidates: readonly ProxyRoute[]): string | null {
  const paths = candidates
    .map((candidate) => (candidate.kind === 'direct' ? DIRECT_PATH : routeAddress(candidate)))
    .filter((path): path is string => path !== null);
  return paths.length > 1 ? paths.join(', ') : null;
}

export function evaluateRouteCheck(
  id: 'route_server' | 'route_llm',
  target: RouteInput,
): PerimeterCheck {
  if (!target.host) {
    return { id, state: 'unknown', reasonCode: 'route_unconfigured' };
  }
  if (target.route.kind === 'unknown') {
    return { id, state: 'fail', reasonCode: 'route_unknown', detail: target.host };
  }
  const offered = describeRouteCandidates(target.candidates ?? []);
  if (offered) {
    return { id, state: 'ok', reasonCode: 'route_proxy_list', detail: offered };
  }
  return target.route.kind === 'proxy'
    ? {
        id,
        state: 'ok',
        reasonCode: 'route_proxy',
        detail: routeAddress(target.route) ?? undefined,
      }
    : { id, state: 'ok', reasonCode: 'route_direct', detail: target.host };
}

export function evaluateLocalProxyCheck(
  px: LocalProxyFacts | null,
  executableMissing: boolean = false,
): PerimeterCheck {
  if (!px) return { id: 'px', state: 'unknown', reasonCode: 'live_px_unconfigured' };
  const detail = `http://${px.host}:${px.port}`;
  if (!px.listening) {
    return executableMissing
      ? { id: 'px', state: 'fail', reasonCode: 'live_px_executable_missing' }
      : { id: 'px', state: 'fail', reasonCode: 'live_px_unreachable', detail };
  }
  if (!isLoopbackHost(px.host)) {
    return { id: 'px', state: 'fail', reasonCode: 'live_px_not_loopback', detail };
  }
  if (!px.upstreamWorks) {
    return { id: 'px', state: 'fail', reasonCode: 'live_px_upstream_dead', detail };
  }
  return { id: 'px', state: 'ok', reasonCode: 'live_px_ok', detail };
}

export function evaluateEntryCheck(
  entry: SubprocessEntry,
  conflict: { routes: string[] } | null,
): PerimeterCheck {
  if (conflict) {
    return {
      id: 'entry',
      state: 'fail',
      reasonCode: 'entry_conflict',
      detail: conflict.routes.join(', '),
    };
  }
  if (entry.kind === 'loopback_proxy') {
    return {
      id: 'entry',
      state: 'ok',
      reasonCode: 'entry_local_proxy',
      detail: `http://${entry.host}:${entry.port}`,
    };
  }
  switch (entry.route.kind) {
    case 'proxy':
      return {
        id: 'entry',
        state: 'ok',
        reasonCode: 'entry_system_proxy',
        detail: routeAddress(entry.route) ?? undefined,
      };
    case 'direct':
      return { id: 'entry', state: 'ok', reasonCode: 'entry_direct' };
    default:
      return { id: 'entry', state: 'fail', reasonCode: 'entry_unknown' };
  }
}

export type TicketAcceptance = ProxyProbeVerdict | 'not_in_play';

export function deriveTicketAcceptance(routes: PerimeterRouteInputs): TicketAcceptance {
  const px = routes.localProxy;
  const inPlay = routes.goosar.route.kind === 'proxy' || px?.listening === true;
  if (!inPlay) return 'not_in_play';
  if (!px) return 'not_run';
  return px.probe ?? (px.upstreamWorks ? 'carried' : 'not_run');
}

export interface KerberosPrincipalFacts {
  cachePrincipal: string | null;
  resolvedPrincipal: string | null;
}

export function evaluateKerberosCheck(
  supported: boolean,
  ticket: KerberosTicketState,
  acceptance: TicketAcceptance = 'not_in_play',
  principals?: KerberosPrincipalFacts,
): PerimeterCheck | null {
  if (!supported) return null;
  if (
    principals?.cachePrincipal &&
    principals.resolvedPrincipal &&
    principals.cachePrincipal !== principals.resolvedPrincipal &&
    (ticket === 'valid' || ticket === 'expiring_soon')
  ) {
    return { id: 'kerberos', state: 'fail', reasonCode: 'principal_mismatch' };
  }
  switch (ticket) {
    case 'valid':
    case 'expiring_soon':
      if (acceptance === 'auth_rejected') {
        return {
          id: 'kerberos',
          state: 'fail',
          reasonCode: 'ticket_not_accepted',
        };
      }
      if (ticket === 'expiring_soon') {
        return {
          id: 'kerberos',
          state: 'degraded',
          reasonCode: 'ticket_expiring_soon',
        };
      }
      if (acceptance === 'carried') {
        return { id: 'kerberos', state: 'ok', reasonCode: 'ticket_valid' };
      }
      if (acceptance === 'not_in_play') {
        return {
          id: 'kerberos',
          state: 'unknown',
          reasonCode: 'ticket_valid_unconfirmed_outside_perimeter',
        };
      }
      return {
        id: 'kerberos',
        state: 'unknown',
        reasonCode: 'ticket_valid_unconfirmed',
      };
    case 'expired':
      return { id: 'kerberos', state: 'fail', reasonCode: 'ticket_expired' };
    case 'none':
      return { id: 'kerberos', state: 'fail', reasonCode: 'ticket_none' };
    case 'unknown':
      return { id: 'kerberos', state: 'unknown', reasonCode: 'ticket_unknown' };
  }
}

export function evaluateReachabilityCheck(
  id: 'server' | 'llm',
  fact: HttpReachability,
): PerimeterCheck {
  const prefix = id === 'server' ? 'live_server' : 'live_llm';
  switch (fact.kind) {
    case 'response':
      return fact.status >= 500
        ? { id, state: 'degraded', reasonCode: `${prefix}_degraded` }
        : { id, state: 'ok', reasonCode: `${prefix}_ok` };
    case 'unreachable':
      return { id, state: 'fail', reasonCode: `${prefix}_unreachable` };
    case 'unconfigured':
    case 'skipped':
      return { id, state: 'unknown', reasonCode: `${prefix}_unconfigured` };
  }
}

export function evaluateDaemonCheck(fact: DaemonReachability): PerimeterCheck {
  switch (fact.kind) {
    case 'running':
      return { id: 'daemon', state: 'ok', reasonCode: 'live_daemon_ok' };
    case 'stopped':
      return { id: 'daemon', state: 'fail', reasonCode: 'live_daemon_stopped' };
    case 'unknown':
    case 'skipped':
      return {
        id: 'daemon',
        state: 'unknown',
        reasonCode: 'live_daemon_unknown',
      };
  }
}

function removedPackagesField(
  removedPackages: readonly RemovedPackageStatus[],
): Pick<PerimeterCheck, 'removedPackages'> {
  if (removedPackages.length === 0) return {};
  return {
    removedPackages: removedPackages.map((pkg) =>
      pkg.state === 'pending' ? `${pkg.type}:${pkg.name}…` : `${pkg.type}:${pkg.name}`,
    ),
  };
}

function provisioningStatusCheck(
  state: 'syncing' | 'ok' | 'fail',
  counts: { installed: number; total: number },
  restartPending: boolean,
  preservedLegacyPaths: readonly string[],
  removedPackages: readonly RemovedPackageStatus[] = [],
): PerimeterCheck {
  const legacy: Pick<PerimeterCheck, 'preservedLegacyPaths' | 'removedPackages'> = {
    ...(preservedLegacyPaths.length > 0 ? { preservedLegacyPaths: [...preservedLegacyPaths] } : {}),
    ...removedPackagesField(removedPackages),
  };
  switch (state) {
    case 'fail':
      return {
        id: 'provisioning',
        state: 'fail',
        reasonCode: 'live_provisioning_fail',
        counts,
        ...legacy,
      };
    case 'syncing':
      return {
        id: 'provisioning',
        state: 'degraded',
        reasonCode: 'live_provisioning_pending',
        counts,
        ...legacy,
      };
    case 'ok':
      if (restartPending) {
        return {
          id: 'provisioning',
          state: 'degraded',
          reasonCode: 'live_provisioning_restart_pending',
          counts,
          restartPending: true,
          ...legacy,
        };
      }
      return {
        id: 'provisioning',
        state: 'ok',
        reasonCode: 'live_provisioning_ok',
        counts,
        ...legacy,
      };
  }
}

export function evaluateProvisioningCheck(fact: ProvisioningReachability): PerimeterCheck {
  switch (fact.kind) {
    case 'status':
      return provisioningStatusCheck(
        fact.state,
        { installed: fact.installed, total: fact.total },
        fact.restartPending === true,
        fact.preservedLegacyPaths ?? [],
        fact.removedPackages ?? [],
      );
    case 'unconfigured':
      return {
        id: 'provisioning',
        state: 'ok',
        reasonCode: 'live_provisioning_not_configured',
        ...removedPackagesField(fact.removedPackages ?? []),
      };
    case 'unknown':
    case 'skipped':
      return {
        id: 'provisioning',
        state: 'unknown',
        reasonCode: 'live_provisioning_unknown',
      };
  }
}

export function aggregateLiveOverall(checks: readonly PerimeterCheck[]): PerimeterCheckState {
  if (checks.length === 0) return 'unknown';
  if (checks.some((check) => check.state === 'fail')) return 'fail';
  if (checks.some((check) => check.state === 'degraded')) return 'degraded';
  if (checks.some((check) => check.state === 'unknown')) return 'degraded';
  return 'ok';
}

export interface RouteInput {
  host: string | null;
  route: ProxyRoute;
  candidates?: readonly ProxyRoute[];
}

export interface PerimeterRouteInputs {
  goosar: RouteInput;
  llm: RouteInput;
  localProxy: LocalProxyFacts | null;
  entry: SubprocessEntry;
  conflict: { routes: string[] } | null;
  localProxyExecutableMissing?: boolean;
}

export interface PerimeterLiveInputs {
  caBundlePresent: boolean;
  routes: PerimeterRouteInputs;
  kerberosSupported: boolean;
  kerberosTicket: KerberosTicketState;
  kerberosPrincipals?: KerberosPrincipalFacts;
  server: HttpReachability;
  llm: LlmAuthReachability;
  daemon: DaemonReachability;
  provisioning: ProvisioningReachability;
  checkedAt: number;
}

export function buildPerimeterLiveStatus(inputs: PerimeterLiveInputs): PerimeterLiveStatus {
  const checks: PerimeterCheck[] = [
    evaluateRouteCheck('route_server', inputs.routes.goosar),
    evaluateRouteCheck('route_llm', inputs.routes.llm),
    evaluateEntryCheck(inputs.routes.entry, inputs.routes.conflict),
    evaluateLocalProxyCheck(
      inputs.routes.localProxy,
      inputs.routes.localProxyExecutableMissing === true,
    ),
    evaluateCaCheck(inputs.caBundlePresent),
  ];
  const kerberos = evaluateKerberosCheck(
    inputs.kerberosSupported,
    inputs.kerberosTicket,
    deriveTicketAcceptance(inputs.routes),
    inputs.kerberosPrincipals,
  );
  if (kerberos) checks.push(kerberos);
  checks.push(evaluateReachabilityCheck('server', inputs.server));
  checks.push(evaluateLlmAuthCheck(inputs.llm));
  checks.push(evaluateDaemonCheck(inputs.daemon));
  checks.push(evaluateProvisioningCheck(inputs.provisioning));
  return {
    overall: aggregateLiveOverall(checks),
    checks,
    checkedAt: inputs.checkedAt,
  };
}
export type PerimeterDaemonRestartOutcome =
  | { kind: 'restarted' }
  | { kind: 'not_running' }
  | { kind: 'foreign' }
  | { kind: 'failed'; message: string };

export interface LastKinitOutcome {
  at: number;
  kind: 'kinit' | 'renew';
  ok: boolean;
  reason?: string;
}

export type PerimeterKinitResult =
  { ok: true; state: PerimeterStateView } | { ok: false; reason: string; message: string };

export function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function parseNoProxyList(value: unknown): string[] {
  const entries: string[] =
    typeof value === 'string'
      ? value.split(',')
      : Array.isArray(value)
        ? value.filter((entry): entry is string => typeof entry === 'string')
        : [];
  const cleaned: string[] = [];
  for (const entry of entries) {
    const trimmed = entry.trim();
    if (trimmed.length > 0 && !cleaned.includes(trimmed)) cleaned.push(trimmed);
  }
  return cleaned;
}

function parseNoProxy(value: unknown): string[] {
  return mergeNoProxy(DEFAULT_PERIMETER_NO_PROXY, parseNoProxyList(value));
}

function parseHost(value: unknown): string {
  if (typeof value !== 'string') return DEFAULT_PERIMETER_SETTINGS.proxyHost;
  const host = value.trim();
  if (host.length === 0 || /[\s/#@:]/.test(host)) {
    return DEFAULT_PERIMETER_SETTINGS.proxyHost;
  }
  return host;
}

function parsePort(value: unknown): number {
  if (typeof value !== 'number' || !Number.isInteger(value)) {
    return DEFAULT_PERIMETER_SETTINGS.proxyPort;
  }
  if (value < 1 || value > 65535) return DEFAULT_PERIMETER_SETTINGS.proxyPort;
  return value;
}

export function parsePerimeterSettings(raw: string | null): PerimeterSettings {
  if (!raw) return { ...DEFAULT_PERIMETER_SETTINGS };
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { ...DEFAULT_PERIMETER_SETTINGS };
  }
  if (!isRecord(parsed) || !isRecord(parsed.proxy)) {
    return { ...DEFAULT_PERIMETER_SETTINGS };
  }
  const proxy = parsed.proxy;
  return {
    proxyHost: parseHost(proxy.host),
    proxyPort: parsePort(proxy.port),
    noProxy: parseNoProxy(proxy.noProxy),
  };
}

export type DesktopDocumentBase =
  { ok: true; base: Record<string, unknown> } | { ok: false; message: string };

export function parseDesktopDocumentBase(
  existingRaw: string | null,
  action: string,
): DesktopDocumentBase {
  if (existingRaw === null || existingRaw.trim().length === 0) {
    return { ok: true, base: {} };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(existingRaw);
  } catch (err) {
    return {
      ok: false,
      message: `desktop.json is not valid JSON (${
        err instanceof Error ? err.message : 'parse failed'
      }) — fix or remove it before ${action}`,
    };
  }
  if (!isRecord(parsed)) {
    return {
      ok: false,
      message: `desktop.json is not a JSON object — fix or remove it before ${action}`,
    };
  }
  const refusal = futureDesktopSchemaRefusal(parsed);
  if (refusal !== null) return { ok: false, message: refusal };
  return { ok: true, base: parsed };
}

export function backfillBootFields(base: Record<string, unknown>): Record<string, unknown> {
  const hasApiUrl = typeof base.apiUrl === 'string' && base.apiUrl.trim().length > 0;
  return {
    ...base,
    ...(base.schemaVersion === CURRENT_DESKTOP_SCHEMA_VERSION
      ? {}
      : { schemaVersion: CURRENT_DESKTOP_SCHEMA_VERSION }),
    ...(hasApiUrl ? {} : { apiUrl: DEFAULT_RUNTIME_CONFIG.apiUrl }),
  };
}

export function perimeterProxyUrl(settings: PerimeterSettings): string {
  return `http://${settings.proxyHost}:${settings.proxyPort}`;
}

export interface PerimeterCaPaths {
  caBundlePath: string | null;
  corpCaPath: string | null;
}

export function deriveKinitRealm(email: string): string | null {
  if (typeof email !== 'string') return null;
  const at = email.lastIndexOf('@');
  if (at < 0) return null;
  const domain = email.slice(at + 1).trim();
  return domain.length > 0 ? domain.toUpperCase() : null;
}

export function deriveKinitPrincipal(email: string, realmOverride?: string | null): string | null {
  const trimmed = (typeof email === 'string' ? email : '').trim();
  if (trimmed.length === 0) return null;
  const at = trimmed.lastIndexOf('@');
  const localPart = at > 0 ? trimmed.slice(0, at) : trimmed;
  if (localPart.length === 0 || /[\s@/]/.test(localPart)) return null;

  const override = realmOverride?.trim();
  const realm =
    override && override.length > 0 ? override.toUpperCase() : deriveKinitRealm(trimmed);
  if (!realm) return null;

  const principal = `${localPart}@${realm}`;
  return isValidKerberosPrincipal(principal) ? principal : null;
}

export function isValidKerberosPrincipal(principal: string): boolean {
  if (typeof principal !== 'string') return false;
  return /^[^\s@/]+(?:\/[^\s@/]+)?@[^\s@/]+$/.test(principal.trim());
}

export const KERBEROS_EXPIRING_SOON_MS = 60 * 60_000;

export type KerberosTicketState =
  | 'valid'
  /** Present but within KERBEROS_EXPIRING_SOON_MS of expiry — warn now. */
  | 'expiring_soon'
  /** Present but the TGT is past its expiry. */
  | 'expired'
  /** No ticket in the credentials cache. */
  | 'none'
  /** klist was unavailable/unreadable (spawn failure, non-macOS, odd output). */
  | 'unknown';

export type TicketSource = 'kinit_app' | 'renewed' | 'cache';

export function deriveTicketSource(lastKinit: LastKinitOutcome | null | undefined): TicketSource {
  if (!lastKinit || lastKinit.ok !== true) return 'cache';
  return lastKinit.kind === 'kinit' ? 'kinit_app' : 'renewed';
}

export interface KerberosTicketStatus {
  state: KerberosTicketState;
  expiresAt: number | null;
}

const KLIST_MONTHS: Record<string, number> = {
  Jan: 0,
  Feb: 1,
  Mar: 2,
  Apr: 3,
  May: 4,
  Jun: 5,
  Jul: 6,
  Aug: 7,
  Sep: 8,
  Oct: 9,
  Nov: 10,
  Dec: 11,
};

const KLIST_TIME = String.raw`(\w{3})\s+(\d{1,2})\s+(\d{2}):(\d{2}):(\d{2})\s+(\d{4})`;
const KLIST_ROW = new RegExp(String.raw`^${KLIST_TIME}\s+${KLIST_TIME}\s+(\S+)`, 'gm');

function klistTimeToMs(
  mon: string,
  day: string,
  hh: string,
  mm: string,
  ss: string,
  year: string,
): number | null {
  const month = KLIST_MONTHS[mon];
  if (month === undefined) return null;
  const ms = new Date(
    Number(year),
    month,
    Number(day),
    Number(hh),
    Number(mm),
    Number(ss),
  ).getTime();
  return Number.isNaN(ms) ? null : ms;
}

function latestKrbtgtExpiry(output: string): number | null {
  let latest: number | null = null;
  for (const match of output.matchAll(KLIST_ROW)) {
    const principal = match[13];
    if (!principal.startsWith('krbtgt/')) continue;
    const expires = klistTimeToMs(match[7], match[8], match[9], match[10], match[11], match[12]);
    if (expires !== null && (latest === null || expires > latest)) {
      latest = expires;
    }
  }
  return latest;
}

const KLIST_RENEW_LINE = new RegExp(String.raw`renew\s+(?:till|until)[:]?\s+${KLIST_TIME}`, 'i');

export function parseKlistRenewUntil(output: string): number | null {
  const match = KLIST_RENEW_LINE.exec(output);
  if (!match) return null;
  return klistTimeToMs(match[1], match[2], match[3], match[4], match[5], match[6]);
}

const KLIST_NO_TICKET =
  /no credentials cache|no ticket|no kerberos|cache not found|failed to parse uuid|krb5_cc_(?:get_principal|resolve)|krb5_cc_.*not? *(?:found|exist)/i;

export function parseKlistTicket(
  output: string,
  nowMs: number,
  expiringSoonMs: number = KERBEROS_EXPIRING_SOON_MS,
): KerberosTicketStatus {
  const expiresAt = latestKrbtgtExpiry(output);
  if (expiresAt !== null) {
    if (nowMs >= expiresAt) return { state: 'expired', expiresAt };
    if (expiresAt - nowMs <= expiringSoonMs) {
      return { state: 'expiring_soon', expiresAt };
    }
    return { state: 'valid', expiresAt };
  }
  if (KLIST_NO_TICKET.test(output)) return { state: 'none', expiresAt: null };
  return { state: 'unknown', expiresAt: null };
}

export interface KlistCacheIdentity {
  cache: string | null;
  realm: string | null;
  principal: string | null;
}

const KLIST_CACHE_LINE = /^\s*Credentials cache:\s*(\S+)/m;
const KLIST_PRINCIPAL_LINE = /^\s*Principal:\s*(\S+)/m;

export function parseKlistCacheIdentity(output: string): KlistCacheIdentity {
  const cache = KLIST_CACHE_LINE.exec(output)?.[1] ?? null;
  const principal = KLIST_PRINCIPAL_LINE.exec(output)?.[1] ?? null;
  const at = principal?.lastIndexOf('@') ?? -1;
  const realm = principal && at >= 0 && at < principal.length - 1 ? principal.slice(at + 1) : null;
  return { cache, realm, principal };
}

export interface PerimeterExtras {
  realm: string | null;
  principalDomain: string | null;
  noProxy?: string[];
}

export const DEFAULT_PERIMETER_EXTRAS: PerimeterExtras = Object.freeze({
  realm: null,
  principalDomain: null,
  noProxy: [],
});

export function parsePerimeterExtras(raw: string | null): PerimeterExtras {
  if (!raw) return { ...DEFAULT_PERIMETER_EXTRAS };
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { ...DEFAULT_PERIMETER_EXTRAS };
  }
  if (!isRecord(parsed) || !isRecord(parsed.perimeter)) {
    return { ...DEFAULT_PERIMETER_EXTRAS };
  }
  const block = parsed.perimeter;
  const realm =
    typeof block.realm === 'string' && block.realm.trim().length > 0
      ? block.realm.trim().toUpperCase()
      : null;
  const principalDomain =
    typeof block.principalDomain === 'string' && block.principalDomain.trim().length > 0
      ? block.principalDomain.trim().toUpperCase()
      : null;
  return {
    realm,
    principalDomain,
    noProxy: parseNoProxyList(block.noProxy),
  };
}

export type PerimeterExtrasPatch = Partial<{
  realm: string | null;
}>;

export type PerimeterExtrasWriteResult =
  { ok: true; json: string; extras: PerimeterExtras } | { ok: false; message: string };

export function applyPerimeterExtras(
  existingRaw: string | null,
  patch: PerimeterExtrasPatch,
): PerimeterExtrasWriteResult {
  const parsed = parseDesktopDocumentBase(existingRaw, 'changing the perimeter settings');
  if (!parsed.ok) return { ok: false, message: parsed.message };

  const base = parsed.base;
  const existingPerimeter = isRecord(base.perimeter) ? base.perimeter : {};
  const nextPerimeter: Record<string, unknown> = { ...existingPerimeter };

  if ('realm' in patch) {
    if (patch.realm && patch.realm.trim().length > 0) {
      nextPerimeter.realm = patch.realm.trim().toUpperCase();
    } else {
      delete nextPerimeter.realm;
    }
  }
  const next: Record<string, unknown> = {
    ...backfillBootFields(base),
    perimeter: nextPerimeter,
  };
  const json = `${JSON.stringify(next, null, 2)}\n`;
  return { ok: true, json, extras: parsePerimeterExtras(json) };
}

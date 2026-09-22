import { isLoopbackHost, type ProxyRoute } from './system-proxy';

export interface LocalProxyInputs {
  managementEnabled: boolean;
  upstream: ProxyRoute;
  listen: { host: string; port: number };
  executable: string | null;
  foreignProxyWorks: boolean;
  noProxy: readonly string[];
}

export type LocalProxyPlan =
  | {
      action: 'run';
      listen: { host: string; port: number };
      upstream: { host: string; port: number };
      executable: string;
      noProxy: string[];
    }
  | { action: 'stop'; reason: 'route_unknown' | 'route_direct' }
  | { action: 'disabled'; reason: 'management_disabled' }
  | { action: 'refuse'; reason: 'listen_not_loopback' }
  | { action: 'leave_foreign'; reason: 'foreign_proxy_works' }
  | { action: 'unavailable'; reason: 'executable_missing' };

export function planLocalProxy(inputs: LocalProxyInputs): LocalProxyPlan {
  if (!inputs.managementEnabled) {
    return { action: 'disabled', reason: 'management_disabled' };
  }
  if (inputs.upstream.kind === 'unknown') {
    return { action: 'stop', reason: 'route_unknown' };
  }
  if (inputs.upstream.kind === 'direct') {
    return { action: 'stop', reason: 'route_direct' };
  }
  if (!isLoopbackHost(inputs.listen.host)) {
    return { action: 'refuse', reason: 'listen_not_loopback' };
  }
  if (inputs.foreignProxyWorks) {
    return { action: 'leave_foreign', reason: 'foreign_proxy_works' };
  }
  if (!inputs.executable) {
    return { action: 'unavailable', reason: 'executable_missing' };
  }
  return {
    action: 'run',
    listen: { ...inputs.listen },
    upstream: { host: inputs.upstream.host, port: inputs.upstream.port },
    executable: inputs.executable,
    noProxy: [...inputs.noProxy],
  };
}

export function localProxyArgs(plan: Extract<LocalProxyPlan, { action: 'run' }>): string[] {
  const args = [
    `--proxy=${plan.upstream.host}:${plan.upstream.port}`,
    `--listen=${plan.listen.host}`,
    `--port=${plan.listen.port}`,
  ];
  if (plan.noProxy.length > 0) {
    args.push(`--noproxy=${plan.noProxy.join(',')}`);
  }
  return args;
}

import { describe, it, expect } from 'vitest';
import { planLocalProxy, localProxyArgs, type LocalProxyPlan } from './local-proxy';
import { parseResolvedProxy, type ProxyRoute } from './system-proxy';

const upstream: ProxyRoute = {
  kind: 'proxy',
  host: 'isa.corp',
  port: 8080,
  scheme: 'http',
};

const base = {
  managementEnabled: true,
  listen: { host: '127.0.0.1', port: 3128 },
  executable: '/opt/homebrew/bin/px',
  foreignProxyWorks: false,
  noProxy: ['localhost', '127.0.0.1'],
};

const plan = (over: Partial<Parameters<typeof planLocalProxy>[0]> = {}) =>
  planLocalProxy({ upstream, ...base, ...over });

describe('planLocalProxy', () => {
  it('runs a local entry point on the upstream the system resolved', () => {
    expect(plan()).toEqual<LocalProxyPlan>({
      action: 'run',
      listen: { host: '127.0.0.1', port: 3128 },
      upstream: { host: 'isa.corp', port: 8080 },
      executable: '/opt/homebrew/bin/px',
      noProxy: ['localhost', '127.0.0.1'],
    });
  });

  it('stops rather than reusing a remembered upstream when the route is unknown', () => {
    expect(plan({ upstream: { kind: 'unknown' } })).toEqual({
      action: 'stop',
      reason: 'route_unknown',
    });
  });

  it('stops when the system resolved a direct route', () => {
    expect(plan({ upstream: { kind: 'direct' } })).toEqual({
      action: 'stop',
      reason: 'route_direct',
    });
  });

  it('refuses to start anything that is not bound to the loopback', () => {
    expect(plan({ listen: { host: '0.0.0.0', port: 3128 } })).toEqual({
      action: 'refuse',
      reason: 'listen_not_loopback',
    });
  });

  it('leaves a working proxy it did not start alone', () => {
    expect(plan({ foreignProxyWorks: true })).toEqual({
      action: 'leave_foreign',
      reason: 'foreign_proxy_works',
    });
  });

  it('does nothing at all when management is switched off', () => {
    expect(plan({ managementEnabled: false })).toEqual({
      action: 'disabled',
      reason: 'management_disabled',
    });
  });

  it('does nothing when there is no executable to run', () => {
    expect(plan({ executable: null })).toEqual({
      action: 'unavailable',
      reason: 'executable_missing',
    });
  });
});

describe('localProxyArgs', () => {
  const runPlan = plan() as Extract<LocalProxyPlan, { action: 'run' }>;

  it('passes the upstream, the loopback bind and the bypass list, and nothing else', () => {
    expect(localProxyArgs(runPlan)).toEqual([
      '--proxy=isa.corp:8080',
      '--listen=127.0.0.1',
      '--port=3128',
      '--noproxy=localhost,127.0.0.1',
    ]);
  });

  it("carries no PAC credential into argv when the system's answer had one", () => {
    const credentialed = plan({
      upstream: parseResolvedProxy('PROXY alice:s3cret@isa.corp:8080'),
    }) as Extract<LocalProxyPlan, { action: 'run' }>;

    expect(localProxyArgs(credentialed)).toContain('--proxy=isa.corp:8080');
    expect(localProxyArgs(credentialed).join(' ')).not.toContain('s3cret');
  });

  it('carries no credential of any kind on the command line', () => {
    const joined = localProxyArgs(runPlan).join(' ').toLowerCase();
    for (const word of ['password', 'passwd', 'token', 'secret', 'key', 'user']) {
      expect(joined).not.toContain(word);
    }
  });
});

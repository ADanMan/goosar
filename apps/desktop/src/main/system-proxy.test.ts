import { describe, it, expect, vi } from 'vitest';
import { describeEntry, resolveNetworkRoutes, type RouteResolutionDeps } from './system-proxy';

const deps = (over: Partial<RouteResolutionDeps> = {}): RouteResolutionDeps => ({
  resolveProxy: vi.fn(async () => 'DIRECT'),
  probeListening: vi.fn(async () => false),
  probeThroughProxy: vi.fn(async () => 'no_response' as const),
  ...over,
});

const TARGETS = {
  goosarUrl: 'https://goosar.ru',
  llmApiBase: 'https://gw.internal/v1',
};

describe('resolveNetworkRoutes', () => {
  it('asks the system for every address it needs, never a constant', async () => {
    const resolveProxy = vi.fn(async (url: string) =>
      url.startsWith('https://goosar.ru') ? 'PROXY isa.corp:8080' : 'DIRECT',
    );

    const snapshot = await resolveNetworkRoutes(TARGETS, null, deps({ resolveProxy }));

    expect(resolveProxy).toHaveBeenCalledWith('https://goosar.ru');
    expect(resolveProxy).toHaveBeenCalledWith('https://gw.internal/v1');
    expect(snapshot.goosar.route).toEqual({
      kind: 'proxy',
      host: 'isa.corp',
      port: 8080,
      scheme: 'http',
    });
    expect(snapshot.llm.route).toEqual({ kind: 'direct' });
    expect(snapshot.env.HTTP_PROXY).toBe('http://isa.corp:8080');
    expect(snapshot.env.NO_PROXY.split(',')).toContain('gw.internal');
  });

  it('reads a failed resolution as unknown rather than direct', async () => {
    const snapshot = await resolveNetworkRoutes(
      { goosarUrl: 'https://goosar.ru', llmApiBase: null },
      null,
      deps({
        resolveProxy: vi.fn(async () => {
          throw new Error('resolver unavailable');
        }),
      }),
    );

    expect(snapshot.goosar.route).toEqual({ kind: 'unknown' });
    expect(snapshot.env.NO_PROXY ?? '').not.toContain('goosar.ru');
    expect(snapshot.unresolvedHosts).toEqual(['goosar.ru']);
  });

  it('routes subprocesses through the local proxy once a probe gets through it', async () => {
    const snapshot = await resolveNetworkRoutes(
      TARGETS,
      { host: '127.0.0.1', port: 3128 },
      deps({
        resolveProxy: vi.fn(async () => 'PROXY isa.corp:8080'),
        probeListening: vi.fn(async () => true),
        probeThroughProxy: vi.fn(async () => 'carried' as const),
      }),
    );

    expect(snapshot.entry).toEqual({
      kind: 'loopback_proxy',
      host: '127.0.0.1',
      port: 3128,
    });
    expect(snapshot.env.HTTP_PROXY).toBe('http://127.0.0.1:3128');
  });

  it('does not route through a local proxy that only listens', async () => {
    const probeThroughProxy = vi.fn(async () => 'auth_rejected' as const);
    const snapshot = await resolveNetworkRoutes(
      TARGETS,
      { host: '127.0.0.1', port: 3128 },
      deps({
        resolveProxy: vi.fn(async () => 'PROXY isa.corp:8080'),
        probeListening: vi.fn(async () => true),
        probeThroughProxy,
      }),
    );

    expect(probeThroughProxy).toHaveBeenCalled();
    expect(snapshot.localProxy?.upstreamWorks).toBe(false);
    expect(snapshot.localProxy?.probe).toBe('auth_rejected');
    expect(snapshot.env.HTTP_PROXY).toBe('http://isa.corp:8080');
  });

  it('does not claim a local proxy works when nothing could be probed through it', async () => {
    const probeThroughProxy = vi.fn(async () => 'carried' as const);
    const snapshot = await resolveNetworkRoutes(
      { goosarUrl: null, llmApiBase: null },
      { host: '127.0.0.1', port: 3128 },
      deps({ probeListening: vi.fn(async () => true), probeThroughProxy }),
    );

    expect(probeThroughProxy).not.toHaveBeenCalled();
    expect(snapshot.localProxy?.upstreamWorks).toBe(false);
    expect(snapshot.localProxy?.probe).toBe('not_run');
    expect(snapshot.entry).toEqual({ kind: 'system', route: { kind: 'unknown' } });
  });

  it('never probes a loopback address, which would bypass the proxy anyway', async () => {
    const probeThroughProxy = vi.fn(async () => 'carried' as const);
    const snapshot = await resolveNetworkRoutes(
      { goosarUrl: 'http://localhost:8083', llmApiBase: null },
      { host: '127.0.0.1', port: 3128 },
      deps({
        resolveProxy: vi.fn(async () => 'PROXY isa.corp:8080'),
        probeListening: vi.fn(async () => true),
        probeThroughProxy,
      }),
    );

    expect(probeThroughProxy).not.toHaveBeenCalled();
    expect(snapshot.localProxy?.upstreamWorks).toBe(false);
  });

  it('probes the LLM gateway rather than the goosar', async () => {
    const probeThroughProxy = vi.fn(async () => 'carried' as const);
    await resolveNetworkRoutes(
      TARGETS,
      { host: '127.0.0.1', port: 3128 },
      deps({
        resolveProxy: vi.fn(async () => 'PROXY isa.corp:8080'),
        probeListening: vi.fn(async () => true),
        probeThroughProxy,
      }),
    );

    expect(probeThroughProxy).toHaveBeenCalledWith(
      'http://127.0.0.1:3128',
      'https://gw.internal/v1',
    );
  });

  it('falls back to the goosar address when there is no gateway', async () => {
    const probeThroughProxy = vi.fn(async () => 'carried' as const);
    await resolveNetworkRoutes(
      { goosarUrl: 'https://goosar.ru', llmApiBase: null },
      { host: '127.0.0.1', port: 3128 },
      deps({
        resolveProxy: vi.fn(async () => 'PROXY isa.corp:8080'),
        probeListening: vi.fn(async () => true),
        probeThroughProxy,
      }),
    );

    expect(probeThroughProxy).toHaveBeenCalledWith('http://127.0.0.1:3128', 'https://goosar.ru');
  });

  it('skips the local-proxy probes entirely when none is configured', async () => {
    const probeListening = vi.fn(async () => true);
    const snapshot = await resolveNetworkRoutes(TARGETS, null, deps({ probeListening }));

    expect(probeListening).not.toHaveBeenCalled();
    expect(snapshot.localProxy).toBeNull();
  });

  it('keeps every route the PAC answer offered, not only the first', async () => {
    const snapshot = await resolveNetworkRoutes(
      { goosarUrl: 'https://goosar.ru', llmApiBase: null },
      null,
      deps({
        resolveProxy: vi.fn(async () => 'PROXY isa.corp:8080; PROXY backup.corp:8080; DIRECT'),
      }),
    );

    expect(snapshot.goosar.candidates).toEqual([
      { kind: 'proxy', host: 'isa.corp', port: 8080, scheme: 'http' },
      { kind: 'proxy', host: 'backup.corp', port: 8080, scheme: 'http' },
      { kind: 'direct' },
    ]);
    expect(snapshot.env.HTTP_PROXY).toBe('http://isa.corp:8080');
  });

  it('keeps a PAC credential out of everything but the subprocess environment', async () => {
    const snapshot = await resolveNetworkRoutes(
      { goosarUrl: 'https://goosar.ru', llmApiBase: null },
      null,
      deps({
        resolveProxy: vi.fn(async () => 'PROXY alice:s3cret@isa.corp:8080'),
      }),
    );

    expect(describeEntry(snapshot.entry)).toBe('http://***@isa.corp:8080');
    expect(describeEntry(snapshot.entry)).not.toContain('s3cret');
    expect(snapshot.goosar.route).toMatchObject({ host: 'isa.corp' });
    expect(snapshot.env.HTTP_PROXY).toBe('http://alice:s3cret@isa.corp:8080');
  });

  it('carries no credential-bearing field', async () => {
    const snapshot = await resolveNetworkRoutes(
      TARGETS,
      { host: '127.0.0.1', port: 3128 },
      deps({ probeListening: vi.fn(async () => true) }),
    );

    const serialized = JSON.stringify(snapshot).toLowerCase();
    for (const word of ['authorization', 'token', 'password', 'apikey', 'api_key', 'ticket']) {
      expect(serialized).not.toContain(word);
    }
  });
});

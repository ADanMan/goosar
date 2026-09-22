import { describe, expect, it } from 'vitest';

import {
  resolveBrowserApiBaseUrl,
  resolveBrowserWsUrl,
  resolveFrontendOrigin,
  resolveDevDocsUrl,
  resolveDevRemoteApiUrl,
  resolveDocsUrl,
  resolveRemoteApiUrl,
  runtimeRewriteDestination,
} from './runtime-urls';

describe('resolveRemoteApiUrl', () => {
  it('prefers REMOTE_API_URL when explicitly configured', () => {
    expect(
      resolveRemoteApiUrl({
        REMOTE_API_URL: 'http://backend:8080',
        NEXT_PUBLIC_API_URL: 'http://localhost:19000',
        PORT: '18080',
      }),
    ).toBe('http://backend:8080');
  });

  it('uses NEXT_PUBLIC_API_URL when REMOTE_API_URL is unset', () => {
    expect(
      resolveRemoteApiUrl({
        NEXT_PUBLIC_API_URL: 'http://localhost:19000',
        PORT: '18080',
      }),
    ).toBe('http://localhost:19000');
  });

  it('does not infer a backend URL from frontend or backend port env vars', () => {
    expect(
      resolveRemoteApiUrl({
        BACKEND_PORT: '28080',
        API_PORT: '38080',
        SERVER_PORT: '48080',
        PORT: '3000',
      }),
    ).toBeUndefined();
    expect(resolveRemoteApiUrl({ PORT: '3000' })).toBeUndefined();
  });

  it('does not use relative public API URLs for server-side rewrites', () => {
    expect(
      resolveRemoteApiUrl({
        NEXT_PUBLIC_API_URL: '/api',
      }),
    ).toBeUndefined();
  });

  it('ignores whitespace-only or invalid backend URL values', () => {
    expect(
      resolveRemoteApiUrl({
        REMOTE_API_URL: '  ',
        NEXT_PUBLIC_API_URL: 'ftp://api.example.com',
        PORT: '19080',
      }),
    ).toBeUndefined();
  });

  it('returns undefined when no API origin is configured', () => {
    expect(resolveRemoteApiUrl({})).toBeUndefined();
  });
});

describe('resolveDocsUrl', () => {
  it('uses DOCS_URL when configured', () => {
    expect(resolveDocsUrl({ DOCS_URL: ' http://docs:4000/ ' })).toBe('http://docs:4000');
  });

  it('returns undefined when no docs origin is configured', () => {
    expect(resolveDocsUrl({})).toBeUndefined();
  });

  it('ignores relative or invalid docs URL values', () => {
    expect(resolveDocsUrl({ DOCS_URL: '/docs' })).toBeUndefined();
    expect(resolveDocsUrl({ DOCS_URL: 'ftp://docs.example.com' })).toBeUndefined();
  });
});

describe('browser runtime URLs', () => {
  it('exposes NEXT_PUBLIC_API_URL at server render time', () => {
    expect(
      resolveBrowserApiBaseUrl({
        NEXT_PUBLIC_API_URL: ' https://api.example.com/ ',
      }),
    ).toBe('https://api.example.com');
  });

  it('derives browser websocket URL from the public API URL', () => {
    expect(
      resolveBrowserWsUrl({
        NEXT_PUBLIC_API_URL: 'https://api.example.com/base',
      }),
    ).toBe('wss://api.example.com/base/ws');
  });

  it('prefers an explicit browser websocket URL', () => {
    expect(
      resolveBrowserWsUrl({
        NEXT_PUBLIC_API_URL: 'https://api.example.com',
        NEXT_PUBLIC_WS_URL: ' wss://ws.example.com/socket/ ',
      }),
    ).toBe('wss://ws.example.com/socket');
  });

  it('falls back to same-origin websocket derivation for relative public API URLs', () => {
    expect(
      resolveBrowserWsUrl({
        NEXT_PUBLIC_API_URL: '/api',
      }),
    ).toBeUndefined();
  });
});

describe('runtimeRewriteDestination', () => {
  it('keeps same-origin fallback when no runtime upstreams are configured', () => {
    expect(runtimeRewriteDestination('/api/config', {})).toBeUndefined();
    expect(runtimeRewriteDestination('/auth/send-code', {})).toBeUndefined();
    expect(runtimeRewriteDestination('/uploads/workspaces/a.png', {})).toBeUndefined();
    expect(runtimeRewriteDestination('/ws', {})).toBeUndefined();
    expect(runtimeRewriteDestination('/docs/zh', {})).toBeUndefined();
  });

  it('keeps same-origin fallback for runtime API paths when only frontend PORT is configured', () => {
    expect(
      runtimeRewriteDestination('/api/config', {
        PORT: '3000',
      }),
    ).toBeUndefined();
  });

  it('does not rewrite runtime API paths to relative public API URLs', () => {
    expect(
      runtimeRewriteDestination('/api/config', {
        NEXT_PUBLIC_API_URL: '/api',
      }),
    ).toBeUndefined();
  });

  it('maps backend HTTP paths to the runtime API origin', () => {
    expect(
      runtimeRewriteDestination('/api/config', {
        REMOTE_API_URL: 'http://backend:8080',
      }),
    ).toBe('http://backend:8080/api/config');
    expect(
      runtimeRewriteDestination('/auth/send-code', {
        REMOTE_API_URL: 'http://backend:8080',
      }),
    ).toBe('http://backend:8080/auth/send-code');
    expect(
      runtimeRewriteDestination('/uploads/workspaces/a.png', {
        REMOTE_API_URL: 'http://backend:8080',
      }),
    ).toBe('http://backend:8080/uploads/workspaces/a.png');
  });

  it('does not rewrite frontend auth callback pages', () => {
    expect(runtimeRewriteDestination('/auth/callback', {})).toBeUndefined();
    expect(runtimeRewriteDestination('/auth/hg-sso/callback', {})).toBeUndefined();
  });

  it('maps docs paths to the runtime docs origin', () => {
    expect(
      runtimeRewriteDestination('/docs/zh/agents', {
        DOCS_URL: 'http://goosar-docs:3000',
      }),
    ).toBe('http://goosar-docs:3000/docs/zh/agents');
  });

  it('maps websocket paths to the runtime API origin', () => {
    expect(
      runtimeRewriteDestination('/ws', {
        REMOTE_API_URL: 'http://backend:8080',
      }),
    ).toBe('http://backend:8080/ws');
  });
});

describe('dev-only fallbacks', () => {
  it('falls back to the conventional local backend port', () => {
    expect(resolveDevRemoteApiUrl({})).toBe('http://localhost:8080');
  });

  it('honors BACKEND_PORT for the dev backend fallback', () => {
    expect(resolveDevRemoteApiUrl({ BACKEND_PORT: '19080' })).toBe('http://localhost:19080');
  });

  it('prefers configured origins over the dev fallbacks', () => {
    expect(resolveDevRemoteApiUrl({ REMOTE_API_URL: 'http://backend:8080' })).toBe(
      'http://backend:8080',
    );
    expect(resolveDevDocsUrl({ DOCS_URL: 'http://docs:4000' })).toBe('http://docs:4000');
  });

  it('falls back to the local docs port', () => {
    expect(resolveDevDocsUrl({})).toBe('http://localhost:4000');
  });
});

describe('resolveFrontendOrigin', () => {
  const FALLBACK = 'http://localhost:3000';

  it('survives the unexpanded FRONTEND_ORIGIN that ships in .env', () => {
    expect(resolveFrontendOrigin(['http://localhost:${FRONTEND_PORT}'], FALLBACK)).toBe(FALLBACK);
  });

  it('rejects an authority with no scheme instead of returning the origin "null"', () => {
    expect(resolveFrontendOrigin(['localhost:3001'], FALLBACK)).toBe(FALLBACK);
    expect(resolveFrontendOrigin(['localhost:3001', 'http://localhost:3001'], FALLBACK)).toBe(
      'http://localhost:3001',
    );
  });

  it('rejects schemes a browser cannot be pointed at', () => {
    expect(resolveFrontendOrigin(['file:///tmp/app'], FALLBACK)).toBe(FALLBACK);
    expect(resolveFrontendOrigin(['ws://localhost:3001'], FALLBACK)).toBe(FALLBACK);
  });

  it('says why it ignored a candidate, and says when it fell back', () => {
    const messages: string[] = [];
    expect(
      resolveFrontendOrigin(['http://localhost:${FRONTEND_PORT}'], FALLBACK, (message) =>
        messages.push(message),
      ),
    ).toBe(FALLBACK);

    expect(messages).toHaveLength(2);
    expect(messages[0]).toContain('http://localhost:${FRONTEND_PORT}');
    expect(messages[1]).toContain(FALLBACK);
  });

  it('stays quiet when a candidate is usable', () => {
    const messages: string[] = [];
    expect(
      resolveFrontendOrigin(['http://localhost:3001'], FALLBACK, (message) =>
        messages.push(message),
      ),
    ).toBe('http://localhost:3001');
    expect(messages).toEqual([]);
  });

  it('honours priority order and skips unset or blank values', () => {
    expect(
      resolveFrontendOrigin(['http://localhost:4000', 'http://localhost:3001'], FALLBACK),
    ).toBe('http://localhost:4000');
    expect(resolveFrontendOrigin([undefined, '', '   ', 'http://localhost:3001'], FALLBACK)).toBe(
      'http://localhost:3001',
    );
    expect(resolveFrontendOrigin([undefined, ''], FALLBACK)).toBe(FALLBACK);
  });

  it('normalises to an origin', () => {
    expect(resolveFrontendOrigin(['http://localhost:3001/'], FALLBACK)).toBe(
      'http://localhost:3001',
    );
    expect(resolveFrontendOrigin(['https://goosar.example.com/login?x=1'], FALLBACK)).toBe(
      'https://goosar.example.com',
    );
  });
});

import { describe, expect, it } from 'vitest';
import { NextRequest } from 'next/server';
import { DEFAULT_LOCALE } from '@goosar/core/i18n';
import { GOOSAR_LOCALE_HEADER } from './lib/locale-routing';
import { proxy } from './proxy';

function makeRequest(
  path: string,
  cookies: Record<string, string> = {},
  host = 'app.goosar.test',
  extraHeaders: Record<string, string> = {},
) {
  const cookieHeader = Object.entries(cookies)
    .map(([key, value]) => `${key}=${value}`)
    .join('; ');

  const headers: Record<string, string> = { ...extraHeaders };
  if (cookieHeader) headers['cookie'] = cookieHeader;

  return new NextRequest(`https://${host}${path}`, { headers });
}

function redirectLocation(path: string, cookies: Record<string, string> = {}, host?: string) {
  return proxy(makeRequest(path, cookies, host)).headers.get('location');
}

function restoreEnv(key: string, value: string | undefined) {
  if (value === undefined) delete process.env[key];
  else process.env[key] = value;
}

function withoutRuntimeUpstreams(run: () => void) {
  const previousRemoteApiUrl = process.env.REMOTE_API_URL;
  const previousDocsUrl = process.env.DOCS_URL;
  const previousPublicApiUrl = process.env.NEXT_PUBLIC_API_URL;
  const previousPort = process.env.PORT;
  delete process.env.REMOTE_API_URL;
  delete process.env.DOCS_URL;
  delete process.env.NEXT_PUBLIC_API_URL;
  process.env.PORT = '3000';

  try {
    run();
  } finally {
    restoreEnv('REMOTE_API_URL', previousRemoteApiUrl);
    restoreEnv('DOCS_URL', previousDocsUrl);
    restoreEnv('NEXT_PUBLIC_API_URL', previousPublicApiUrl);
    restoreEnv('PORT', previousPort);
  }
}

describe('proxy legacy workspace route redirects', () => {
  const sessionCookies = {
    goosar_logged_in: '1',
    last_workspace_slug: 'acme',
  };

  it.each([
    ['issues', '/acme/issues'],
    ['projects', '/acme/projects'],
    ['agents', '/acme/agents'],
    ['squads', '/acme/squads'],
    ['inbox', '/acme/inbox'],
    ['my-issues', '/acme/my-issues'],
    ['autopilots', '/acme/autopilots'],
    ['runtimes', '/acme/runtimes'],
    ['skills', '/acme/skills'],
    ['settings', '/acme/settings'],
    ['usage', '/acme/usage'],
  ])('redirects legacy /%s URLs through the last workspace slug', (segment, expectedPath) => {
    expect(redirectLocation(`/${segment}?tab=all`, sessionCookies)).toBe(
      `https://app.goosar.test${expectedPath}?tab=all`,
    );
  });

  it('preserves nested legacy paths and query strings', () => {
    expect(redirectLocation('/squads/squad-123?view=members', sessionCookies)).toBe(
      'https://app.goosar.test/acme/squads/squad-123?view=members',
    );
  });

  it('sends logged-out legacy URLs to login', () => {
    expect(redirectLocation('/usage?tab=billing')).toBe(
      'https://app.goosar.test/login?tab=billing',
    );
  });

  it('sends logged-in legacy URLs without a last workspace cookie to root', () => {
    expect(redirectLocation('/squads', { goosar_logged_in: '1' })).toBe('https://app.goosar.test/');
  });

  it('does not redirect workspace-scoped URLs whose first segment is already a slug', () => {
    expect(redirectLocation('/acme/squads', sessionCookies)).toBeNull();
  });

  it('redirects app-host root URLs to the last workspace', () => {
    expect(redirectLocation('/', sessionCookies)).toBe('https://app.goosar.test/acme/issues');
  });

  it.each(['goosar.ru', 'www.goosar.ru'])(
    'does not redirect public marketing root on %s',
    (host) => {
      expect(redirectLocation('/', sessionCookies, host)).toBeNull();
    },
  );

  it('still redirects explicit legacy app routes on the public marketing host', () => {
    expect(redirectLocation('/issues/ABC-123', sessionCookies, 'goosar.ru')).toBe(
      'https://goosar.ru/acme/issues/ABC-123',
    );
  });
});

describe('proxy runtime upstream rewrites', () => {
  it('does not rewrite API requests when no runtime API origin is configured', () => {
    withoutRuntimeUpstreams(() => {
      const res = proxy(makeRequest('/api/config?x=1'));

      expect(res.status).toBe(200);
      expect(res.headers.get('x-middleware-rewrite')).toBeNull();
      expect(res.headers.get(`x-middleware-request-${GOOSAR_LOCALE_HEADER}`)).toBe(DEFAULT_LOCALE);
    });
  });

  it('does not rewrite docs requests when no runtime docs origin is configured', () => {
    withoutRuntimeUpstreams(() => {
      const res = proxy(makeRequest('/docs/zh'));

      expect(res.status).toBe(200);
      expect(res.headers.get('x-middleware-rewrite')).toBeNull();
      expect(res.headers.get(`x-middleware-request-${GOOSAR_LOCALE_HEADER}`)).toBe(DEFAULT_LOCALE);
    });
  });

  it('rewrites API requests to the runtime API origin', () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = 'http://backend:8080';
    try {
      const res = proxy(makeRequest('/api/config?x=1'));

      expect(res.status).toBe(200);
      expect(res.headers.get('x-middleware-rewrite')).toBe('http://backend:8080/api/config?x=1');
    } finally {
      restoreEnv('REMOTE_API_URL', previous);
    }
  });

  it('rewrites docs requests to the runtime docs origin', () => {
    const previous = process.env.DOCS_URL;
    process.env.DOCS_URL = 'http://docs:4000';
    try {
      const res = proxy(makeRequest('/docs/zh/agents'));

      expect(res.status).toBe(200);
      expect(res.headers.get('x-middleware-rewrite')).toBe('http://docs:4000/docs/zh/agents');
    } finally {
      restoreEnv('DOCS_URL', previous);
    }
  });

  it('rewrites websocket requests to the runtime API origin', () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = 'http://backend:8080';
    try {
      const res = proxy(makeRequest('/ws'));

      expect(res.status).toBe(200);
      expect(res.headers.get('x-middleware-rewrite')).toBe('http://backend:8080/ws');
    } finally {
      restoreEnv('REMOTE_API_URL', previous);
    }
  });

  it('does not rewrite frontend auth callback pages', () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = 'http://backend:8080';
    try {
      const res = proxy(makeRequest('/auth/callback'));

      expect(res.status).toBe(200);
      expect(res.headers.get('x-middleware-rewrite')).toBeNull();
      expect(res.headers.get(`x-middleware-request-${GOOSAR_LOCALE_HEADER}`)).toBe(DEFAULT_LOCALE);
    } finally {
      restoreEnv('REMOTE_API_URL', previous);
    }
  });
});

describe('proxy root and locale handling', () => {
  it('redirects logged-in root visits to the last workspace', () => {
    const res = proxy(
      makeRequest('/', {
        goosar_logged_in: '1',
        last_workspace_slug: 'acme',
      }),
    );

    expect(res.status).toBe(307);
    expect(res.headers.get('location')).toBe('https://app.goosar.test/acme/issues');
  });

  it('forwards locale on login requests', () => {
    const res = proxy(makeRequest('/login', { 'goosar-locale': 'zh-Hans' }));

    expect(res.status).toBe(200);
    expect(res.headers.get('location')).toBeNull();
    expect(res.headers.get(`x-middleware-request-${GOOSAR_LOCALE_HEADER}`)).toBe('zh-Hans');
  });

  it('serves the default locale to a clean profile despite an English Accept-Language', () => {
    const res = proxy(
      makeRequest('/login', {}, 'app.goosar.test', {
        'accept-language': 'en-US,en;q=0.9',
      }),
    );

    expect(res.status).toBe(200);
    expect(res.headers.get(`x-middleware-request-${GOOSAR_LOCALE_HEADER}`)).toBe(DEFAULT_LOCALE);
  });

  it('ignores Accept-Language for every other browser language too', () => {
    for (const acceptLanguage of [
      'zh-CN,zh;q=0.9,en;q=0.8',
      'ko-KR,ko;q=0.9,en;q=0.8',
      'ja-JP,ja;q=0.9,en;q=0.8',
    ]) {
      const res = proxy(
        makeRequest('/login', {}, 'app.goosar.test', {
          'accept-language': acceptLanguage,
        }),
      );

      expect(res.headers.get(`x-middleware-request-${GOOSAR_LOCALE_HEADER}`)).toBe(DEFAULT_LOCALE);
    }
  });

  it('keeps an explicitly stored English cookie over a Russian Accept-Language', () => {
    const res = proxy(
      makeRequest('/login', { 'goosar-locale': 'en' }, 'app.goosar.test', {
        'accept-language': 'ru-RU,ru;q=0.9',
      }),
    );

    expect(res.headers.get(`x-middleware-request-${GOOSAR_LOCALE_HEADER}`)).toBe('en');
  });
});

import { NextResponse, type NextRequest } from 'next/server';
import { LOCALE_COOKIE } from '@goosar/core/i18n';
import { GOOSAR_LOCALE_HEADER, resolveLocaleFromCookie } from './lib/locale-routing';
import { runtimeRewriteDestination } from './config/runtime-urls';
import { isOfficialMarketingHost } from './lib/public-host';
import { buildDocumentImageCSP } from './lib/image-policy-csp';

const DOCUMENT_IMAGE_CSP = buildDocumentImageCSP(process.env);

const LEGACY_ROUTE_SEGMENTS = new Set([
  'issues',
  'projects',
  'agents',
  'squads',
  'inbox',
  'my-issues',
  'autopilots',
  'runtimes',
  'skills',
  'settings',
  'usage',
]);

function resolveLocale(req: NextRequest): string {
  return resolveLocaleFromCookie(req.cookies.get(LOCALE_COOKIE)?.value);
}

function nextWithLocale(req: NextRequest): NextResponse {
  const headers = new Headers(req.headers);
  headers.set(GOOSAR_LOCALE_HEADER, resolveLocale(req));
  const res = NextResponse.next({ request: { headers } });
  if (DOCUMENT_IMAGE_CSP !== null && !isBackendProxiedPath(req.nextUrl.pathname)) {
    res.headers.set('Content-Security-Policy', DOCUMENT_IMAGE_CSP);
  }
  return res;
}

function isBackendProxiedPath(pathname: string): boolean {
  if (pathname === '/ws' || pathname === '/docs') return true;
  return ['/api/', '/auth/', '/uploads/', '/docs/'].some((prefix) => pathname.startsWith(prefix));
}

export function proxy(req: NextRequest) {
  const { pathname } = req.nextUrl;
  const runtimeDestination = runtimeRewriteDestination(pathname, process.env);
  if (runtimeDestination) {
    const url = new URL(runtimeDestination);
    url.search = req.nextUrl.search;
    return NextResponse.rewrite(url);
  }

  const hasSession = req.cookies.has('goosar_logged_in');
  const lastSlug = req.cookies.get('last_workspace_slug')?.value;

  const firstSegment = pathname.split('/')[1] ?? '';
  if (LEGACY_ROUTE_SEGMENTS.has(firstSegment)) {
    const url = req.nextUrl.clone();

    if (!hasSession) {
      url.pathname = '/login';
      return NextResponse.redirect(url);
    }

    if (lastSlug) {
      url.pathname = `/${lastSlug}${pathname}`;
      return NextResponse.redirect(url);
    }

    url.pathname = '/';
    return NextResponse.redirect(url);
  }

  if (
    pathname === '/' &&
    hasSession &&
    lastSlug &&
    !isOfficialMarketingHost(req.nextUrl.hostname)
  ) {
    const url = req.nextUrl.clone();
    url.pathname = `/${lastSlug}/issues`;
    return NextResponse.redirect(url);
  }

  return nextWithLocale(req);
}

export const config = {
  matcher: [
    '/api/:path*',
    '/auth/:path*',
    '/uploads/:path*',
    '/docs/:path*',
    '/ws',
    '/((?!api|_next/static|_next/image|favicon.ico|.*\\.).*)',
  ],
};

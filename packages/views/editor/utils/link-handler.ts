// Общие утилиты обработки ссылок редактора: клик в ProseMirror, ссылки
// в readonly-контенте, кнопка открытия в карточке.

import { isGlobalPath, isReservedSlug } from '@goosar/core/paths';

const WORKSPACE_ROUTE_SEGMENTS = new Set([
  'usage',
  'issues',
  'projects',
  'autopilots',
  'agents',
  'chat',
  'inbox',
  'my-issues',
  'runtimes',
  'skills',
  'settings',
  'capabilities',
]);

function isWorkspaceScopedPath(pathname: string): boolean {
  const first = pathname.split('/')[1] ?? '';
  if (!first) return false;
  let segment: string;
  try {
    segment = decodeURIComponent(first);
  } catch {
    return false;
  }
  return !isReservedSlug(segment.toLowerCase());
}

export function toInternalAppPath(href: string, appOrigin?: string | null): string | null {
  if (!appOrigin) return null;
  let target: URL;
  let app: URL;
  try {
    target = new URL(href);
    app = new URL(appOrigin);
  } catch {
    return null;
  }
  if (target.origin !== app.origin) return null;
  if (target.protocol !== 'http:' && target.protocol !== 'https:') return null;
  if (!isWorkspaceScopedPath(target.pathname)) return null;
  return `${target.pathname}${target.search}${target.hash}`;
}

export function openLink(
  href: string,
  currentSlug?: string | null,
  appOrigin?: string | null,
): void {
  const internalPath = href.startsWith('/') ? href : toInternalAppPath(href, appOrigin);
  if (internalPath) {
    let path = internalPath;
    if (currentSlug && !isGlobalPath(path)) {
      const firstSegment = path.split('/')[1];
      if (firstSegment && WORKSPACE_ROUTE_SEGMENTS.has(firstSegment)) {
        path = `/${currentSlug}${path}`;
      }
      // Otherwise the first segment is either already a slug (e.g. "acme" in
      // "/acme/issues") or something unknown (e.g. "/foo"). Leave it alone —
      // the user wrote what they meant.
    }
    window.dispatchEvent(new CustomEvent('goosar:navigate', { detail: { path } }));
  } else {
    window.open(href, '_blank', 'noopener,noreferrer');
  }
}

export function isMentionHref(href: string | null | undefined): href is string {
  return !!href && href.startsWith('mention://');
}

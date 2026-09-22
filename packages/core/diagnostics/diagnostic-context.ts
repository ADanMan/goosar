// Где, по мнению приложения, находится пользователь — единственное поле, без которого отчёт о зависании бесполезен.

let route: string | null = null;

const MAX_ROUTE_LENGTH = 256;

export function setDiagnosticRoute(next: string | null): void {
  if (typeof next !== 'string') {
    route = null;
    return;
  }
  const trimmed = next.trim();
  route = trimmed ? trimmed.slice(0, MAX_ROUTE_LENGTH) : null;
}

export function getDiagnosticRoute(): string | null {
  return route;
}

export function resetDiagnosticContext(): void {
  route = null;
}

type RoutePattern = readonly string[];

const WORKSPACE_ROUTES: readonly RoutePattern[] = [
  ['issues'],
  ['issues', ':id'],
  ['projects'],
  ['projects', ':id'],
  ['autopilots'],
  ['autopilots', ':id'],
  ['agents'],
  ['agents', 'new'],
  ['agents', ':id'],
  ['members', ':id'],
  ['squads'],
  ['squads', ':id'],
  ['inbox'],
  ['chat'],
  ['my-issues'],
  ['usage'],
  ['billing'],
  ['runtimes'],
  ['runtimes', ':id'],
  ['runtimes', ':id', 'runtime', ':runtimeId'],
  ['skills'],
  ['skills', ':id'],
  ['settings'],
  ['capabilities'],
  ['attachments', ':id', 'preview'],
];

const GLOBAL_ROUTES: readonly RoutePattern[] = [
  ['login'],
  ['signup'],
  ['logout'],
  ['workspaces', 'new'],
  ['invite', ':id'],
  ['invitations'],
  ['onboarding'],
  ['auth', 'callback'],
];

const WORKSPACE_SECTIONS = new Set(WORKSPACE_ROUTES.map((route) => route[0]!));
const GLOBAL_SECTIONS = new Set(GLOBAL_ROUTES.map((route) => route[0]!));

export function bucketDiagnosticPath(path: string): string {
  const [pathname = ''] = path.split(/[?#]/);
  const segments = pathname.split('/').filter(Boolean);
  if (segments.length === 0) return '/';

  const globalRoute = matchRoute(segments, GLOBAL_ROUTES);
  if (globalRoute) return `/${globalRoute.join('/')}`;

  const first = segments[0]!;
  if (GLOBAL_SECTIONS.has(first)) return `/${first}/*`;

  const rest = segments.slice(1);
  if (rest.length === 0) return '/:slug';

  const scoped = matchRoute(rest, WORKSPACE_ROUTES);
  if (scoped) return `/:slug/${scoped.join('/')}`;

  const section = rest[0]!;
  return WORKSPACE_SECTIONS.has(section) ? `/:slug/${section}/*` : '/:slug/*';
}

function matchRoute(
  segments: readonly string[],
  patterns: readonly RoutePattern[],
): RoutePattern | null {
  let best: RoutePattern | null = null;
  let bestLiterals = -1;

  for (const pattern of patterns) {
    if (pattern.length !== segments.length) continue;

    let literals = 0;
    let matches = true;
    for (let i = 0; i < pattern.length; i += 1) {
      const slot = pattern[i]!;
      if (slot.startsWith(':')) continue;
      if (slot !== segments[i]) {
        matches = false;
        break;
      }
      literals += 1;
    }

    if (matches && literals > bestLiterals) {
      best = pattern;
      bestLiterals = literals;
    }
  }

  return best;
}

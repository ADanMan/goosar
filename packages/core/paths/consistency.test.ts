import { describe, it, expect } from 'vitest';
import { paths, isGlobalPath } from './paths';
import { RESERVED_SLUGS } from './reserved-slugs';

describe('paths.workspace() shape', () => {
  it('exposes the expected parameterless workspace route methods', () => {
    const ws = paths.workspace('__probe__');
    const parameterlessRoutes = Object.entries(ws)
      .filter(([, fn]) => typeof fn === 'function' && fn.length === 0)
      .map(([key]) => key);

    expect(new Set(parameterlessRoutes)).toEqual(
      new Set([
        'root',
        'usage',
        'issues',
        'projects',
        'autopilots',
        'agents',
        'newAgent',
        'chat',
        'squads',
        'inbox',
        'myIssues',
        'runtimes',
        'skills',
        'squads',
        'settings',
        'capabilities',
      ]),
    );
  });

  it('each parameterless route emits /{slug}/{segment}', () => {
    const ws = paths.workspace('acme');
    const expectedSegments: Array<[string, string]> = [
      ['usage', 'usage'],
      ['issues', 'issues'],
      ['projects', 'projects'],
      ['autopilots', 'autopilots'],
      ['agents', 'agents'],
      ['newAgent', 'agents/new'],
      ['chat', 'chat'],
      ['squads', 'squads'],
      ['inbox', 'inbox'],
      ['myIssues', 'my-issues'],
      ['runtimes', 'runtimes'],
      ['skills', 'skills'],
      ['squads', 'squads'],
      ['settings', 'settings'],
      ['capabilities', 'capabilities'],
    ];
    const wsAsAny = ws as unknown as Record<string, () => string>;
    for (const [method, segment] of expectedSegments) {
      const fn = wsAsAny[method];
      expect(typeof fn).toBe('function');
      expect(fn!()).toBe(`/acme/${segment}`);
    }
  });
});

describe('global path / reserved slug consistency', () => {
  const globalPrefixes = ['/login', '/logout', '/signup', '/workspaces/', '/invite/', '/auth/'];

  it('isGlobalPath agrees with the canonical global prefix list', () => {
    for (const prefix of globalPrefixes) {
      expect(isGlobalPath(prefix)).toBe(true);
    }
    expect(isGlobalPath('/acme/issues')).toBe(false);
    expect(isGlobalPath('/')).toBe(false);
  });

  it("every global prefix's first path segment is a reserved slug", () => {
    for (const prefix of globalPrefixes) {
      const firstSegment = prefix.split('/').filter(Boolean)[0];
      if (!firstSegment) continue;
      expect(
        RESERVED_SLUGS.has(firstSegment),
        `'${firstSegment}' is a global path prefix but not a reserved slug — ` +
          `a workspace could be created with this slug and shadow the global route`,
      ).toBe(true);
    }
  });
});

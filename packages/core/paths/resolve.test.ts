import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import type { Workspace } from '../types';
import { paths } from './paths';
import { resolvePostAuthDestination } from './resolve';

function makeWs(slug: string): Workspace {
  return {
    id: `id-${slug}`,
    name: slug,
    slug,
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: slug.toUpperCase(),
    avatar_url: null,
    created_at: '',
    updated_at: '',
  };
}

describe('resolvePostAuthDestination', () => {
  it('!onboarded → /onboarding (even with a workspace)', () => {
    const ws = [makeWs('acme')];
    expect(resolvePostAuthDestination(ws, false)).toBe(paths.onboarding());
    expect(resolvePostAuthDestination([], false)).toBe(paths.onboarding());
  });

  it('onboarded + workspace[0] → /<first.slug>/issues', () => {
    const ws = [makeWs('acme'), makeWs('beta')];
    expect(resolvePostAuthDestination(ws, true)).toBe(paths.workspace('acme').issues());
  });

  it('onboarded + no workspace → /workspaces/new', () => {
    expect(resolvePostAuthDestination([], true)).toBe(paths.newWorkspace());
  });
});

describe('post-auth routing depends on the server flag alone', () => {
  const code = readFileSync(
    resolve(dirname(fileURLToPath(import.meta.url)), 'resolve.ts'),
    'utf8',
  ).replace(/\/\*[\s\S]*?\*\/|\/\/[^\n]*/g, '');

  it('knows nothing about the local completion mark', () => {
    expect(code).not.toMatch(/completion-mark|PendingOnboardingCompletion/);
  });

  it('reads onboarding state only from users.onboarded_at', () => {
    expect(code).toMatch(/s\.user\?\.onboarded_at\s*!==?\s*null/);
  });

  it('routes a not-onboarded user to /onboarding whatever else is true', () => {
    expect(resolvePostAuthDestination([makeWs('acme')], false)).toBe(paths.onboarding());
  });
});

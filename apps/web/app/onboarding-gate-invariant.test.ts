import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const LAYOUT = resolve(dirname(fileURLToPath(import.meta.url)), '[workspaceSlug]/layout.tsx');

function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\/|\/\/[^\n]*/g, '');
}

describe('web workspace gate', () => {
  const code = stripComments(readFileSync(LAYOUT, 'utf8'));

  it('redirects on the server flag alone', () => {
    expect(code).toMatch(/user\.onboarded_at\s*===?\s*null/);
  });

  it('knows nothing about the local completion mark', () => {
    expect(code).not.toMatch(/[Cc]ompletion[Mm]ark|PendingOnboarding/);
  });
});

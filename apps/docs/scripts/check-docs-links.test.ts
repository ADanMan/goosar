import { describe, expect, it } from 'vitest';
import { checkDocsLinks, collectProductDocsLinks } from './check-docs-links.mjs';

describe('docs links emitted by the product', () => {
  it('finds the links at all (guards against a silently empty scan)', () => {
    const links = collectProductDocsLinks() as { path: string }[];
    expect(links.length).toBeGreaterThan(0);
    expect(links.map((l) => l.path)).toContain('/install-agent-runtime');
    expect(links.map((l) => l.path)).toContain('/custom-runtimes');
  });

  it('every one resolves to a docs page, with anchors pinned', () => {
    expect(checkDocsLinks()).toEqual([]);
  });
});

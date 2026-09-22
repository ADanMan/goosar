import { beforeEach, describe, expect, it, vi } from 'vitest';

const existingDocs = vi.hoisted(() => new Set<string>());

vi.mock('node:fs', () => ({
  existsSync: vi.fn((path: string) => {
    const normalized = path.replaceAll('\\', '/');
    return [...existingDocs].some((suffix) => normalized.endsWith(suffix));
  }),
}));

const pages = new Map<string, { url: string }>([
  ['ru:', { url: '/' }],
  ['en:', { url: '/en' }],
  ['ru:security', { url: '/security' }],
  ['en:security', { url: '/en/security' }],
  ['ru:developers/architecture', { url: '/developers/architecture' }],
  ['en:developers/architecture', { url: '/en/developers/architecture' }],
]);

vi.mock('@/lib/source', () => ({
  source: {
    getPage: vi.fn((slugs: string[], lang: string) => {
      return pages.get(`${lang}:${slugs.join('/')}`) ?? null;
    }),
  },
}));

beforeEach(() => {
  existingDocs.clear();
  existingDocs.add('index.mdx');
  existingDocs.add('security.mdx');
  existingDocs.add('developers/architecture.mdx');
  existingDocs.add('developers/architecture.en.mdx');
});

describe('docsAlternates', () => {
  it('omits English hreflang when no English MDX file exists for the page', async () => {
    const { docsAlternates } = await import('./site');

    expect(docsAlternates(['security'])).toEqual({
      canonical: 'https://www.goosar.ru/docs/security',
      languages: {
        ru: 'https://www.goosar.ru/docs/security',
        'x-default': 'https://www.goosar.ru/docs/security',
      },
    });
  });

  it('omits English hreflang even when source.getPage returns a page for English', async () => {
    const { docsAlternates } = await import('./site');

    expect(docsAlternates(['security']).languages).not.toHaveProperty('en');
  });

  it('includes English hreflang when a real *.en.mdx page exists', async () => {
    const { docsAlternates } = await import('./site');

    expect(docsAlternates(['developers', 'architecture'])).toEqual({
      canonical: 'https://www.goosar.ru/docs/developers/architecture',
      languages: {
        ru: 'https://www.goosar.ru/docs/developers/architecture',
        en: 'https://www.goosar.ru/docs/en/developers/architecture',
        'x-default': 'https://www.goosar.ru/docs/developers/architecture',
      },
    });
  });

  it('keeps the root alternates limited to real localized MDX pages', async () => {
    const { docsAlternates } = await import('./site');

    expect(docsAlternates([])).toEqual({
      canonical: 'https://www.goosar.ru/docs',
      languages: {
        ru: 'https://www.goosar.ru/docs',
        'x-default': 'https://www.goosar.ru/docs',
      },
    });
  });
});

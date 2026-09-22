import { describe, expect, it } from 'vitest';
import { docsSlugStaticParams } from './static-params';

// `source.generateParams()` hands back loosely-typed params (`lang: string`),
// so the inputs here mirror that shape — the `lang` strings are validated and
// narrowed by `docsSlugStaticParams` itself.
type RawParam = { lang: string; slug: string[] };

describe('docsSlugStaticParams', () => {
  it('returns every localized slug page and drops the home param', () => {
    // The only transform is dropping the empty-slug home param (rendered by
    // `[lang]/page.tsx`, not the catch-all route).
    const params: RawParam[] = [
      { lang: 'ru', slug: [] },
      { lang: 'ru', slug: ['security'] },
      { lang: 'ru', slug: ['developers', 'conventions'] },
      { lang: 'en', slug: [] },
      { lang: 'en', slug: ['developers', 'conventions'] },
    ];

    expect(docsSlugStaticParams(params)).toEqual([
      { lang: 'ru', slug: ['security'] },
      { lang: 'ru', slug: ['developers', 'conventions'] },
      { lang: 'en', slug: ['developers', 'conventions'] },
    ]);
  });

  it('drops unknown languages and de-duplicates repeated params', () => {
    const params: RawParam[] = [
      { lang: 'en', slug: ['developers', 'architecture'] },
      { lang: 'en', slug: ['developers', 'architecture'] },
      { lang: 'ja', slug: ['developers', 'architecture'] },
    ];

    expect(docsSlugStaticParams(params)).toEqual([
      { lang: 'en', slug: ['developers', 'architecture'] },
    ]);
  });
});

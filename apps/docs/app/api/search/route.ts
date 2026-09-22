import { source } from '@/lib/source';
import { createFromSource } from 'fumadocs-core/search/server';

// Orama's built-in English regex strips every non-Latin character, so a
// Cyrillic query would either return nothing or throw. Tokenize by Unicode
// letter/digit runs instead, which keeps Russian words and Latin identifiers
// (product names, CLI commands) whole.
function tokenizeUnicode(raw: string): string[] {
  return raw.toLowerCase().match(/[\p{L}\p{N}_]+/gu) ?? [];
}

export const { GET } = createFromSource(source, {
  localeMap: {
    ru: {
      components: {
        tokenizer: {
          language: 'english',
          normalizationCache: new Map(),
          tokenize: tokenizeUnicode,
        },
      },
    },
  },
});

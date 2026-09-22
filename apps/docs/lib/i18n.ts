import { defineI18n } from 'fumadocs-core/i18n';

// Russian is the default; English (/en/) is kept for the developer section.
// hideLocale: 'default-locale' keeps Russian URLs prefix-free (`/docs/`) while
// English lives under `/docs/en/...`.
// parser: 'dot' picks up `page.en.mdx` and `meta.en.json`.
// Locales may be partially translated: Fumadocs falls back to the default
// language for any page that has no `<slug>.en.mdx`.
export const i18n = defineI18n({
  languages: ['ru', 'en'],
  defaultLanguage: 'ru',
  hideLocale: 'default-locale',
  parser: 'dot',
});

export type Lang = (typeof i18n.languages)[number];

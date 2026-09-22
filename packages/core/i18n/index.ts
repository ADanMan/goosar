// Серверно-безопасный вход i18n: без React и DOM.
export type { LocaleAdapter, LocaleResources, SupportedLocale } from './types';
export { DEFAULT_LOCALE, SUPPORTED_LOCALES } from './types';
export { matchLocale, pickLocale } from './pick-locale';
export { LOCALE_COOKIE } from './browser-cookie-adapter';
export { DOCS_BASE_PATH, DOCS_ORIGIN, docsLocalePath, docsUrl } from './docs-url';

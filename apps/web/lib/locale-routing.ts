import { matchLocale, SUPPORTED_LOCALES, type SupportedLocale } from '@goosar/core/i18n';

export const GOOSAR_LOCALE_HEADER = 'x-goosar-locale';

export function isSupportedLocale(value: string | null): value is SupportedLocale {
  return value !== null && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

export function resolveLocaleFromCookie(cookieLocale?: string | null): SupportedLocale {
  return matchLocale(cookieLocale ? [cookieLocale] : []);
}

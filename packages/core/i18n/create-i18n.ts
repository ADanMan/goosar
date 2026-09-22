import i18next, { type i18n as I18n } from 'i18next';
import { initReactI18next } from 'react-i18next';
import type { LocaleResources, SupportedLocale } from './types';

export function createI18n(
  locale: SupportedLocale,
  resources: Record<string, LocaleResources>,
): I18n {
  const instance = i18next.createInstance();
  instance.use(initReactI18next).init({
    lng: locale,
    fallbackLng: 'en',
    resources,
    interpolation: { escapeValue: false },
    initAsync: false,
    react: { useSuspense: false },
  });
  return instance;
}

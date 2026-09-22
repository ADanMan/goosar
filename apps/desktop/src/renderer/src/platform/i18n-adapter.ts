import type { LocaleAdapter, SupportedLocale } from '@goosar/core/i18n';

const STORAGE_KEY = 'goosar-locale';

const EXPLICIT_CHOICE_KEY = 'goosar-locale-explicit';
const EXPLICIT_CHOICE_VALUE = '1';

export function createDesktopLocaleAdapter(): LocaleAdapter {
  return {
    getUserChoice() {
      try {
        if (window.localStorage.getItem(EXPLICIT_CHOICE_KEY) !== EXPLICIT_CHOICE_VALUE) {
          return null;
        }
        return window.localStorage.getItem(STORAGE_KEY);
      } catch {
        return null;
      }
    },
    persist(locale: SupportedLocale) {
      try {
        window.localStorage.setItem(STORAGE_KEY, locale);
        window.localStorage.setItem(EXPLICIT_CHOICE_KEY, EXPLICIT_CHOICE_VALUE);
      } catch {
        // Best-effort
      }
    },
  };
}

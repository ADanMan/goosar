export type SupportedLocale = 'en' | 'zh-Hans' | 'ko' | 'ja' | 'ru';

export const SUPPORTED_LOCALES: SupportedLocale[] = ['en', 'zh-Hans', 'ko', 'ja', 'ru'];
export const DEFAULT_LOCALE: SupportedLocale = 'ru';

export type LocaleResources = Record<string, Record<string, unknown>>;

export interface LocaleAdapter {
  getUserChoice(): string | null;
  persist(locale: SupportedLocale): void;
}

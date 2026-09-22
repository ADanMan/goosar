// Renderer → main: локаль, в которой рендерер реально отрисовался.
export const UI_LOCALE_CHANNEL = 'ui:locale';

export type DesktopUiLocale = 'en' | 'zh-Hans' | 'ko' | 'ja' | 'ru';

export const DESKTOP_UI_LOCALES: DesktopUiLocale[] = ['en', 'zh-Hans', 'ko', 'ja', 'ru'];

export const DEFAULT_DESKTOP_UI_LOCALE: DesktopUiLocale = 'ru';

export function parseUiLocale(value: unknown): DesktopUiLocale | null {
  if (typeof value !== 'string') return null;
  const locale = value.trim();
  return (DESKTOP_UI_LOCALES as readonly string[]).includes(locale)
    ? (locale as DesktopUiLocale)
    : null;
}

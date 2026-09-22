// Единственный источник ответа «какая локаль документации у этого читателя».
const DOCS_LOCALE_SEGMENT: Record<string, string> = {
  en: 'en',
};

export const DOCS_ORIGIN = 'https://goosar.ru';
export const DOCS_BASE_PATH = '/docs';

export function docsLocalePath(language: string | null | undefined): string {
  const base = (language ?? '').toLowerCase().split('-')[0] ?? '';
  const segment = DOCS_LOCALE_SEGMENT[base];
  return segment ? `${DOCS_BASE_PATH}/${segment}` : DOCS_BASE_PATH;
}

export function docsUrl(language: string | null | undefined, path = ''): string {
  return `${DOCS_ORIGIN}${docsLocalePath(language)}${path}`;
}

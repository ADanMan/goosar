/**
 * Приводит относительный URL вложения к абсолютному, используя настроенный
 * базовый адрес API.
 *
 * Нужно потому, что бэкенд без настроенного подписывающего сервиса (CDN)
 * возвращает URL вложений в виде относительных путей вида
 * `/api/attachments/{id}/download`. Для веба это нормально (резолвится
 * относительно document base), а RN требует абсолютный http(s) URL и для
 * Linking.openURL, и для <Image>.
 *
 * Контракт:
 *   - null / undefined / "" → null;
 *   - уже абсолютный URL → возвращается без изменений;
 *   - относительный путь → базовый URL + путь, с одним разделяющим слэшем.
 */

const API_URL = process.env.EXPO_PUBLIC_API_URL ?? '';

export function resolveAttachmentUrlWithBase(
  rawUrl: string | null | undefined,
  baseUrl: string,
): string | null {
  if (!rawUrl) return null;
  if (!rawUrl.startsWith('/')) return rawUrl;
  const trimmedBaseUrl = baseUrl.replace(/\/+$/, '');
  return `${trimmedBaseUrl}${rawUrl}`;
}

export function resolveAttachmentUrl(rawUrl: string | null | undefined): string | null {
  return resolveAttachmentUrlWithBase(rawUrl, API_URL);
}

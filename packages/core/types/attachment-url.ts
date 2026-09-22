/**
 * Хелперы для стабильных URL вложений, которые сохраняются в markdown-тело
 * (описания issue, комментарии, сообщения чата).
 *
 * `att.url` для бэкенда LocalStorage — это подписанная ссылка со сроком
 * жизни 30 минут, нужная только для загрузки ресурса браузером без заголовка
 * Authorization. Сохранять такую ссылку в тело комментария нельзя — она
 * протухнет. То же верно для подписанных редиректов CloudFront.
 *
 * Поэтому сохраняется стабильный путь, содержащий только id вложения:
 * /api/attachments/{id}/download сам разрешает workspace по записи вложения
 * и переподписывает (или проксирует) ссылку на каждый запрос, так что путь
 * остаётся рабочим, пока запись вложения не удалена.
 */

const DOWNLOAD_PREFIX = '/api/attachments/';
const DOWNLOAD_SUFFIX = '/download';

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function attachmentDownloadPath(attachmentId: string): string {
  return `${DOWNLOAD_PREFIX}${attachmentId}${DOWNLOAD_SUFFIX}`;
}

export function attachmentIdFromDownloadURL(rawURL: string): string | undefined {
  if (!rawURL) return undefined;

  let path = rawURL;
  const qi = path.indexOf('?');
  if (qi >= 0) path = path.slice(0, qi);
  const hi = path.indexOf('#');
  if (hi >= 0) path = path.slice(0, hi);

  if (/^https?:\/\//i.test(path)) {
    try {
      path = new URL(path).pathname;
    } catch {
      return undefined;
    }
  }

  if (!path.startsWith(DOWNLOAD_PREFIX)) return undefined;
  if (!path.endsWith(DOWNLOAD_SUFFIX)) return undefined;

  const id = path.slice(DOWNLOAD_PREFIX.length, path.length - DOWNLOAD_SUFFIX.length);
  if (!UUID_RE.test(id)) return undefined;
  return id;
}

function stripQueryAndFragment(url: string): string {
  return url.split(/[?#]/, 1)[0] ?? '';
}

function contentReferencesURL(content: string, url?: string): boolean {
  if (!url) return false;
  if (content.includes(url)) return true;
  const stable = stripQueryAndFragment(url);
  return stable !== '' && content.includes(stable);
}

export function contentReferencesAttachment(
  content: string,
  attachment: {
    id: string;
    url: string;
    download_url?: string;
    markdown_url?: string;
  },
): boolean {
  if (!content) return false;
  if (content.includes(attachmentDownloadPath(attachment.id))) return true;
  if (contentReferencesURL(content, attachment.url)) return true;
  if (contentReferencesURL(content, attachment.download_url)) return true;
  if (contentReferencesURL(content, attachment.markdown_url)) return true;
  return false;
}

// Content-Security-Policy документов приложения — вторая точка применения
// политики внешних изображений markdown (первая — Go-middleware для ответов backend).

export type ExternalImagesMode = 'allow' | 'block' | 'allowlist';

type Env = Record<string, string | undefined>;

export function externalImagesMode(env: Env): ExternalImagesMode {
  const value = (env.GOOSAR_EXTERNAL_IMAGES ?? '').trim().toLowerCase();
  if (value === '' || value === 'allow') return 'allow';
  if (value === 'allowlist') return 'allowlist';
  return 'block';
}

const CSP_SOURCE_RE = /^(?:https?:\/\/)?[A-Za-z0-9][A-Za-z0-9.-]*(?::\d{1,5})?$/;

function sanitizeImageHostEntry(raw: string): string {
  let entry = raw.trim();
  if (entry === '') return '';
  if (entry.includes('://')) {
    try {
      const url = new URL(entry);
      if ((url.protocol !== 'http:' && url.protocol !== 'https:') || url.hostname === '') {
        return '';
      }
      entry = `${url.protocol}//${url.host}`;
    } catch {
      return '';
    }
  }
  return CSP_SOURCE_RE.test(entry) ? entry : '';
}

export function buildDocumentImageCSP(env: Env): string | null {
  const mode = externalImagesMode(env);
  if (mode === 'allow') return null;

  const sources = ["'self'", 'data:'];
  const seen = new Set<string>(sources);
  const append = (entry: string): void => {
    if (entry === '' || seen.has(entry)) return;
    seen.add(entry);
    sources.push(entry);
  };

  append(sanitizeImageHostEntry(env.CLOUDFRONT_DOMAIN ?? ''));
  append(sanitizeImageHostEntry(env.LOCAL_UPLOAD_BASE_URL ?? ''));

  if (mode === 'allowlist') {
    for (const raw of (env.GOOSAR_IMAGE_HOSTS ?? '').split(',')) {
      append(sanitizeImageHostEntry(raw));
    }
  }

  return `img-src ${sources.join(' ')}`;
}

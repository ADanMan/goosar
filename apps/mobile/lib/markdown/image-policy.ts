/**
 * Политика внешних изображений в markdown для мобильного клиента.
 * Логика скопирована с веб-версии (без прямого импорта): мобильный
 * клиент получает политику явным параметром из /api/config, а не через
 * реактивный стейт.
 *
 * Семантика должна совпадать с вебом:
 *   - режим `allow` (по умолчанию, включая старые серверы без поля или
 *     неудачный запрос конфига) — внешние изображения грузятся как есть;
 *   - `block` / `allowlist` — внешние http(s)-изображения не загружаются,
 *     кроме тех же origin, `data:image/*` и доверенных хостов; неизвестные
 *     непустые значения режима трактуются как `block`;
 *   - изображения, разрешённые через найденное вложение, считаются
 *     доверенными по построению.
 */

export type ExternalImagesMode = 'allow' | 'block' | 'allowlist';

export function normalizeExternalImagesMode(raw?: string): ExternalImagesMode {
  const value = (raw ?? '').trim().toLowerCase();
  if (value === '' || value === 'allow') return 'allow';
  if (value === 'allowlist') return 'allowlist';
  return 'block';
}

function normalizeHost(entry: string): string {
  const trimmed = entry.trim().toLowerCase();
  if (trimmed === '') return '';
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//.test(trimmed) ? trimmed : `https://${trimmed}`;
  try {
    return new URL(withScheme).hostname;
  } catch {
    return '';
  }
}

export function buildTrustedImageHosts(input: {
  imageHosts?: readonly string[];
  cdnDomain?: string;
  apiBaseUrl?: string;
}): ReadonlySet<string> {
  const hosts = new Set<string>();
  for (const entry of [
    ...(input.imageHosts ?? []),
    input.cdnDomain ?? '',
    input.apiBaseUrl ?? '',
  ]) {
    const host = normalizeHost(entry);
    if (host !== '') hosts.add(host);
  }
  return hosts;
}

const SCHEME_RE = /^[a-z][a-z0-9+.-]*:/i;

function stripControlChars(value: string): string {
  let out = '';
  for (const ch of value) {
    if ((ch.codePointAt(0) ?? 0) > 0x20) out += ch;
  }
  return out;
}

export function isTrustedMarkdownImageUri(
  uri: string,
  mode: ExternalImagesMode,
  trustedHosts: ReadonlySet<string>,
): boolean {
  if (typeof uri !== 'string' || uri === '') return false;
  if (/^data:image\//i.test(uri)) return true;

  const cleaned = stripControlChars(uri).replace(/\\/g, '/');
  if (cleaned === '') return false;

  const isProtocolRelative = cleaned.startsWith('//');
  if (!isProtocolRelative && !SCHEME_RE.test(cleaned)) {
    return true;
  }
  if (!isProtocolRelative && !/^https?:/i.test(cleaned)) return false;

  if (mode === 'allow') return true;

  try {
    const url = new URL(isProtocolRelative ? `https:${cleaned}` : cleaned);
    return trustedHosts.has(url.hostname.toLowerCase());
  } catch {
    return false;
  }
}

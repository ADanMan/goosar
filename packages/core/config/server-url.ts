/**
 * Нормализует введённый пользователем адрес сервера, чтобы обычный `http://`
 * адрес не мог незаметно сломать bearer-авторизацию.
 *
 * Апгрейд адреса до `https://` до того, как он будет сохранён или использован,
 * полностью убирает редирект, при котором браузер/Electron не пробрасывают
 * заголовок `Authorization` через кросс-доменный редирект схемы.
 *
 * Правила:
 *  - обрезает пробелы; пустое значение остаётся пустым
 *  - значению без схемы добавляется `https://`
 *  - `http://` апгрейдится до `https://`, кроме loopback и приватных/LAN-адресов,
 *    чтобы локальные dev-серверы продолжали работать по обычному http
 *  - `https://` не трогается
 *  - обрезается завершающий слэш
 *
 * Чистая функция без побочных эффектов. Не валидирует результат дополнительно —
 * значение, которое всё равно не парсится как URL, возвращается обрезанным,
 * но неизменным, чтобы реальную проблему показала валидация вызывающего поля.
 */
export function normalizeServerUrl(value: string): string {
  const trimmed = (value ?? '').trim();
  if (trimmed === '') return trimmed;

  const hasScheme = /^[a-zA-Z][a-zA-Z\d+.-]*:\/\//.test(trimmed);
  const candidate = hasScheme ? trimmed : `https://${trimmed}`;

  let url: URL;
  try {
    url = new URL(candidate);
  } catch {
    return trimmed;
  }

  if (url.protocol === 'http:' && !isLoopbackOrPrivateHost(url.hostname)) {
    url.protocol = 'https:';
  }

  return trimTrailingSlash(url.toString());
}

function isLoopbackOrPrivateHost(hostname: string): boolean {
  const host = hostname.toLowerCase();
  if (host === 'localhost' || host === '[::1]') return true;
  if (/^127\./.test(host)) return true; 
  if (/^10\./.test(host)) return true; 
  if (/^192\.168\./.test(host)) return true; 
  if (/^172\.(1[6-9]|2\d|3[01])\./.test(host)) return true; 
  return false;
}

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, '');
}

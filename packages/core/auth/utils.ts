/**
 * Проверяет URL для редиректа после логина и возвращает его, только если он безопасен.
 *
 * Допускаются только относительные пути с одним слэшем (например, `/invite/abc`).
 * Для небезопасного или пустого значения возвращает `null` — запасной вариант
 * выбирает вызывающий код.
 *
 * Отклоняет: пустую строку/null, абсолютные и protocol-relative URL,
 * пути с обратными слэшами и пути с управляющими ASCII-символами.
 */
export function sanitizeNextUrl(raw: string | null): string | null {
  if (!raw) return null;
  if (!raw.startsWith('/') || raw.startsWith('//')) return null;
  // eslint-disable-next-line no-control-regex -- intentional: rejecting control chars is the whole point
  if (/[\x00-\x1f\\]/.test(raw)) return null;
  return raw;
}

/**
 * Преобразует ошибки аутентификации от бэкенда в понятные пользователю
 * сообщения. Бэкенд отдаёт сырые английские тексты, пригодные для логов,
 * но не для показа как есть — известные случаи маппятся на дружелюбные
 * формулировки, остальное падает на переданный по умолчанию текст.
 */
export function mapAuthError(err: unknown, fallback: string): string {
  if (!(err instanceof Error)) return fallback;
  const msg = err.message.toLowerCase();
  if (/invalid|incorrect|wrong/.test(msg)) {
    return "That code didn't match. Double-check and try again.";
  }
  if (/expired/.test(msg)) {
    return 'That code has expired. Tap resend to get a new one.';
  }
  if (/rate.?limit|too many|throttle/.test(msg)) {
    return 'Too many attempts. Wait a moment and try again.';
  }
  if (/network|fetch|timeout|unreachable/.test(msg)) {
    return "Can't reach Goosar. Check your connection and retry.";
  }
  return fallback;
}

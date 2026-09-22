// Заведомо безобидные исключения браузера, которые являются чистым шумом в `$exception`.

const BENIGN_MESSAGE_PATTERNS: RegExp[] = [/ResizeObserver loop/i];

export function isBenignException(properties: Record<string, unknown> | undefined): boolean {
  if (!properties || typeof properties !== 'object') return false;

  const messages: unknown[] = [properties.$exception_message];
  const list = properties.$exception_list;
  if (Array.isArray(list)) {
    for (const entry of list) {
      if (entry && typeof entry === 'object' && 'value' in entry) {
        messages.push((entry as { value: unknown }).value);
      }
    }
  }

  for (const message of messages) {
    if (typeof message !== 'string') continue;
    for (const pattern of BENIGN_MESSAGE_PATTERNS) {
      if (pattern.test(message)) return true;
    }
  }
  return false;
}

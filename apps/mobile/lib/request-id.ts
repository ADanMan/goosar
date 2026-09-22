// Идентификатор запроса: 8 случайных символов base36 плюс 4 символа времени.
// Отправляется в X-Request-ID для корреляции логов.
export function createRequestId(): string {
  const rand = Math.random().toString(36).slice(2, 10);
  const ts = Date.now().toString(36).slice(-4);
  return `${rand}${ts}`;
}

/**
 * Извлекает машиночитаемую часть из сырого текста ошибки рантайма.
 *
 * Ошибки задач доходят до клиента одной плоской строкой, которую демон
 * собрал из JSON-RPC error-фрейма процесса агента. К моменту, когда строка
 * попадает в чат-пузырь, у UI есть только она — поэтому классификация,
 * которую рантайм уже сделал (поле `data`), иначе просто выбрасывается.
 * Эта функция достаёт её обратно, чтобы можно было показать что-то верное
 * о причине вместо общей фразы "агент столкнулся с ошибкой, попробуйте ещё раз".
 *
 * Намеренно консервативна: нераспознанная форма возвращает `null`, и вызывающий
 * код сохраняет прежнее значение. Разбор никогда не подменяет исходный текст —
 * каждый вызывающий обязан всё равно где-то показывать `raw` (в деталях, в логе).
 *
 * Чистая и без зависимостей, чтобы её можно было импортировать напрямую в мобильном приложении.
 */
export interface RuntimeFailureSignature {
  code: number | null;
  data: string | null;
  message: string;
}

const EMPTY_MESSAGES = new Set([
  'internal error',
  'invalid params',
  'invalid request',
  'method not found',
  'parse error',
  'server error',
]);

const FRAME_SUFFIX = /\s*\(code=(-?\d+)(?:,\s*data=([\s\S]*))?\)\s*$/;

const DAEMON_PREFIX = /^[\w.-]+ [\w/]+ failed:\s*/;

const METHOD_PREFIX = /^[\w.]+\/[\w.]+:\s*/;

function stripWrappers(text: string): string {
  let out = text.trim();
  const before = out;
  out = out.replace(DAEMON_PREFIX, '');
  if (out !== before) out = out.replace(METHOD_PREFIX, '');
  return out.trim();
}

export function parseRuntimeFailure(
  raw: string | null | undefined,
): RuntimeFailureSignature | null {
  if (typeof raw !== 'string') return null;
  const text = raw.trim();
  if (!text) return null;

  const frame = FRAME_SUFFIX.exec(text);
  if (!frame) return null;

  const code = Number.parseInt(frame[1] ?? '', 10);
  const data = (frame[2] ?? '').trim();
  const message = stripWrappers(text.slice(0, frame.index));

  return {
    code: Number.isNaN(code) ? null : code,
    data: data.length > 0 ? data : null,
    message: EMPTY_MESSAGES.has(message.toLowerCase()) ? '' : message,
  };
}

export type RuntimeFailureCause = 'llm_not_configured' | 'agent_unresolved';

const AGENT_UNRESOLVED = /no usable default_agent|agent .* not found/i;

export function runtimeFailureCause(
  signature: RuntimeFailureSignature | null,
): RuntimeFailureCause | null {
  if (!signature) return null;
  const { data } = signature;
  if (!data) return null;
  if (data === 'llm_not_configured') return 'llm_not_configured';
  if (AGENT_UNRESOLVED.test(data)) return 'agent_unresolved';
  return null;
}

export function usesRuntimeMessage(
  cause: RuntimeFailureCause | null,
  signature: RuntimeFailureSignature | null,
): boolean {
  return cause === 'llm_not_configured' && (signature?.message.length ?? 0) > 0;
}

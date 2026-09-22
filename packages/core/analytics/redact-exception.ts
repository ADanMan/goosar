// Вычищение персональных данных из событий `$exception` перед отправкой с клиента.

const REDACTED = '[redacted]';

const MAX_MESSAGE_LENGTH = 500;

const PATTERNS: Array<[RegExp, string]> = [
  [/[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}/gi, REDACTED],
  [/((?:https?|file|goosar):\/\/[^\s?#]*)[?#]\S*/gi, `$1?${REDACTED}`],
  [/\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]+/gi, `$1 ${REDACTED}`],
  [
    /\b((?:x[-_])?api[-_]?key|token|password|passwd|pwd|secret)(\s*[:=]\s*)\S+/gi,
    `$1$2${REDACTED}`,
  ],
  [/((?:^|[\s"'`(=:])(?:\/Users\/|\/home\/))[^/\s"'`]+/g, `$1${REDACTED}`],
  [/([A-Za-z]:\\Users\\)[^\\\s"'`]+/g, `$1${REDACTED}`],
  [/\b\d{1,3}(?:\.\d{1,3}){3}\b/g, REDACTED],
  [/\b[A-Za-z0-9_-]{24,}\b/g, REDACTED],
  [/"[^"\n]{40,}"/g, `"${REDACTED}"`],
  [/'[^'\n]{40,}'/g, `'${REDACTED}'`],
  [/`[^`\n]{40,}`/g, `\`${REDACTED}\``],
];

export function redactText(input: unknown): unknown {
  if (typeof input !== 'string' || input.length === 0) return input;
  let out = input;
  for (const [pattern, replacement] of PATTERNS) {
    out = out.replace(pattern, replacement);
  }
  if (out.length > MAX_MESSAGE_LENGTH) {
    out = `${out.slice(0, MAX_MESSAGE_LENGTH)}… [truncated]`;
  }
  return out;
}

export function redactExceptionProperties(
  properties: Record<string, unknown> | undefined,
): Record<string, unknown> | undefined {
  if (!properties || typeof properties !== 'object') return properties;

  if ('$exception_message' in properties) {
    properties.$exception_message = redactText(properties.$exception_message);
  }

  const list = properties.$exception_list;
  if (Array.isArray(list)) {
    for (const entry of list) {
      if (entry && typeof entry === 'object' && 'value' in entry) {
        (entry as { value: unknown }).value = redactText((entry as { value: unknown }).value);
      }
    }
  }

  return properties;
}

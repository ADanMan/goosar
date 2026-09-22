// Редактирование секретов в логах главного процесса: значения полей, чьё имя
// содержит key/token/secret/password на любом уровне вложенности, заменяются
// маркером до записи в файл.
import { redactAddressCredentials } from '../shared/system-proxy';

const SECRET_KEY_PATTERN = /key|token|secret|password/i;
const MAX_REDACTION_DEPTH = 8;

const STRING_ASSIGNMENT_PATTERN =
  /([A-Za-z0-9_-]*(?:key|token|secret|password|credential)s?\s*[=:]\s*["']?)([^\s"'&,;]+)/gi;
const BEARER_PATTERN = /\b(Bearer\s+)[A-Za-z0-9._~+/=-]+/g;

export const REDACTED_VALUE = '<redacted>';

export function redactSecretsInString(value: string): string {
  return redactAddressCredentials(
    value
      .replace(STRING_ASSIGNMENT_PATTERN, `$1${REDACTED_VALUE}`)
      .replace(BEARER_PATTERN, `$1${REDACTED_VALUE}`),
  );
}

export function redactSecretsDeep(value: unknown): unknown {
  return redact(value, 0, new WeakSet());
}

function redact(value: unknown, depth: number, seen: WeakSet<object>): unknown {
  if (typeof value === 'string') return redactSecretsInString(value);
  if (value === null || typeof value !== 'object') return value;
  if (!Array.isArray(value) && !isPlainObject(value)) return value;
  if (seen.has(value)) return '[circular]';
  if (depth >= MAX_REDACTION_DEPTH) return '[max-depth]';

  seen.add(value);
  try {
    if (Array.isArray(value)) {
      return value.map((item) => redact(item, depth + 1, seen));
    }
    const result: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(value)) {
      result[key] = SECRET_KEY_PATTERN.test(key) ? REDACTED_VALUE : redact(item, depth + 1, seen);
    }
    return result;
  } finally {
    seen.delete(value);
  }
}

function isPlainObject(value: object): value is Record<string, unknown> {
  const proto: unknown = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

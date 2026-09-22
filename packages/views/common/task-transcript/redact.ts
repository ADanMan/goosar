// Клиентский запасной вариант редактирования секретов в выводе агента;
// основное редактирование делает сервер.

const patterns: { re: RegExp; replacement: string }[] = [
  { re: /\bAKIA[0-9A-Z]{16}\b/g, replacement: '[REDACTED AWS KEY]' },
  {
    re: /(?:aws_secret_access_key|secret_?access_?key)\s*[=:]\s*[A-Za-z0-9/+=]{40}/gi,
    replacement: '[REDACTED AWS SECRET]',
  },
  {
    re: /-----BEGIN[A-Z\s]*PRIVATE KEY-----[\s\S]*?-----END[A-Z\s]*PRIVATE KEY-----/g,
    replacement: '[REDACTED PRIVATE KEY]',
  },
  {
    re: /\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b/g,
    replacement: '[REDACTED GITHUB TOKEN]',
  },
  { re: /\bgithub_pat_[A-Za-z0-9_]{20,255}\b/g, replacement: '[REDACTED GITHUB TOKEN]' },
  { re: /\bAIza[0-9A-Za-z_-]{35}([^0-9A-Za-z_-]|$)/g, replacement: '[REDACTED GOOGLE API KEY]$1' },
  { re: /\bglpat-[A-Za-z0-9_-]{20,}\b/g, replacement: '[REDACTED GITLAB TOKEN]' },
  { re: /\bsk-[A-Za-z0-9_-]{20,}\b/g, replacement: '[REDACTED API KEY]' },
  { re: /\bxox[bporas]-[A-Za-z0-9-]{10,}\b/g, replacement: '[REDACTED SLACK TOKEN]' },
  {
    re: /\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b/g,
    replacement: '[REDACTED JWT]',
  },
  { re: /\bBearer\s+[A-Za-z0-9\-._~+/]+=*/gi, replacement: 'Bearer [REDACTED]' },
  {
    re: /(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?:\/\/[^:\s]+:[^@\s]+@/gi,
    replacement: '[REDACTED CONNECTION STRING]@',
  },
  {
    re: /(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+/gi,
    replacement: '[REDACTED CREDENTIAL]',
  },
];

export function redactSecrets(text: string): string {
  let result = text;
  for (const { re, replacement } of patterns) {
    result = result.replace(re, replacement);
  }
  return result;
}

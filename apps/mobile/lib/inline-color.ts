/**
 * Детерминированный подбор цвета для лейблов, созданных прямо в интерфейсе.
 * Палитра и хеш-функция совпадают с веб-версией, чтобы одно и то же имя
 * лейбла получало один и тот же цвет независимо от платформы создания.
 */
const INLINE_COLORS = [
  '#ef4444',
  '#f97316',
  '#eab308',
  '#22c55e',
  '#14b8a6',
  '#3b82f6',
  '#6366f1',
  '#a855f7',
  '#ec4899',
  '#64748b',
] as const;

export function pickInlineColor(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  }
  return INLINE_COLORS[hash % INLINE_COLORS.length] ?? INLINE_COLORS[0];
}

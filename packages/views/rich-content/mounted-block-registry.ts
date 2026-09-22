/**
 * Фиксирует, какие rich-блоки уже были смонтированы в рамках текущей сессии
 * страницы.
 *
 * Гарантия "смонтировать один раз" не может держаться только на состоянии
 * компонента: список чата виртуализирован, и прокрутка далеко полностью
 * размонтирует строку вместе с любым локальным флагом `mounted`. Прокрутка
 * назад заново запускала бы Mermaid, строила новый песочничный iframe и
 * теряла pan/zoom — ровно ту повторную стоимость, ради устранения которой
 * существует ленивая оболочка.
 *
 * Ключ — хеш от языка и исходного текста, поэтому одна и та же диаграмма
 * узнаётся везде, где встречается снова.
 *
 * Намеренно на уровне модуля, а не в сторе: это учёт рендера, а не состояние
 * приложения. Никогда не сохраняется и не читается на сервере.
 */

const MAX_TRACKED_BLOCKS = 500;

const mountedBlocks = new Set<string>();

function hashSource(source: string): string {
  let hash = 5381;
  for (let i = 0; i < source.length; i++) {
    hash = ((hash << 5) + hash) ^ source.charCodeAt(i);
  }
  return (hash >>> 0).toString(36);
}

export function mountedBlockKey(language: string, source: string): string {
  return `${language}:${hashSource(source)}`;
}

export function hasBlockMounted(key: string): boolean {
  return mountedBlocks.has(key);
}

export function markBlockMounted(key: string): void {
  if (mountedBlocks.has(key)) return;
  if (mountedBlocks.size >= MAX_TRACKED_BLOCKS) {
    const oldest = mountedBlocks.values().next();
    if (!oldest.done) mountedBlocks.delete(oldest.value);
  }
  mountedBlocks.add(key);
}

export function resetMountedBlocks(): void {
  mountedBlocks.clear();
}

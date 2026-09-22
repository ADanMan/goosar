// Экранирование символов, ломающих синтаксис markdown-ссылок: [ ] \ ( ).
export function escapeMarkdownLabel(text: string): string {
  return text.replace(/[[\]\\()]/g, (ch) => `\\${ch}`);
}

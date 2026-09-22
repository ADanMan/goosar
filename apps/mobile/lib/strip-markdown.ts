// Упрощённая очистка markdown для компактных превью (например, чип «Ответ на X»).
// Не полный парсер.
export function stripMarkdown(md: string): string {
  if (!md) return '';
  return (
    md
      // Images first (the leading `!` distinguishes from a plain link).
      .replace(/!\[[^\]]*\]\([^)]*\)/g, '📷')
      // Mention links — keep the label (already includes `@` or the
      // issue identifier verbatim per `mention-extension.ts`).
      .replace(/\[([^\]]+)\]\(mention:\/\/[^)]+\)/g, '$1')
      // Plain links last — strip the URL, keep the label.
      .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
      // Collapse multi-blank-line runs to single \n so the preview budget
      // isn't spent on whitespace.
      .replace(/\n{2,}/g, '\n')
      .trim()
  );
}

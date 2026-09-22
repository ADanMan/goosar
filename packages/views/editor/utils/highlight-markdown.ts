// Read-only преобразование ==text== → <mark>; редактор разбирает этот синтаксис
// нативно, react-markdown — нет.

import { findLiteralRanges, matchHighlightAt } from './highlight-match';

export function highlightToHtml(markdown: string): string {
  if (!markdown.includes('==')) return markdown;
  const ranges = findLiteralRanges(markdown);

  let result = '';
  let cursor = 0;
  let i = markdown.indexOf('==');
  while (i !== -1) {
    const match = matchHighlightAt(markdown, i, ranges);
    if (match) {
      result += markdown.slice(cursor, i);
      result += `<mark>${match.inner}</mark>`;
      cursor = match.end;
      i = markdown.indexOf('==', match.end);
    } else {
      i = markdown.indexOf('==', i + 2);
    }
  }
  result += markdown.slice(cursor);
  return result;
}

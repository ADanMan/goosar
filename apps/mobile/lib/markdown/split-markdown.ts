// Разбиение markdown на сегменты для гибридного рендера: enriched-markdown
// не позволяет внедрять React в листовые узлы.
import { marked, type Tokens } from 'marked';

export type MarkdownSegment =
  | { type: 'prose'; content: string }
  | { type: 'code'; lang: string | undefined; code: string }
  | { type: 'image'; uri: string; alt: string };

export function splitMarkdown(input: string): MarkdownSegment[] {
  if (!input) return [];

  const tokens = marked.lexer(input);
  const out: MarkdownSegment[] = [];
  let proseBuffer = '';

  const flushProse = () => {
    const trimmed = proseBuffer.replace(/^\s+|\s+$/g, '');
    if (trimmed.length > 0) {
      out.push({ type: 'prose', content: trimmed });
    }
    proseBuffer = '';
  };

  for (const token of tokens) {
    if (token.type === 'code') {
      flushProse();
      const t = token as Tokens.Code;
      out.push({
        type: 'code',
        lang: t.lang ? t.lang : undefined,
        code: t.text,
      });
      continue;
    }

    if (token.type === 'paragraph') {
      const para = token as Tokens.Paragraph;
      const inline = para.tokens ?? [];
      const hasImage = inline.some((t) => t.type === 'image');

      if (!hasImage) {
        proseBuffer += para.raw;
        continue;
      }

      let textBuffer = '';
      const flushText = () => {
        const trimmed = textBuffer.trim();
        if (trimmed.length > 0) {
          proseBuffer += trimmed + '\n\n';
        }
        textBuffer = '';
      };

      for (const t of inline) {
        if (t.type === 'image') {
          flushText();
          flushProse();
          const img = t as Tokens.Image;
          out.push({
            type: 'image',
            uri: img.href,
            alt: img.text ?? '',
          });
        } else {
          textBuffer += (t as { raw?: string }).raw ?? '';
        }
      }
      flushText();
      continue;
    }

    proseBuffer += (token as { raw?: string }).raw ?? '';
  }

  flushProse();
  return out;
}

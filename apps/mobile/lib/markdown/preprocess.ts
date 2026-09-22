// Чистые строковые преобразования до разбора markdown: старые шорткоды
// упоминаний → современные mention-ссылки, и т. п. Идемпотентны.
import { preprocessMentionShortcodes } from '@goosar/core/markdown';

const ATTACHMENT_UUID =
  '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}';
const FILE_CARD_URL = `/uploads/[^)]*|https?://[^)]+|/api/attachments/${ATTACHMENT_UUID}/download`;
const FILE_LINE_RE = new RegExp(`^!file\\[((?:\\\\.|[^\\]\\\\])*)\\]\\((${FILE_CARD_URL})\\)$`);

function unescapeFileLabel(label: string): string {
  return label.replace(/\\([[\]\\()])/g, '$1');
}

function escapeLinkLabel(name: string): string {
  return name.replace(/([\\[\]])/g, '\\$1');
}

function preprocessFileCards(input: string): string {
  return input
    .split('\n')
    .map((line) => {
      const m = line.trim().match(FILE_LINE_RE);
      if (!m) return line;
      const label = escapeLinkLabel(unescapeFileLabel(m[1]!));
      return `[📎 ${label}](${m[2]})`;
    })
    .join('\n');
}

const TASK_DONE_RE = /^(\s*[-*+]\s+\[[xX]\]\s+)(.+)$/gm;

function preprocessTaskListStrikethrough(input: string): string {
  return input.replace(TASK_DONE_RE, (match, prefix, body) => {
    const trimmed = body.trim();
    if (trimmed.startsWith('~~') && trimmed.endsWith('~~')) return match;
    return `${prefix}~~${body}~~`;
  });
}

function stripHtml(input: string): string {
  return input
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/<br\s*\/?>/gi, '  \n')
    .replace(/<\/?[a-z][^>]*>/gi, '');
}

export function preprocessMobileMarkdown(input: string): string {
  if (!input) return '';
  return preprocessTaskListStrikethrough(
    preprocessFileCards(preprocessMentionShortcodes(stripHtml(input))),
  );
}

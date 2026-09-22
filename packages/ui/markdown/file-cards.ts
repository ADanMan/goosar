// Препроцессинг карточек файлов в markdown: синтаксис !file[name](url)
// превращается в div для react-markdown.

const IMAGE_EXTS = /\.(png|jpe?g|gif|webp|svg|ico|bmp|tiff?)$/i;

const ATTACHMENT_UUID_SOURCE =
  '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}';
const ATTACHMENT_DOWNLOAD_URL_SOURCE = `/api/attachments/${ATTACHMENT_UUID_SOURCE}/download`;
const ATTACHMENT_DOWNLOAD_URL_RE = new RegExp(`^${ATTACHMENT_DOWNLOAD_URL_SOURCE}$`);

export const FILE_CARD_URL_PATTERN = new RegExp(
  `/uploads/[^)]*|https?:\\/\\/[^)]+|${ATTACHMENT_DOWNLOAD_URL_SOURCE}`,
);

export function isAllowedFileCardHref(href: string): boolean {
  return /^(https?:\/\/|\/uploads\/)/i.test(href) || ATTACHMENT_DOWNLOAD_URL_RE.test(href);
}

const NEW_FILE_CARD_RE = new RegExp(
  `^!file\\[((?:\\\\.|[^\\]\\\\])*)\\]\\((${FILE_CARD_URL_PATTERN.source})\\)$`,
);

const FILE_LINK_LINE = /^\[([^\]]+)\]\((https?:\/\/[^)]+)\)$/;

function escapeAttr(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
}

function toFileCardHtml(filename: string, url: string): string {
  return `<div data-type="fileCard" data-href="${escapeAttr(url)}" data-filename="${escapeAttr(filename)}"></div>`;
}

export function isCdnUrl(url: string, cdnDomain: string): boolean {
  try {
    const u = new URL(url);
    return u.hostname === cdnDomain || u.hostname.endsWith('.amazonaws.com');
  } catch {
    return false;
  }
}

export function isFileCardUrl(url: string, cdnDomain: string): boolean {
  try {
    return isCdnUrl(url, cdnDomain) && !IMAGE_EXTS.test(new URL(url).pathname);
  } catch {
    return false;
  }
}

export function preprocessFileCards(markdown: string, cdnDomain: string): string {
  return markdown
    .split('\n')
    .map((line) => {
      const trimmed = line.trim();

      const newMatch = trimmed.match(NEW_FILE_CARD_RE);
      if (newMatch) {
        const filename = newMatch[1]!.replace(/\\([[\]\\()])/g, '$1');
        return toFileCardHtml(filename, newMatch[2]!);
      }

      const match = trimmed.match(FILE_LINK_LINE);
      if (!match) return line;
      const filename = match[1]!;
      const url = match[2]!;
      if (!isFileCardUrl(url, cdnDomain)) return line;
      return toFileCardHtml(filename, url);
    })
    .join('\n');
}

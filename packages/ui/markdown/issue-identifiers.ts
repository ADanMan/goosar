import {
  detectLinks,
  findCodeRanges,
  findMarkdownLinkRanges,
  isInsideCode,
  rangesOverlap,
} from './linkify';

const IDENTIFIER_RE = /(?<![A-Za-z0-9_-])([A-Z][A-Z0-9]*-\d+)(?![A-Za-z0-9_-])/g;

export const ISSUE_IDENTIFIER_PATTERN = /^[A-Z][A-Z0-9]*-\d+$/;

export function isIssueIdentifier(value: string): boolean {
  return ISSUE_IDENTIFIER_PATTERN.test(value);
}

export function preprocessIssueIdentifiers(text: string): string {
  if (!/[A-Z][A-Z0-9]*-\d/.test(text)) return text;

  const codeRanges = findCodeRanges(text);
  const linkRanges = findMarkdownLinkRanges(text);
  const detectedLinks = detectLinks(text);

  IDENTIFIER_RE.lastIndex = 0;
  let result = '';
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = IDENTIFIER_RE.exec(text)) !== null) {
    const identifier = match[1];
    if (!identifier) continue;
    const start = match.index;
    const end = start + identifier.length;
    const range = { start, end };

    if (isInsideCode(start, codeRanges)) continue;
    if (linkRanges.some((r) => rangesOverlap(range, r))) continue;
    if (detectedLinks.some((l) => rangesOverlap(range, l))) continue;

    const after = text[end];
    if (after === '.' && /[A-Za-z0-9]/.test(text[end + 1] ?? '')) continue;
    if (after === '/' || text[start - 1] === '/') continue;
    if (text[start - 1] === '.') continue;

    result += text.slice(lastIndex, start);
    result += `[${identifier}](mention://issue/${identifier})`;
    lastIndex = end;
  }

  if (lastIndex === 0) return text;
  result += text.slice(lastIndex);
  return result;
}

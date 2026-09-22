import LinkifyIt from 'linkify-it';

const linkify = new LinkifyIt();

const FILE_EXTENSIONS =
  'ts|tsx|js|jsx|mjs|cjs|md|json|yaml|yml|py|go|rs|css|scss|less|html|htm|txt|log|sh|bash|zsh|swift|kt|java|c|cpp|h|hpp|rb|php|xml|toml|ini|cfg|conf|env|sql|graphql|vue|svelte|astro|prisma|dockerfile|makefile|gitignore';

const FILE_PATH_REGEX = new RegExp(
  `(?:^|[\\s([{<])((\\/|~\\/|\\.\\/)[\\w\\-./@]+\\.(?:${FILE_EXTENSIONS}))(?=[\\s)\\]}.,;:!?>]|$)`,
  'gi',
);

const CJK_URL_TERMINATOR_REGEX = /[！-／：-＠［-｀｛-～、。「-】]/;

const TRAILING_MD_DELIMITER = /[*~]+$/;

interface DetectedLink {
  type: 'url' | 'email' | 'file';
  text: string;
  url: string;
  start: number;
  end: number;
}

export interface CodeRange {
  start: number;
  end: number;
}

export function shouldAutoLink(value: string): boolean {
  return (
    /^https?:\/\//i.test(value) ||
    /^www\./i.test(value) ||
    /^[^\s@/:]+@[^\s@/:]+\.[^\s@/:]+$/.test(value)
  );
}

export function findCodeRanges(text: string): CodeRange[] {
  const ranges: CodeRange[] = [];

  const fencedRegex = /```[\s\S]*?```/g;
  let match;
  while ((match = fencedRegex.exec(text)) !== null) {
    ranges.push({ start: match.index, end: match.index + match[0].length });
  }

  const displayMathRegex = /\$\$[\s\S]*?\$\$/g;
  while ((match = displayMathRegex.exec(text)) !== null) {
    const pos = match.index;
    const insideOther = ranges.some((r) => pos >= r.start && pos < r.end);
    if (!insideOther) {
      ranges.push({ start: pos, end: pos + match[0].length });
    }
  }

  const inlineMathRegex = /(?<!\$)\$(?!\$)([^$\n]+)\$(?!\$)/g;
  while ((match = inlineMathRegex.exec(text)) !== null) {
    const pos = match.index;
    const insideOther = ranges.some((r) => pos >= r.start && pos < r.end);
    if (!insideOther) {
      ranges.push({ start: pos, end: pos + match[0].length });
    }
  }

  const inlineRegex = /(?<!`)`(?!`)([^`\n]+)`(?!`)/g;
  while ((match = inlineRegex.exec(text)) !== null) {
    const pos = match.index;
    const insideOther = ranges.some((r) => pos >= r.start && pos < r.end);
    if (!insideOther) {
      ranges.push({ start: pos, end: pos + match[0].length });
    }
  }

  return ranges;
}

export function isInsideCode(pos: number, ranges: CodeRange[]): boolean {
  return ranges.some((r) => pos >= r.start && pos < r.end);
}

function isEscaped(text: string, index: number): boolean {
  let slashCount = 0;
  for (let i = index - 1; i >= 0 && text[i] === '\\'; i--) {
    slashCount++;
  }
  return slashCount % 2 === 1;
}

function findMatchingBracket(text: string, openIndex: number): number {
  let depth = 0;

  for (let i = openIndex; i < text.length; i++) {
    if (isEscaped(text, i)) continue;

    const char = text[i];
    if (char === '[') {
      depth++;
    } else if (char === ']') {
      depth--;
      if (depth === 0) return i;
    }
  }

  return -1;
}

function findInlineLinkEnd(text: string, openParenIndex: number): number {
  let depth = 0;

  for (let i = openParenIndex; i < text.length; i++) {
    if (isEscaped(text, i)) continue;

    const char = text[i];
    if (char === '(') {
      depth++;
    } else if (char === ')') {
      depth--;
      if (depth === 0) return i + 1;
    }
  }

  return -1;
}

export function findMarkdownLinkRanges(text: string): CodeRange[] {
  const ranges: CodeRange[] = [];

  for (let i = 0; i < text.length; i++) {
    if (text[i] !== '[' || isEscaped(text, i)) continue;
    if (ranges.some((r) => i >= r.start && i < r.end)) continue;

    const labelEnd = findMatchingBracket(text, i);
    if (labelEnd === -1) continue;

    const start = i > 0 && text[i - 1] === '!' && !isEscaped(text, i - 1) ? i - 1 : i;
    const nextChar = text[labelEnd + 1];

    if (nextChar === '(') {
      const end = findInlineLinkEnd(text, labelEnd + 1);
      if (end !== -1) {
        ranges.push({ start, end });
        i = end - 1;
      }
      continue;
    }

    if (nextChar === '[') {
      const referenceEnd = findMatchingBracket(text, labelEnd + 1);
      if (referenceEnd !== -1) {
        ranges.push({ start, end: referenceEnd + 1 });
        i = referenceEnd;
      }
    }
  }

  return ranges;
}

function isAlreadyLinked(text: string, linkStart: number, linkEnd: number): boolean {
  const before = text.slice(Math.max(0, linkStart - 2), linkStart);
  if (before.endsWith('](')) return true;

  if (before.endsWith('][')) return true;

  const charBefore = text[linkStart - 1];
  const charAfter = text[linkEnd];
  if (charBefore === '[' && charAfter === ']') return true;

  return false;
}

export function rangesOverlap(
  a: { start: number; end: number },
  b: { start: number; end: number },
): boolean {
  return a.start < b.end && b.start < a.end;
}

function collectLinkifyMatches(text: string, offset: number, out: DetectedLink[]): void {
  const matches = linkify.match(text);
  if (!matches) return;

  for (const match of matches) {
    const cjkIdx = match.text.search(CJK_URL_TERMINATOR_REGEX);
    if (cjkIdx === 0) continue; 

    const truncate = cjkIdx > 0;
    const matchText = (truncate ? match.text.slice(0, cjkIdx) : match.text).replace(
      TRAILING_MD_DELIMITER,
      '',
    );

    if (matchText.length > 0 && shouldAutoLink(matchText)) {
      const trimmed = matchText.length !== match.text.length;
      const schemePrefix = match.url.slice(0, match.url.length - match.text.length);
      const matchUrl =
        match.schema === '' && /^www\./i.test(matchText)
          ? `https://${matchText}`
          : trimmed
            ? schemePrefix + matchText
            : match.url;

      out.push({
        type: match.schema === 'mailto:' ? 'email' : 'url',
        text: matchText,
        url: matchUrl,
        start: match.index + offset,
        end: match.index + matchText.length + offset,
      });
    }

    if (truncate) {
      const tailStart = match.index + cjkIdx + 1;
      collectLinkifyMatches(text.slice(tailStart), offset + tailStart, out);
      return;
    }
  }
}

export function detectLinks(text: string): DetectedLink[] {
  const links: DetectedLink[] = [];

  collectLinkifyMatches(text, 0, links);

  FILE_PATH_REGEX.lastIndex = 0;
  let fileMatch;
  while ((fileMatch = FILE_PATH_REGEX.exec(text)) !== null) {
    const path = fileMatch[1];
    if (!path) continue; 

    const fullMatch = fileMatch[0];
    const pathOffset = fullMatch.indexOf(path);
    const start = fileMatch.index + pathOffset;

    const pathRange = { start, end: start + path.length };
    const overlapsUrl = links.some((link) => rangesOverlap(pathRange, link));
    if (overlapsUrl) continue;

    links.push({
      type: 'file',
      text: path,
      url: path, // File paths are passed as-is to onFileClick handler
      start,
      end: start + path.length,
    });
  }

  return links.sort((a, b) => a.start - b.start);
}

export function preprocessLinks(text: string): string {
  if (!linkify.pretest(text) && !/[~/.]\//.test(text)) {
    return text;
  }

  const codeRanges = findCodeRanges(text);
  const markdownLinkRanges = findMarkdownLinkRanges(text);
  const links = detectLinks(text);

  if (links.length === 0) return text;

  let result = '';
  let lastIndex = 0;

  for (const link of links) {
    if (isInsideCode(link.start, codeRanges)) continue;

    if (markdownLinkRanges.some((range) => rangesOverlap(link, range))) continue;

    if (isAlreadyLinked(text, link.start, link.end)) continue;

    result += text.slice(lastIndex, link.start);

    result += `[${link.text}](${link.url})`;

    lastIndex = link.end;
  }

  result += text.slice(lastIndex);

  return result;
}

export function hasLinks(text: string): boolean {
  return linkify.pretest(text) || /[~/.]\/[\w]/.test(text);
}

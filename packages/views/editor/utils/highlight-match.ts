// Единственный источник правил разбора ==text==: используется и токенизатором
// редактора, и read-only преобразованием.

export interface Range {
  start: number;
  end: number;
}

const BLANK_LINE_RE = /\r?\n[ \t]*\r?\n/;

export function findLiteralRanges(text: string): Range[] {
  const ranges: Range[] = [];
  const add = (re: RegExp) => {
    let m: RegExpExecArray | null;
    re.lastIndex = 0;
    while ((m = re.exec(text)) !== null) {
      const start = m.index;
      if (ranges.some((r) => start >= r.start && start < r.end)) continue;
      ranges.push({ start, end: start + m[0].length });
    }
  };
  add(/```[\s\S]*?```/g); 
  add(/\$\$[\s\S]*?\$\$/g); 
  add(/(?<!\$)\$(?!\$)[^$\n]+\$(?!\$)/g); 
  add(/(?<!`)`(?!`)[^`\n]+`(?!`)/g); 
  return ranges;
}

function isInside(pos: number, ranges: Range[]): boolean {
  return ranges.some((r) => pos >= r.start && pos < r.end);
}

export function matchHighlightAt(
  text: string,
  i: number,
  ranges?: Range[],
): { end: number; inner: string } | null {
  if (text[i] !== '=' || text[i + 1] !== '=') return null;
  const innerStart = i + 2;
  if (innerStart >= text.length) return null;
  if (/\s/.test(text[innerStart]!)) return null;

  const r = ranges ?? findLiteralRanges(text);
  if (isInside(i, r)) return null; 

  const blankRel = text.slice(innerStart).search(BLANK_LINE_RE);
  const scanLimit = blankRel === -1 ? text.length : innerStart + blankRel;

  for (let j = innerStart + 1; j <= scanLimit && j + 1 < text.length; j++) {
    if (text[j] !== '=' || text[j + 1] !== '=') continue;
    if (isInside(j, r)) continue; 
    if (/\s/.test(text[j - 1]!)) continue; 
    return { end: j + 2, inner: text.slice(innerStart, j) };
  }
  return null;
}

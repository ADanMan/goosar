import { pinyin } from 'pinyin-pro';

export function matchesPinyin(name: string, query: string): boolean {
  if (!query) return true;

  if (!/[\u4e00-\u9fff]/.test(name)) return false;

  const q = query.toLowerCase();

  const full = pinyin(name, { toneType: 'none', type: 'array', v: true });
  const fullStr = full.join('');

  if (fullStr.startsWith(q)) return true;

  const initials = full.map((p) => p[0] || '').join('');
  if (initials.startsWith(q)) return true;

  return hybridMatch(full, q);
}

function hybridMatch(pinyinArr: string[], query: string): boolean {
  return match(pinyinArr, 0, query, 0);
}

function match(arr: string[], ai: number, q: string, qi: number): boolean {
  if (qi >= q.length) return true;
  if (ai >= arr.length) return false;

  const syllable = arr[ai]!;

  for (let len = syllable.length; len >= 1; len--) {
    if (qi + len > q.length) continue;
    if (q.substring(qi, qi + len) === syllable.substring(0, len)) {
      if (match(arr, ai + 1, q, qi + len)) return true;
    }
  }

  return false;
}

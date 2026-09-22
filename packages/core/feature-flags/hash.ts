// 32-битный FNV-1a для детерминированного распределения по процентным
// раскаткам; алгоритм совпадает с Go-стороной.

const utf8 = new TextEncoder();

function fnv1a(parts: ReadonlyArray<string>): number {
  let hash = 0x811c9dc5;
  for (let p = 0; p < parts.length; p++) {
    if (p > 0) {
      hash ^= 0;
      hash = Math.imul(hash, 0x01000193);
    }
    const bytes = utf8.encode(parts[p]!);
    for (let i = 0; i < bytes.length; i++) {
      hash ^= bytes[i]!;
      hash = Math.imul(hash, 0x01000193);
    }
  }
  return hash >>> 0;
}

export function bucketFor(key: string, identifier: string): number {
  return fnv1a([key, identifier]) % 100;
}

export function inPercent(key: string, identifier: string, percent: number): boolean {
  if (percent <= 0) return false;
  if (percent >= 100) return true;
  return bucketFor(key, identifier) < percent;
}

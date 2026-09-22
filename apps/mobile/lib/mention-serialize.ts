// Сериализация упоминаний для мобильного композера: RN TextInput не умеет
// встроенные view, поэтому каждое @ из пикера помечается невидимым разделителем
// U+2063.

const SENTINEL = '⁣';

export type MentionType = 'member' | 'agent' | 'squad' | 'all' | 'issue';

export interface MentionMarker {
  type: MentionType;
  id: string;
  name: string;
}

export function tokenAtCursor(
  text: string,
  cursor: number,
): { start: number; query: string } | null {
  if (cursor < 1 || cursor > text.length) return null;

  let i = cursor - 1;
  while (i >= 0) {
    const ch = text[i];
    if (ch === '@') break;
    if (ch === undefined || /\s/.test(ch)) return null;
    i--;
  }
  if (i < 0 || text[i] !== '@') return null;

  if (i > 0 && text[i - 1] === SENTINEL) return null;

  if (i > 0) {
    const prev = text[i - 1];
    if (prev !== undefined && !/\s/.test(prev)) return null;
  }

  const query = text.slice(i + 1, cursor);
  if (/\s/.test(query)) return null;
  return { start: i, query };
}

export function insertMention(
  text: string,
  query: { start: number; queryLength: number },
  mention: MentionMarker,
): {
  newText: string;
  newSelection: { start: number; end: number };
  marker: MentionMarker;
} {
  const before = text.slice(0, query.start);
  const after = text.slice(query.start + 1 + query.queryLength);
  const insert = `${SENTINEL}@${mention.name} `;
  const newText = before + insert + after;
  const cursor = before.length + insert.length;
  return {
    newText,
    newSelection: { start: cursor, end: cursor },
    marker: mention,
  };
}

export function serializeMentions(text: string, markers: MentionMarker[]): string {
  if (!text.includes(SENTINEL)) return text;

  const out: string[] = [];
  let cursor = 0;
  let markerIndex = 0;
  let abort = false;

  while (cursor < text.length) {
    const sentinelAt = text.indexOf(SENTINEL, cursor);
    if (sentinelAt === -1) {
      out.push(text.slice(cursor));
      break;
    }
    out.push(text.slice(cursor, sentinelAt));

    if (text[sentinelAt + 1] !== '@') {
      cursor = sentinelAt + 1;
      continue;
    }

    let wordEnd = sentinelAt + 2;
    while (wordEnd < text.length && !/\s/.test(text[wordEnd]!)) wordEnd++;
    const word = text.slice(sentinelAt + 2, wordEnd);

    const marker = markers[markerIndex];
    if (!marker || marker.name !== word) {
      abort = true;
      break;
    }

    const label = marker.type === 'issue' ? marker.name : `@${marker.name}`;
    out.push(`[${label}](mention://${marker.type}/${marker.id})`);
    markerIndex++;
    cursor = wordEnd;
  }

  if (abort || markerIndex !== markers.length) {
    return text.replace(new RegExp(SENTINEL, 'g'), '');
  }
  return out.join('');
}

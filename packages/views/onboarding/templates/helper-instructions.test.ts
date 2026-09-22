import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { HELPER_DESCRIPTION, HELPER_INSTRUCTIONS } from './helper-instructions';

const GO_TWIN_RELATIVE_PATH = '../../../../server/internal/handler/agent_helper_content.go';

const goSource = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), GO_TWIN_RELATIVE_PATH),
  'utf8',
);

function goMapBody(mapName: string): string {
  const header = `var ${mapName} = map[string]string{`;
  const headerIdx = goSource.indexOf(header);
  if (headerIdx < 0) {
    throw new Error(`map ${mapName} not found in ${GO_TWIN_RELATIVE_PATH}`);
  }
  const bodyStart = headerIdx + header.length;
  const bodyEnd = goSource.indexOf('\n}', bodyStart);
  if (bodyEnd < 0) {
    throw new Error(`map ${mapName} has no closing brace`);
  }
  return goSource.slice(bodyStart, bodyEnd);
}

function parseGoStringExpression(body: string, start: number): string {
  const isSpace = (ch: string | undefined): boolean => ch === ' ' || ch === '\t' || ch === '\n';
  let cursor = start;
  let value = '';
  for (;;) {
    const ch = body[cursor];
    if (ch === '`') {
      const close = body.indexOf('`', cursor + 1);
      if (close < 0) throw new Error('unterminated Go raw string');
      value += body.slice(cursor + 1, close);
      cursor = close + 1;
    } else if (ch === '"') {
      const match = /^"(?:[^"\\\n]|\\.)*"/.exec(body.slice(cursor));
      if (!match) throw new Error('unterminated Go interpreted string');
      value += JSON.parse(match[0]) as string;
      cursor += match[0].length;
    } else {
      throw new Error(`expected a Go string literal at offset ${cursor}`);
    }
    while (isSpace(body[cursor])) cursor += 1;
    if (body[cursor] === '+') {
      cursor += 1;
      while (isSpace(body[cursor])) cursor += 1;
      continue;
    }
    if (body[cursor] === ',') return value;
    throw new Error(`expected "+" or "," after segment at offset ${cursor}`);
  }
}

function parseGoStringMap(mapName: string): Record<string, string> {
  const body = goMapBody(mapName);
  const entries: Record<string, string> = {};
  const keyPattern = /\n\t"([a-z]{2})": /g;
  for (const match of body.matchAll(keyPattern)) {
    entries[match[1]!] = parseGoStringExpression(body, match.index + match[0].length);
  }
  return entries;
}

describe('Helper content twins (Go agent_helper_content.go vs TS templates)', () => {
  it('covers the same languages on both sides', () => {
    expect(Object.keys(parseGoStringMap('helperInstructionsByLang')).sort()).toEqual(
      Object.keys(HELPER_INSTRUCTIONS).sort(),
    );
    expect(Object.keys(parseGoStringMap('helperDescriptionByLang')).sort()).toEqual(
      Object.keys(HELPER_DESCRIPTION).sort(),
    );
  });

  it('helperInstructionsByLang matches HELPER_INSTRUCTIONS line by line', () => {
    const goInstructions = parseGoStringMap('helperInstructionsByLang');
    for (const [lang, tsValue] of Object.entries(HELPER_INSTRUCTIONS)) {
      expect(
        (goInstructions[lang] ?? '').split('\n'),
        `instructions diverged for "${lang}"`,
      ).toEqual(tsValue.split('\n'));
      expect(goInstructions[lang], `instructions diverged for "${lang}"`).toBe(tsValue);
    }
  });

  it('helperDescriptionByLang matches HELPER_DESCRIPTION', () => {
    const goDescriptions = parseGoStringMap('helperDescriptionByLang');
    for (const [lang, tsValue] of Object.entries(HELPER_DESCRIPTION)) {
      expect(goDescriptions[lang], `description diverged for "${lang}"`).toBe(tsValue);
    }
  });
});

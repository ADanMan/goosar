import { readdirSync, readFileSync } from 'node:fs';
import { join, relative, sep } from 'node:path';
import { describe, expect, it } from 'vitest';

const VIEWS_ROOT = join(import.meta.dirname, '..');

const SCAN_ROOTS: readonly { label: string; dir: string }[] = [
  { label: 'views', dir: VIEWS_ROOT },
  { label: 'core', dir: join(VIEWS_ROOT, '..', 'core') },
];

const KNOWN_UNCONVERTED = [
  'views/billing/billing-test-page.tsx',
];

const AMBIENT_LOCALE_PATTERNS: readonly { label: string; pattern: RegExp }[] = [
  {
    label: 'toLocale*String() with no locale',
    pattern: /\.toLocale(?:Date|Time)?String\(\s*(?:\)|\[\s*\]|undefined\b)/,
  },
  {
    label: 'Intl formatter with no locale',
    pattern: /\bIntl\.[A-Za-z]+Format\(\s*(?:\)|\[\s*\]|undefined\b)(?![^;\n]*resolvedOptions)/,
  },
  {
    label: 'navigator.language',
    pattern: /\bnavigator\.languages?\b/,
  },
];

const FORMATS_THROUGH_PLATFORM = /\.toLocale(?:Date|Time)?String\(|\bIntl\.[A-Za-z]+Format\(/;

const OPTIONAL_LOCALE_PARAM = /\b(?:ui)?[Ll]ocale\s*\?:/;

describe('UI-locale formatting', () => {
  it('has no ambient-locale formatting outside the known-unconverted files', () => {
    const offenders = sourceFiles()
      .filter(({ id }) => !KNOWN_UNCONVERTED.includes(id))
      .flatMap(({ id, source }) =>
        AMBIENT_LOCALE_PATTERNS.filter(({ pattern }) => pattern.test(source)).map(
          ({ label }) => `${id}: ${label}`,
        ),
      );

    expect(offenders).toEqual([]);
  });

  it('has no optional locale parameter in a file that formats through the platform', () => {
    const offenders = sourceFiles()
      .filter(
        ({ source }) => FORMATS_THROUGH_PLATFORM.test(source) && OPTIONAL_LOCALE_PARAM.test(source),
      )
      .map(({ id }) => `${id}: optional locale parameter`);

    expect(offenders).toEqual([]);
  });

  it('keeps the known-unconverted list honest', () => {
    const sources = new Map(sourceFiles().map(({ id, source }) => [id, source]));
    const stale = KNOWN_UNCONVERTED.filter((id) => {
      const source = sources.get(id);
      if (source === undefined) return true;
      return !AMBIENT_LOCALE_PATTERNS.some(({ pattern }) => pattern.test(source));
    });

    expect(stale).toEqual([]);
  });
});

interface ScannedFile {
  id: string;
  source: string;
}

function sourceFiles(): ScannedFile[] {
  const files: ScannedFile[] = [];
  const walk = (root: string, label: string, dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === 'node_modules' || entry.name === 'locales') continue;
        walk(root, label, full);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name)) continue;
      if (/\.test\.tsx?$/.test(entry.name)) continue;
      files.push({
        id: `${label}/${relative(root, full).split(sep).join('/')}`,
        source: stripComments(readFileSync(full, 'utf8')),
      });
    }
  };
  for (const { label, dir } of SCAN_ROOTS) walk(dir, label, dir);
  return files;
}

function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .split('\n')
    .filter((line) => !/^\s*\/\//.test(line))
    .join('\n');
}

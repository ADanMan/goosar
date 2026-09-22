import { readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { PARTIAL_LOCALES, RESOURCES } from './index';

const LOCALES_DIR = dirname(fileURLToPath(import.meta.url));

function jsonNamespacesIn(locale: string): string[] {
  return readdirSync(resolve(LOCALES_DIR, locale))
    .filter((name) => name.endsWith('.json'))
    .map((name) => name.replace(/\.json$/, ''))
    .sort();
}

type Json = Record<string, unknown>;

function flattenKeys(obj: unknown, prefix = ''): string[] {
  if (obj === null || typeof obj !== 'object') return [prefix];
  const entries = Object.entries(obj as Json);
  if (entries.length === 0) return [];
  return entries.flatMap(([k, v]) => flattenKeys(v, prefix ? `${prefix}.${k}` : k));
}

function normalizePlural(key: string): string {
  return key.replace(/_(zero|one|two|few|many|other)$/, '_count');
}

function keySet(bundle: Record<string, unknown>): Set<string> {
  return new Set(flattenKeys(bundle).map(normalizePlural));
}

const en = RESOURCES.en;
const translatedLocales = Object.keys(RESOURCES).filter((locale) => locale !== 'en');

function scopeOf(locale: string): readonly string[] {
  return PARTIAL_LOCALES[locale as keyof typeof PARTIAL_LOCALES] ?? Object.keys(en);
}

describe('locale bundle parity', () => {
  it('registers every JSON file in RESOURCES (EN)', () => {
    expect(Object.keys(en).sort()).toEqual(jsonNamespacesIn('en'));
  });

  it('declares partial locales that exist and claim real namespaces', () => {
    for (const [locale, namespaces] of Object.entries(PARTIAL_LOCALES)) {
      expect(Object.keys(RESOURCES)).toContain(locale);
      expect(namespaces?.length ?? 0).toBeGreaterThan(0);
      const unknown = (namespaces ?? []).filter((ns) => !(ns in en));
      expect(unknown).toEqual([]);
    }
  });

  for (const locale of translatedLocales) {
    const bundle = RESOURCES[locale as keyof typeof RESOURCES];
    const scope = scopeOf(locale);

    it(`declares exactly its ${
      locale in PARTIAL_LOCALES ? 'declared' : 'full'
    } namespace set in ${locale}`, () => {
      expect(Object.keys(bundle).sort()).toEqual([...scope].sort());
    });

    it(`registers every JSON file in RESOURCES (${locale})`, () => {
      expect(Object.keys(bundle).sort()).toEqual(jsonNamespacesIn(locale));
    });

    for (const ns of scope) {
      it(`${ns}: ${locale} covers every EN key`, () => {
        const enKeys = keySet(en[ns] ?? {});
        const translatedKeys = keySet(bundle[ns] ?? {});
        const missing = [...enKeys].filter((k) => !translatedKeys.has(k));
        expect(missing).toEqual([]);
      });

      it(`${ns}: EN covers every ${locale} key`, () => {
        const enKeys = keySet(en[ns] ?? {});
        const translatedKeys = keySet(bundle[ns] ?? {});
        const extra = [...translatedKeys].filter((k) => !enKeys.has(k));
        expect(extra).toEqual([]);
      });
    }
  }
});

describe('dead plural-key guard', () => {
  for (const locale of translatedLocales) {
    const categories = new Intl.PluralRules(locale).resolvedOptions().pluralCategories;
    if (categories.includes('one')) continue;

    const bundle = RESOURCES[locale as keyof typeof RESOURCES];
    it(`${locale} ships no dead _one keys (plural categories: ${categories.join('/')})`, () => {
      const offenders = Object.keys(bundle).flatMap((ns) =>
        flattenKeys(bundle[ns])
          .filter((key) => key.endsWith('_one'))
          .map((key) => `${ns}:${key}`),
      );
      expect(offenders).toEqual([]);
    });
  }
});

describe('ru glossary', () => {
  const RU_WORKSPACE_STEM = /пространств\p{L}*/giu;
  const PRECEDED_BY_RABOCH = /рабоч\p{L}*\s*$/iu;

  it('always spells workspace as «рабочее пространство»', () => {
    const bundle = RESOURCES.ru;
    const offenders: string[] = [];
    for (const ns of Object.keys(bundle)) {
      for (const key of flattenKeys(bundle[ns])) {
        const value = key
          .split('.')
          .reduce<unknown>((node, part) => (node as Json)?.[part], bundle[ns]);
        if (typeof value !== 'string') continue;
        for (const match of value.matchAll(RU_WORKSPACE_STEM)) {
          const before = value.slice(0, match.index);
          if (!PRECEDED_BY_RABOCH.test(before)) {
            offenders.push(`${ns}:${key} → "${match[0]}"`);
          }
        }
      }
    }
    expect(offenders).toEqual([]);
  });
});

describe('plural form completeness', () => {
  for (const locale of Object.keys(RESOURCES)) {
    const categories = new Intl.PluralRules(locale).resolvedOptions().pluralCategories;
    const bundle = RESOURCES[locale as keyof typeof RESOURCES];

    it(`${locale} spells every plural key in all of ${categories.join('/')}`, () => {
      const incomplete: string[] = [];
      for (const ns of Object.keys(bundle)) {
        const keys = flattenKeys(bundle[ns]);
        const stems = new Set(
          keys
            .filter((key) => /_(zero|one|two|few|many|other)$/.test(key))
            .map((key) => key.replace(/_(zero|one|two|few|many|other)$/, '')),
        );
        for (const stem of stems) {
          const missing = categories.filter((category) => !keys.includes(`${stem}_${category}`));
          if (missing.length > 0) {
            incomplete.push(`${ns}:${stem} missing ${missing.join('/')}`);
          }
        }
      }
      expect(incomplete).toEqual([]);
    });
  }
});

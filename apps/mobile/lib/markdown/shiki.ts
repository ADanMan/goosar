// Подсветка кода Shiki: тот же движок и темы, что в вебе, но рендер
// в RN <Text>.
import { createHighlighterCore, type HighlighterCore, type ThemedToken } from '@shikijs/core';
import { createNativeEngine, isNativeEngineAvailable } from 'react-native-shiki-engine';

import githubLight from '@shikijs/themes/github-light';
import githubDark from '@shikijs/themes/github-dark';

import bash from '@shikijs/langs/bash';
import go from '@shikijs/langs/go';
import javascript from '@shikijs/langs/javascript';
import json from '@shikijs/langs/json';
import jsx from '@shikijs/langs/jsx';
import markdown from '@shikijs/langs/markdown';
import python from '@shikijs/langs/python';
import rust from '@shikijs/langs/rust';
import sql from '@shikijs/langs/sql';
import tsx from '@shikijs/langs/tsx';
import typescript from '@shikijs/langs/typescript';
import yaml from '@shikijs/langs/yaml';

const LANGS = [bash, go, javascript, json, jsx, markdown, python, rust, sql, tsx, typescript, yaml];

const LANG_ALIASES: Record<string, string> = {
  ts: 'typescript',
  js: 'javascript',
  py: 'python',
  rs: 'rust',
  sh: 'bash',
  zsh: 'bash',
  shell: 'bash',
  yml: 'yaml',
  md: 'markdown',
};

const KNOWN_LANGS: ReadonlySet<string> = new Set([
  'bash',
  'go',
  'javascript',
  'json',
  'jsx',
  'markdown',
  'python',
  'rust',
  'sql',
  'tsx',
  'typescript',
  'yaml',
]);

export const SHIKI_THEME_LIGHT = 'github-light';
export const SHIKI_THEME_DARK = 'github-dark';

let highlighterPromise: Promise<HighlighterCore | null> | null = null;

function initHighlighter(): Promise<HighlighterCore | null> {
  if (!isNativeEngineAvailable()) {
    console.warn(
      '[shiki] react-native-shiki-engine native module unavailable — code blocks will render plain. Did you rebuild the dev client?',
    );
    return Promise.resolve(null);
  }
  return createHighlighterCore({
    themes: [githubLight, githubDark],
    langs: LANGS,
    engine: createNativeEngine(),
  }).catch((err) => {
    console.warn('[shiki] highlighter init failed:', err);
    return null;
  });
}

export function prewarmHighlighter(): void {
  highlighterPromise ??= initHighlighter();
}

export function resolveLang(input: string | undefined): string | null {
  if (!input) return null;
  const lower = input.toLowerCase().trim();
  const resolved = LANG_ALIASES[lower] ?? lower;
  return KNOWN_LANGS.has(resolved) ? resolved : null;
}

export interface HighlightedToken {
  content: string;
  color?: string;
}

export interface HighlightedLine {
  tokens: HighlightedToken[];
}

export async function highlight(
  code: string,
  lang: string,
  theme: string,
): Promise<HighlightedLine[] | null> {
  highlighterPromise ??= initHighlighter();
  const hl = await highlighterPromise;
  if (!hl) return null;
  try {
    const tokens = hl.codeToTokensBase(code, { lang, theme });
    return tokens.map((line: ThemedToken[]) => ({
      tokens: line.map((t) => ({ content: t.content, color: t.color })),
    }));
  } catch (err) {
    console.warn(`[shiki] highlight failed for lang=${lang}:`, err);
    return null;
  }
}

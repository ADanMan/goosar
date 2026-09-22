/**
 * Проверка границ импорта.
 *
 * Смысл этой проверки в том, что в продукте должен быть ровно ОДИН
 * readonly-рендерер. Это свойство не поддерживается само собой: самый
 * дешёвый способ добавить фичу в чат всегда будет выглядеть как "отрендерить
 * этот кусок через обычный компонент Markdown" или "передать сюда кастомный
 * рендерер кода", и каждый такой случай незаметно воссоздаёт вторую цепочку.
 *
 * Тесты читают реальное дерево исходников, поэтому падают на коммите, который
 * вводит форк, а не спустя месяцы, когда поверхности уже заметно разошлись.
 */

import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const VIEWS_ROOT = join(__dirname, '..');

const PRODUCT_SURFACES = ['chat', 'issues', 'skills', 'autopilots', 'inbox'];

function walk(dir: string, out: string[] = []): string[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (entry === 'node_modules' || entry.startsWith('.')) continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.tsx?$/.test(full) && !/\.test\.tsx?$/.test(full)) out.push(full);
  }
  return out;
}

function stripComments(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/.*$/gm, '$1');
}

function sourceFiles(subdirs: string[]): { path: string; text: string }[] {
  return subdirs
    .flatMap((d) => walk(join(VIEWS_ROOT, d)))
    .map((path) => ({
      path: relative(VIEWS_ROOT, path),
      text: stripComments(readFileSync(path, 'utf8')),
    }));
}

describe('RichContent import boundary', () => {
  it('no product surface imports the generic ui Markdown renderer', () => {
    const offenders = sourceFiles(PRODUCT_SURFACES)
      .filter(
        ({ text }) =>
          /from\s+["']@goosar\/ui\/markdown["']/.test(text) &&
          /\bMarkdown\b|\bMemoizedMarkdown\b|\bStreamingMarkdown\b/.test(text),
      )
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it('the deleted chat markdown bridge has not come back', () => {
    const offenders = sourceFiles([...PRODUCT_SURFACES, 'common', 'editor'])
      .filter(({ text }) => /common\/markdown/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it('no product surface builds its own react-markdown pipeline', () => {
    const offenders = sourceFiles([...PRODUCT_SURFACES, 'common', 'editor'])
      .filter(({ text }) => /from\s+["']react-markdown["']/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it('only the canonical renderer configures the sanitize schema', () => {
    const offenders = sourceFiles([...PRODUCT_SURFACES, 'common', 'editor'])
      .filter(({ text }) => /rehype-sanitize|markdownSanitizeSchema/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it('only rich-code-block dispatches on a fence language', () => {
    const TIPTAP_NODEVIEW = 'editor/extensions/code-block-view.tsx';

    const offenders = sourceFiles([...PRODUCT_SURFACES, 'common', 'editor'])
      .filter(({ path }) => path !== TIPTAP_NODEVIEW)
      .filter(({ text }) => /(?:lang|language)\w*\s*===\s*["'](?:mermaid|html)["']/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it('Tiptap reuses the leaf components but never imports RichContent', () => {
    const offenders = walk(join(VIEWS_ROOT, 'editor'))
      .filter((p) => !/readonly-content\.tsx$/.test(p))
      .map((path) => ({
        path: relative(VIEWS_ROOT, path),
        text: stripComments(readFileSync(path, 'utf8')),
      }))
      .filter(({ text }) => /\bRichContent\b/.test(text))
      .map(({ path }) => path);

    expect(offenders).toEqual([]);
  });

  it('the canonical renderer stays in views, not ui', () => {
    const uiRoot = join(VIEWS_ROOT, '..', 'ui');
    const offenders = walk(uiRoot)
      .map((path) => ({ path, text: stripComments(readFileSync(path, 'utf8')) }))
      .filter(({ text }) => /\bRichContent\b|from\s+["']@goosar\/views/.test(text))
      .map(({ path }) => relative(uiRoot, path));

    expect(offenders).toEqual([]);
  });
});

describe('chat renders every text entry through RichContent', () => {
  const chatList = readFileSync(join(VIEWS_ROOT, 'chat/components/chat-message-list.tsx'), 'utf8');

  it('uses RichContent and no other markdown renderer', () => {
    expect(chatList).toMatch(/\bRichContent\b/);
    expect(chatList).not.toMatch(/MemoizedMarkdown|<Markdown\b/);
  });

  it('keys the live row and the persisted assistant row on the task', () => {
    expect(chatList).toMatch(/task:\$\{/);
    expect(chatList).not.toMatch(/computeItemKey=\{\(_, msg\) => msg\.id\}/);
  });
});

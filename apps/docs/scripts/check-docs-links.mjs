// Verifies that every docs link the product can emit resolves to a real docs
// page, and that the page still pins the anchor the link points at.
//
// Two link shapes are collected from app/package source:
//   1. docsUrl(<lang>, "<path>")  — the shared helper in packages/core/i18n
//   2. a literal "https://goosar.ru/docs/<path>" (issue templates, copy)
//
// Russian is the default docs language, so the page for a slug is `<slug>.mdx`
// and headings pin their anchor ids as `## Заголовок [#anchor]`. This script
// keeps a product deep link from silently rotting when a page is removed or a
// heading id is dropped.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, dirname, extname } from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
export const DOCS_ROOT = join(HERE, '..');
export const REPO_ROOT = join(DOCS_ROOT, '..', '..');

const SOURCE_ROOTS = ['packages', 'apps'];
const SOURCE_EXTENSIONS = new Set(['.ts', '.tsx']);
const SKIP_DIRS = new Set(['node_modules', '.next', '.source', 'dist', 'build', 'venv', '.venv']);

// docsUrl(i18n.language, "/agents#anchor") — second argument, when present.
const DOCS_URL_WITH_PATH = /docsUrl\(\s*[^,()]+,\s*["'`]([^"'`]*)["'`]/g;
// docsUrl(i18n.language) — no path argument, meaning the docs root.
const DOCS_URL_ROOT = /docsUrl\(\s*[^,()]+\s*\)/g;
// Literal absolute docs links. The optional `/en` segment is the English locale.
const LITERAL_DOCS_URL = /https:\/\/goosar\.ru\/docs(?:\/en)?([^\s"'`)\]。，、]*)/g;

function* walk(dir) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    if (SKIP_DIRS.has(entry)) continue;
    const full = join(dir, entry);
    let stats;
    try {
      stats = statSync(full);
    } catch {
      // A dangling symlink (a stale venv from a local agent build, a removed
      // target) must not abort the whole scan; there is nothing to read there.
      continue;
    }
    if (stats.isDirectory()) {
      yield* walk(full);
    } else if (SOURCE_EXTENSIONS.has(extname(entry)) && !entry.includes('.test.')) {
      yield full;
    }
  }
}

/** Collect every docs path the product can emit, as `{ path, source }`. */
export function collectProductDocsLinks(repoRoot = REPO_ROOT) {
  const links = [];
  for (const root of SOURCE_ROOTS) {
    for (const file of walk(join(repoRoot, root))) {
      const content = readFileSync(file, 'utf8');
      const relative = file.slice(repoRoot.length + 1);
      for (const match of content.matchAll(DOCS_URL_WITH_PATH)) {
        links.push({ path: match[1], source: relative });
      }
      for (const _ of content.matchAll(DOCS_URL_ROOT)) {
        links.push({ path: '', source: relative });
      }
      for (const match of content.matchAll(LITERAL_DOCS_URL)) {
        // Prose links end in sentence punctuation; that is not part of the path.
        links.push({ path: match[1].replace(/[.,;:!?]+$/, ''), source: relative });
      }
    }
  }
  return links;
}

function pageStem(path) {
  const [pathname] = path.split('#');
  const slug = pathname.replace(/^\/+|\/+$/g, '');
  return slug === '' ? 'index' : slug;
}

function pageFor(path, docsRoot, suffix) {
  return join(docsRoot, 'content', 'docs', `${pageStem(path)}${suffix}`);
}

function readOrNull(file) {
  try {
    return readFileSync(file, 'utf8');
  } catch {
    return null;
  }
}

/**
 * Returns a list of human-readable problems. An empty list means every docs
 * link the product can emit points at a page that exists, and every anchor
 * is pinned in that page.
 */
export function checkDocsLinks({ repoRoot = REPO_ROOT, docsRoot = DOCS_ROOT } = {}) {
  const problems = [];
  for (const { path, source } of collectProductDocsLinks(repoRoot)) {
    const pageFile = pageFor(path, docsRoot, '.mdx');
    const page = readOrNull(pageFile);
    if (page === null) {
      problems.push(`${source}: "${path}" has no docs page (expected ${pageStem(path)}.mdx)`);
      continue;
    }
    const anchor = path.split('#')[1];
    if (!anchor) continue;
    if (!page.includes(`[#${anchor}]`)) {
      problems.push(
        `${source}: "${path}" — anchor #${anchor} is not pinned in ${pageFile.slice(docsRoot.length + 1)}`,
      );
    }
  }
  return problems;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const problems = checkDocsLinks();
  if (problems.length > 0) {
    console.error('Broken docs links:');
    for (const problem of problems) console.error(`  - ${problem}`);
    process.exit(1);
  }
  console.log('All product docs links resolve, with anchors intact.');
}

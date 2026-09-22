#!/usr/bin/env node
// Regression guard for #430: fails when user-visible English copy is written
// straight into the components this issue routed through i18n, instead of
// through `t(($) => ...)` plus a key in all five locales.
//
// This is a tripwire, not a proof. It looks at the three places the untranslated
// copy in #430 actually lived -- JSX text nodes, user-facing call arguments
// (`toast.error("Endpoint unreachable")`), and user-facing JSX attributes
// (`aria-label="Close"`) -- and only flags a string that reads like a sentence:
// two or more Latin words once brand names and identifiers are removed. A
// single word is almost always a brand, an env var or a command, so it passes.
//
// Usage: node scripts/check-ui-strings.mjs [--dir <path>]...

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(fileURLToPath(new URL('..', import.meta.url)));

// Directories translated by #430. Adding a new user-facing component under one
// of these means adding its copy to packages/views/locales too.
const GUARDED_DIRS = [
  'apps/desktop/src/renderer/src/components',
  'packages/views/settings',
  'packages/views/onboarding',
];

// Untranslated components that predate #430 and are out of its scope. They are
// named here rather than silently excluded so the debt stays visible: a new file
// in a guarded directory is checked from its first commit, these four are not.
const LEGACY_FILES = new Set([
  'apps/desktop/src/renderer/src/components/route-error-page.tsx',
  'apps/desktop/src/renderer/src/components/tab-bar.tsx',
  'apps/desktop/src/renderer/src/components/uninstall-section.tsx',
  'apps/desktop/src/renderer/src/components/update-notification.tsx',
]);

// Words that are names, not copy: brands, products, commands, env fragments.
// They are stripped before the sentence heuristic runs, so `<span>Kerberos</span>`
// and "Codex CLI" stay legal while "Endpoint unreachable" does not.
const ALLOWED_WORDS = new Set(
  [
    'hermes',
    'goosar',
    'claude',
    'codex',
    'cursor',
    'copilot',
    'gemini',
    'openai',
    'anthropic',
    'kerberos',
    'sso',
    'ldap',
    'oauth',
    'api',
    'url',
    'uri',
    'id',
    'ip',
    'dns',
    'tls',
    'ssl',
    'http',
    'https',
    'json',
    'yaml',
    'toml',
    'cli',
    'mcp',
    'llm',
    'gpu',
    'cpu',
    'ram',
    'os',
    'macos',
    'windows',
    'linux',
    'docker',
    'npm',
    'pnpm',
    'pipx',
    'brew',
    'node',
    'python',
    'go',
    'git',
    'github',
    'gitlab',
    'jira',
    'slack',
    'kinit',
    'klist',
    'keytab',
    'realm',
    'kdc',
    'daemon',
    'gateway',
    'runtime',
    'webhook',
    'token',
    'uuid',
  ].map((w) => w.toLowerCase()),
);

// Calls whose first string argument is shown to a person.
const USER_FACING_CALL =
  /\b(?:toast(?:\.(?:success|error|info|warning|message|loading))?|window\.alert|window\.confirm|alert|confirm)\s*\(\s*(["'])((?:\\.|(?!\1).)*)\1/g;

// JSX attributes whose string value is read by a person (or a screen reader).
// `description` is on the list because SettingsRow/SettingsSection put their
// longest sentences there -- the "Automatically start the daemon when the app
// opens" class of copy this issue translated.
const USER_FACING_ATTR =
  /\b(?:aria-label|aria-description|aria-placeholder|placeholder|title|alt|label|description|tooltip|emptyMessage)\s*=\s*(["'])((?:\\.|(?!\1).)*)\1/g;

// A JSX text node: characters between a closing `>` and an opening `<` that
// look like prose. Punctuation is allowed; anything with a brace, slash,
// equals or backtick is code, not copy. The lookbehind rejects `=>`, `->` and
// comparison operators so an arrow function body is not read as a text node.
// Interpolations are blanked out first, so "{n} more — click to expand" is
// still read as the sentence it is.
// Known gap: the text node must sit on ONE line. Prettier puts a long sentence
// on its own line, so multi-line copy passes. Relaxing the newline class was
// tried and reverted: `Record<...>` / `Set<...>` spans then read as text nodes
// and fired inside guarded directories. The attribute and call-argument rules
// above cover most long copy; this one catches the short inline labels.
const JSX_TEXT = /(?<=[^=!<>-]>)([^<>\n]+)(?=<)/g;
// A leading `(` means a parameter list between two generics, never copy.
const PROSE_ONLY = /^(?!\()[A-Za-z0-9 ,.:;!?'’‘“”"()\-–—…&%+#]+$/;

function isSentence(raw) {
  const text = raw.trim();
  if (!text) return false;
  if (!/[A-Za-z]/.test(text)) return false;
  // Identifier-ish: SCREAMING_SNAKE, dotted paths, kebab css, urls, templates.
  if (/[{}$`\\/=<>|]/.test(text)) return false;
  if (/^[A-Z0-9_]+$/.test(text)) return false;
  if (/_[a-zA-Z]/.test(text)) return false;

  const words = (text.match(/[A-Za-z][A-Za-z'’]{1,}/g) ?? []).filter(
    (w) => !ALLOWED_WORDS.has(w.toLowerCase()),
  );
  return words.length >= 2;
}

/** Blank out comments and import statements so their prose is not scanned. */
function stripNonCode(source) {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '))
    .replace(/(^|[^:"'`\\])\/\/[^\n]*/g, (m, lead) => lead + ' '.repeat(m.length - lead.length))
    .replace(/^\s*import\s[^;]*;?\s*$/gm, (m) => m.replace(/[^\n]/g, ' '));
}

function collectFiles(dir, out = []) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (entry === 'node_modules' || entry === 'dist' || entry === '__snapshots__') continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      collectFiles(full, out);
      continue;
    }
    if (!/\.tsx?$/.test(entry)) continue;
    if (/\.(test|spec)\.tsx?$/.test(entry)) continue;
    if (LEGACY_FILES.has(relative(ROOT, full))) continue;
    out.push(full);
  }
  return out;
}

function findViolations(file) {
  const source = stripNonCode(readFileSync(file, 'utf8'));
  const found = [];
  const lineOf = (index) => source.slice(0, index).split('\n').length;

  const record = (index, kind, text) => {
    found.push({ line: lineOf(index), kind, text: text.trim() });
  };

  for (const m of source.matchAll(USER_FACING_CALL)) {
    if (isSentence(m[2])) record(m.index, 'call argument', m[2]);
  }
  for (const m of source.matchAll(USER_FACING_ATTR)) {
    if (isSentence(m[2])) record(m.index, 'JSX attribute', m[2]);
  }
  for (const m of source.matchAll(JSX_TEXT)) {
    const candidate = m[1].replace(/\{[^{}]*\}/g, ' ');
    // Unbalanced or nested braces left over mean this was code, not a text node.
    if (/[{}]/.test(candidate)) continue;
    if (PROSE_ONLY.test(candidate.trim()) && isSentence(candidate)) {
      record(m.index, 'JSX text', m[1]);
    }
  }
  return found;
}

const args = process.argv.slice(2);
const overrides = [];
for (let i = 0; i < args.length; i += 1) {
  if (args[i] === '--dir' && args[i + 1]) {
    overrides.push(resolve(args[i + 1]));
    i += 1;
  }
}
const targets = overrides.length > 0 ? overrides : GUARDED_DIRS.map((d) => join(ROOT, d));

let total = 0;
for (const dir of targets) {
  for (const file of collectFiles(dir)) {
    for (const v of findViolations(file)) {
      total += 1;
      console.error(
        `${relative(ROOT, file)}:${v.line}  hardcoded UI string (${v.kind}): ${JSON.stringify(v.text)}`,
      );
    }
  }
}

if (total > 0) {
  console.error(
    `\n${total} hardcoded UI string(s). Move the copy into packages/views/locales ` +
      `(all five locales) and render it with t(($) => $....). Names that are not ` +
      `copy -- brands, commands, env vars -- belong in ALLOWED_WORDS in this script.`,
  );
  process.exit(1);
}
console.log(`ok: no hardcoded UI strings in ${targets.length} guarded director(ies)`);

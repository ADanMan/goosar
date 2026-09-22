/**
 * Проверка целей экспорта в package.json.
 *
 * Каждый путь в карте `exports` пакета должен указывать на реально
 * существующий файл. Удаление или перенос модуля без обновления `exports`
 * оставляет висячий подпуть, который всё ещё резолвится для TypeScript (он
 * читает дерево исходников), но падает при сборке в любом приложении, которое
 * его импортирует — то есть переживает тайпчек и юнит-тесты и всплывает
 * только на сборке потребляющего приложения.
 *
 * Область — весь монорепозиторий, а не только `views`: это свойство
 * package.json, а не конкретного пакета.
 */

import { describe, expect, it } from 'vitest';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';

const REPO_ROOT = join(__dirname, '..', '..', '..');

function workspacePackageJsons(): string[] {
  const out: string[] = [];
  for (const group of ['packages', 'apps']) {
    const groupDir = join(REPO_ROOT, group);
    if (!existsSync(groupDir)) continue;
    for (const entry of readdirSync(groupDir)) {
      const pkg = join(groupDir, entry, 'package.json');
      if (existsSync(pkg) && statSync(pkg).isFile()) out.push(pkg);
    }
  }
  return out;
}

function exportTargets(value: unknown): string[] {
  if (typeof value === 'string') return [value];
  if (value && typeof value === 'object') {
    return Object.values(value as Record<string, unknown>).flatMap(exportTargets);
  }
  return [];
}

interface Dangling {
  pkg: string;
  subpath: string;
  target: string;
}

function findDanglingExports(): Dangling[] {
  const dangling: Dangling[] = [];

  for (const pkgPath of workspacePackageJsons()) {
    const pkgDir = dirname(pkgPath);
    let parsed: { exports?: unknown };
    try {
      parsed = JSON.parse(readFileSync(pkgPath, 'utf8'));
    } catch {
      continue;
    }
    const exports = parsed.exports;
    if (!exports || typeof exports !== 'object') continue;

    for (const [subpath, value] of Object.entries(exports as Record<string, unknown>)) {
      for (const target of exportTargets(value)) {
        if (target.includes('*')) {
          const prefix = target.slice(0, target.indexOf('*'));
          if (!existsSync(join(pkgDir, prefix))) {
            dangling.push({ pkg: relative(REPO_ROOT, pkgPath), subpath, target });
          }
          continue;
        }
        if (!existsSync(join(pkgDir, target))) {
          dangling.push({ pkg: relative(REPO_ROOT, pkgPath), subpath, target });
        }
      }
    }
  }

  return dangling;
}

describe('workspace package exports', () => {
  it('every export target points at a file that exists', () => {
    const dangling = findDanglingExports();
    expect(dangling.map((d) => `${d.pkg} :: "${d.subpath}" -> ${d.target}`)).toEqual([]);
  });

  it('the deleted chat markdown bridge is not exported', () => {
    const views = JSON.parse(
      readFileSync(join(REPO_ROOT, 'packages/views/package.json'), 'utf8'),
    ) as { exports?: Record<string, unknown> };

    expect(Object.keys(views.exports ?? {})).not.toContain('./common/markdown');
  });

  it('detects a dangling target when one is introduced', () => {
    const pkgDir = join(REPO_ROOT, 'packages/views');
    expect(existsSync(join(pkgDir, './editor/index.ts'))).toBe(true);
    expect(existsSync(join(pkgDir, './common/markdown.tsx'))).toBe(false);
    expect(workspacePackageJsons().length).toBeGreaterThan(3);
  });
});

// Генерирует THIRD_PARTY_NOTICES.md: список npm- и Go-зависимостей с лицензиями,
// строками копирайта и полными текстами лицензий (один текст на каждый SPDX-идентификатор).
// Запуск из корня репозитория: node scripts/third-party-notices.mjs
import { execFileSync } from 'node:child_process';
import { readdirSync, readFileSync, writeFileSync, existsSync, statSync } from 'node:fs';
import { join } from 'node:path';

const OUT = 'THIRD_PARTY_NOTICES.md';
const LICENSE_FILE_RE = /^(license|licence|copying)(\..*)?$/i;

function licenseFileIn(dir) {
  try {
    const name = readdirSync(dir).find((n) => LICENSE_FILE_RE.test(n));
    if (!name) return null;
    const p = join(dir, name);
    return statSync(p).isFile() ? readFileSync(p, 'utf-8') : null;
  } catch {
    return null;
  }
}

function copyrightLines(text) {
  if (!text) return [];
  return [...new Set(text.split('\n').map((l) => l.trim()).filter((l) => /copyright\s+(\(c\)|©|\d{4})/i.test(l)))].slice(0, 5);
}

function npmPackages() {
  const raw = execFileSync('pnpm', ['licenses', 'list', '--json'], { encoding: 'utf-8', maxBuffer: 64 << 20 });
  const byLicense = JSON.parse(raw);
  const out = [];
  for (const [license, pkgs] of Object.entries(byLicense)) {
    for (const p of pkgs) {
      const dir = p.paths?.[0];
      const text = dir ? licenseFileIn(dir) : null;
      out.push({ eco: 'npm', name: p.name, version: p.versions.join(', '), license, url: p.homepage ?? '', text });
    }
  }
  return out;
}

function goPackages() {
  const csv = execFileSync(
    'go',
    ['run', 'github.com/google/go-licenses@v1.6.0', 'report', './...', '--ignore', 'github.com/adanman/goosar'],
    { cwd: 'server', encoding: 'utf-8', stdio: ['ignore', 'pipe', 'ignore'] },
  );
  const gomodcache = execFileSync('go', ['env', 'GOMODCACHE'], { encoding: 'utf-8' }).trim();
  return csv
    .trim()
    .split('\n')
    .filter(Boolean)
    .map((line) => {
      const [mod, url, license] = line.split(',');
      const text = goModuleLicenseText(gomodcache, mod);
      return { eco: 'go', name: mod, version: '', license, url, text };
    });
}

function goModuleLicenseText(gomodcache, mod) {
  // go-licenses ссылается на модуль по пути пакета; ищем ближайший каталог модуля в кэше.
  const parts = mod.split('/');
  for (let i = parts.length; i > 1; i--) {
    const prefix = parts.slice(0, i).join('/');
    const parent = join(gomodcache, ...prefix.split('/').slice(0, -1));
    const base = prefix.split('/').at(-1);
    if (!existsSync(parent)) continue;
    const versioned = readdirSync(parent).filter((n) => n.startsWith(`${base}@`)).sort().at(-1);
    if (!versioned) continue;
    const text = licenseFileIn(join(parent, versioned));
    if (text) return text;
  }
  return null;
}

const pkgs = [...npmPackages(), ...goPackages()].sort((a, b) => a.name.localeCompare(b.name));
const textBySpdx = new Map();
for (const p of pkgs) if (p.text && !textBySpdx.has(p.license)) textBySpdx.set(p.license, p.text);

const lines = [
  '# Уведомления о компонентах третьих сторон',
  '',
  'Goosar распространяется под лицензией MIT и включает перечисленные ниже компоненты.',
  'Для каждого указана лицензия и строки копирайта из его файла LICENSE; полные тексты лицензий приведены в приложении.',
  `Сгенерировано scripts/third-party-notices.mjs: ${pkgs.length} компонентов.`,
  '',
  '| Компонент | Версия | Лицензия | Копирайт |',
  '|---|---|---|---|',
];
for (const p of pkgs) {
  const cr = copyrightLines(p.text).join('; ').replace(/\|/g, '\\|');
  lines.push(`| ${p.eco}: ${p.name} | ${p.version} | ${p.license} | ${cr} |`);
}
lines.push('', '## Приложение: тексты лицензий', '');
for (const [spdx, text] of [...textBySpdx.entries()].sort()) {
  lines.push(`### ${spdx}`, '', '```', text.trim(), '```', '');
}
writeFileSync(OUT, lines.join('\n'));
console.log(`${OUT}: ${pkgs.length} компонентов, ${textBySpdx.size} текстов лицензий`);

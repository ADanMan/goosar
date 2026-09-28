#!/usr/bin/env node
// Visual-standard guard (ADR-0007, T-035).
//
// 1. Colour literals: hex / rgb() / rgba() / hsl() / oklch() literals and raw
//    Tailwind palette steps (bg-red-500) are
//    rejected in component code under packages/** and apps/web/features/**
//    (plus the desktop renderer). Colours come from tokens.css via var(--…).
// 2. Arbitrary type sizes and radii: `text-[…px|rem]`, text-4xl…9xl and
//    `rounded-[…]` are rejected; the scales live in tokens.css.
// 3. Token contrast: parses packages/ui/styles/tokens.css, resolves the
//    light (:root) and dark (.dark) themes and checks every declared
//    foreground/background pair against WCAG 2.2 AA — 4.5:1 for text,
//    3:1 for UI components and large text.
//
// Usage: node scripts/check-ui-tokens.mjs [--verbose]
// Exit code 1 on any violation.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, extname } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = join(fileURLToPath(new URL(".", import.meta.url)), "..");
const VERBOSE = process.argv.includes("--verbose");

// ---------------------------------------------------------------------------
// Allow-list: files whose literals are data, not theme colours. Each entry
// carries the reason; keep it short and specific.
const LITERAL_ALLOW = new Map([
  // User-picked label colours: the palette *is* the data being chosen.
  ["packages/views/common/label-colors.ts", "палитра цветов меток — данные выбора пользователя"],
  // The free-form picker paints a hue spectrum and a white/black value ramp.
  ["packages/views/common/color-picker.tsx", "спектр оттенков и шкала яркости свободного выбора цвета"],
  // Canvas 2D fillStyle for the exported JPEG cannot resolve CSS variables.
  ["packages/views/common/avatar-crop-dialog.tsx", "фон JPEG при экспорте через Canvas API, var(--…) там не работает"],
  // Third-party brand marks must keep their official colours.
  ["packages/views/onboarding/components/brand-icons.tsx", "официальные цвета логотипов провайдеров"],
  // Mermaid renders into an isolated SVG and needs concrete colour values.
  ["packages/views/editor/mermaid-diagram.tsx", "Mermaid требует конкретные значения цветов темы"],
  // Recharts attribute selectors target library defaults, not our palette.
  ["packages/ui/components/ui/chart.tsx", "селекторы атрибутов recharts ([stroke='#ccc']), не цвет темы"],
]);

const SCAN_DIRS = [
  "packages/ui/components",
  "packages/ui/lib",
  "packages/core/projects",
  "packages/core/issues",
  "packages/ui/markdown",
  "packages/views",
  "apps/web/features",
  "apps/desktop/src/renderer/src",
];
const EXTS = new Set([".ts", ".tsx"]);
const SKIP = /(node_modules|\.test\.|\.spec\.|__tests__|\/locales\/|\/i18n\/|\/test\/)/;

function walk(dir, out = []) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const name of entries) {
    const p = join(dir, name);
    const st = statSync(p);
    if (st.isDirectory()) walk(p, out);
    else if (EXTS.has(extname(p))) out.push(p);
  }
  return out;
}

// Strip comments so documentation may mention literals.
function stripComments(src) {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
    .replace(/(^|[^:"'`\\])\/\/.*$/gm, (m, p1) => p1 + " ".repeat(m.length - p1.length));
}

// Hex must look like a colour inside a class/style string: preceded by
// `#` right after a quote, bracket, colon, space or `(`, and 3/4/6/8 digits.
const HEX = /(?<=["'`\[(:\s,])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![0-9a-zA-Z_-])/g;
const FN = /\b(?:rgba?|hsla?|oklch|oklab|lab|lch)\(\s*[\d.]/g;
const TEXT_ARB = /\btext-\[\d+(?:\.\d+)?(?:px|rem|em)\]/g;
const TEXT_BIG = /\btext-(?:[4-9])xl\b/g;
// Radii come from the --radius-* ramp (rounded-xs…4xl, rounded-full).
const RADIUS_ARB = /\brounded(?:-[trblse]{1,2})?-\[(?!inherit\])[^\]]+\]/g;
// Raw Tailwind palette steps bypass the theme (no dark pair, no contrast
// guarantee): bg-red-500, text-emerald-600/80 …
const RAW_PALETTE =
  /(?<![\w-])(?:bg|text|border|ring|fill|stroke|from|via|to|outline|decoration|divide|shadow|accent|caret)-(?:red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|slate|gray|zinc|neutral|stone)-\d{2,3}\b/g;

const problems = [];
for (const d of SCAN_DIRS) {
  for (const file of walk(join(ROOT, d))) {
    const rel = relative(ROOT, file).split("\\").join("/");
    if (SKIP.test(rel)) continue;
    const src = stripComments(readFileSync(file, "utf8"));
    const lines = src.split("\n");
    lines.forEach((line, i) => {
      if (!LITERAL_ALLOW.has(rel)) {
        for (const m of line.matchAll(HEX)) {
          // `#123` issue references inside plain strings are not colours:
          // require a letter a-f or ≥ 6 digits for a 3/4-digit hex.
          const v = m[0].slice(1);
          if (v.length <= 4 && /^\d+$/.test(v)) continue;
          problems.push(`${rel}:${i + 1}: литерал цвета ${m[0]}`);
        }
        for (const m of line.matchAll(FN)) problems.push(`${rel}:${i + 1}: литерал цвета ${m[0]}…)`);
        for (const m of line.matchAll(RAW_PALETTE)) problems.push(`${rel}:${i + 1}: цвет палитры вместо токена ${m[0]}`);
      }
      for (const m of line.matchAll(TEXT_ARB)) problems.push(`${rel}:${i + 1}: произвольный размер ${m[0]}`);
      for (const m of line.matchAll(TEXT_BIG)) problems.push(`${rel}:${i + 1}: размер вне шкалы ${m[0]}`);
      for (const m of line.matchAll(RADIUS_ARB)) problems.push(`${rel}:${i + 1}: произвольный радиус ${m[0]}`);
    });
  }
}

// ---------------------------------------------------------------------------
// Contrast
function oklchToLinearSrgb(L, C, H) {
  const h = (H * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.291485548 * b;
  const l = l_ ** 3;
  const m = m_ ** 3;
  const s = s_ ** 3;
  return [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ].map((x) => Math.min(1, Math.max(0, x)));
}
const luminance = ([r, g, b]) => 0.2126 * r + 0.7152 * g + 0.0722 * b;
function ratio(c1, c2) {
  const a = luminance(c1);
  const b = luminance(c2);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

const tokensSrc = readFileSync(join(ROOT, "packages/ui/styles/tokens.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
function block(selectorRe) {
  const m = tokensSrc.match(selectorRe);
  if (!m) throw new Error(`tokens.css: блок ${selectorRe} не найден`);
  const vars = {};
  for (const d of m[1].matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) vars[d[1]] = d[2].trim();
  return vars;
}
const light = block(/:root\s*,\s*\.landing-light\s*\{([\s\S]*?)\n\}/);
const dark = { ...light, ...block(/\n\.dark\s*\{([\s\S]*?)\n\}/) };

function resolve(theme, name, depth = 0) {
  const v = theme[name];
  if (v == null) throw new Error(`токен --${name} не объявлен`);
  const ref = v.match(/^var\(--([\w-]+)\)$/);
  if (ref && depth < 8) return resolve(theme, ref[1], depth + 1);
  const m = v.match(/^oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*(?:\/\s*([\d.]+)(%?))?\s*\)$/);
  if (!m) return null;
  let alpha = m[4] == null ? 1 : Number(m[4]) / (m[5] ? 100 : 1);
  return { rgb: oklchToLinearSrgb(Number(m[1]), Number(m[2]), Number(m[3])), alpha };
}
// Alpha-blend in linear light (approximation adequate for thresholds).
function over(fg, bg) {
  if (fg.alpha >= 1) return fg.rgb;
  return fg.rgb.map((c, i) => c * fg.alpha + bg[i] * (1 - fg.alpha));
}

const SURFACES = ["background", "card", "popover", "muted", "app-shell", "surface-hover"];
const TEXT = 4.5;
const UI = 3;
// [foreground, backgrounds, threshold]
const PAIRS = [
  ["foreground", SURFACES, TEXT],
  ["muted-foreground", SURFACES, TEXT],
  ["surface-foreground", ["surface", "surface-hover", "surface-selected"], TEXT],
  ["surface-selected-foreground", ["surface-selected"], TEXT],
  ["brand", ["background", "card", "muted"], TEXT],
  ["brand-foreground", ["brand"], TEXT],
  ["primary-foreground", ["primary"], TEXT],
  ["secondary-foreground", ["secondary"], TEXT],
  ["accent-foreground", ["accent"], TEXT],
  ["destructive", ["background", "card", "popover"], TEXT],
  ["success", ["background", "card", "popover"], TEXT],
  ["warning", ["background", "card", "popover"], TEXT],
  ["warning-foreground", ["warning"], TEXT],
  ["info", ["background", "card", "popover"], TEXT],
  ["sidebar-foreground", ["sidebar", "sidebar-accent"], TEXT],
  ["sidebar-accent-foreground", ["sidebar-accent"], TEXT],
  ["inverse-foreground", ["inverse"], TEXT],
  ["inverse-muted-foreground", ["inverse"], TEXT],
  ["accent-beak", ["background", "card", "muted"], UI],
  ["status-backlog", ["background", "card", "muted"], UI],
  ["status-todo", ["background", "card", "muted"], UI],
  ["status-doing", ["background", "card", "muted"], UI],
  ["status-review", ["background", "card", "muted"], UI],
  ["status-done", ["background", "card", "muted"], UI],
  ["input", ["background", "card"], UI],
  ["ring", ["background", "card"], UI],
];

const report = [];
for (const [themeName, theme] of [["light", light], ["dark", dark]]) {
  for (const [fg, bgs, min] of PAIRS) {
    for (const bgName of bgs) {
      const bg = resolve(theme, bgName);
      const f = resolve(theme, fg);
      if (!bg || !f) {
        problems.push(`tokens.css [${themeName}]: не удалось разобрать --${fg} или --${bgName}`);
        continue;
      }
      const base = over(bg, resolve(theme, "background").rgb);
      const r = ratio(over(f, base), base);
      report.push({ themeName, fg, bgName, r, min });
      if (r < min) problems.push(`tokens.css [${themeName}]: --${fg} на --${bgName} = ${r.toFixed(2)}:1 < ${min}:1`);
    }
  }
}

if (VERBOSE) {
  for (const x of report) {
    console.log(`${x.themeName.padEnd(5)} ${("--" + x.fg).padEnd(30)} на ${("--" + x.bgName).padEnd(20)} ${x.r.toFixed(2)}:1 (≥ ${x.min})`);
  }
}

if (problems.length) {
  console.error(`check-ui-tokens: ${problems.length} нарушений ADR-0007`);
  for (const p of problems) console.error("  " + p);
  process.exit(1);
}
console.log(`check-ui-tokens: ок — литералов цветов и произвольных размеров нет, ${report.length} пар токенов проходят пороги WCAG 2.2 AA`);

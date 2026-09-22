#!/usr/bin/env node
// Vendors the Google Fonts payload used by next/font/google so an offline
// (perimeter) build never talks to fonts.googleapis.com / fonts.gstatic.com.
//
// Run this on a CONNECTED machine (normally via offline/make-kit.sh):
//
//   node offline/gen-font-kit.mjs \
//     --out offline/kit/context/fonts \
//     --prefix /offline/kit/context/fonts
//
// It reuses the exact URL/CSS logic of the Next.js version pinned by
// apps/web (next/dist/compiled/@next/font), fetches the CSS and every font
// file it references, rewrites the CSS to point at local paths under
// --prefix, and emits mocked-responses.js. During the offline docker build,
// Dockerfile.web sets NEXT_FONT_GOOGLE_MOCKED_RESPONSES to that file, which
// makes next/font/google read CSS from the map and font files from disk
// instead of the network (the same escape hatch Next's own test-suite uses).
//
// FONT_DECLARATIONS below must mirror every next/font/google call in the
// repo. If a layout adds or changes a font, the offline build fails loudly
// with "Missing mocked response for URL: ..." — regenerate the kit after
// updating the list.

import { createRequire } from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, '..');

// Every next/font/google call site in the repository. Keep options identical
// to the source (only family/weights/styles/display affect the request URL,
// but mirroring exactly keeps drift obvious):
// - apps/web/app/layout.tsx
// - apps/web/app/(landing)/layout.tsx
// - apps/docs/app/[lang]/layout.tsx (not part of the self-host images, but
//   included so a full-workspace offline build also resolves)
const FONT_DECLARATIONS = [
  // apps/web/app/layout.tsx + apps/docs/app/[lang]/layout.tsx (same URL)
  { functionName: 'Inter', options: { subsets: ['latin'], variable: '--font-inter' } },
  {
    functionName: 'Geist_Mono',
    options: {
      subsets: ['latin'],
      variable: '--font-mono',
      fallback: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Consolas', 'monospace'],
    },
  },
  {
    functionName: 'Source_Serif_4',
    options: {
      subsets: ['latin'],
      style: ['normal', 'italic'],
      variable: '--font-serif',
      fallback: [
        'ui-serif',
        'Iowan Old Style',
        'Apple Garamond',
        'Baskerville',
        'Times New Roman',
        'serif',
      ],
    },
  },
  // apps/docs/app/[lang]/layout.tsx (upright-only variant, distinct URL)
  {
    functionName: 'Source_Serif_4',
    options: {
      subsets: ['latin'],
      style: ['normal'],
      variable: '--font-serif',
      fallback: [
        'ui-serif',
        'Iowan Old Style',
        'Apple Garamond',
        'Baskerville',
        'Times New Roman',
        'serif',
      ],
    },
  },
  // apps/web/app/(landing)/layout.tsx
  {
    functionName: 'Instrument_Serif',
    options: { subsets: ['latin'], weight: '400', variable: '--font-serif' },
  },
  {
    functionName: 'Noto_Serif_SC',
    options: { subsets: ['latin'], weight: '400', variable: '--font-serif-zh' },
  },
];

function parseArgs(argv) {
  const args = { out: '', prefix: '' };
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === '--out') {
      args.out = argv[i + 1] ?? '';
      i += 1;
    } else if (argv[i] === '--prefix') {
      args.prefix = argv[i + 1] ?? '';
      i += 1;
    } else {
      throw new Error(`Unknown argument: ${argv[i]}`);
    }
  }
  if (!args.out || !args.prefix) {
    throw new Error(
      'Usage: gen-font-kit.mjs --out <dir> --prefix <absolute path the CSS will reference at build time>',
    );
  }
  if (!args.prefix.startsWith('/')) {
    // next/font/google only reads local font files for URLs starting with
    // "/", so a relative prefix would silently break the offline build.
    throw new Error(`--prefix must be an absolute path, got: ${args.prefix}`);
  }
  return args;
}

function loadNextFontInternals() {
  // Resolve next from apps/web so the kit matches the exact version the web
  // image builds with.
  const webRequire = createRequire(path.join(repoRoot, 'apps', 'web', 'package.json'));
  let nextPkgPath;
  try {
    nextPkgPath = webRequire.resolve('next/package.json');
  } catch {
    throw new Error(
      'Could not resolve `next` from apps/web. Run `pnpm install` at the repo root first.',
    );
  }
  const nextDir = path.dirname(nextPkgPath);
  const googleDir = path.join(nextDir, 'dist', 'compiled', '@next', 'font', 'dist', 'google');
  const req = createRequire(path.join(googleDir, 'noop.js'));
  return {
    nextVersion: webRequire('next/package.json').version,
    validateGoogleFontFunctionCall: req('./validate-google-font-function-call.js')
      .validateGoogleFontFunctionCall,
    getFontAxes: req('./get-font-axes.js').getFontAxes,
    getGoogleFontsUrl: req('./get-google-fonts-url.js').getGoogleFontsUrl,
    fetchCSSFromGoogleFonts: req('./fetch-css-from-google-fonts.js').fetchCSSFromGoogleFonts,
    findFontFilesInCss: req('./find-font-files-in-css.js').findFontFilesInCss,
    fetchFontFile: req('./fetch-font-file.js').fetchFontFile,
  };
}

async function main() {
  const { out, prefix } = parseArgs(process.argv.slice(2));
  if (process.env.NEXT_FONT_GOOGLE_MOCKED_RESPONSES) {
    // The generator must do REAL fetches; a leftover mock env would recurse.
    throw new Error('Unset NEXT_FONT_GOOGLE_MOCKED_RESPONSES before generating the kit.');
  }

  const internals = loadNextFontInternals();
  const outDir = path.resolve(repoRoot, out);
  fs.mkdirSync(outDir, { recursive: true });

  /** @type {Record<string, string>} url -> rewritten CSS */
  const mockedResponses = {};
  const fontFileCount = { total: 0 };

  for (const { functionName, options } of FONT_DECLARATIONS) {
    const { fontFamily, weights, styles, display, selectedVariableAxes } =
      internals.validateGoogleFontFunctionCall(functionName, options);
    const fontAxes = internals.getFontAxes(fontFamily, weights, styles, selectedVariableAxes);
    const url = internals.getGoogleFontsUrl(fontFamily, fontAxes, display);

    if (mockedResponses[url]) {
      console.log(`= ${fontFamily}: already vendored (${url})`);
      continue;
    }

    console.log(`> ${fontFamily}: ${url}`);
    let css = await internals.fetchCSSFromGoogleFonts(url, fontFamily, false);

    const fontFiles = internals.findFontFilesInCss(css);
    for (const { googleFontFileUrl } of fontFiles) {
      const extMatch = /\.(woff2?|eot|ttf|otf)(\?.*)?$/.exec(googleFontFileUrl);
      const ext = extMatch ? extMatch[1] : 'woff2';
      const hash = crypto.createHash('sha256').update(googleFontFileUrl).digest('hex').slice(0, 16);
      const fileName = `${fontFamily.toLowerCase().replace(/[^a-z0-9]+/g, '-')}-${hash}.${ext}`;
      const filePath = path.join(outDir, fileName);
      if (!fs.existsSync(filePath)) {
        const buffer = await internals.fetchFontFile(googleFontFileUrl, false);
        fs.writeFileSync(filePath, buffer);
      }
      css = css.split(googleFontFileUrl).join(`${prefix}/${fileName}`);
      fontFileCount.total += 1;
    }
    console.log(`  ${fontFiles.length} font file(s)`);
    mockedResponses[url] = css;
  }

  const mockPath = path.join(outDir, 'mocked-responses.js');
  fs.writeFileSync(
    mockPath,
    '// Generated by offline/gen-font-kit.mjs — do not edit by hand.\n' +
      '// Consumed via NEXT_FONT_GOOGLE_MOCKED_RESPONSES during offline builds.\n' +
      `module.exports = ${JSON.stringify(mockedResponses, null, 2)};\n`,
  );
  fs.writeFileSync(
    path.join(outDir, 'manifest.json'),
    `${JSON.stringify(
      {
        generatedAt: new Date().toISOString(),
        nextVersion: internals.nextVersion,
        prefix,
        cssUrls: Object.keys(mockedResponses),
      },
      null,
      2,
    )}\n`,
  );
  console.log(
    `Done: ${Object.keys(mockedResponses).length} CSS response(s), ` +
      `${fontFileCount.total} font file reference(s) -> ${outDir}`,
  );
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
});

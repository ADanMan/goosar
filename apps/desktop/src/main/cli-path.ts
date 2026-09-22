import { lstat, readlink, rm, symlink } from 'fs/promises';
import { basename, dirname, isAbsolute, join, resolve } from 'path';

export type CliPathResult =
  | { state: 'installed'; link: string; target: string }
  | { state: 'already'; link: string; target: string }
  | { state: 'foreign'; link: string; existing: string }
  | { state: 'no-permission'; link: string; target: string; hint: string }
  | { state: 'unsupported'; target: string; hint: string };

export interface InstallCliOnPathOptions {
  target: string;
  linkDir?: string;
  platform?: NodeJS.Platform;
}

const DEFAULT_LINK_DIR = '/usr/local/bin';

function looksLikeOurs(existing: string): boolean {
  return (
    /Goosar\.app\//.test(existing) ||
    /app\.asar\.unpacked\/resources\/bin\/goosar/.test(existing) ||
    /goosar-desktop.*\/resources\/bin\/goosar/.test(existing) ||
    /\/Goosar\/bin\/goosar$/.test(existing)
  );
}

export async function installCliOnPath(opts: InstallCliOnPathOptions): Promise<CliPathResult> {
  const platform = opts.platform ?? process.platform;
  const target = opts.target;
  if (platform === 'win32') {
    return { state: 'unsupported', target, hint: dirname(target) };
  }
  const linkDir = opts.linkDir ?? DEFAULT_LINK_DIR;
  const link = join(linkDir, basename(target));
  const hint = `sudo ln -sf "${target}" "${link}"`;

  const existing = await lstat(link).catch(() => null);
  if (existing) {
    if (!existing.isSymbolicLink()) {
      return { state: 'foreign', link, existing: link };
    }
    const raw = await readlink(link);
    const current = isAbsolute(raw) ? raw : resolve(dirname(link), raw);
    if (current === target) return { state: 'already', link, target };
    const currentExists = await lstat(current).then(
      () => true,
      () => false,
    );
    if (!currentExists || looksLikeOurs(current)) {
      try {
        await rm(link);
      } catch {
        return { state: 'no-permission', link, target, hint };
      }
    } else {
      return { state: 'foreign', link, existing: current };
    }
  }

  try {
    await symlink(target, link);
  } catch {
    return { state: 'no-permission', link, target, hint };
  }
  return { state: 'installed', link, target };
}

export type CliPathLang = 'en' | 'ru';

export function describeCliPathResult(result: CliPathResult, lang: CliPathLang): string {
  const ru = lang === 'ru';
  switch (result.state) {
    case 'installed':
      return ru
        ? `Команда goosar установлена: ${result.link}. Откройте новый терминал.`
        : `The goosar command is installed at ${result.link}. Open a new terminal.`;
    case 'already':
      return ru
        ? `Команда goosar уже установлена: ${result.link}.`
        : `The goosar command is already installed at ${result.link}.`;
    case 'foreign':
      return ru
        ? `По пути ${result.link} уже есть другой goosar (${result.existing}). Он не тронут — удалите его, если хотите использовать версию из приложения.`
        : `Another goosar already lives at ${result.link} (${result.existing}). It was left alone — remove it to use the one bundled with the app.`;
    case 'no-permission':
      return ru
        ? `Нет прав на запись в ${dirname(result.link)}. Выполните в терминале:\n${result.hint}`
        : `No permission to write to ${dirname(result.link)}. Run in a terminal:\n${result.hint}`;
    case 'unsupported':
      return ru
        ? `На Windows добавьте в PATH каталог:\n${result.hint}`
        : `On Windows, add this directory to PATH:\n${result.hint}`;
  }
}

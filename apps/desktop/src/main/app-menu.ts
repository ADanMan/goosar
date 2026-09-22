import { app, Menu } from 'electron';
import type { MenuItemConstructorOptions } from 'electron';

export interface AppMenuLabels {
  help: string;
  debugLogging: string;
  openLogsFolder: string;
  installCli: string;
}

const labelsByLocale: Record<string, AppMenuLabels> = {
  en: {
    help: 'Help',
    debugLogging: 'Debug Logging',
    openLogsFolder: 'Open Logs Folder',
    installCli: 'Install Command Line Tool',
  },
  ru: {
    help: 'Справка',
    debugLogging: 'Отладочный журнал',
    openLogsFolder: 'Открыть папку логов',
    installCli: 'Установить командную строку',
  },
  'zh-Hans': {
    help: '帮助',
    debugLogging: '调试日志',
    openLogsFolder: '打开日志文件夹',
    installCli: '安装命令行工具',
  },
  ja: {
    help: 'ヘルプ',
    debugLogging: 'デバッグログ',
    openLogsFolder: 'ログフォルダを開く',
    installCli: 'コマンドラインツールをインストール',
  },
  ko: {
    help: '도움말',
    debugLogging: '디버그 로그',
    openLogsFolder: '로그 폴더 열기',
    installCli: '명령줄 도구 설치',
  },
};

export function pickAppMenuLabels(preferredLanguage: string | undefined): AppMenuLabels {
  const preferred = preferredLanguage?.toLowerCase() ?? '';
  if (preferred.startsWith('ru')) return labelsByLocale.ru;
  if (preferred.startsWith('zh')) return labelsByLocale['zh-Hans'];
  if (preferred.startsWith('ja')) return labelsByLocale.ja;
  if (preferred.startsWith('ko')) return labelsByLocale.ko;
  return labelsByLocale.en;
}

export interface AppMenuTemplateDeps {
  platform: NodeJS.Platform;
  labels: AppMenuLabels;
  isDebugLoggingEnabled: () => boolean;
  onToggleDebugLogging: (enabled: boolean) => void;
  onOpenLogsFolder: () => void;
  onInstallCli: () => void;
}

export function buildAppMenuTemplate(deps: AppMenuTemplateDeps): MenuItemConstructorOptions[] {
  const helpSubmenu: MenuItemConstructorOptions[] = [
    {
      label: deps.labels.installCli,
      click: () => deps.onInstallCli(),
    },
    {
      label: deps.labels.openLogsFolder,
      click: () => deps.onOpenLogsFolder(),
    },
    { type: 'separator' },
    {
      label: deps.labels.debugLogging,
      type: 'checkbox',
      checked: deps.isDebugLoggingEnabled(),
      click: (menuItem) => deps.onToggleDebugLogging(menuItem.checked === true),
    },
  ];

  return [
    ...(deps.platform === 'darwin'
      ? [{ role: 'appMenu' } satisfies MenuItemConstructorOptions]
      : []),
    { role: 'fileMenu' },
    { role: 'editMenu' },
    { role: 'viewMenu' },
    { role: 'windowMenu' },
    { label: deps.labels.help, role: 'help', submenu: helpSubmenu },
  ];
}

export interface InstallAppMenuDeps {
  isDebugLoggingEnabled: () => boolean;
  setDebugLogging: (enabled: boolean) => Promise<void> | void;
  openLogsFolder: () => void;
  installCli: () => void;
}

export function installAppMenu(deps: InstallAppMenuDeps): () => void {
  const rebuild = (): void => {
    const labels = pickAppMenuLabels(app.getPreferredSystemLanguages()[0]);
    const template = buildAppMenuTemplate({
      platform: process.platform,
      labels,
      isDebugLoggingEnabled: deps.isDebugLoggingEnabled,
      onToggleDebugLogging: (enabled) => {
        void Promise.resolve(deps.setDebugLogging(enabled))
          .catch(() => undefined)
          .finally(rebuild);
      },
      onOpenLogsFolder: deps.openLogsFolder,
      onInstallCli: deps.installCli,
    });
    Menu.setApplicationMenu(Menu.buildFromTemplate(template));
  };
  rebuild();
  return rebuild;
}

import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { MenuItemConstructorOptions } from 'electron';

const ctx = vi.hoisted(() => ({
  builtTemplates: [] as unknown[][],
  setApplicationMenu: vi.fn(),
  preferredLanguagesRef: { current: ['en-US'] as string[] },
}));

vi.mock('electron', () => ({
  Menu: {
    buildFromTemplate: (template: unknown[]) => {
      ctx.builtTemplates.push(template);
      return { template };
    },
    setApplicationMenu: ctx.setApplicationMenu,
  },
  app: {
    getPreferredSystemLanguages: () => ctx.preferredLanguagesRef.current,
  },
}));

import { buildAppMenuTemplate, installAppMenu, pickAppMenuLabels } from './app-menu';

type ClickableItem = MenuItemConstructorOptions & {
  click?: (menuItem: { checked?: boolean }) => void;
};

function helpSubmenu(template: MenuItemConstructorOptions[]): ClickableItem[] {
  const help = template.find((item) => item.role === 'help');
  return (help?.submenu ?? []) as ClickableItem[];
}

function makeDeps(overrides: Partial<Parameters<typeof buildAppMenuTemplate>[0]> = {}) {
  return {
    platform: 'darwin' as NodeJS.Platform,
    labels: pickAppMenuLabels('en-US'),
    isDebugLoggingEnabled: () => false,
    onToggleDebugLogging: vi.fn(),
    onOpenLogsFolder: vi.fn(),
    onInstallCli: vi.fn(),
    ...overrides,
  };
}

beforeEach(() => {
  ctx.builtTemplates.length = 0;
  ctx.setApplicationMenu.mockClear();
  ctx.preferredLanguagesRef.current = ['en-US'];
});

describe('pickAppMenuLabels', () => {
  it('falls back to English', () => {
    expect(pickAppMenuLabels(undefined).debugLogging).toBe('Debug Logging');
    expect(pickAppMenuLabels('fr-FR').openLogsFolder).toBe('Open Logs Folder');
  });

  it('has a Russian label set (T-01 / #564)', () => {
    expect(pickAppMenuLabels('ru-RU').installCli).toBe('Установить командную строку');
    expect(pickAppMenuLabels('ru').openLogsFolder).toBe('Открыть папку логов');
  });

  it('matches zh/ja/ko by prefix like the context menu does', () => {
    expect(pickAppMenuLabels('zh-Hant-TW').openLogsFolder).toBe('打开日志文件夹');
    expect(pickAppMenuLabels('ja-JP').openLogsFolder).toBe('ログフォルダを開く');
    expect(pickAppMenuLabels('ko-KR').openLogsFolder).toBe('로그 폴더 열기');
  });
});

describe('buildAppMenuTemplate', () => {
  it('keeps the default-menu roles, with the app menu only on macOS', () => {
    const darwin = buildAppMenuTemplate(makeDeps());
    expect(darwin.map((item) => item.role)).toEqual([
      'appMenu',
      'fileMenu',
      'editMenu',
      'viewMenu',
      'windowMenu',
      'help',
    ]);

    const linux = buildAppMenuTemplate(makeDeps({ platform: 'linux' }));
    expect(linux.map((item) => item.role)).toEqual([
      'fileMenu',
      'editMenu',
      'viewMenu',
      'windowMenu',
      'help',
    ]);
  });

  it('reflects the current debug state in the checkbox', () => {
    const off = helpSubmenu(buildAppMenuTemplate(makeDeps()));
    const offToggle = off.find((item) => item.type === 'checkbox');
    expect(offToggle?.checked).toBe(false);

    const on = helpSubmenu(buildAppMenuTemplate(makeDeps({ isDebugLoggingEnabled: () => true })));
    const onToggle = on.find((item) => item.type === 'checkbox');
    expect(onToggle?.checked).toBe(true);
  });

  it('forwards the requested checkbox state on click', () => {
    const deps = makeDeps();
    const items = helpSubmenu(buildAppMenuTemplate(deps));
    const toggle = items.find((item) => item.type === 'checkbox');
    toggle?.click?.({ checked: true });
    expect(deps.onToggleDebugLogging).toHaveBeenCalledWith(true);
    toggle?.click?.({ checked: false });
    expect(deps.onToggleDebugLogging).toHaveBeenCalledWith(false);
  });

  it('wires the install-CLI item under Help (T-01 / #564)', () => {
    const deps = makeDeps();
    const items = helpSubmenu(buildAppMenuTemplate(deps));
    const install = items.find((item) => item.label === deps.labels.installCli);
    expect(install).toBeDefined();
    (install as ClickableItem | undefined)?.click?.({});
    expect(deps.onInstallCli).toHaveBeenCalledTimes(1);
  });

  it('wires the open-logs item', () => {
    const deps = makeDeps();
    const items = helpSubmenu(buildAppMenuTemplate(deps));
    const openLogs = items.find((item) => item.label === deps.labels.openLogsFolder);
    (openLogs as ClickableItem | undefined)?.click?.({});
    expect(deps.onOpenLogsFolder).toHaveBeenCalledTimes(1);
  });
});

describe('installAppMenu', () => {
  it('installs the menu and rebuilds it after a toggle settles', async () => {
    let debugEnabled = false;
    const setDebugLogging = vi.fn(async (enabled: boolean) => {
      debugEnabled = enabled;
    });
    installAppMenu({
      isDebugLoggingEnabled: () => debugEnabled,
      setDebugLogging,
      openLogsFolder: vi.fn(),
      installCli: vi.fn(),
    });
    expect(ctx.setApplicationMenu).toHaveBeenCalledTimes(1);

    const firstTemplate = ctx.builtTemplates[0] as MenuItemConstructorOptions[];
    const toggle = helpSubmenu(firstTemplate).find((item) => item.type === 'checkbox');
    expect(toggle?.checked).toBe(false);

    toggle?.click?.({ checked: true });
    expect(setDebugLogging).toHaveBeenCalledWith(true);
    await vi.waitFor(() => {
      expect(ctx.setApplicationMenu).toHaveBeenCalledTimes(2);
    });

    const secondTemplate = ctx.builtTemplates[1] as MenuItemConstructorOptions[];
    const rebuiltToggle = helpSubmenu(secondTemplate).find((item) => item.type === 'checkbox');
    expect(rebuiltToggle?.checked).toBe(true);
  });

  it('rebuilds (reverting the checkbox) even when persisting rejects', async () => {
    installAppMenu({
      isDebugLoggingEnabled: () => false,
      setDebugLogging: vi.fn(() => Promise.reject(new Error('disk full'))),
      openLogsFolder: vi.fn(),
      installCli: vi.fn(),
    });
    const template = ctx.builtTemplates[0] as MenuItemConstructorOptions[];
    const toggle = helpSubmenu(template).find((item) => item.type === 'checkbox');
    toggle?.click?.({ checked: true });
    await vi.waitFor(() => {
      expect(ctx.setApplicationMenu).toHaveBeenCalledTimes(2);
    });
    const rebuilt = ctx.builtTemplates[1] as MenuItemConstructorOptions[];
    const rebuiltToggle = helpSubmenu(rebuilt).find((item) => item.type === 'checkbox');
    expect(rebuiltToggle?.checked).toBe(false);
  });
});

import { describe, expect, it, vi, beforeEach } from 'vitest';

type CapturedMenuItem = {
  label?: string;
  role?: string;
  type?: string;
  click?: () => void;
};
const ctx = vi.hoisted(() => ({
  capturedItems: [] as CapturedMenuItem[][],
  browserWindowFromWebContents: vi.fn(),
  popupSpy: vi.fn(),
  clipboardWriteText: vi.fn(),
  openExternalSpy: vi.fn().mockResolvedValue(undefined),
  preferredLanguagesSpy: vi.fn(() => ['ja-JP']),
}));

vi.mock('electron', () => {
  class MockMenu {
    items: CapturedMenuItem[] = [];
    constructor() {
      ctx.capturedItems.push(this.items);
    }
    append(item: CapturedMenuItem) {
      this.items.push(item);
    }
    popup(opts: unknown) {
      ctx.popupSpy(opts);
    }
  }
  class MockMenuItem {
    label?: string;
    role?: string;
    type?: string;
    click?: () => void;
    constructor(opts: CapturedMenuItem) {
      Object.assign(this, opts);
    }
  }
  return {
    BrowserWindow: { fromWebContents: ctx.browserWindowFromWebContents },
    Menu: MockMenu,
    MenuItem: MockMenuItem,
    app: {
      getPreferredSystemLanguages: ctx.preferredLanguagesSpy,
    },
    clipboard: { writeText: ctx.clipboardWriteText },
    shell: { openExternal: ctx.openExternalSpy },
  };
});

import { installContextMenu, recordRendererUiLocale } from './context-menu';
import type { DesktopUiLocale } from '../shared/ui-locale';

const RU_OPEN = 'Открыть ссылку в браузере';
const RU_COPY = 'Скопировать адрес ссылки';

type ContextMenuParams = {
  selectionText: string;
  isEditable: boolean;
  linkURL: string;
  editFlags: {
    canCut: boolean;
    canCopy: boolean;
    canPaste: boolean;
    canSelectAll: boolean;
  };
};

type Listener = (event: unknown, params: ContextMenuParams) => void;

function makeWebContents() {
  const handlers: Listener[] = [];
  return {
    on(event: string, fn: Listener) {
      if (event === 'context-menu') handlers.push(fn);
    },
    fire(params: ContextMenuParams) {
      for (const h of handlers) h({}, params);
    },
  };
}

const baseEditFlags = {
  canCut: false,
  canCopy: false,
  canPaste: false,
  canSelectAll: false,
};

describe('installContextMenu — link items', () => {
  beforeEach(() => {
    ctx.capturedItems.length = 0;
    ctx.popupSpy.mockClear();
    ctx.clipboardWriteText.mockClear();
    ctx.openExternalSpy.mockClear();
    ctx.browserWindowFromWebContents.mockReset();
    ctx.preferredLanguagesSpy.mockClear();
  });

  it('adds the open-link and copy-address items when right-clicking an http(s) link', () => {
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire({
      ...baseSelection({ linkURL: 'https://goosar.ru/welcome' }),
    });

    const labels = lastMenuLabels();
    expect(labels).toContain(RU_OPEN);
    expect(labels).toContain(RU_COPY);

    invokeByLabel(RU_OPEN);
    expect(ctx.openExternalSpy).toHaveBeenCalledWith('https://goosar.ru/welcome');

    invokeByLabel(RU_COPY);
    expect(ctx.clipboardWriteText).toHaveBeenCalledWith('https://goosar.ru/welcome');
    expect(ctx.popupSpy).toHaveBeenCalledTimes(1);
  });

  it('does NOT add link items when the cursor is over a non-http(s) URL', () => {
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire(baseSelection({ linkURL: 'javascript:alert(1)' }));
    const labels = lastMenuLabelsOrEmpty();
    expect(labels).not.toContain(RU_OPEN);
    expect(labels).not.toContain(RU_COPY);
  });

  it('does NOT add link items when there is no link under the cursor', () => {
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire({
      selectionText: 'hello',
      isEditable: false,
      linkURL: '',
      editFlags: { ...baseEditFlags, canCopy: true },
    });
    const labels = lastMenuLabelsOrEmpty();
    expect(labels).not.toContain(RU_OPEN);
    expect(menuItemRoles()).toContain('copy');
  });
});

describe('installContextMenu — link item copy follows the UI locale', () => {
  beforeEach(() => {
    ctx.capturedItems.length = 0;
    ctx.popupSpy.mockClear();
    ctx.browserWindowFromWebContents.mockReset();
    ctx.preferredLanguagesSpy.mockClear();
  });

  it('uses the product default until a renderer reports its locale', () => {
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire(baseSelection({ linkURL: 'https://goosar.ru' }));

    expect(lastMenuLabels()).toContain(RU_OPEN);
    expect(lastMenuLabels()).toContain(RU_COPY);
  });

  it('never consults the OS language', () => {
    const wc = makeWebContents();
    installContextMenu(wc as never);
    wc.fire(baseSelection({ linkURL: 'https://goosar.ru' }));

    expect(lastMenuLabels()).toContain(RU_OPEN);
    expect(ctx.preferredLanguagesSpy).not.toHaveBeenCalled();
  });

  it('follows the locale the renderer reported, in every shipped language', () => {
    const expected: Record<DesktopUiLocale, string> = {
      en: 'Open Link in Browser',
      'zh-Hans': '在浏览器中打开链接',
      ja: 'ブラウザでリンクを開く',
      ko: '브라우저에서 링크 열기',
      ru: RU_OPEN,
    };
    for (const [locale, openLabel] of Object.entries(expected)) {
      const wc = makeWebContents();
      installContextMenu(wc as never);
      recordRendererUiLocale(wc as never, locale as DesktopUiLocale);
      wc.fire(baseSelection({ linkURL: 'https://goosar.ru' }));

      expect(lastMenuLabels()).toContain(openLabel);
      expect(ctx.preferredLanguagesSpy).not.toHaveBeenCalled();
    }
  });

  it('tracks a later report — the renderer reloads on a language switch', () => {
    const wc = makeWebContents();
    installContextMenu(wc as never);
    recordRendererUiLocale(wc as never, 'en');
    wc.fire(baseSelection({ linkURL: 'https://goosar.ru' }));
    expect(lastMenuLabels()).toContain('Open Link in Browser');

    recordRendererUiLocale(wc as never, 'ru');
    wc.fire(baseSelection({ linkURL: 'https://goosar.ru' }));
    expect(lastMenuLabels()).toContain(RU_OPEN);
  });

  it('keeps windows apart — an issue window has its own renderer', () => {
    const main = makeWebContents();
    const issue = makeWebContents();
    installContextMenu(main as never);
    installContextMenu(issue as never);
    recordRendererUiLocale(main as never, 'en');

    issue.fire(baseSelection({ linkURL: 'https://goosar.ru' }));
    expect(lastMenuLabels()).toContain(RU_OPEN);

    main.fire(baseSelection({ linkURL: 'https://goosar.ru' }));
    expect(lastMenuLabels()).toContain('Open Link in Browser');
  });
});

function baseSelection(over: Partial<ContextMenuParams>): ContextMenuParams {
  return {
    selectionText: '',
    isEditable: false,
    linkURL: '',
    editFlags: { ...baseEditFlags },
    ...over,
  };
}

function lastMenu(): CapturedMenuItem[] {
  const last = ctx.capturedItems[ctx.capturedItems.length - 1];
  if (!last) throw new Error('no menu was constructed');
  return last;
}

function lastMenuLabelsOrEmpty(): string[] {
  const last = ctx.capturedItems[ctx.capturedItems.length - 1] ?? [];
  return last.map((i) => i.label ?? '');
}

function lastMenuLabels(): string[] {
  return lastMenu().map((i) => i.label ?? '');
}

function menuItemRoles(): string[] {
  return lastMenu().map((i) => i.role ?? '');
}

function invokeByLabel(label: string): void {
  const item = lastMenu().find((i) => i.label === label);
  if (!item) throw new Error(`menu item not found: ${label}`);
  item.click?.();
}

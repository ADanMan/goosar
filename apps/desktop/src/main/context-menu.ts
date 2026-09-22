import { BrowserWindow, Menu, MenuItem, clipboard, type WebContents } from 'electron';
import { isSafeExternalHttpUrl, openExternalSafely } from './external-url';
import { DEFAULT_DESKTOP_UI_LOCALE, type DesktopUiLocale } from '../shared/ui-locale';

export function installContextMenu(webContents: WebContents): void {
  webContents.on('context-menu', (_event, params) => {
    const { editFlags, selectionText, isEditable, linkURL } = params;
    const hasSelection = selectionText.trim().length > 0;
    const linkIsHttpUrl = !!linkURL && isSafeExternalHttpUrl(linkURL);
    const labels = pickLabels(webContents);

    const menu = new Menu();

    if (isEditable && editFlags.canCut) {
      menu.append(new MenuItem({ role: 'cut' }));
    }
    if (hasSelection && editFlags.canCopy) {
      menu.append(new MenuItem({ role: 'copy' }));
    }
    if (isEditable && editFlags.canPaste) {
      menu.append(new MenuItem({ role: 'paste' }));
    }
    if (isEditable && editFlags.canSelectAll) {
      if (menu.items.length > 0) {
        menu.append(new MenuItem({ type: 'separator' }));
      }
      menu.append(new MenuItem({ role: 'selectAll' }));
    }

    if (linkIsHttpUrl) {
      if (menu.items.length > 0) {
        menu.append(new MenuItem({ type: 'separator' }));
      }
      menu.append(
        new MenuItem({
          label: labels.openLink,
          click: () => {
            void openExternalSafely(linkURL);
          },
        }),
      );
      menu.append(
        new MenuItem({
          label: labels.copyLinkAddress,
          click: () => {
            clipboard.writeText(linkURL);
          },
        }),
      );
    }

    if (menu.items.length === 0) return;
    const window = BrowserWindow.fromWebContents(webContents) ?? undefined;
    menu.popup({ window });
  });
}

type ContextMenuLabels = {
  openLink: string;
  copyLinkAddress: string;
};

const labelsByLocale: Record<DesktopUiLocale, ContextMenuLabels> = {
  en: {
    openLink: 'Open Link in Browser',
    copyLinkAddress: 'Copy Link Address',
  },
  ru: {
    openLink: 'Открыть ссылку в браузере',
    copyLinkAddress: 'Скопировать адрес ссылки',
  },
  'zh-Hans': {
    openLink: '在浏览器中打开链接',
    copyLinkAddress: '复制链接地址',
  },
  ja: {
    openLink: 'ブラウザでリンクを開く',
    copyLinkAddress: 'リンクのアドレスをコピー',
  },
  ko: {
    openLink: '브라우저에서 링크 열기',
    copyLinkAddress: '링크 주소 복사',
  },
};

const uiLocaleByWebContents = new WeakMap<WebContents, DesktopUiLocale>();

export function recordRendererUiLocale(webContents: WebContents, locale: DesktopUiLocale): void {
  uiLocaleByWebContents.set(webContents, locale);
}

function pickLabels(webContents: WebContents): ContextMenuLabels {
  return labelsByLocale[uiLocaleByWebContents.get(webContents) ?? DEFAULT_DESKTOP_UI_LOCALE];
}

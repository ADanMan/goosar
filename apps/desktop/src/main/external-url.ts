import { shell, type BrowserWindow } from 'electron';

export function isSafeExternalHttpUrl(url: string): boolean {
  return getHttpProtocol(url) !== null;
}

export function openExternalSafely(url: string): Promise<void> | void {
  if (getHttpProtocol(url) === null) {
    console.warn(`[security] blocked openExternal: ${describeScheme(url)}`);
    return;
  }
  return shell.openExternal(url);
}

export function downloadURLSafely(win: BrowserWindow, url: string): void {
  if (getHttpProtocol(url) === null) {
    console.warn(`[security] blocked downloadURL: ${describeScheme(url)}`);
    return;
  }
  win.webContents.downloadURL(url);
}

function getHttpProtocol(url: string): 'http:' | 'https:' | null {
  try {
    const { protocol } = new URL(url);
    if (protocol === 'http:' || protocol === 'https:') return protocol;
    return null;
  } catch {
    return null;
  }
}

function describeScheme(url: string): string {
  try {
    return `scheme=${new URL(url).protocol}`;
  } catch {
    return 'invalid URL';
  }
}

// Открыть URL в браузере по умолчанию на любой платформе; в Electron —
// через IPC-канал shell.openExternal.
export function openExternal(url: string): void {
  if (typeof window === 'undefined') return;
  const desktopAPI = (
    window as unknown as {
      desktopAPI?: { openExternal?: (u: string) => Promise<void> | void };
    }
  ).desktopAPI;
  if (desktopAPI?.openExternal) {
    void desktopAPI.openExternal(url);
    return;
  }
  window.open(url, '_blank', 'noopener,noreferrer');
}

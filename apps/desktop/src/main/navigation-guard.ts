// Защита навигации верхнего уровня в окнах рендерера: только проверка origin.
// Внутренние ссылки обрабатываются иначе — клиентский роутинг не вызывает
// will-navigate.

export type NavigationGuardWindow = {
  webContents: {
    on(
      event: 'will-navigate',
      listener: (event: { preventDefault(): void }, url: string) => void,
    ): unknown;
  };
};

export function isTrustedRendererURL(url: string, trustedURL: string): boolean {
  let target: URL;
  let trusted: URL;
  try {
    target = new URL(url);
    trusted = new URL(trustedURL);
  } catch {
    return false;
  }

  if (trusted.protocol === 'file:') {
    return target.protocol === 'file:' && target.pathname === trusted.pathname;
  }
  return target.protocol !== 'file:' && target.origin === trusted.origin;
}

export function describeBlockedNavigation(url: string): string {
  try {
    const { protocol, host } = new URL(url);
    return host ? `${protocol}//${host}` : protocol;
  } catch {
    return 'invalid URL';
  }
}

export function installNavigationGuard(window: NavigationGuardWindow, trustedURL: string): void {
  window.webContents.on('will-navigate', (event, url) => {
    if (isTrustedRendererURL(url, trustedURL)) return;
    event.preventDefault();
    console.warn(
      `[security] blocked will-navigate to a non-renderer origin: ${describeBlockedNavigation(url)}`,
    );
  });
}

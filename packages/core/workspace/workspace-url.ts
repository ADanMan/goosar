// Брендовый хост, показываемый как префикс URL воркспейса в облаке Goosar.
const BRAND_WORKSPACE_HOST = 'goosar.ru';

export function workspaceUrlHost(daemonAppUrl: string | null | undefined): string {
  const trimmed = daemonAppUrl?.trim();
  if (!trimmed) return BRAND_WORKSPACE_HOST;
  try {
    return new URL(trimmed).host || BRAND_WORKSPACE_HOST;
  } catch {
    const bare = trimmed
      .replace(/^.*?:\/\//, '')
      .replace(/[/?#].*$/, '')
      .trim();
    return bare || BRAND_WORKSPACE_HOST;
  }
}

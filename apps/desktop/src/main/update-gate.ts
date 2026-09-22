// Жёсткий выключатель самообновления десктопа, задаваемый при сборке.

declare const __GOOSAR_DESKTOP_NO_UPDATER__: boolean | undefined;

export function updaterHardDisabled(env: NodeJS.ProcessEnv = process.env): boolean {
  if (
    typeof __GOOSAR_DESKTOP_NO_UPDATER__ !== 'undefined' &&
    __GOOSAR_DESKTOP_NO_UPDATER__ === true
  ) {
    return true;
  }
  const raw = env.GOOSAR_DESKTOP_NO_UPDATER;
  return raw === '1' || raw === 'true';
}

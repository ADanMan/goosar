import type { DownloadAssets } from './parse-release-assets';

const CONFIGURED_ASSET_ENV: ReadonlyArray<[keyof DownloadAssets, string]> = [
  ['macArm64Dmg', 'GOOSAR_MAC_DMG_URL'],
  ['macX64Dmg', 'GOOSAR_MAC_X64_DMG_URL'],
  ['winX64Exe', 'GOOSAR_WIN_X64_EXE_URL'],
  ['linuxAmd64AppImage', 'GOOSAR_LINUX_X64_APPIMAGE_URL'],
  ['linuxArm64AppImage', 'GOOSAR_LINUX_ARM64_APPIMAGE_URL'],
];

export function readConfiguredAssets(
  env: Record<string, string | undefined>,
): Partial<DownloadAssets> {
  const assets: Partial<DownloadAssets> = {};
  for (const [key, name] of CONFIGURED_ASSET_ENV) {
    const value = env[name]?.trim();
    if (value) assets[key] = value;
  }
  return assets;
}

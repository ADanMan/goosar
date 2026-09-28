// Разбор списка ассетов GitHub Releases в карту установщиков для /download;
// вспомогательные файлы и CLI-архивы пропускаются.

export interface GitHubAsset {
  name: string;
  browser_download_url: string;
  size?: number;
}

export interface DownloadAssets {
  macArm64Dmg?: string;
  macArm64Zip?: string;
  macX64Dmg?: string;
  macX64Zip?: string;
  winX64Exe?: string;
  winArm64Exe?: string;
  linuxAmd64AppImage?: string;
  linuxAmd64Deb?: string;
  linuxAmd64Rpm?: string;
  linuxArm64AppImage?: string;
  linuxArm64Deb?: string;
  linuxArm64Rpm?: string;
}

export type DownloadAssetKey = keyof DownloadAssets;

// Byte size per installer, keyed the same way as DownloadAssets, so
// `all-platforms.tsx` can show it next to the matching link. Populated from
// the GitHub Releases API `size` field — never present for manually
// configured URLs (`configured-assets.ts`), which is fine: the size is
// simply omitted for those.
export type DownloadAssetSizes = Partial<Record<DownloadAssetKey, number>>;

const DESKTOP_ARTIFACT_RE =
  /^goosar-desktop-[^-]+-(mac|windows|linux)-([a-z0-9_]+)\.(dmg|zip|exe|AppImage|deb|rpm)$/i;

// The combined checksums file the release job publishes for the Desktop
// installers (see .github/workflows/release.yml, job `desktop-checksums`).
// Matches with or without the .txt extension in case that ever changes.
const CHECKSUMS_ASSET_RE = /^sha256sums(\.txt)?$/i;

function normalizeLinuxArch(arch: string): 'amd64' | 'arm64' | null {
  const a = arch.toLowerCase();
  if (a === 'amd64' || a === 'x86_64') return 'amd64';
  if (a === 'arm64' || a === 'aarch64') return 'arm64';
  return null;
}

function isDesktopArtifactCandidate(name: string): boolean {
  if (name.endsWith('.blockmap') || name.endsWith('.yml')) return false;
  if (name.startsWith('checksums')) return false;
  return true;
}

function resolveDesktopAssetKey(name: string): DownloadAssetKey | null {
  const match = DESKTOP_ARTIFACT_RE.exec(name);
  if (!match) return null;
  const platform = match[1];
  const arch = match[2];
  const ext = match[3];
  if (!platform || !arch || !ext) return null;
  const archLower = arch.toLowerCase();
  const extLower = ext.toLowerCase();

  if (platform === 'mac') {
    if (archLower === 'arm64') {
      if (extLower === 'dmg') return 'macArm64Dmg';
      if (extLower === 'zip') return 'macArm64Zip';
    } else if (archLower === 'x64') {
      if (extLower === 'dmg') return 'macX64Dmg';
      if (extLower === 'zip') return 'macX64Zip';
    }
    return null;
  }
  if (platform === 'windows') {
    if (extLower !== 'exe') return null;
    if (archLower === 'x64') return 'winX64Exe';
    if (archLower === 'arm64') return 'winArm64Exe';
    return null;
  }
  // platform === 'linux'
  const normalized = normalizeLinuxArch(arch);
  if (!normalized) return null;
  if (normalized === 'amd64') {
    if (extLower === 'appimage') return 'linuxAmd64AppImage';
    if (extLower === 'deb') return 'linuxAmd64Deb';
    if (extLower === 'rpm') return 'linuxAmd64Rpm';
  } else {
    if (extLower === 'appimage') return 'linuxArm64AppImage';
    if (extLower === 'deb') return 'linuxArm64Deb';
    if (extLower === 'rpm') return 'linuxArm64Rpm';
  }
  return null;
}

export function parseReleaseAssets(raw: GitHubAsset[]): DownloadAssets {
  const out: DownloadAssets = {};
  for (const asset of raw) {
    if (!isDesktopArtifactCandidate(asset.name)) continue;
    const key = resolveDesktopAssetKey(asset.name);
    if (!key) continue;
    out[key] = asset.browser_download_url;
  }
  return out;
}

export function parseReleaseAssetSizes(raw: GitHubAsset[]): DownloadAssetSizes {
  const out: DownloadAssetSizes = {};
  for (const asset of raw) {
    if (!isDesktopArtifactCandidate(asset.name)) continue;
    const key = resolveDesktopAssetKey(asset.name);
    if (!key) continue;
    if (typeof asset.size === 'number' && asset.size > 0) out[key] = asset.size;
  }
  return out;
}

// URL of the SHA256SUMS asset for the Desktop installers, if the release
// published one.
export function findChecksumsUrl(raw: GitHubAsset[]): string | undefined {
  return raw.find((asset) => CHECKSUMS_ASSET_RE.test(asset.name))?.browser_download_url;
}

export function hasAnyAsset(assets: DownloadAssets): boolean {
  return Object.values(assets).some((v) => typeof v === 'string');
}

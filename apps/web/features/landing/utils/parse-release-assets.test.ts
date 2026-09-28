import { describe, expect, it } from 'vitest';
import {
  findChecksumsUrl,
  parseReleaseAssets,
  parseReleaseAssetSizes,
} from './parse-release-assets';

function asset(name: string, size?: number) {
  return {
    name,
    browser_download_url: `https://github.test/releases/${name}`,
    ...(size !== undefined ? { size } : {}),
  };
}

describe('parseReleaseAssets', () => {
  it('keeps both Apple Silicon and Intel macOS installers', () => {
    const assets = parseReleaseAssets([
      asset('goosar-desktop-0.4.2-mac-arm64.dmg'),
      asset('goosar-desktop-0.4.2-mac-arm64.zip'),
      asset('goosar-desktop-0.4.2-mac-x64.dmg'),
      asset('goosar-desktop-0.4.2-mac-x64.zip'),
      asset('goosar-desktop-0.4.2-mac-x64.dmg.blockmap'),
      asset('latest-x64-mac.yml'),
    ]);

    expect(assets).toEqual({
      macArm64Dmg: 'https://github.test/releases/goosar-desktop-0.4.2-mac-arm64.dmg',
      macArm64Zip: 'https://github.test/releases/goosar-desktop-0.4.2-mac-arm64.zip',
      macX64Dmg: 'https://github.test/releases/goosar-desktop-0.4.2-mac-x64.dmg',
      macX64Zip: 'https://github.test/releases/goosar-desktop-0.4.2-mac-x64.zip',
    });
  });
});

describe('parseReleaseAssetSizes', () => {
  it('maps each desktop installer to its byte size', () => {
    const sizes = parseReleaseAssetSizes([
      asset('goosar-desktop-0.4.2-mac-arm64.dmg', 104_857_600),
      asset('goosar-desktop-0.4.2-linux-amd64.AppImage', 98_765_432),
      asset('goosar-desktop-0.4.2-mac-x64.dmg.blockmap', 12_345),
      asset('latest-x64-mac.yml', 999),
    ]);

    expect(sizes).toEqual({
      macArm64Dmg: 104_857_600,
      linuxAmd64AppImage: 98_765_432,
    });
  });

  it('omits an asset with no size or a zero/negative size', () => {
    const sizes = parseReleaseAssetSizes([
      asset('goosar-desktop-0.4.2-win-x64.exe'),
      asset('goosar-desktop-0.4.2-win-arm64.exe', 0),
    ]);

    expect(sizes).toEqual({});
  });
});

describe('findChecksumsUrl', () => {
  it('finds the SHA256SUMS asset regardless of case', () => {
    const url = findChecksumsUrl([
      asset('goosar-desktop-0.4.2-mac-arm64.dmg'),
      asset('SHA256SUMS.txt'),
    ]);

    expect(url).toBe('https://github.test/releases/SHA256SUMS.txt');
  });

  it('returns undefined when the release publishes no checksums asset', () => {
    const url = findChecksumsUrl([asset('goosar-desktop-0.4.2-mac-arm64.dmg')]);

    expect(url).toBeUndefined();
  });

  it('does not match the CLI checksums.txt from goreleaser', () => {
    const url = findChecksumsUrl([asset('checksums.txt')]);

    expect(url).toBeUndefined();
  });
});

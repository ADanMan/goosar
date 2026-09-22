import { afterEach, describe, expect, it, vi } from 'vitest';
import DownloadPage from './page';
import { DownloadClient } from './download-client';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe('DownloadPage', () => {
  it('renders self-served assets without any GitHub call when the release fallback is off', async () => {
    vi.stubEnv('GOOSAR_DOWNLOAD_GITHUB_RELEASES', 'off');
    vi.stubEnv('GOOSAR_MAC_DMG_URL', 'https://downloads.test/goosar.dmg');
    vi.stubEnv('GOOSAR_WIN_X64_EXE_URL', 'https://downloads.test/goosar.exe');
    vi.stubEnv('GOOSAR_LINUX_X64_APPIMAGE_URL', '  ');
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const element = await DownloadPage();

    expect(fetchMock).not.toHaveBeenCalled();
    expect(element.type).toBe(DownloadClient);
    const props = element.props as {
      release: unknown;
      configuredAssets?: Record<string, string>;
    };
    expect(props.configuredAssets).toEqual({
      macArm64Dmg: 'https://downloads.test/goosar.dmg',
      winX64Exe: 'https://downloads.test/goosar.exe',
    });
    expect(props.release).toEqual({
      version: null,
      publishedAt: null,
      htmlUrl: null,
      assets: {},
    });
  });
});

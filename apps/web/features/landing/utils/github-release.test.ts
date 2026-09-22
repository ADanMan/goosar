import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchLatestRelease } from './github-release';

const SAMPLE_LATEST_ASSET = {
  name: 'goosar-desktop-0.2.14-mac-arm64.dmg',
  browser_download_url:
    'https://github.com/adanman/goosar/releases/download/v0.2.14/goosar-desktop-0.2.14-mac-arm64.dmg',
};

const SAMPLE_PREV_ASSET = {
  name: 'goosar-desktop-0.2.13-mac-arm64.dmg',
  browser_download_url:
    'https://github.com/adanman/goosar/releases/download/v0.2.13/goosar-desktop-0.2.13-mac-arm64.dmg',
};

function releasePayload(overrides: {
  tag: string;
  publishedMinutesAgo?: number;
  asset?: { name: string; browser_download_url: string };
  prerelease?: boolean;
  draft?: boolean;
}) {
  const published = new Date(
    Date.now() - (overrides.publishedMinutesAgo ?? 0) * 60_000,
  ).toISOString();
  return {
    tag_name: overrides.tag,
    published_at: published,
    html_url: `https://github.com/adanman/goosar/releases/tag/${overrides.tag}`,
    prerelease: overrides.prerelease ?? false,
    draft: overrides.draft ?? false,
    assets: overrides.asset ? [overrides.asset] : [],
  };
}

function mockFetchWithReleases(releases: unknown[]) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(releases), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe('fetchLatestRelease', () => {
  it('uses previous release when latest was published within the fresh window', async () => {
    mockFetchWithReleases([
      releasePayload({
        tag: 'v0.2.14',
        publishedMinutesAgo: 10,
        asset: SAMPLE_LATEST_ASSET,
      }),
      releasePayload({
        tag: 'v0.2.13',
        publishedMinutesAgo: 60 * 24,
        asset: SAMPLE_PREV_ASSET,
      }),
    ]);

    const result = await fetchLatestRelease();
    expect(result.version).toBe('v0.2.13');
    expect(result.assets.macArm64Dmg).toBe(SAMPLE_PREV_ASSET.browser_download_url);
  });

  it('uses latest release once it is older than the fresh window', async () => {
    mockFetchWithReleases([
      releasePayload({
        tag: 'v0.2.14',
        publishedMinutesAgo: 120,
        asset: SAMPLE_LATEST_ASSET,
      }),
      releasePayload({
        tag: 'v0.2.13',
        publishedMinutesAgo: 60 * 24,
        asset: SAMPLE_PREV_ASSET,
      }),
    ]);

    const result = await fetchLatestRelease();
    expect(result.version).toBe('v0.2.14');
    expect(result.assets.macArm64Dmg).toBe(SAMPLE_LATEST_ASSET.browser_download_url);
  });

  it('falls back to latest when there is no previous release', async () => {
    mockFetchWithReleases([
      releasePayload({
        tag: 'v0.0.1',
        publishedMinutesAgo: 5,
        asset: SAMPLE_LATEST_ASSET,
      }),
    ]);

    const result = await fetchLatestRelease();
    expect(result.version).toBe('v0.0.1');
  });

  it('skips prereleases and drafts in the candidate list', async () => {
    mockFetchWithReleases([
      releasePayload({
        tag: 'v0.2.15-rc.1',
        publishedMinutesAgo: 30,
        prerelease: true,
      }),
      releasePayload({
        tag: 'v0.2.14',
        publishedMinutesAgo: 120,
        asset: SAMPLE_LATEST_ASSET,
      }),
    ]);

    const result = await fetchLatestRelease();
    expect(result.version).toBe('v0.2.14');
  });

  it('returns an empty release shape when the API errors', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('rate limited', { status: 403 }));
    vi.stubGlobal('fetch', fetchMock);
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});

    const result = await fetchLatestRelease();
    expect(result).toEqual({
      version: null,
      publishedAt: null,
      htmlUrl: null,
      assets: {},
    });
    expect(warnSpy).toHaveBeenCalled();
    warnSpy.mockRestore();
  });

  it('returns an empty release shape when all candidates are filtered out', async () => {
    mockFetchWithReleases([
      releasePayload({ tag: 'v0.2.15-rc.1', prerelease: true }),
      releasePayload({ tag: 'v0.2.14-draft', draft: true }),
    ]);

    const result = await fetchLatestRelease();
    expect(result.version).toBeNull();
    expect(result.assets).toEqual({});
  });

  it('never forwards GITHUB_TOKEN to api.github.com', async () => {
    vi.stubEnv('GITHUB_TOKEN', 'ghp_should-never-leave-the-server');
    const fetchMock = mockFetchWithReleases([
      releasePayload({
        tag: 'v0.2.14',
        publishedMinutesAgo: 120,
        asset: SAMPLE_LATEST_ASSET,
      }),
    ]);

    const result = await fetchLatestRelease();
    expect(result.version).toBe('v0.2.14');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const call = fetchMock.mock.calls[0] as [string, RequestInit | undefined];
    const headers = (call[1]?.headers ?? {}) as Record<string, string>;
    expect(Object.keys(headers).map((h) => h.toLowerCase())).not.toContain('authorization');
    expect(JSON.stringify(call)).not.toContain('ghp_should-never-leave-the-server');
  });

  it('skips the GitHub call entirely when GOOSAR_DOWNLOAD_GITHUB_RELEASES=off', async () => {
    vi.stubEnv('GOOSAR_DOWNLOAD_GITHUB_RELEASES', 'off');
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const result = await fetchLatestRelease();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(result).toEqual({
      version: null,
      publishedAt: null,
      htmlUrl: null,
      assets: {},
    });
  });

  it("treats 'false' and '0' as disabled too", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    vi.stubEnv('GOOSAR_DOWNLOAD_GITHUB_RELEASES', 'FALSE');
    expect((await fetchLatestRelease()).version).toBeNull();
    vi.stubEnv('GOOSAR_DOWNLOAD_GITHUB_RELEASES', '0');
    expect((await fetchLatestRelease()).version).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

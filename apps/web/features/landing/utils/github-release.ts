import { parseReleaseAssets, type DownloadAssets } from './parse-release-assets';

export interface LatestRelease {
  version: string | null;
  publishedAt: string | null;
  htmlUrl: string | null;
  assets: DownloadAssets;
}

const GITHUB_RELEASES_URL = 'https://api.github.com/repos/adanman/goosar/releases?per_page=2';

const REVALIDATE_SECONDS = 300;

const FRESH_RELEASE_WINDOW_MS = 60 * 60 * 1000;

interface GitHubReleasePayload {
  tag_name?: string;
  published_at?: string;
  html_url?: string;
  prerelease?: boolean;
  draft?: boolean;
  assets?: Array<{ name: string; browser_download_url: string }>;
}

function isGithubReleaseFetchDisabled(): boolean {
  const raw = process.env.GOOSAR_DOWNLOAD_GITHUB_RELEASES?.trim().toLowerCase();
  return raw === 'off' || raw === 'false' || raw === '0';
}

export async function fetchLatestRelease(): Promise<LatestRelease> {
  if (isGithubReleaseFetchDisabled()) {
    return emptyRelease();
  }

  try {
    const res = await fetch(GITHUB_RELEASES_URL, {
      next: { revalidate: REVALIDATE_SECONDS },
      headers: {
        Accept: 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28',
      },
    });
    if (!res.ok) {
      throw new Error(`GitHub API responded ${res.status}`);
    }
    const data = (await res.json()) as GitHubReleasePayload[];

    const stable = data.filter((r) => !r.prerelease && !r.draft);
    const latest = stable[0];
    if (!latest) {
      return emptyRelease();
    }
    const previous = stable[1];
    const chosen = previous && isWithinFreshWindow(latest) ? previous : latest;

    return {
      version: chosen.tag_name ?? null,
      publishedAt: chosen.published_at ?? null,
      htmlUrl: chosen.html_url ?? null,
      assets: parseReleaseAssets(chosen.assets ?? []),
    };
  } catch (err) {
    console.warn('[download] fetchLatestRelease failed:', err);
    return emptyRelease();
  }
}

function isWithinFreshWindow(release: GitHubReleasePayload): boolean {
  if (!release.published_at) return false;
  const publishedAt = Date.parse(release.published_at);
  if (Number.isNaN(publishedAt)) return false;
  return Date.now() - publishedAt < FRESH_RELEASE_WINDOW_MS;
}

function emptyRelease(): LatestRelease {
  return {
    version: null,
    publishedAt: null,
    htmlUrl: null,
    assets: {},
  };
}

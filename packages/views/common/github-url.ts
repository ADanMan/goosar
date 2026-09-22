// Для URL GitHub возвращает owner/repo, чтобы усечённые подписи оставались
// читаемыми.
export function githubShortLabel(url: string): string {
  const scp = url.match(/^(?:[^@/]+@)?github\.com:([^/]+)\/([^/]+?)(?:\.git)?$/);
  if (scp) return midTruncate(`${scp[1]}/${scp[2]}`);
  try {
    const u = new URL(url);
    if (u.hostname === 'github.com' || u.hostname === 'www.github.com') {
      const [owner, repo] = u.pathname.split('/').filter(Boolean);
      if (owner && repo) return midTruncate(`${owner}/${repo.replace(/\.git$/, '')}`);
    }
  } catch {
    // not a parseable URL — fall through and return as-is
  }
  return url;
}

export function midTruncate(s: string, maxLen = 40): string {
  if (s.length <= maxLen) return s;
  const tail = Math.floor((maxLen - 1) / 2);
  const head = maxLen - 1 - tail;
  return `${s.slice(0, head)}…${s.slice(-tail)}`;
}

// Разбор переменной GOOSAR_GITHUB_STARS в число звёзд для бейджа в шапке лендинга.
export function parseGithubStarsEnv(raw: string | undefined): number | null {
  const trimmed = raw?.trim();
  if (!trimmed || !/^\d+$/.test(trimmed)) return null;
  const value = Number.parseInt(trimmed, 10);
  return Number.isSafeInteger(value) ? value : null;
}

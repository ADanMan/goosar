// Форматирование прошедших секунд как Ns или Nm Ms; нулевые секунды опускаются.
export function formatElapsedSecs(secs: number): string {
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return s ? `${m}m ${s}s` : `${m}m`;
}

export function formatElapsedMs(ms: number): string {
  return formatElapsedSecs(Math.max(0, Math.round(ms / 1000)));
}

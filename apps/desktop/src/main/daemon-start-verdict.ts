// Вердикт о запуске демона: отличает реальный успех от медленного старта.
const READINESS_TIMEOUT_EXIT_CODE = 2;
const READINESS_TIMEOUT_MARKER = 'did not confirm readiness';

export function isDaemonReadinessTimeout(
  err: { code?: number | string | null } | null | undefined,
  stderr: string | Buffer | undefined,
): boolean {
  if (!err) return false;
  if (err.code !== READINESS_TIMEOUT_EXIT_CODE) return false;
  return String(stderr ?? '').includes(READINESS_TIMEOUT_MARKER);
}

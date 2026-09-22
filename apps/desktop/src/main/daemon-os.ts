// Обнаружение демона, которым десктоп не может управлять: liveness читается по HTTP,
// но запуск/остановка идут через нативный CLI в пространстве процессов хоста
// (например, демон внутри WSL2 на Windows).

export function normalizeHostOS(platform: NodeJS.Platform): string {
  return platform === 'win32' ? 'windows' : platform;
}

export function isDaemonExternallyManaged(daemonOS: string | undefined, hostOS: string): boolean {
  if (typeof daemonOS !== 'string' || daemonOS.length === 0) return false;
  return daemonOS !== hostOS;
}

export async function daemonLifecycleUnreachable(
  readDaemonOS: () => Promise<string | undefined>,
  hostOS: string,
): Promise<boolean> {
  return isDaemonExternallyManaged(await readDaemonOS(), hostOS);
}

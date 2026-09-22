// Чистая логика решения для проверки версии демона. Вынесена отдельно,
// чтобы тестироваться без Electron.

export interface VersionCheckHealth {
  status?: string;
  cli_version?: string;
  active_task_count?: number;
}

export type VersionAction = 'restart' | 'defer' | 'ok' | 'not_running';

export function decideVersionAction(
  bundled: string | null,
  running: VersionCheckHealth | null,
): VersionAction {
  if (!running || running.status !== 'running') return 'not_running';

  const runningVersion = running.cli_version;
  if (!bundled || !runningVersion) return 'ok';
  if (runningVersion === bundled) return 'ok';

  const activeTasks = running.active_task_count ?? 0;
  if (activeTasks > 0) return 'defer';
  return 'restart';
}

export type IdleRestartAction = 'restart' | 'defer' | 'not_running';

export function decideIdleRestartAction(running: VersionCheckHealth | null): IdleRestartAction {
  if (!running || running.status !== 'running') return 'not_running';
  const activeTasks = running.active_task_count ?? 0;
  return activeTasks > 0 ? 'defer' : 'restart';
}

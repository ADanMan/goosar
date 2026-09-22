export type DaemonState =
  | 'running'
  | 'stopped'
  | 'starting'
  | 'stopping'
  | 'installing_cli'
  | 'cli_not_found'
  // The daemon can't start because the server rejected its credentials (the
  // cached PAT expired / was revoked, or the session token is dead). Without
  // this, an auth failure silently sticks at "starting" forever — see #3512.
  | 'auth_expired';

export interface DaemonStatus {
  state: DaemonState;
  pid?: number;
  uptime?: string;
  daemonId?: string;
  deviceName?: string;
  agents?: string[];
  workspaceCount?: number;
  profile?: string;
  serverUrl?: string;
  externallyManaged?: boolean;
}

export interface DaemonPrefs {
  autoStart: boolean;
  autoStop: boolean;
}

export type LocalRuntimeProbe =
  | {
      probeResult: 'success';
      runtimeCount: number;
      providerSummary: Record<string, number>;
      onlineCount: number;
      offlineCount: number;
    }
  | { probeResult: 'error' };

export const DAEMON_STATE_COLORS: Record<DaemonState, string> = {
  running: 'bg-emerald-500',
  stopped: 'bg-muted-foreground/40',
  starting: 'bg-amber-500 animate-pulse',
  stopping: 'bg-amber-500 animate-pulse',
  installing_cli: 'bg-sky-500 animate-pulse',
  cli_not_found: 'bg-red-500',
  auth_expired: 'bg-red-500',
};

export function formatUptime(uptime?: string): string {
  if (!uptime) return '';
  const match = uptime.match(/(?:(\d+)h)?(\d+)m/);
  if (!match) return uptime;
  const h = match[1] ? `${match[1]}h ` : '';
  const m = match[2] ? `${match[2]}m` : '';
  return `${h}${m}`.trim() || uptime;
}

export function daemonStatusAlive(status: string | undefined): boolean {
  return status === 'running' || status === 'starting';
}

export function daemonStateDescription(state: DaemonState, runtimeCount: number): string {
  switch (state) {
    case 'running':
      if (runtimeCount === 0) {
        return 'Running, but no runtimes have registered yet.';
      }
      if (runtimeCount === 1) {
        return 'Running here · 1 runtime available for tasks.';
      }
      return `Running here · ${runtimeCount} runtimes available for tasks.`;
    case 'stopped':
      return "Not running · this device can't take new tasks.";
    case 'starting':
      return 'Starting up the local daemon…';
    case 'stopping':
      return 'Shutting down the local daemon…';
    case 'installing_cli':
      return 'Setting up the runtime for the first time. Only happens once.';
    case 'cli_not_found':
      return "Setup failed · couldn't download the runtime. Check your network.";
    case 'auth_expired':
      return 'Sign-in expired · sign in again to bring this device back online.';
  }
}

export interface DoctorRow {
  id: string;
  name: string;
  status: 'ok' | 'missing' | 'outdated' | 'skipped' | (string & {});
  detail?: string;
  message: string;
  fix: string;
}

export interface DoctorReport {
  ok: boolean;
  results: DoctorRow[];
  checkedAt: number;
}

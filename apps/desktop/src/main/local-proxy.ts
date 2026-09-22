import { spawn as nodeSpawn } from 'child_process';
import { accessSync, constants } from 'fs';
import { delimiter, join } from 'path';
import { localProxyArgs, type LocalProxyPlan } from '../shared/local-proxy';

export interface LocalProxyChild {
  kill: () => void;
  onExit: (callback: () => void) => void;
}

export interface LocalProxyDeps {
  spawn: (executable: string, args: string[]) => LocalProxyChild;
}

export type LocalProxyOutcome =
  | 'started'
  | 'restarted'
  | 'stopped'
  | 'unchanged'
  | 'left_foreign'
  | 'refused'
  | 'unavailable'
  | 'disabled';

function planKey(plan: Extract<LocalProxyPlan, { action: 'run' }>): string {
  return [
    plan.executable,
    `${plan.upstream.host}:${plan.upstream.port}`,
    `${plan.listen.host}:${plan.listen.port}`,
    plan.noProxy.join(','),
  ].join('|');
}

export interface LocalProxySupervisor {
  apply: (plan: LocalProxyPlan) => Promise<LocalProxyOutcome>;
  isOurs: () => boolean;
  stop: () => void;
}

export function createLocalProxySupervisor(deps: LocalProxyDeps): LocalProxySupervisor {
  let child: LocalProxyChild | null = null;
  let key: string | null = null;

  const stop = (): void => {
    if (!child) return;
    child.kill();
    child = null;
    key = null;
  };

  return {
    isOurs: () => child !== null,
    stop,
    apply: async (plan) => {
      switch (plan.action) {
        case 'stop':
          if (!child) return 'unchanged';
          stop();
          return 'stopped';
        case 'refuse':
          stop();
          return 'refused';
        case 'leave_foreign':
          stop();
          return 'left_foreign';
        case 'unavailable':
          return 'unavailable';
        case 'disabled':
          stop();
          return 'disabled';
        case 'run': {
          const nextKey = planKey(plan);
          if (child && key === nextKey) return 'unchanged';
          const replacing = child !== null;
          stop();
          const started = deps.spawn(plan.executable, localProxyArgs(plan));
          started.onExit(() => {
            if (child === started) {
              child = null;
              key = null;
            }
          });
          child = started;
          key = nextKey;
          return replacing ? 'restarted' : 'started';
        }
      }
    },
  };
}

export function localProxyManagementEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
  return env.GOOSAR_MANAGE_LOCAL_PROXY !== '0';
}

const LOCAL_PROXY_NAMES = ['px'] as const;

const EXTRA_LOOKUP_DIRS = ['/opt/homebrew/bin', '/usr/local/bin', '/usr/bin'] as const;

export type RuntimeDirResolver = (packageName: string) => string | null;

let runtimeDirResolver: RuntimeDirResolver = () => null;

export function setLocalProxyRuntimeResolver(fn: RuntimeDirResolver): void {
  runtimeDirResolver = fn;
}

function windowsLookupDirs(env: NodeJS.ProcessEnv): string[] {
  const dirs: string[] = [];
  if (env.LOCALAPPDATA) dirs.push(join(env.LOCALAPPDATA, 'Programs', 'px'));
  if (env['ProgramFiles']) dirs.push(join(env['ProgramFiles'], 'px'));
  return dirs;
}

export function findLocalProxyExecutable(
  env: NodeJS.ProcessEnv = process.env,
  home: string | undefined = env.HOME,
  platform: NodeJS.Platform = process.platform,
): string | null {
  const binName = platform === 'win32' ? 'px.exe' : 'px';

  const provisionedDir = runtimeDirResolver('px');
  if (provisionedDir) {
    const candidate = join(provisionedDir, 'bin', binName);
    try {
      accessSync(candidate, constants.X_OK);
      return candidate;
    } catch {
      // Provisioned but not executable/present; fall through to PATH.
    }
  }

  const dirs = [
    ...(env.PATH ? env.PATH.split(delimiter) : []),
    ...EXTRA_LOOKUP_DIRS,
    ...(home ? [join(home, '.local', 'bin')] : []),
    ...(platform === 'win32' ? windowsLookupDirs(env) : []),
  ];
  for (const dir of dirs) {
    if (!dir) continue;
    for (const name of LOCAL_PROXY_NAMES) {
      const candidate = join(dir, platform === 'win32' ? `${name}.exe` : name);
      try {
        accessSync(candidate, constants.X_OK);
        return candidate;
      } catch {
        // Not here; keep looking.
      }
    }
  }
  return null;
}

export const REAL_LOCAL_PROXY_DEPS: LocalProxyDeps = {
  spawn: (executable, args) => {
    const proc = nodeSpawn(executable, args, {
      stdio: 'ignore',
      detached: false,
    });
    proc.on('error', () => {});
    return {
      kill: () => {
        proc.kill();
      },
      onExit: (callback) => proc.on('exit', callback),
    };
  },
};

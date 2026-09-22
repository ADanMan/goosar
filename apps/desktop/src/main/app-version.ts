import { app } from 'electron';
import { execFileSync } from 'node:child_process';

export function getAppVersion(): string {
  if (app.isPackaged) {
    return app.getVersion();
  }
  try {
    const raw = execFileSync(
      'git',
      ['describe', '--tags', '--match', 'v[0-9]*', '--always', '--dirty'],
      {
        cwd: app.getAppPath(),
        encoding: 'utf-8',
        stdio: ['ignore', 'pipe', 'ignore'],
      },
    ).trim();
    if (!raw) return app.getVersion();
    return raw.replace(/^v/, '');
  } catch {
    return app.getVersion();
  }
}

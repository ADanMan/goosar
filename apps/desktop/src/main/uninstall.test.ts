import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  chmodSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { dirname, join } from 'node:path';

import {
  classifyRuntimeOwnership,
  installedBundlePath,
  performUninstall,
  planUninstall,
  type UninstallContext,
} from './uninstall';
import type { UninstallItem } from '../shared/uninstall-types';

const REAL_PATHS = [
  join(homedir(), '.hermes'),
  join(homedir(), '.goosar'),
  join(homedir(), '.local', 'bin'),
  join(homedir(), 'Library', 'Application Support'),
];

function snapshotRealPaths(): string[] {
  return REAL_PATHS.map((path) => {
    if (!existsSync(path)) return `${path}: absent`;
    try {
      return `${path}: ${readdirSync(path).sort().join('|')}`;
    } catch {
      return `${path}: unreadable`;
    }
  });
}

const REAL_PATHS_BEFORE = snapshotRealPaths();

const canRelyOnPermissions = (process.getuid?.() ?? 0) !== 0;

let home: string;

function ctxFor(overrides: Partial<UninstallContext> = {}): UninstallContext {
  return {
    home,
    env: { HOME: home },
    platform: 'darwin',
    userDataDir: join(home, 'Library', 'Application Support', 'Goosar'),
    executablePath: join(home, 'Applications', 'Goosar.app', 'Contents', 'MacOS', 'Goosar'),
    isPackaged: true,
    stopDaemon: async () => ({ stopped: true }),
    ...overrides,
  };
}

function file(path: string, contents = 'x'): string {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, contents);
  return path;
}

function installEverything(ctx: UninstallContext): void {
  const hermes = join(home, '.hermes');
  file(join(hermes, 'runtime', 'versions', '1.0.0', 'bin', 'hermes'), '#!/bin/sh\n');
  file(join(hermes, 'runtime', 'versions', '1.0.0', 'VERSION'), '1.0.0\n');
  symlinkSync('versions/1.0.0', join(hermes, 'runtime', 'current'));
  mkdirSync(join(hermes, 'runtime', 'bin'), { recursive: true });
  symlinkSync(
    join(hermes, 'runtime', 'current', 'bin', 'hermes'),
    join(hermes, 'runtime', 'bin', 'hermes'),
  );

  file(join(hermes, 'config.user.yaml'), 'llm:\n  api_key: sk-live-secret\n');
  file(join(hermes, 'agents', 'researcher.md'), '# researcher\n');
  file(join(hermes, 'skills', 'writing', 'SKILL.md'), '# writing\n');
  file(join(hermes, 'crons', 'daily.yaml'), 'schedule: 0 9 * * *\n');
  file(join(hermes, '.history', 'session-1.json'), '[]');
  file(join(hermes, '.event', 'queue'), '{}');
  file(join(hermes, '.logs', 'agent.log'), 'boot\n');

  file(join(home, '.goosar', 'desktop.json'), '{}');
  file(join(home, '.goosar', 'desktop_prefs.json'), '{}');
  file(
    join(home, '.goosar', 'profiles', 'desktop-localhost-8080', 'config.json'),
    '{"token":"gsl_secret"}',
  );

  file(join(ctx.userDataDir, 'window-state.json'), '{}');
  file(join(ctx.userDataDir, 'bin', 'goosar'), '#!/bin/sh\n');
}

function installUserOwnedAgent(version = '9.9.9'): void {
  const runtime = join(home, '.hermes', 'runtime');
  file(join(runtime, 'versions', version, 'bin', 'hermes'), '#!/bin/sh\n');
  file(join(runtime, 'versions', version, 'VERSION'), `${version}\n`);
  symlinkSync(join('versions', version), join(runtime, 'current'));
  file(join(home, '.hermes', 'config.user.yaml'), 'llm:\n  api_key: sk-their-own-key\n');
  file(join(home, '.hermes', '.event', 'queue'), '{}');
  file(join(home, '.hermes', '.logs', 'agent.log'), 'boot\n');
  linkUserBinIntoRuntime();
}

function linkUserBinIntoRuntime(): string {
  mkdirSync(join(home, '.local', 'bin'), { recursive: true });
  const bin = join(home, '.local', 'bin', 'hermes');
  symlinkSync(join(home, '.hermes', 'runtime', 'current', 'bin', 'hermes'), bin);
  return bin;
}

function writeForeignUserBin(): string {
  mkdirSync(join(home, '.local', 'bin'), { recursive: true });
  const bin = join(home, '.local', 'bin', 'hermes');
  writeFileSync(bin, '#!/bin/sh\nexec /repo/agent/.venv/bin/hermes "$@"\n');
  chmodSync(bin, 0o755);
  return bin;
}

function paths(items: readonly UninstallItem[]): string[] {
  return items.map((item) => item.path).sort();
}

function ids(items: readonly UninstallItem[]): string[] {
  return items.map((item) => item.id);
}

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'hermes-uninstall-'));
});

afterEach(() => {
  try {
    chmodSync(join(home, '.hermes'), 0o755);
  } catch {
    // Not every test creates it.
  }
  rmSync(home, { recursive: true, force: true });
});

describe('planUninstall', () => {
  it('enumerates the install with a path and a size for every entry', async () => {
    const ctx = ctxFor();
    installEverything(ctx);

    const plan = await planUninstall(ctx);

    expect(ids(plan.software)).toEqual([
      'agent-runtime',
      'agent-events',
      'agent-logs',
      'app-data',
      'goosar-server-address',
      'goosar-daemon-prefs',
      'goosar-profile:desktop-localhost-8080',
    ]);
    expect(ids(plan.userData)).toEqual([
      'agent-config',
      'agent-agents',
      'agent-skills',
      'agent-crons',
      'agent-history',
    ]);
    for (const item of [...plan.software, ...plan.userData]) {
      expect(item.path.startsWith(home)).toBe(true);
      expect(item.label.length).toBeGreaterThan(0);
      expect(item.bytes).toBeGreaterThan(0);
    }
  });

  it('lists nothing that is not on disk', async () => {
    const ctx = ctxFor();
    file(join(home, '.hermes', 'config.user.yaml'), 'llm:\n');

    const plan = await planUninstall(ctx);

    expect(plan.software).toEqual([]);
    expect(ids(plan.userData)).toEqual(['agent-config']);
  });

  it('counts the runtime once, not once per symlink into it', async () => {
    const ctx = ctxFor();
    installEverything(ctx);

    const plan = await planUninstall(ctx);
    const runtime = plan.software.find((item) => item.id === 'agent-runtime');

    const version = join(home, '.hermes', 'runtime', 'versions', '1.0.0');
    const realBytes =
      lstatSync(join(version, 'bin', 'hermes')).size +
      lstatSync(join(version, 'VERSION')).size +
      lstatSync(join(home, '.hermes', 'runtime', 'current')).size +
      lstatSync(join(home, '.hermes', 'runtime', 'bin', 'hermes')).size;
    expect(runtime?.bytes).toBe(realBytes);
  });

  it("claims nothing from an agent the user installed with hermes's own installer", async () => {
    const ctx = ctxFor();
    installUserOwnedAgent();
    file(join(home, '.goosar', 'desktop.json'), '{}');

    const plan = await planUninstall(ctx);

    const theirs = [
      join(home, '.hermes', 'runtime'),
      join(home, '.local', 'bin', 'hermes'),
      join(home, '.hermes', '.event'),
      join(home, '.hermes', '.logs'),
    ];
    const keptPaths = plan.kept.map((entry) => entry.path);
    for (const path of theirs) {
      expect(paths(plan.software)).not.toContain(path);
      expect(keptPaths).toContain(path);
    }
    expect(paths(plan.software)).toEqual([join(home, '.goosar', 'desktop.json')]);
    expect(ids(plan.userData)).toEqual(['agent-config']);
  });

  it('says why it is standing back from a runtime it did not install', async () => {
    const ctx = ctxFor();
    installUserOwnedAgent();

    const plan = await planUninstall(ctx);
    const runtime = plan.kept.find((entry) => entry.path === join(home, '.hermes', 'runtime'));

    expect(runtime?.reason).toMatch(/did not install/i);
    expect(runtime?.reason).toContain(join(home, '.hermes', 'runtime', 'bin'));
  });

  it('claims nothing when a hand-installed runtime happens to match this build', async () => {
    const ctx = ctxFor();
    installUserOwnedAgent('1.0.0');

    const plan = await planUninstall(ctx);

    expect(paths(plan.software)).not.toContain(join(home, '.hermes', 'runtime'));
    expect(plan.kept.map((entry) => entry.path)).toContain(join(home, '.hermes', 'runtime'));
  });

  it('stands back from a runtime both this app and the user have installed into', async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const bin = linkUserBinIntoRuntime();
    const runtime = join(home, '.hermes', 'runtime');

    const plan = await planUninstall(ctx);
    const keptPaths = plan.kept.map((entry) => entry.path);

    expect(paths(plan.software)).not.toContain(runtime);
    expect(paths(plan.software)).not.toContain(bin);
    expect(keptPaths).toContain(runtime);
    expect(keptPaths).toContain(bin);
    expect(plan.kept.find((entry) => entry.path === runtime)?.reason).toMatch(/no way to tell/i);
    expect(ids(plan.software)).toEqual([
      'app-data',
      'goosar-server-address',
      'goosar-daemon-prefs',
      'goosar-profile:desktop-localhost-8080',
    ]);
  });

  it('never claims ~/.local/bin/hermes, whatever it points at', async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const bin = linkUserBinIntoRuntime();

    const plan = await planUninstall(ctx);

    expect(paths(plan.software)).not.toContain(bin);
    expect(plan.kept.find((entry) => entry.path === bin)?.reason).toMatch(/did not install/i);
  });

  it('keeps a foreign ~/.local/bin/hermes and says why', async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const bin = writeForeignUserBin();

    const plan = await planUninstall(ctx);

    expect(paths(plan.software)).not.toContain(bin);
    const kept = plan.kept.find((entry) => entry.path === bin);
    expect(kept?.reason).toMatch(/did not install/i);
  });

  it('keeps goosar CLI profiles the app did not create', async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const own = file(join(home, '.goosar', 'config.json'), '{}');
    const handMade = join(home, '.goosar', 'profiles', 'work');
    file(join(handMade, 'config.json'), '{}');

    const plan = await planUninstall(ctx);
    const keptPaths = plan.kept.map((entry) => entry.path);

    expect(keptPaths).toContain(own);
    expect(keptPaths).toContain(handMade);
    expect(paths(plan.software)).not.toContain(own);
    expect(paths(plan.software)).not.toContain(handMade);
  });

  it('claims no agent service files when this app never installed a runtime', async () => {
    const ctx = ctxFor();
    writeForeignUserBin();
    file(join(home, '.hermes', '.event', 'queue'), '{}');
    file(join(home, '.hermes', '.logs', 'agent.log'), 'boot\n');
    file(join(home, '.hermes', 'config.user.yaml'), 'llm:\n  api_key: theirs\n');
    file(join(home, '.goosar', 'desktop.json'), '{}');

    const plan = await planUninstall(ctx);

    expect(paths(plan.software)).toEqual([join(home, '.goosar', 'desktop.json')]);
    expect(plan.kept.map((entry) => entry.reason).join(' ')).toMatch(/never installed a runtime/i);
    expect(ids(plan.userData)).toEqual(['agent-config']);
  });

  it('names the bundle the app cannot delete for itself', async () => {
    const ctx = ctxFor();
    installEverything(ctx);

    const plan = await planUninstall(ctx);

    expect(plan.manualSteps).toHaveLength(1);
    expect(plan.manualSteps[0].path).toBe(join(home, 'Applications', 'Goosar.app'));
    expect(plan.manualSteps[0].instruction).toMatch(/quit/i);
  });

  it('offers no manual step for an unpackaged dev run', async () => {
    const ctx = ctxFor({ isPackaged: false });
    installEverything(ctx);

    expect((await planUninstall(ctx)).manualSteps).toEqual([]);
  });

  it('leaves the POSIX user bin out of the reckoning on Windows', async () => {
    const ctx = ctxFor({ platform: 'win32' });
    installEverything(ctx);
    const bin = writeForeignUserBin();

    const plan = await planUninstall(ctx);

    expect(paths(plan.software)).not.toContain(bin);
    expect(plan.kept.map((entry) => entry.path)).not.toContain(bin);
  });
});

describe('classifyRuntimeOwnership', () => {
  it('reports none when there is no runtime at all', async () => {
    expect(await classifyRuntimeOwnership(ctxFor())).toBe('none');
  });

  it('reports managed for the layout our --bin override produces', async () => {
    const ctx = ctxFor();
    installEverything(ctx);

    expect(await classifyRuntimeOwnership(ctx)).toBe('managed');
  });

  it("reports foreign for the layout hermes's own installer produces", async () => {
    const ctx = ctxFor();
    installUserOwnedAgent();

    expect(await classifyRuntimeOwnership(ctx)).toBe('foreign');
  });

  it("reports shared when both installers' marks are present", async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    linkUserBinIntoRuntime();

    expect(await classifyRuntimeOwnership(ctx)).toBe('shared');
  });

  it('does not read ~/.local/bin as a claim on Windows', async () => {
    const ctx = ctxFor({ platform: 'win32' });
    installUserOwnedAgent();

    expect(await classifyRuntimeOwnership(ctx)).toBe('foreign');
  });
});

describe('installedBundlePath', () => {
  it('cuts a macOS executable path back to its .app', () => {
    expect(
      installedBundlePath({
        platform: 'darwin',
        isPackaged: true,
        executablePath: '/Applications/Goosar.app/Contents/MacOS/Goosar',
      }),
    ).toBe('/Applications/Goosar.app');
  });

  it('reports nothing while running from a dev checkout', () => {
    expect(
      installedBundlePath({
        platform: 'darwin',
        isPackaged: false,
        executablePath: '/repo/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron',
      }),
    ).toBeNull();
  });
});

describe('performUninstall', () => {
  it("removes exactly what it enumerated, and keeps the user's work", async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const plan = await planUninstall(ctx);

    const outcome = await performUninstall(ctx, { includeUserData: false });

    expect(outcome.status).toBe('removed');
    expect(outcome.failed).toEqual([]);
    expect(paths(outcome.removed)).toEqual(paths(plan.software));
    for (const item of outcome.removed) {
      expect(existsSync(item.path)).toBe(false);
    }
    for (const item of plan.userData) {
      expect(existsSync(item.path)).toBe(true);
    }
  });

  it('keeps the API key on disk unless the user asks for it to go', async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const config = join(home, '.hermes', 'config.user.yaml');

    const outcome = await performUninstall(ctx, { includeUserData: false });

    expect(existsSync(config)).toBe(true);
    const kept = outcome.kept.find((entry) => entry.path === config);
    expect(kept).toBeTruthy();
    expect(outcome.status).toBe('removed');
  });

  it('treats anything that is not an explicit opt-in as keep', async () => {
    const ctx = ctxFor();
    installEverything(ctx);

    const outcome = await performUninstall(ctx, {
      includeUserData: false,
    });

    expect(existsSync(join(home, '.hermes', 'agents', 'researcher.md'))).toBe(true);
    expect(paths(outcome.removed)).not.toContain(join(home, '.hermes', 'agents'));
  });

  it("removes the user's data only when explicitly asked", async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const plan = await planUninstall(ctx);

    const outcome = await performUninstall(ctx, { includeUserData: true });

    expect(outcome.status).toBe('removed');
    expect(paths(outcome.removed)).toEqual(paths([...plan.software, ...plan.userData]));
    expect(existsSync(join(home, '.hermes', 'config.user.yaml'))).toBe(false);
    expect(existsSync(join(home, '.hermes', '.history'))).toBe(false);
    expect(existsSync(join(home, '.hermes'))).toBe(false);
  });

  it('never touches a foreign hermes, even with everything else opted in', async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const bin = writeForeignUserBin();
    const before = lstatSync(bin);

    const outcome = await performUninstall(ctx, { includeUserData: true });

    expect(existsSync(bin)).toBe(true);
    expect(lstatSync(bin).mtimeMs).toBe(before.mtimeMs);
    expect(paths(outcome.removed)).not.toContain(bin);
    expect(outcome.kept.map((entry) => entry.path)).toContain(bin);
  });

  it("leaves a developer's own agent service files where they are", async () => {
    const ctx = ctxFor();
    writeForeignUserBin();
    file(join(home, '.hermes', '.event', 'queue'), '{}');
    file(join(home, '.hermes', '.logs', 'agent.log'), 'boot\n');
    file(join(home, '.goosar', 'desktop.json'), '{}');

    const outcome = await performUninstall(ctx, { includeUserData: true });

    expect(existsSync(join(home, '.hermes', '.event'))).toBe(true);
    expect(existsSync(join(home, '.hermes', '.logs'))).toBe(true);
    expect(paths(outcome.removed)).toEqual([join(home, '.goosar', 'desktop.json')]);
  });

  it('leaves a user-installed agent on disk after a software-only uninstall', async () => {
    const ctx = ctxFor();
    installUserOwnedAgent();
    file(join(home, '.goosar', 'desktop.json'), '{}');
    const bin = join(home, '.local', 'bin', 'hermes');
    const binBefore = lstatSync(bin);

    const outcome = await performUninstall(ctx, { includeUserData: false });

    for (const path of [
      join(home, '.hermes', 'runtime'),
      join(home, '.hermes', 'runtime', 'current'),
      join(home, '.hermes', '.event'),
      join(home, '.hermes', '.logs'),
      join(home, '.hermes', 'config.user.yaml'),
      bin,
    ]) {
      expect(existsSync(path)).toBe(true);
    }
    expect(lstatSync(bin).mtimeMs).toBe(binBefore.mtimeMs);
    expect(paths(outcome.removed)).toEqual([join(home, '.goosar', 'desktop.json')]);
  });

  it('never removes ~/.local/bin/hermes, even with everything opted in', async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const bin = linkUserBinIntoRuntime();

    const outcome = await performUninstall(ctx, { includeUserData: true });

    expect(existsSync(bin)).toBe(true);
    expect(paths(outcome.removed)).not.toContain(bin);
    expect(outcome.kept.map((entry) => entry.path)).toContain(bin);
    expect(existsSync(join(home, '.hermes', 'runtime'))).toBe(true);
  });

  it("leaves the user's own goosar profiles alone", async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    const handMade = file(join(home, '.goosar', 'profiles', 'work', 'config.json'), '{}');

    await performUninstall(ctx, { includeUserData: true });

    expect(existsSync(handMade)).toBe(true);
    expect(existsSync(join(home, '.goosar', 'profiles', 'desktop-localhost-8080'))).toBe(false);
    expect(existsSync(join(home, '.goosar'))).toBe(true);
  });

  it('stops the daemon before it deletes anything', async () => {
    const ctx = ctxFor();
    installEverything(ctx);
    let runtimeAtStopTime: boolean | null = null;

    const outcome = await performUninstall(
      {
        ...ctx,
        stopDaemon: async () => {
          runtimeAtStopTime = existsSync(join(home, '.hermes', 'runtime'));
          return { stopped: true };
        },
      },
      { includeUserData: false },
    );

    expect(runtimeAtStopTime).toBe(true);
    expect(outcome.status).toBe('removed');
  });

  it('removes nothing when the daemon will not stop, and says so', async () => {
    const ctx = ctxFor();
    installEverything(ctx);

    const outcome = await performUninstall(
      {
        ...ctx,
        stopDaemon: async () => ({
          stopped: false,
          detail: 'A daemon is still answering on port 19514 (pid 4242).',
        }),
      },
      { includeUserData: true },
    );

    expect(outcome.status).toBe('blocked');
    expect(outcome.removed).toEqual([]);
    expect(outcome.daemon.detail).toMatch(/pid 4242/);
    expect(existsSync(join(home, '.hermes', 'runtime'))).toBe(true);
    expect(existsSync(ctx.userDataDir)).toBe(true);
    expect(existsSync(join(home, '.hermes', 'config.user.yaml'))).toBe(true);
  });

  it('treats a throwing stop as a refusal, not as permission to delete', async () => {
    const ctx = ctxFor();
    installEverything(ctx);

    const outcome = await performUninstall(
      {
        ...ctx,
        stopDaemon: async () => {
          throw new Error('goosar CLI is not installed');
        },
      },
      { includeUserData: false },
    );

    expect(outcome.status).toBe('blocked');
    expect(outcome.daemon).toMatchObject({ stopped: false });
    expect(outcome.daemon.detail).toMatch(/goosar CLI is not installed/);
    expect(existsSync(join(home, '.hermes', 'runtime'))).toBe(true);
  });

  it.skipIf(!canRelyOnPermissions)(
    'reports a half-finished removal as partial and names what is left',
    async () => {
      const ctx = ctxFor();
      installEverything(ctx);
      chmodSync(join(home, '.hermes'), 0o555);

      const outcome = await performUninstall(ctx, { includeUserData: false });

      chmodSync(join(home, '.hermes'), 0o755);

      expect(outcome.status).toBe('partial');
      const leftBehind = outcome.failed.map((failure) => failure.item.path);
      expect(leftBehind).toContain(join(home, '.hermes', 'runtime'));
      for (const failure of outcome.failed) {
        expect(existsSync(failure.item.path)).toBe(true);
        expect(failure.message.length).toBeGreaterThan(0);
      }
      expect(paths(outcome.removed)).toContain(ctx.userDataDir);
      expect(existsSync(ctx.userDataDir)).toBe(false);
      const plan = paths((await planUninstall(ctx)).software);
      expect([...paths(outcome.removed), ...leftBehind].sort().length).toBeGreaterThanOrEqual(
        plan.length,
      );
    },
  );

  it('reports nothing_to_remove on a machine with no install', async () => {
    const ctx = ctxFor();

    const outcome = await performUninstall(ctx, { includeUserData: true });

    expect(outcome.status).toBe('nothing_to_remove');
    expect(outcome.removed).toEqual([]);
    expect(outcome.failed).toEqual([]);
  });

  it('carries the manual step through to the outcome', async () => {
    const ctx = ctxFor();
    installEverything(ctx);

    const outcome = await performUninstall(ctx, { includeUserData: false });

    expect(outcome.manualSteps[0]?.path).toBe(join(home, 'Applications', 'Goosar.app'));
  });

  it('honours HERMES_HOME instead of assuming ~/.hermes', async () => {
    const elsewhere = join(home, 'custom-agent-home');
    const ctx = ctxFor({ env: { HOME: home, HERMES_HOME: elsewhere } });
    file(join(elsewhere, 'runtime', 'versions', '1.0.0', 'bin', 'hermes'), '#!/bin/sh\n');
    file(join(elsewhere, 'runtime', 'bin', 'hermes'), '#!/bin/sh\n');
    file(join(elsewhere, 'config.user.yaml'), 'llm:\n');

    const outcome = await performUninstall(ctx, { includeUserData: false });

    expect(existsSync(join(elsewhere, 'runtime'))).toBe(false);
    expect(existsSync(join(elsewhere, 'config.user.yaml'))).toBe(true);
    expect(paths(outcome.removed)).toEqual([join(elsewhere, 'runtime')]);
  });
});

describe('test safety', () => {
  it('leaves the real home untouched', () => {
    expect(snapshotRealPaths()).toEqual(REAL_PATHS_BEFORE);
  });
});

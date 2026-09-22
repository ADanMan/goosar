#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { access, readdir, readFile, stat } from 'node:fs/promises';
import { constants } from 'node:fs';
import { isAbsolute, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const DEFAULT_TIMEOUT_MS = 30_000;

const SKILL_MANIFEST_FILE = 'SKILL.md';
const SKILL_DESCRIPTOR_FILE = 'skill.json';

async function exists(path) {
  try {
    await access(path, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

function runToCompletion({ command, args = [], cwd, env, timeoutMs = DEFAULT_TIMEOUT_MS }) {
  return new Promise((resolvePromise) => {
    let child;
    try {
      child = spawn(command, args, {
        cwd,
        env: env ?? process.env,
        stdio: ['ignore', 'pipe', 'pipe'],
        detached: true,
      });
    } catch (err) {
      resolvePromise({
        ok: false,
        error: `failed to spawn ${command}: ${err instanceof Error ? err.message : err}`,
      });
      return;
    }

    let settled = false;
    let childExited = false;
    let stderr = '';

    const killProcessTree = (signal) => {
      if (!child.pid) return;
      try {
        process.kill(-child.pid, signal);
      } catch {
        try {
          child.kill(signal);
        } catch {
          // already gone
        }
      }
    };

    const finish = (result) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      killProcessTree('SIGTERM');
      if (childExited || !child.pid) {
        resolvePromise(result);
        return;
      }
      const killTimer = setTimeout(() => {
        if (!childExited) killProcessTree('SIGKILL');
      }, 500);
      killTimer.unref?.();
      const ceilingTimer = setTimeout(() => {
        if (!childExited) {
          process.stderr.write(
            `[verify-skill] warning: child ${child.pid} did not exit after SIGKILL; resolving without a clean reap\n`,
          );
        }
        resolvePromise(result);
      }, 2500);
      child.once('exit', () => {
        clearTimeout(killTimer);
        clearTimeout(ceilingTimer);
        resolvePromise(result);
      });
    };

    const timer = setTimeout(() => {
      finish({
        ok: false,
        error: `timed out after ${timeoutMs}ms`,
        stderr: stderr.trim() || undefined,
      });
    }, timeoutMs);
    timer.unref?.();

    child.stderr?.on('data', (chunk) => {
      stderr += chunk.toString('utf-8');
    });
    child.on('error', (err) => {
      finish({ ok: false, error: `failed to spawn ${command}: ${err.message}` });
    });
    child.on('exit', (code, signal) => {
      childExited = true;
      finish({ ok: code === 0, code, signal, stderr: stderr.trim() || undefined });
    });
  });
}

export async function readSkillDescriptor(skillDir) {
  const path = join(skillDir, SKILL_DESCRIPTOR_FILE);
  let raw;
  try {
    raw = await readFile(path, 'utf-8');
  } catch {
    return {};
  }
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch (err) {
    throw new Error(`${path} is not valid JSON: ${err instanceof Error ? err.message : err}`);
  }
}

const CHROMIUM_EXECUTABLE_SUBPATHS = {
  darwin: [
    join('chrome-mac', 'Chromium.app', 'Contents', 'MacOS', 'Chromium'),
    join(
      'chrome-mac',
      'Google Chrome for Testing.app',
      'Contents',
      'MacOS',
      'Google Chrome for Testing',
    ),
    join('chrome-headless-shell-mac', 'chrome-headless-shell'),
  ],
  linux: [
    join('chrome-linux', 'chrome'),
    join('chrome-linux', 'headless_shell'),
    join('chrome-headless-shell-linux', 'chrome-headless-shell'),
  ],
  win32: [join('chrome-win', 'chrome.exe'), join('chrome-win', 'headless_shell.exe')],
};

export async function findChromiumExecutable(browsersPath, platform = process.platform) {
  if (!browsersPath) return null;
  let entries;
  try {
    entries = await readdir(browsersPath, { withFileTypes: true });
  } catch {
    return null;
  }
  const subpaths = CHROMIUM_EXECUTABLE_SUBPATHS[platform] ?? CHROMIUM_EXECUTABLE_SUBPATHS.linux;
  const browserDirs = entries
    .filter((e) => e.isDirectory() && e.name.startsWith('chromium'))
    .map((e) => e.name)
    .sort();
  for (const dir of browserDirs) {
    for (const sub of subpaths) {
      const candidate = join(browsersPath, dir, sub);
      try {
        const s = await stat(candidate);
        if (s.isFile()) return candidate;
      } catch {
        // keep scanning
      }
    }
  }
  return null;
}

export async function verifySkill(skillDir, { browsersPath, timeoutMs = DEFAULT_TIMEOUT_MS } = {}) {
  const checks = {};

  const hasSkillMd = await exists(join(skillDir, SKILL_MANIFEST_FILE));
  checks.skillMd = hasSkillMd;
  if (!hasSkillMd) {
    return { healthy: false, checks, error: `missing ${SKILL_MANIFEST_FILE}` };
  }

  let descriptor;
  try {
    descriptor = await readSkillDescriptor(skillDir);
  } catch (err) {
    return { healthy: false, checks, error: err instanceof Error ? err.message : String(err) };
  }

  if (descriptor.verify && typeof descriptor.verify.command === 'string') {
    const rawCommand = descriptor.verify.command;
    const command =
      /[\\/]/.test(rawCommand) && !isAbsolute(rawCommand) ? join(skillDir, rawCommand) : rawCommand;
    const result = await runToCompletion({
      command,
      args: Array.isArray(descriptor.verify.args) ? descriptor.verify.args : [],
      cwd: skillDir,
      env: descriptor.verify.env ? { ...process.env, ...descriptor.verify.env } : process.env,
      timeoutMs,
    });
    checks.script = result.ok ? 'ok' : 'failed';
    if (!result.ok) {
      return {
        healthy: false,
        checks,
        error: `verify command failed: ${result.error ?? `exit ${result.code}`}${
          result.stderr ? ` (${result.stderr})` : ''
        }`,
      };
    }
  }

  if (descriptor.requiresPlaywright === true) {
    if (!browsersPath) {
      checks.playwright = 'no-browsers-path';
      return {
        healthy: false,
        checks,
        error:
          'skill requires Playwright but no shared browsers path was provided ' +
          '(PLAYWRIGHT_BROWSERS_PATH / --browsers)',
      };
    }
    const chromium = await findChromiumExecutable(browsersPath);
    if (!chromium) {
      checks.playwright = 'chromium-not-found';
      return {
        healthy: false,
        checks,
        error: `no Chromium executable found under shared browsers path ${browsersPath}`,
      };
    }
    const launch = await runToCompletion({
      command: chromium,
      args: ['--headless=new', '--no-sandbox', '--version'],
      timeoutMs,
    });
    checks.playwright = launch.ok ? 'ok' : 'launch-failed';
    if (!launch.ok) {
      return {
        healthy: false,
        checks,
        error: `Chromium at ${chromium} did not launch: ${launch.error ?? `exit ${launch.code}`}`,
      };
    }
  }

  return { healthy: true, checks };
}

export async function verifyStagedSkills(stagingDir, options = {}) {
  let entries;
  try {
    entries = await readdir(stagingDir, { withFileTypes: true });
  } catch (err) {
    return [
      {
        name: null,
        healthy: false,
        error: `cannot read staging dir ${stagingDir}: ${err instanceof Error ? err.message : err}`,
      },
    ];
  }
  const reports = [];
  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name.startsWith('.')) continue;
    const result = await verifySkill(join(stagingDir, entry.name), options);
    reports.push({ name: entry.name, ...result });
  }
  return reports;
}

function parseArgv(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i];
    if (flag === '--dir') out.dir = argv[++i];
    else if (flag === '--all') out.all = argv[++i];
    else if (flag === '--browsers') out.browsers = argv[++i];
    else if (flag === '--timeout') out.timeoutMs = Number.parseInt(argv[++i], 10);
  }
  return out;
}

async function main() {
  const opts = parseArgv(process.argv.slice(2));
  const timeoutMs = Number.isFinite(opts.timeoutMs) ? opts.timeoutMs : undefined;
  const browsersPath = opts.browsers ?? process.env.PLAYWRIGHT_BROWSERS_PATH ?? undefined;

  if (opts.all) {
    const reports = await verifyStagedSkills(resolve(opts.all), { browsersPath, timeoutMs });
    console.log(JSON.stringify(reports, null, 2));
    const failed = reports.filter((r) => r.healthy === false);
    process.exit(failed.length === 0 ? 0 : 1);
  }

  if (opts.dir) {
    const result = await verifySkill(resolve(opts.dir), { browsersPath, timeoutMs });
    console.log(JSON.stringify(result, null, 2));
    process.exit(result.healthy ? 0 : 1);
  }

  console.error(
    'usage: verify-skill.mjs (--all <stagingDir> | --dir <skillDir>) [--browsers <playwrightBrowsersRoot>] [--timeout ms]',
  );
  process.exit(2);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { spawnSync } from 'node:child_process';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { findChromiumExecutable, verifySkill, verifyStagedSkills } from './verify-skill.mjs';

let work;

function makeSkill(name, { skillMd = true, descriptor } = {}) {
  const dir = join(work, name);
  mkdirSync(dir, { recursive: true });
  if (skillMd) writeFileSync(join(dir, 'SKILL.md'), `# ${name}\n`);
  if (descriptor) writeFileSync(join(dir, 'skill.json'), JSON.stringify(descriptor));
  return dir;
}

function makeBrowsers() {
  const root = join(work, 'browsers');
  const relBySub = {
    darwin: join('chrome-mac', 'Chromium.app', 'Contents', 'MacOS', 'Chromium'),
    linux: join('chrome-linux', 'chrome'),
  };
  const sub = relBySub[process.platform] ?? relBySub.linux;
  const exe = join(root, 'chromium-1200', sub);
  mkdirSync(join(exe, '..'), { recursive: true });
  writeFileSync(exe, "#!/bin/sh\necho 'Chromium 120.0'\nexit 0\n");
  chmodSync(exe, 0o755);
  return root;
}

function isAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

async function waitUntilDead(pids, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline && pids.some(isAlive)) {
    await new Promise((r) => setTimeout(r, 50));
  }
}

beforeEach(() => {
  work = mkdtempSync(join(tmpdir(), 'verify-skill-'));
});

afterEach(() => {
  rmSync(work, { recursive: true, force: true });
});

describe('verifySkill — structural', () => {
  it('passes a prompt-only skill that has a SKILL.md', async () => {
    const dir = makeSkill('plain');
    const result = await verifySkill(dir);
    expect(result.healthy).toBe(true);
    expect(result.checks.skillMd).toBe(true);
  });

  it('fails a directory with no SKILL.md', async () => {
    const dir = makeSkill('broken', { skillMd: false });
    const result = await verifySkill(dir);
    expect(result.healthy).toBe(false);
    expect(result.error).toMatch(/missing SKILL\.md/);
  });

  it('fails a skill with a malformed skill.json', async () => {
    const dir = makeSkill('bad-json');
    writeFileSync(join(dir, 'skill.json'), '{ not json');
    const result = await verifySkill(dir);
    expect(result.healthy).toBe(false);
    expect(result.error).toMatch(/not valid JSON/);
  });
});

describe('verifySkill — runnable script', () => {
  it('passes when the declared verify command exits 0', async () => {
    const dir = makeSkill('scripted', {
      descriptor: { verify: { command: process.execPath, args: ['-e', 'process.exit(0)'] } },
    });
    const result = await verifySkill(dir, { timeoutMs: 10_000 });
    expect(result.healthy).toBe(true);
    expect(result.checks.script).toBe('ok');
  });

  it('fails when the declared verify command exits non-zero', async () => {
    const dir = makeSkill('scripted-bad', {
      descriptor: { verify: { command: process.execPath, args: ['-e', 'process.exit(3)'] } },
    });
    const result = await verifySkill(dir, { timeoutMs: 10_000 });
    expect(result.healthy).toBe(false);
    expect(result.checks.script).toBe('failed');
    expect(result.error).toMatch(/verify command failed/);
  });

  it('force-kills a verify command that ignores SIGTERM and still returns', async () => {
    const pidFile = join(work, 'skill-pid.txt');
    const dir = makeSkill('hangs', {
      descriptor: {
        verify: {
          command: process.execPath,
          args: [
            '-e',
            "process.on('SIGTERM', () => {}); require('fs').writeFileSync(process.env.PID_FILE, String(process.pid)); setInterval(() => {}, 1000);",
          ],
          env: { PID_FILE: pidFile },
        },
      },
    });
    const result = await verifySkill(dir, { timeoutMs: 500 });
    expect(result.healthy).toBe(false);
    expect(result.checks.script).toBe('failed');
    expect(result.error).toMatch(/timed out/);

    const pid = Number(readFileSync(pidFile, 'utf8').trim());
    await waitUntilDead([pid], 4000);
    expect(isAlive(pid)).toBe(false);
  });

  it("has the standalone CLI's process.exit() wait for the SIGKILL escalation to finish (#167)", () => {
    const cliPath = join(process.cwd(), 'scripts', 'verify-skill.mjs');
    const pidFile = join(work, 'cli-pid.txt');
    const dir = makeSkill('hangs-cli', {
      descriptor: {
        verify: {
          command: 'sh',
          args: ['-c', `trap '' TERM; echo $$ > "${pidFile}"; exec sleep 100`],
        },
      },
    });

    const cli = spawnSync(process.execPath, [cliPath, '--dir', dir, '--timeout', '200'], {
      encoding: 'utf8',
    });

    expect(cli.status).toBe(1);
    const pid = Number(readFileSync(pidFile, 'utf8').trim());
    expect(isAlive(pid)).toBe(false);
  });
});

describe('verifySkill — Playwright/Chromium', () => {
  it('fails a browser skill when no shared browsers path is provided', async () => {
    const dir = makeSkill('slides', { descriptor: { requiresPlaywright: true } });
    const result = await verifySkill(dir);
    expect(result.healthy).toBe(false);
    expect(result.checks.playwright).toBe('no-browsers-path');
    expect(result.error).toMatch(/requires Playwright/);
  });

  it('fails a browser skill when the shared path has no Chromium', async () => {
    const dir = makeSkill('slides', { descriptor: { requiresPlaywright: true } });
    const empty = join(work, 'empty-browsers');
    mkdirSync(empty, { recursive: true });
    const result = await verifySkill(dir, { browsersPath: empty });
    expect(result.healthy).toBe(false);
    expect(result.checks.playwright).toBe('chromium-not-found');
  });

  it('passes a browser skill when Chromium launches from the shared path', async () => {
    const dir = makeSkill('slides', { descriptor: { requiresPlaywright: true } });
    const browsers = makeBrowsers();
    const result = await verifySkill(dir, { browsersPath: browsers, timeoutMs: 10_000 });
    expect(result.healthy).toBe(true);
    expect(result.checks.playwright).toBe('ok');
  });
});

describe('findChromiumExecutable', () => {
  it('finds the host-platform Chromium under a browsers root', async () => {
    const browsers = makeBrowsers();
    const exe = await findChromiumExecutable(browsers);
    expect(exe).toBeTruthy();
    expect(exe).toContain('chromium-1200');
  });

  it('returns null for a browsers root with no chromium dir', async () => {
    const empty = join(work, 'none');
    mkdirSync(empty, { recursive: true });
    await expect(findChromiumExecutable(empty)).resolves.toBeNull();
  });
});

describe('verifyStagedSkills', () => {
  it('reports one result per staged skill, valid and malformed', async () => {
    const staging = join(work, 'staging');
    mkdirSync(staging, { recursive: true });
    mkdirSync(join(staging, 'good'), { recursive: true });
    writeFileSync(join(staging, 'good', 'SKILL.md'), '# good\n');
    mkdirSync(join(staging, 'bad'), { recursive: true }); 

    const reports = await verifyStagedSkills(staging);
    const byName = Object.fromEntries(reports.map((r) => [r.name, r]));
    expect(byName.good.healthy).toBe(true);
    expect(byName.bad.healthy).toBe(false);
  });
});

#!/usr/bin/env node

import { spawnSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { envWithLocalBins } from './package.mjs';
import { applyWorktreeDevEnv, repoRootFromScriptDir } from './worktree-dev-env.mjs';

const here = dirname(fileURLToPath(import.meta.url));

applyWorktreeDevEnv(process.env, {
  root: repoRootFromScriptDir(here),
  log: true,
});

function run(command, args, { shell = false, env = process.env } = {}) {
  const result = spawnSync(command, args, {
    stdio: 'inherit',
    env,
    shell,
  });
  if (result.error) {
    console.error(`[dev:desktop] failed to run ${command}: ${result.error.message}`);
    process.exit(1);
  }
  if (result.status !== 0) process.exit(result.status ?? 1);
}

const node = process.execPath;
run(node, [join(here, 'bundle-cli.mjs')]);
run(node, [join(here, 'bundle-agent.mjs')]);
run(node, [join(here, 'brand-dev-electron.mjs')]);

const isWin = process.platform === 'win32';
run('electron-vite', ['dev', ...process.argv.slice(2)], {
  shell: isWin,
  env: envWithLocalBins(process.env),
});

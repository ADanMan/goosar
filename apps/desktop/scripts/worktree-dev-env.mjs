// Изоляция dev-окружения для pnpm dev:desktop по worktree.

import { statSync } from 'node:fs';
import { basename, join } from 'node:path';

const RENDERER_PORT_BASE = 5174;
const OFFSET_MODULO = 1000;

function cksumTable() {
  const table = new Uint32Array(256);
  const POLY = 0x04c11db7;
  for (let i = 0; i < 256; i++) {
    let crc = i << 24;
    for (let bit = 0; bit < 8; bit++) {
      crc = crc & 0x80000000 ? (crc << 1) ^ POLY : crc << 1;
    }
    table[i] = crc >>> 0;
  }
  return table;
}

const TABLE = cksumTable();

export function cksum(buf) {
  let crc = 0;
  for (const byte of buf) {
    crc = (((crc << 8) >>> 0) ^ TABLE[((crc >>> 24) ^ byte) & 0xff]) >>> 0;
  }
  let len = buf.length;
  while (len > 0) {
    crc = (((crc << 8) >>> 0) ^ TABLE[((crc >>> 24) ^ (len & 0xff)) & 0xff]) >>> 0;
    len = Math.floor(len / 256);
  }
  return ~crc >>> 0;
}

export function offsetForPath(path) {
  return cksum(Buffer.from(path)) % OFFSET_MODULO;
}

export function rendererPortForPath(path) {
  return RENDERER_PORT_BASE + offsetForPath(path);
}

export function appSuffixForPath(path) {
  const slug =
    basename(path)
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '') || 'worktree';
  return `${slug}-${offsetForPath(path)}`;
}

export function isLinkedWorktree(root) {
  try {
    return statSync(join(root, '.git')).isFile();
  } catch {
    return false;
  }
}

export function repoRootFromScriptDir(scriptDir) {
  return join(scriptDir, '..', '..', '..');
}

export function applyWorktreeDevEnv(env, { root, log = false } = {}) {
  const hasPort = Boolean(env.DESKTOP_RENDERER_PORT);
  const hasSuffix = Boolean(env.DESKTOP_APP_SUFFIX);
  if (hasPort && hasSuffix) return env; 
  if (!isLinkedWorktree(root)) return env; 

  if (!hasPort) env.DESKTOP_RENDERER_PORT = String(rendererPortForPath(root));
  if (!hasSuffix) env.DESKTOP_APP_SUFFIX = appSuffixForPath(root);

  if (log) {
    console.log(
      `[dev:desktop] worktree isolation → renderer port ${env.DESKTOP_RENDERER_PORT}, ` +
        `app "Goosar Canary ${env.DESKTOP_APP_SUFFIX}"`,
    );
  }
  return env;
}

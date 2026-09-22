import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { resolveServerLaunch, verifyMcpServer, verifyStagedServers } from './verify-mcp-server.mjs';

const HEALTHY_SERVER = `
process.stdin.setEncoding("utf8");
let buf = "";
const send = (o) => process.stdout.write(JSON.stringify(o) + "\\n");
process.stdin.on("data", (chunk) => {
  buf += chunk;
  let i;
  while ((i = buf.indexOf("\\n")) >= 0) {
    const line = buf.slice(0, i).trim();
    buf = buf.slice(i + 1);
    if (!line) continue;
    const msg = JSON.parse(line);
    if (msg.method === "initialize") {
      send({ jsonrpc: "2.0", id: msg.id, result: { protocolVersion: "2025-06-18", serverInfo: { name: "fake", version: "1.0" }, capabilities: { tools: {} } } });
    } else if (msg.method === "tools/list") {
      send({ jsonrpc: "2.0", id: msg.id, result: { tools: [{ name: "ping" }, { name: "pong" }] } });
    }
  }
});
`;

const CRASHING_SERVER = `process.stderr.write("boom\\n"); process.exit(1);`;

const STUBBORN_SERVER = `
import { writeFileSync } from "node:fs";
import { spawn } from "node:child_process";
const grandchild = spawn(
  process.execPath,
  ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000);"],
  { stdio: "ignore" },
);
writeFileSync(process.env.PID_FILE, process.pid + "\\n" + grandchild.pid + "\\n");
process.on("SIGTERM", () => {}); // swallow SIGTERM — only SIGKILL stops us
setInterval(() => {}, 1000); // stay alive, never complete the handshake
`;

const REJECTING_SERVER = `
process.stdin.setEncoding("utf8");
let buf = "";
const send = (o) => process.stdout.write(JSON.stringify(o) + "\\n");
process.stdin.on("data", (chunk) => {
  buf += chunk;
  let i;
  while ((i = buf.indexOf("\\n")) >= 0) {
    const line = buf.slice(0, i).trim();
    buf = buf.slice(i + 1);
    if (!line) continue;
    const msg = JSON.parse(line);
    if (msg.id != null) send({ jsonrpc: "2.0", id: msg.id, error: { code: -32000, message: "nope" } });
  }
});
`;

let work;

function writeServer(name, body) {
  const path = join(work, name);
  writeFileSync(path, body);
  return path;
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
  work = mkdtempSync(join(tmpdir(), 'verify-mcp-'));
});

afterEach(() => {
  rmSync(work, { recursive: true, force: true });
});

describe('verifyMcpServer', () => {
  it('reports a server healthy and lists its tools', async () => {
    const server = writeServer('healthy.mjs', HEALTHY_SERVER);
    const result = await verifyMcpServer({
      command: process.execPath,
      args: [server],
      timeoutMs: 10_000,
    });
    expect(result.healthy).toBe(true);
    expect(result.protocolVersion).toBe('2025-06-18');
    expect(result.serverInfo?.name).toBe('fake');
    expect(result.tools).toEqual(['ping', 'pong']);
  });

  it('reports a crashing server unhealthy', async () => {
    const server = writeServer('crash.mjs', CRASHING_SERVER);
    const result = await verifyMcpServer({
      command: process.execPath,
      args: [server],
      timeoutMs: 10_000,
    });
    expect(result.healthy).toBe(false);
    expect(result.error).toBeTruthy();
  });

  it('reports a server that rejects initialize unhealthy', async () => {
    const server = writeServer('reject.mjs', REJECTING_SERVER);
    const result = await verifyMcpServer({
      command: process.execPath,
      args: [server],
      timeoutMs: 10_000,
    });
    expect(result.healthy).toBe(false);
    expect(result.error).toMatch(/initialize failed/);
  });

  it('reports an unspawnable command unhealthy instead of throwing', async () => {
    const result = await verifyMcpServer({
      command: join(work, 'no-such-binary'),
      timeoutMs: 5_000,
    });
    expect(result.healthy).toBe(false);
    expect(result.error).toMatch(/spawn/);
  });

  it('times out a server that never answers', async () => {
    const server = writeServer('silent.mjs', 'setInterval(() => {}, 1000);');
    const result = await verifyMcpServer({
      command: process.execPath,
      args: [server],
      timeoutMs: 500,
    });
    expect(result.healthy).toBe(false);
    expect(result.error).toMatch(/timed out/);
  });

  it('force-kills a SIGTERM-ignoring server and its descendants, and still returns', async () => {
    const pidFile = join(work, 'pids.txt');
    const server = writeServer('stubborn.mjs', STUBBORN_SERVER);
    const result = await verifyMcpServer({
      command: process.execPath,
      args: [server],
      env: { ...process.env, PID_FILE: pidFile },
      timeoutMs: 500,
    });
    expect(result.healthy).toBe(false);
    expect(result.error).toMatch(/timed out/);

    const [pid, grandPid] = readFileSync(pidFile, 'utf8').trim().split('\n').map(Number);
    await waitUntilDead([pid, grandPid], 4000);
    expect(isAlive(pid)).toBe(false);
    expect(isAlive(grandPid)).toBe(false);
  });

  it("has the standalone CLI's process.exit() wait for the SIGKILL escalation to finish", () => {
    const cliPath = join(process.cwd(), 'scripts', 'verify-mcp-server.mjs');
    const pidFile = join(work, 'cli-pid.txt');
    const scriptBody = `trap '' TERM; echo $$ > "${pidFile}"; exec sleep 100`;

    const cli = spawnSync(
      process.execPath,
      [cliPath, '--command', 'sh', '--arg', '-c', '--arg', scriptBody, '--timeout', '200'],
      { encoding: 'utf8' },
    );

    expect(cli.status).toBe(1);
    const pid = Number(readFileSync(pidFile, 'utf8').trim());
    expect(isAlive(pid)).toBe(false);
  });
});

describe('resolveServerLaunch', () => {
  it('reads a launch descriptor and resolves a relative command against the dir', async () => {
    const dir = join(work, 'srv');
    mkdirSync(dir, { recursive: true });
    writeFileSync(
      join(dir, 'mcp-server.json'),
      JSON.stringify({ command: 'bin/run', args: ['--stdio'] }),
    );
    const launch = await resolveServerLaunch(dir);
    expect(launch.command).toBe(join(dir, 'bin', 'run'));
    expect(launch.args).toEqual(['--stdio']);
    expect(launch.cwd).toBe(dir);
  });

  it('returns null when the dir has no descriptor', async () => {
    const dir = join(work, 'bare');
    mkdirSync(dir, { recursive: true });
    await expect(resolveServerLaunch(dir)).resolves.toBeNull();
  });
});

describe('verifyStagedServers', () => {
  it('verifies each staged server that carries a descriptor', async () => {
    const staging = join(work, 'staging');
    const okDir = join(staging, 'ok');
    mkdirSync(okDir, { recursive: true });
    const okServer = join(okDir, 'server.mjs');
    writeFileSync(okServer, HEALTHY_SERVER);
    writeFileSync(
      join(okDir, 'mcp-server.json'),
      JSON.stringify({ command: process.execPath, args: [okServer] }),
    );
    mkdirSync(join(staging, 'no-descriptor'), { recursive: true });

    const reports = await verifyStagedServers(staging, { timeoutMs: 10_000 });
    const byName = Object.fromEntries(reports.map((r) => [r.name, r]));
    expect(byName.ok.healthy).toBe(true);
    expect(byName.ok.tools).toEqual(['ping', 'pong']);
    expect(byName['no-descriptor'].skipped).toBe(true);
  });
});

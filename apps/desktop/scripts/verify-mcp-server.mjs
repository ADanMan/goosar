#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { readFile, readdir } from 'node:fs/promises';
import { join, isAbsolute, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const DEFAULT_TIMEOUT_MS = 30_000;

const CLIENT_PROTOCOL_VERSION = '2025-06-18';

export function verifyMcpServer({ command, args = [], cwd, env, timeoutMs = DEFAULT_TIMEOUT_MS }) {
  return new Promise((resolvePromise) => {
    let child;
    try {
      child = spawn(command, args, {
        cwd,
        env: env ?? process.env,
        stdio: ['pipe', 'pipe', 'pipe'],
        detached: true,
      });
    } catch (err) {
      resolvePromise({
        healthy: false,
        error: `failed to spawn ${command}: ${err instanceof Error ? err.message : err}`,
      });
      return;
    }

    let settled = false;
    let childExited = false;
    let stdout = '';
    let stderr = '';
    const pending = new Map(); 
    let nextId = 1;

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
      try {
        child.stdin.end();
      } catch {
        // stdin may already be closed.
      }
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
            `[verify-mcp-server] warning: child ${child.pid} did not exit after SIGKILL; resolving without a clean reap\n`,
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
        healthy: false,
        error: `timed out after ${timeoutMs}ms waiting for the MCP handshake`,
        stderr: stderr.trim() || undefined,
      });
    }, timeoutMs);
    timer.unref?.();

    const send = (message) => {
      try {
        child.stdin.write(`${JSON.stringify(message)}\n`);
      } catch (err) {
        finish({
          healthy: false,
          error: `failed to write to server stdin: ${err instanceof Error ? err.message : err}`,
        });
      }
    };

    const request = (method, params) =>
      new Promise((resolveRequest) => {
        const id = nextId++;
        pending.set(id, resolveRequest);
        send({ jsonrpc: '2.0', id, method, params });
      });

    const notify = (method, params) => send({ jsonrpc: '2.0', method, params });

    child.stdout.on('data', (chunk) => {
      stdout += chunk.toString('utf-8');
      let newlineIndex;
      while ((newlineIndex = stdout.indexOf('\n')) >= 0) {
        const line = stdout.slice(0, newlineIndex).trim();
        stdout = stdout.slice(newlineIndex + 1);
        if (!line) continue;
        let message;
        try {
          message = JSON.parse(line);
        } catch {
          continue;
        }
        if (message && message.id != null && pending.has(message.id)) {
          const resolver = pending.get(message.id);
          pending.delete(message.id);
          resolver(message);
        }
      }
    });

    child.stderr.on('data', (chunk) => {
      stderr += chunk.toString('utf-8');
    });

    child.on('error', (err) => {
      finish({
        healthy: false,
        error: `failed to spawn ${command}: ${err.message}`,
        stderr: stderr.trim() || undefined,
      });
    });

    child.on('exit', (code, signal) => {
      childExited = true;
      if (settled) return;
      finish({
        healthy: false,
        error: `server exited before completing the handshake (code=${code}, signal=${signal})`,
        stderr: stderr.trim() || undefined,
      });
    });

    (async () => {
      const initResponse = await request('initialize', {
        protocolVersion: CLIENT_PROTOCOL_VERSION,
        capabilities: {},
        clientInfo: { name: 'hermes-mcp-verify', version: '1.0.0' },
      });
      if (settled) return;
      if (initResponse.error) {
        finish({
          healthy: false,
          error: `initialize failed: ${JSON.stringify(initResponse.error)}`,
          stderr: stderr.trim() || undefined,
        });
        return;
      }
      const initResult = initResponse.result ?? {};

      notify('notifications/initialized', {});

      const toolsResponse = await request('tools/list', {});
      if (settled) return;
      if (toolsResponse.error) {
        finish({
          healthy: false,
          protocolVersion: initResult.protocolVersion,
          serverInfo: initResult.serverInfo,
          error: `tools/list failed: ${JSON.stringify(toolsResponse.error)}`,
          stderr: stderr.trim() || undefined,
        });
        return;
      }
      const tools = toolsResponse.result?.tools;
      if (!Array.isArray(tools)) {
        finish({
          healthy: false,
          protocolVersion: initResult.protocolVersion,
          serverInfo: initResult.serverInfo,
          error: 'tools/list did not return a tools array',
          stderr: stderr.trim() || undefined,
        });
        return;
      }
      finish({
        healthy: true,
        protocolVersion: initResult.protocolVersion,
        serverInfo: initResult.serverInfo,
        tools: tools.map((t) => t?.name).filter((n) => typeof n === 'string'),
      });
    })();
  });
}

const LAUNCH_DESCRIPTORS = ['mcp-server.json', 'launch.json'];

export async function resolveServerLaunch(serverDir) {
  for (const name of LAUNCH_DESCRIPTORS) {
    let raw;
    try {
      raw = await readFile(join(serverDir, name), 'utf-8');
    } catch {
      continue;
    }
    let descriptor;
    try {
      descriptor = JSON.parse(raw);
    } catch (err) {
      throw new Error(
        `${join(serverDir, name)} is not valid JSON: ${err instanceof Error ? err.message : err}`,
      );
    }
    if (!descriptor || typeof descriptor.command !== 'string') {
      throw new Error(`${join(serverDir, name)} must set a string "command"`);
    }
    const command =
      /[\\/]/.test(descriptor.command) && !isAbsolute(descriptor.command)
        ? join(serverDir, descriptor.command)
        : descriptor.command;
    return {
      command,
      args: Array.isArray(descriptor.args) ? descriptor.args : [],
      env: descriptor.env ? { ...process.env, ...descriptor.env } : process.env,
      cwd: serverDir,
    };
  }
  return null;
}

export async function verifyStagedServers(stagingDir, options = {}) {
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
    const serverDir = join(stagingDir, entry.name);
    let launch;
    try {
      launch = await resolveServerLaunch(serverDir);
    } catch (err) {
      reports.push({
        name: entry.name,
        healthy: false,
        error: err instanceof Error ? err.message : String(err),
      });
      continue;
    }
    if (!launch) {
      reports.push({ name: entry.name, skipped: true, reason: 'no launch descriptor' });
      continue;
    }
    const result = await verifyMcpServer({ ...launch, ...options });
    reports.push({ name: entry.name, ...result });
  }
  return reports;
}

function parseArgv(argv) {
  const out = { args: [] };
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i];
    if (flag === '--dir') out.dir = argv[++i];
    else if (flag === '--all') out.all = argv[++i];
    else if (flag === '--command') out.command = argv[++i];
    else if (flag === '--arg') out.args.push(argv[++i]);
    else if (flag === '--cwd') out.cwd = argv[++i];
    else if (flag === '--timeout') out.timeoutMs = Number.parseInt(argv[++i], 10);
  }
  return out;
}

async function main() {
  const opts = parseArgv(process.argv.slice(2));
  const timeoutMs = Number.isFinite(opts.timeoutMs) ? opts.timeoutMs : undefined;

  if (opts.all) {
    const reports = await verifyStagedServers(resolve(opts.all), { timeoutMs });
    console.log(JSON.stringify(reports, null, 2));
    const failed = reports.filter((r) => r.healthy === false);
    process.exit(failed.length === 0 ? 0 : 1);
  }

  let launch;
  if (opts.dir) {
    launch = await resolveServerLaunch(resolve(opts.dir));
    if (!launch) {
      console.error(`no launch descriptor (${LAUNCH_DESCRIPTORS.join(' / ')}) in ${opts.dir}`);
      process.exit(1);
    }
  } else if (opts.command) {
    launch = {
      command: opts.command,
      args: opts.args,
      cwd: opts.cwd,
      env: process.env,
    };
  } else {
    console.error(
      'usage: verify-mcp-server.mjs (--all <stagingDir> | --dir <serverDir> | --command <cmd> [--arg a ...] [--cwd d]) [--timeout ms]',
    );
    process.exit(2);
  }

  const result = await verifyMcpServer({ ...launch, timeoutMs });
  console.log(JSON.stringify(result, null, 2));
  process.exit(result.healthy ? 0 : 1);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}

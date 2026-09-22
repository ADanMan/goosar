import { describe, it, expect, vi } from 'vitest';
import { checkMcpThroughPx, type McpPxCheckChild, type McpPxCheckDeps } from './mcp-px-check';

function fakeChild(): McpPxCheckChild & {
  emitStdout: (chunk: string) => void;
  emitStderr: (chunk: string) => void;
  emitExit: () => void;
  emitError: (err: Error) => void;
  writes: string[];
  killed: boolean;
} {
  const stdoutCbs: Array<(chunk: string) => void> = [];
  const stderrCbs: Array<(chunk: string) => void> = [];
  const exitCbs: Array<(code: number | null) => void> = [];
  const errorCbs: Array<(err: Error) => void> = [];
  const writes: string[] = [];
  return {
    stdin: { write: (data) => writes.push(data) },
    onStdout: (cb) => stdoutCbs.push(cb),
    onStderr: (cb) => stderrCbs.push(cb),
    onExit: (cb) => exitCbs.push(cb),
    onError: (cb) => errorCbs.push(cb),
    kill: vi.fn(),
    writes,
    killed: false,
    emitStdout: (chunk) => stdoutCbs.forEach((cb) => cb(chunk)),
    emitStderr: (chunk) => stderrCbs.forEach((cb) => cb(chunk)),
    emitExit: () => exitCbs.forEach((cb) => cb(0)),
    emitError: (err) => errorCbs.forEach((cb) => cb(err)),
  };
}

function depsFor(child: McpPxCheckChild): McpPxCheckDeps {
  return { spawn: vi.fn(() => child) };
}

describe('checkMcpThroughPx', () => {
  it('resolves ok on a valid initialize response', async () => {
    const child = fakeChild();
    const promise = checkMcpThroughPx('fake-mcp', [], depsFor(child), {});
    child.emitStdout(
      JSON.stringify({ jsonrpc: '2.0', id: 1, result: { capabilities: {} } }) + '\n',
    );
    await expect(promise).resolves.toBe('ok');
  });

  it('sends the initialize request over stdin', () => {
    const child = fakeChild();
    void checkMcpThroughPx('fake-mcp', [], depsFor(child), {});
    expect(child.writes).toHaveLength(1);
    const sent = JSON.parse(child.writes[0]);
    expect(sent).toMatchObject({
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
    });
  });

  it('spawns with HTTPS_PROXY and HTTP_PROXY pointed at the local px port', () => {
    const child = fakeChild();
    const deps = depsFor(child);
    void checkMcpThroughPx('fake-mcp', ['--stdio'], deps, { FOO: 'bar' });
    expect(deps.spawn).toHaveBeenCalledWith('fake-mcp', ['--stdio'], {
      FOO: 'bar',
      HTTPS_PROXY: 'http://127.0.0.1:3128',
      HTTP_PROXY: 'http://127.0.0.1:3128',
    });
  });

  it('ignores non-JSON-RPC stdout noise before the real response', async () => {
    const child = fakeChild();
    const promise = checkMcpThroughPx('fake-mcp', [], depsFor(child), {});
    child.emitStdout('Loading server...\n');
    child.emitStdout(JSON.stringify({ jsonrpc: '2.0', id: 1, result: {} }) + '\n');
    await expect(promise).resolves.toBe('ok');
  });

  it('resolves mcp_failed(<last stderr line>) when the process exits without answering', async () => {
    const child = fakeChild();
    const promise = checkMcpThroughPx('fake-mcp', [], depsFor(child), {});
    child.emitStderr('Traceback (most recent call last):\n');
    child.emitStderr("ModuleNotFoundError: No module named 'mcp'\n");
    child.emitExit();
    await expect(promise).resolves.toBe("mcp_failed(ModuleNotFoundError: No module named 'mcp')");
  });

  it('resolves proxy_unreachable when stderr shows a proxy connection failure', async () => {
    const child = fakeChild();
    const promise = checkMcpThroughPx('fake-mcp', [], depsFor(child), {});
    child.emitStderr('requests.exceptions.ProxyError: Connection refused\n');
    child.emitExit();
    await expect(promise).resolves.toBe('proxy_unreachable');
  });

  it('resolves proxy_unreachable when the child fails to spawn with an ECONNREFUSED-style error', async () => {
    const child = fakeChild();
    const promise = checkMcpThroughPx('fake-mcp', [], depsFor(child), {});
    child.emitError(new Error('connect ECONNREFUSED 127.0.0.1:3128'));
    await expect(promise).resolves.toBe('proxy_unreachable');
  });

  it('resolves mcp_failed on a plain spawn error unrelated to the proxy', async () => {
    const child = fakeChild();
    const promise = checkMcpThroughPx('fake-mcp', [], depsFor(child), {});
    child.emitError(new Error('spawn fake-mcp ENOENT'));
    await expect(promise).resolves.toBe('mcp_failed(spawn fake-mcp ENOENT)');
  });

  it('times out after the given timeout and kills the child', async () => {
    const child = fakeChild();
    const promise = checkMcpThroughPx('fake-mcp', [], depsFor(child), {}, 25);
    await expect(promise).resolves.toBe('mcp_failed(server did not respond within 20s)');
    expect(child.kill).toHaveBeenCalled();
  });

  it('never responds twice: exit after a resolved response is a no-op', async () => {
    const child = fakeChild();
    const promise = checkMcpThroughPx('fake-mcp', [], depsFor(child), {});
    child.emitStdout(JSON.stringify({ jsonrpc: '2.0', id: 1, result: {} }) + '\n');
    child.emitStderr('some late noise\n');
    child.emitExit();
    await expect(promise).resolves.toBe('ok');
  });
});

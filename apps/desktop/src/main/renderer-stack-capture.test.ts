import { describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import {
  ALLOWED_CDP_METHODS,
  captureHangStack,
  coolDebuggerChannel,
  sendDebuggerCommand,
  warmDebuggerChannel,
  type CdpDebugger,
} from './renderer-stack-capture';

type MessageListener = (event: unknown, method: string, params: Record<string, unknown>) => void;

function makeDebugger(
  options: {
    attached?: boolean;
    pausedFrames?: unknown[] | null;
    failOn?: string;
  } = {},
) {
  const listeners = new Set<MessageListener>();
  const sent: string[] = [];
  let attached = options.attached ?? true;

  const attach = vi.fn(() => {
    if (options.failOn === 'attach') throw new Error('boom: attach');
    attached = true;
  });
  const detach = vi.fn(() => {
    if (options.failOn === 'detach') throw new Error('boom: detach');
    attached = false;
  });

  const dbg: CdpDebugger = {
    isAttached: () => attached,
    attach,
    detach,
    sendCommand: vi.fn(async (method: string) => {
      sent.push(method);
      if (options.failOn === method) throw new Error(`boom: ${method}`);
      if (method === 'Debugger.pause' && options.pausedFrames) {
        for (const listener of listeners) {
          listener({}, 'Debugger.paused', { callFrames: options.pausedFrames });
        }
      }
      return {};
    }),
    on: (_event, listener) => listeners.add(listener as MessageListener),
    off: (_event, listener) => listeners.delete(listener as MessageListener),
  };

  return {
    dbg,
    sent,
    attach,
    detach,
    isAttached: () => attached,
    listenerCount: () => listeners.size,
  };
}

const frame = {
  functionName: 'parseMarkdownChunked',
  url: 'file:///Applications/Goosar.app/Contents/Resources/app.asar/out/renderer/assets/index-abc.js',
  location: { lineNumber: 412, columnNumber: 17 },
  scopeChain: [{ type: 'local', object: { objectId: '{secret}' } }],
  this: { objectId: '{secret}' },
};

describe('CDP allowlist', () => {
  it.each(ALLOWED_CDP_METHODS)('permits %s', async (method) => {
    const { dbg, sent } = makeDebugger();
    await sendDebuggerCommand(dbg, method);
    expect(sent).toEqual([method]);
  });

  it.each([
    'Runtime.evaluate',
    'Runtime.getProperties',
    'Runtime.callFunctionOn',
    'HeapProfiler.takeHeapSnapshot',
    'Debugger.setBreakpointByUrl',
    'Debugger.getScriptSource',
  ])('refuses %s and never reaches the renderer', async (method) => {
    const { dbg, sent } = makeDebugger();
    await expect(sendDebuggerCommand(dbg, method)).rejects.toThrow(/Forbidden CDP method/);
    expect(sent).toEqual([]);
  });

  it('routes every command through sendDebuggerCommand', () => {
    const source = readFileSync(join(__dirname, 'renderer-stack-capture.ts'), 'utf8');
    const directCalls = source.match(/\bdbg\.sendCommand\(/g) ?? [];
    expect(directCalls).toHaveLength(1);
  });
});

describe('captureHangStack', () => {
  it('returns code locations from the paused renderer', async () => {
    const { dbg } = makeDebugger({ pausedFrames: [frame] });

    const stack = await captureHangStack(dbg);

    expect(stack).toEqual([
      {
        functionName: 'parseMarkdownChunked',
        url: 'assets/index-abc.js',
        lineNumber: 412,
        columnNumber: 17,
      },
    ]);
  });

  it('never carries scope handles that could be dereferenced into user data', async () => {
    const { dbg } = makeDebugger({ pausedFrames: [frame] });

    const stack = await captureHangStack(dbg);

    expect(JSON.stringify(stack)).not.toContain('secret');
    expect(stack?.[0]).not.toHaveProperty('scopeChain');
    expect(stack?.[0]).not.toHaveProperty('this');
  });

  it("always resumes — a pause we can't clear would make the hang permanent", async () => {
    const { dbg, sent } = makeDebugger({ pausedFrames: [frame] });

    await captureHangStack(dbg);

    expect(sent).toEqual(['Debugger.pause', 'Debugger.resume']);
  });

  it('resumes even when the renderer never answers the pause', async () => {
    const { dbg, sent } = makeDebugger({ pausedFrames: null });

    const stack = await captureHangStack(dbg, { timeoutMs: 10 });

    expect(stack).toBeNull();
    expect(sent).toContain('Debugger.resume');
  });

  it('resumes even when the pause command itself throws', async () => {
    const { dbg, sent } = makeDebugger({ failOn: 'Debugger.pause' });

    const stack = await captureHangStack(dbg, { timeoutMs: 10 });

    expect(stack).toBeNull();
    expect(sent).toContain('Debugger.resume');
  });

  it('does not leak a message listener per capture', async () => {
    const { dbg, listenerCount } = makeDebugger({ pausedFrames: [frame] });

    await captureHangStack(dbg);
    await captureHangStack(dbg);

    expect(listenerCount()).toBe(0);
  });

  it('returns null when the channel was never warmed', async () => {
    const { dbg, sent } = makeDebugger({ attached: false });

    expect(await captureHangStack(dbg)).toBeNull();
    expect(sent).toEqual([]);
  });

  it('keeps the top of the stack when it is deeper than the cap', async () => {
    const deep = Array.from({ length: 50 }, (_, i) => ({
      ...frame,
      functionName: `fn${i}`,
    }));
    const { dbg } = makeDebugger({ pausedFrames: deep });

    const stack = await captureHangStack(dbg, { maxFrames: 3 });

    expect(stack?.map((f) => f.functionName)).toEqual(['fn0', 'fn1', 'fn2']);
  });
});

describe('warmDebuggerChannel', () => {
  it('attaches and enables when the renderer is healthy', async () => {
    const { dbg, sent } = makeDebugger({ attached: false });

    expect(await warmDebuggerChannel(dbg)).toBe(true);
    expect(sent).toEqual(['Debugger.enable']);
  });

  it('reports failure instead of throwing when another client owns the debugger', async () => {
    const { dbg } = makeDebugger({ failOn: 'Debugger.enable' });

    expect(await warmDebuggerChannel(dbg)).toBe(false);
  });
});

describe('the channel always closes', () => {
  it('detaches even when Debugger.disable throws', async () => {
    const { dbg, detach, isAttached } = makeDebugger({ failOn: 'Debugger.disable' });

    await coolDebuggerChannel(dbg);

    expect(detach).toHaveBeenCalledTimes(1);
    expect(isAttached()).toBe(false);
  });

  it('detaches on the normal path too', async () => {
    const { dbg, sent, detach, isAttached } = makeDebugger();

    await coolDebuggerChannel(dbg);

    expect(sent).toEqual(['Debugger.disable']);
    expect(detach).toHaveBeenCalledTimes(1);
    expect(isAttached()).toBe(false);
  });

  it('does nothing when the channel was never open', async () => {
    const { dbg, detach, sent } = makeDebugger({ attached: false });

    await coolDebuggerChannel(dbg);

    expect(sent).toEqual([]);
    expect(detach).not.toHaveBeenCalled();
  });

  it('survives a detach that itself throws', async () => {
    const { dbg } = makeDebugger({ failOn: 'detach' });

    await expect(coolDebuggerChannel(dbg)).resolves.toBeUndefined();
  });

  it('rolls the attach back when Debugger.enable throws', async () => {
    const { dbg, attach, detach, isAttached } = makeDebugger({
      attached: false,
      failOn: 'Debugger.enable',
    });

    expect(await warmDebuggerChannel(dbg)).toBe(false);

    expect(attach).toHaveBeenCalledTimes(1);
    expect(detach).toHaveBeenCalledTimes(1);
    expect(isAttached()).toBe(false);
  });

  it('leaves an already-attached channel alone when enable throws', async () => {
    const { dbg, detach } = makeDebugger({ attached: true, failOn: 'Debugger.enable' });

    expect(await warmDebuggerChannel(dbg)).toBe(false);

    expect(detach).not.toHaveBeenCalled();
  });

  it('reports failure without throwing when attach itself throws', async () => {
    const { dbg, detach } = makeDebugger({ attached: false, failOn: 'attach' });

    expect(await warmDebuggerChannel(dbg)).toBe(false);
    expect(detach).not.toHaveBeenCalled();
  });
});

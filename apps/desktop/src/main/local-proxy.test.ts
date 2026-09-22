import { describe, it, expect, vi, afterEach } from 'vitest';
import { mkdtempSync, mkdirSync, writeFileSync, chmodSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import {
  createLocalProxySupervisor,
  findLocalProxyExecutable,
  setLocalProxyRuntimeResolver,
} from './local-proxy';
import type { LocalProxyPlan } from '../shared/local-proxy';

function runPlan(over: Partial<Extract<LocalProxyPlan, { action: 'run' }>> = {}) {
  return {
    action: 'run' as const,
    listen: { host: '127.0.0.1', port: 3128 },
    upstream: { host: 'isa.corp', port: 8080 },
    executable: '/opt/px',
    noProxy: ['localhost'],
    ...over,
  };
}

function harness() {
  const events: string[] = [];
  const kills: string[] = [];
  const spawned: Array<{ executable: string; args: string[] }> = [];
  let seq = 0;
  const spawn = vi.fn((executable: string, args: string[]) => {
    spawned.push({ executable, args });
    const id = `child-${++seq}`;
    events.push(`spawn:${id}`);
    return {
      id,
      kill: () => {
        kills.push(id);
        events.push(`kill:${id}`);
      },
      onExit: () => {},
    };
  });
  return {
    events,
    spawned,
    kills,
    supervisor: createLocalProxySupervisor({ spawn }),
  };
}

describe('local proxy supervisor', () => {
  it('starts the planned proxy with the planned command line', async () => {
    const h = harness();
    expect(await h.supervisor.apply(runPlan())).toBe('started');
    expect(h.spawned).toEqual([
      {
        executable: '/opt/px',
        args: ['--proxy=isa.corp:8080', '--listen=127.0.0.1', '--port=3128', '--noproxy=localhost'],
      },
    ]);
  });

  it('does not respawn while the plan is unchanged', async () => {
    const h = harness();
    await h.supervisor.apply(runPlan());
    expect(await h.supervisor.apply(runPlan())).toBe('unchanged');
    expect(h.spawned).toHaveLength(1);
  });

  it('stops the old child before starting one on a new upstream', async () => {
    const h = harness();
    await h.supervisor.apply(runPlan());
    expect(
      await h.supervisor.apply(runPlan({ upstream: { host: 'other.corp', port: 3128 } })),
    ).toBe('restarted');
    expect(h.events).toEqual(['spawn:child-1', 'kill:child-1', 'spawn:child-2']);
    expect(h.spawned).toHaveLength(2);
  });

  it('stops our child when the route stops resolving', async () => {
    const h = harness();
    await h.supervisor.apply(runPlan());
    expect(await h.supervisor.apply({ action: 'stop', reason: 'route_unknown' })).toBe('stopped');
    expect(h.kills).toEqual(['child-1']);
  });

  it('never kills a process it did not start', async () => {
    const h = harness();
    expect(await h.supervisor.apply({ action: 'stop', reason: 'route_direct' })).toBe('unchanged');
    expect(
      await h.supervisor.apply({
        action: 'leave_foreign',
        reason: 'foreign_proxy_works',
      }),
    ).toBe('left_foreign');
    expect(h.kills).toEqual([]);
    expect(h.spawned).toEqual([]);
  });

  it('spawns nothing for a refused or unavailable plan', async () => {
    const h = harness();
    expect(
      await h.supervisor.apply({
        action: 'refuse',
        reason: 'listen_not_loopback',
      }),
    ).toBe('refused');
    expect(
      await h.supervisor.apply({
        action: 'unavailable',
        reason: 'executable_missing',
      }),
    ).toBe('unavailable');
    expect(h.spawned).toEqual([]);
  });
});

describe('findLocalProxyExecutable', () => {
  const tmpDirs: string[] = [];
  afterEach(() => {
    setLocalProxyRuntimeResolver(() => null);
    for (const dir of tmpDirs.splice(0)) rmSync(dir, { recursive: true, force: true });
  });

  function makeExecutable(dir: string, name: string): string {
    mkdirSync(dir, { recursive: true });
    const full = join(dir, name);
    writeFileSync(full, '#!/bin/sh\n');
    chmodSync(full, 0o755);
    return full;
  }

  function tmp(): string {
    const dir = mkdtempSync(join(tmpdir(), 'local-proxy-test-'));
    tmpDirs.push(dir);
    return dir;
  }

  it("returns the provisioned runtime's px even when PATH has a different px", () => {
    const provisionedRoot = tmp();
    const provisionedPx = makeExecutable(join(provisionedRoot, 'bin'), 'px');
    const pathDir = tmp();
    makeExecutable(pathDir, 'px');
    setLocalProxyRuntimeResolver((name) => (name === 'px' ? provisionedRoot : null));

    const found = findLocalProxyExecutable({ PATH: pathDir }, undefined, 'darwin');
    expect(found).toBe(provisionedPx);
  });

  it('falls back to PATH when no provisioning package is installed', () => {
    setLocalProxyRuntimeResolver(() => null);
    const pathDir = tmp();
    const pathPx = makeExecutable(pathDir, 'px');

    const found = findLocalProxyExecutable({ PATH: pathDir }, undefined, 'darwin');
    expect(found).toBe(pathPx);
  });

  it('returns null when px is neither provisioned nor on PATH', () => {
    setLocalProxyRuntimeResolver(() => null);
    const emptyDir = tmp();

    const found = findLocalProxyExecutable({ PATH: emptyDir }, undefined, 'darwin');
    expect(found).toBeNull();
  });

  it('on win32, checks px.exe under the provisioned bin dir', () => {
    const provisionedRoot = tmp();
    const provisionedPx = makeExecutable(join(provisionedRoot, 'bin'), 'px.exe');
    setLocalProxyRuntimeResolver((name) => (name === 'px' ? provisionedRoot : null));

    const found = findLocalProxyExecutable({ PATH: '' }, undefined, 'win32');
    expect(found).toBe(provisionedPx);
  });

  it('on win32, falls back to the standard LOCALAPPDATA/ProgramFiles px locations', () => {
    setLocalProxyRuntimeResolver(() => null);
    const localAppData = tmp();
    const winPx = makeExecutable(join(localAppData, 'Programs', 'px'), 'px.exe');

    const found = findLocalProxyExecutable(
      { PATH: '', LOCALAPPDATA: localAppData },
      undefined,
      'win32',
    );
    expect(found).toBe(winPx);
  });
});

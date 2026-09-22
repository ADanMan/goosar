import { chmodSync, mkdirSync, writeFileSync } from 'fs';
import { mkdtempSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import { afterEach, describe, expect, it } from 'vitest';

import { probeInstalledMcpClient, readAgentRuntimeStatus } from './agent-bootstrap';
import {} from '../shared/agent-runtime-types';

const homes: string[] = [];

function newHome(): string {
  const home = mkdtempSync(join(tmpdir(), 'mcp-check-'));
  homes.push(home);
  return home;
}

afterEach(() => {
  homes.length = 0;
});

function installRuntimeWithProbeAnswer(home: string, answer: string): void {
  const current = join(home, '.hermes', 'runtime', 'current');
  mkdirSync(join(current, 'venv', 'bin'), { recursive: true });
  mkdirSync(join(current, 'bin'), { recursive: true });
  writeFileSync(join(current, 'VERSION'), '0.14.0\n');
  writeFileSync(join(current, 'bin', 'hermes'), '#!/bin/sh\nexit 0\n');
  chmodSync(join(current, 'bin', 'hermes'), 0o755);
  const python = join(current, 'venv', 'bin', 'python');
  writeFileSync(python, `#!/bin/sh\ncat <<'EOF'\n${answer}\nEOF\n`);
  chmodSync(python, 0o755);
}

function ctxFor(home: string) {
  return { home, env: {} as NodeJS.ProcessEnv };
}

describe('post-install MCP client check (#278)', () => {
  it('reports the incompatible runtime that started this, instead of staying silent', async () => {
    const home = newHome();
    installRuntimeWithProbeAnswer(
      home,
      JSON.stringify({
        state: 'incompatible',
        version: '2.0.0',
        tool_fields: ['input_schema', 'name'],
      }),
    );

    const mcp = await probeInstalledMcpClient(ctxFor(home));
    expect(mcp.state).toBe('incompatible');
    if (mcp.state === 'incompatible') {
      expect(mcp.version).toBe('2.0.0');
      expect(mcp.reason).toBeTruthy();
    }
  });

  it('does not claim a working integration when the contract holds', async () => {
    const home = newHome();
    installRuntimeWithProbeAnswer(
      home,
      JSON.stringify({ state: 'ok', version: '1.9.0', tool_fields: ['inputSchema'] }),
    );

    const mcp = await probeInstalledMcpClient(ctxFor(home));
    expect(mcp.state).toBe('ok');
    // The probe checked a CONTRACT, not a server. Inventing "integrations
    // working" here is the same class of lie as the silence it replaces — the
    // copy layer says nothing extra for this state (agent-runtime-copy.test.ts).
  });

  it('a runtime that cannot be asked is unknown, never ok', async () => {
    const home = newHome();
    const current = join(home, '.hermes', 'runtime', 'current');
    mkdirSync(join(current, 'bin'), { recursive: true });
    writeFileSync(join(current, 'VERSION'), '0.14.0\n');

    const mcp = await probeInstalledMcpClient(ctxFor(home));
    expect(mcp.state).toBe('unknown');
    if (mcp.state === 'unknown') expect(mcp.detail).toBeTruthy();
  });

  it('a probe answering nonsense is unknown, never ok', async () => {
    const home = newHome();
    installRuntimeWithProbeAnswer(home, 'not json at all');

    const mcp = await probeInstalledMcpClient(ctxFor(home));
    expect(mcp.state).toBe('unknown');
  });

  it('says a runtime with no MCP SDK has no integrations at all', async () => {
    const home = newHome();
    installRuntimeWithProbeAnswer(
      home,
      JSON.stringify({ state: 'absent', detail: "No module named 'mcp'" }),
    );

    const mcp = await probeInstalledMcpClient(ctxFor(home));
    expect(mcp.state).toBe('absent');
  });

  it('surfaces the verdict on the runtime status a person actually reads', async () => {
    const home = newHome();
    installRuntimeWithProbeAnswer(
      home,
      JSON.stringify({
        state: 'incompatible',
        version: '2.0.0',
        tool_fields: ['input_schema'],
      }),
    );

    const status = await readAgentRuntimeStatus({
      ...ctxFor(home),
      bundledArtifactDir: null,
      platform: 'darwin',
    });
    expect(
      status.state === 'needs_config' || status.state === 'ready'
        ? status.mcpClient?.state
        : undefined,
    ).toBe('incompatible');
  });
});

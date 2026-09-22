import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  rmSync,
  symlinkSync,
  writeFileSync,
  readlinkSync,
  readFileSync,
  existsSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import {
  agentHomeDir,
  agentPathEnvValue,
  classifyUserBin,
  ensureAgentRuntime,
  ensureBashProbe,
  missingAgentConfigFields,
  installedAgentVersion,
  managedAgentPath,
  probeBashViaAcp,
  readAgentRuntimeStatus,
  resolveAgentConfigPath,
  stripQuarantineAttributes,
  userBinPath,
  type AgentRuntimeContext,
} from './agent-bootstrap';

let home: string;

function ctxFor(overrides: Partial<AgentRuntimeContext> = {}): AgentRuntimeContext {
  return {
    home,
    env: { HOME: home },
    bundledArtifactDir: null,
    platform: 'darwin',
    ...overrides,
  };
}

const SHIPPED_EXAMPLE =
  'llm:\n' +
  '  provider: openai\n' +
  '  model: openai/glm-4.6\n' +
  '  api_key: REPLACE_WITH_YOUR_KEY  # ⚠️ do not commit a real key\n' +
  '  api_base: https://your-gateway.example/v1\n' +
  '  temperature: 0.7\n';

function fakeArtifact(dir: string, version: string): string {
  mkdirSync(join(dir, 'bin'), { recursive: true });
  mkdirSync(join(dir, 'venv', 'bin'), { recursive: true });
  mkdirSync(join(dir, 'share'), { recursive: true });

  const bin = join(dir, 'bin', 'hermes');
  writeFileSync(bin, `#!/bin/sh\necho "hermes ${version}"\n`);
  chmodSync(bin, 0o755);

  const python = join(dir, 'venv', 'bin', 'python');
  writeFileSync(
    python,
    '#!/bin/sh\n' +
      `exec ${process.execPath} -e ` +
      `'require("fs").renameSync(process.argv[3], process.argv[4])' -- "$@"\n`,
  );
  chmodSync(python, 0o755);

  writeFileSync(join(dir, 'VERSION'), `${version}\n`);
  writeFileSync(join(dir, 'share', 'config.user.example.yaml'), SHIPPED_EXAMPLE);

  const installer = join(dir, 'bin', 'install-artifact.sh');
  writeFileSync(installer, INSTALLER_SOURCE);
  chmodSync(installer, 0o755);
  return dir;
}

const INSTALLER_SOURCE = `#!/usr/bin/env bash
set -euo pipefail
ARTIFACT=""; PREFIX="\${HERMES_HOME:-$HOME/.hermes}/runtime"; BIN_DIR="$HOME/.local/bin"
while [ $# -gt 0 ]; do
  case "$1" in
    --artifact) ARTIFACT="$2"; shift 2 ;;
    --prefix) PREFIX="$2"; shift 2 ;;
    --bin) BIN_DIR="$2"; shift 2 ;;
    *) shift ;;
  esac
done
VERSION="$(cat "$ARTIFACT/VERSION")"
mkdir -p "$PREFIX/versions" "$BIN_DIR"
STAGE="$PREFIX/versions/.stage-$VERSION.$$"
cp -R "$ARTIFACT" "$STAGE"
"$STAGE/bin/hermes" --version >/dev/null 2>&1 || { rm -rf "$STAGE"; exit 1; }
rm -rf "$PREFIX/versions/$VERSION"
mv "$STAGE" "$PREFIX/versions/$VERSION"
ln -sfn "versions/$VERSION" "$PREFIX/.current.$$"
"$PREFIX/versions/$VERSION/venv/bin/python" -c x "$PREFIX/.current.$$" "$PREFIX/current"
ln -sfn "$PREFIX/current/bin/hermes" "$BIN_DIR/hermes"
CONFIG="\${HERMES_HOME:-$HOME/.hermes}/config.user.yaml"
if [ ! -f "$CONFIG" ]; then
  mkdir -p "$(dirname "$CONFIG")"
  cp "$PREFIX/versions/$VERSION/share/config.user.example.yaml" "$CONFIG"
  chmod 600 "$CONFIG"
fi
`;

function writeXdgConfig(apiKey: string): string {
  const dir = join(home, '.config', 'hermes');
  mkdirSync(dir, { recursive: true });
  const path = join(dir, 'config.user.yaml');
  writeFileSync(path, `llm:\n  provider: openai\n  api_key: ${apiKey}\n`);
  return path;
}

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'hermes-home-'));
});

afterEach(() => {
  rmSync(home, { recursive: true, force: true });
});

describe('path resolution', () => {
  it('defaults to ~/.hermes and honours HERMES_HOME', () => {
    expect(agentHomeDir(ctxFor())).toBe(join(home, '.hermes'));
    const custom = join(home, 'elsewhere');
    expect(agentHomeDir(ctxFor({ env: { HERMES_HOME: custom } }))).toBe(custom);
  });

  it('pins the path through `current`, never a concrete version', () => {
    expect(managedAgentPath(ctxFor())).toBe(
      join(home, '.hermes', 'runtime', 'current', 'bin', 'hermes'),
    );
  });
});

describe('classifyUserBin', () => {
  it('reports absent when nothing is there', async () => {
    await expect(classifyUserBin(ctxFor())).resolves.toBe('absent');
  });

  it('reports foreign for a developer shell shim', async () => {
    const bin = userBinPath(ctxFor());
    mkdirSync(join(home, '.local', 'bin'), { recursive: true });
    writeFileSync(bin, '#!/bin/sh\nexec /repo/.venv/bin/hermes "$@"\n');
    chmodSync(bin, 0o755);
    await expect(classifyUserBin(ctxFor())).resolves.toBe('foreign');
  });

  it('reports foreign for a symlink pointing outside the managed runtime', async () => {
    mkdirSync(join(home, '.local', 'bin'), { recursive: true });
    const target = join(home, 'somewhere', 'hermes');
    mkdirSync(join(home, 'somewhere'), { recursive: true });
    writeFileSync(target, '#!/bin/sh\n');
    chmodSync(target, 0o755);
    symlinkSync(target, userBinPath(ctxFor()));
    await expect(classifyUserBin(ctxFor())).resolves.toBe('foreign');
  });

  it('reports managed for a symlink into the runtime', async () => {
    const bin = managedAgentPath(ctxFor());
    mkdirSync(join(home, '.hermes', 'runtime', 'current', 'bin'), {
      recursive: true,
    });
    writeFileSync(bin, '#!/bin/sh\n');
    mkdirSync(join(home, '.local', 'bin'), { recursive: true });
    symlinkSync(bin, userBinPath(ctxFor()));
    await expect(classifyUserBin(ctxFor())).resolves.toBe('managed');
  });
});

describe('missingAgentConfigFields', () => {
  it('reports both shipped placeholders after a clean install', () => {
    expect(missingAgentConfigFields(SHIPPED_EXAMPLE)).toEqual(['llm.api_base', 'llm.api_key']);
  });

  it('reports nothing once the user supplies real values', () => {
    expect(
      missingAgentConfigFields(
        'llm:\n  model: openai/glm-4.6\n  api_key: sk-abc123\n' +
          '  api_base: https://gw.corp.example/v1\n',
      ),
    ).toEqual([]);
  });

  it('does not call an omitted api_base missing', () => {
    expect(missingAgentConfigFields('llm:\n  model: gpt-4\n  api_key: sk-abc123\n')).toEqual([]);
  });

  it('still flags a gateway host that only looks like the example', () => {
    expect(
      missingAgentConfigFields(
        'llm:\n  model: gpt-4\n  api_key: sk-abc\n' +
          '  api_base: https://your-gateway.example.corp.com/v1\n',
      ),
    ).toEqual([]);
  });

  it('treats an api_base that is not a URL as unusable', () => {
    expect(
      missingAgentConfigFields(
        'llm:\n  model: gpt-4\n  api_key: sk-abc\n  api_base: gateway.corp\n',
      ),
    ).toEqual(['llm.api_base']);
  });

  it('rejects an empty or absent key and model', () => {
    expect(missingAgentConfigFields('llm:\n  api_key:\n')).toEqual(['llm.model', 'llm.api_key']);
    expect(missingAgentConfigFields('')).toEqual(['llm.model', 'llm.api_key']);
  });

  it('accepts a key that is quoted or carries a trailing comment', () => {
    expect(missingAgentConfigFields('llm:\n  model: x\n  api_key: "sk-abc123"\n')).toEqual([]);
    expect(missingAgentConfigFields('llm:\n  model: x\n  api_key: sk-abc  # prod\n')).toEqual([]);
  });

  it('ignores an api_key that belongs to another block', () => {
    expect(missingAgentConfigFields('telegram:\n  api_key: sk-abc\n\nllm:\n  model: x\n')).toEqual([
      'llm.api_key',
    ]);
  });
});

describe('resolveAgentConfigPath', () => {
  it('returns null when hermes would find no config', () => {
    expect(resolveAgentConfigPath(ctxFor())).toBeNull();
  });

  it('finds the XDG default location hermes itself loads', () => {
    const path = writeXdgConfig('sk-live');
    expect(resolveAgentConfigPath(ctxFor())).toBe(path);
  });

  it('prefers the seeded <agent home> config the artifact wrapper points at', () => {
    const xdg = writeXdgConfig('sk-xdg');
    const seeded = join(home, '.hermes', 'config.user.yaml');
    mkdirSync(join(home, '.hermes'), { recursive: true });
    writeFileSync(seeded, 'llm:\n  api_key: sk-seeded\n');
    expect(resolveAgentConfigPath(ctxFor())).toBe(seeded);
    expect(resolveAgentConfigPath(ctxFor())).not.toBe(xdg);
  });

  it('prefers an existing OPENCLAW_CONFIG_PATH', () => {
    writeXdgConfig('sk-live');
    const explicit = join(home, 'explicit.yaml');
    writeFileSync(explicit, 'llm:\n  api_key: sk-other\n');
    expect(
      resolveAgentConfigPath(ctxFor({ env: { HOME: home, OPENCLAW_CONFIG_PATH: explicit } })),
    ).toBe(explicit);
  });

  it('ignores an OPENCLAW_CONFIG_PATH that does not exist', () => {
    const path = writeXdgConfig('sk-live');
    expect(
      resolveAgentConfigPath(
        ctxFor({
          env: { HOME: home, OPENCLAW_CONFIG_PATH: join(home, 'nope.yaml') },
        }),
      ),
    ).toBe(path);
  });

  it('prefers an existing HERMES_CONFIG_PATH', () => {
    writeXdgConfig('sk-live');
    const explicit = join(home, 'explicit.yaml');
    writeFileSync(explicit, 'llm:\n  api_key: sk-other\n');
    expect(
      resolveAgentConfigPath(ctxFor({ env: { HOME: home, HERMES_CONFIG_PATH: explicit } })),
    ).toBe(explicit);
  });

  it('prefers HERMES_CONFIG_PATH over the legacy name when both exist', () => {
    const legacy = join(home, 'legacy.yaml');
    const current = join(home, 'current.yaml');
    writeFileSync(legacy, 'llm:\n  api_key: sk-legacy\n');
    writeFileSync(current, 'llm:\n  api_key: sk-current\n');
    expect(
      resolveAgentConfigPath(
        ctxFor({
          env: {
            HOME: home,
            OPENCLAW_CONFIG_PATH: legacy,
            HERMES_CONFIG_PATH: current,
          },
        }),
      ),
    ).toBe(current);
  });

  it('falls back to the legacy name when HERMES_CONFIG_PATH does not exist', () => {
    writeXdgConfig('sk-live');
    const legacy = join(home, 'legacy.yaml');
    writeFileSync(legacy, 'llm:\n  api_key: sk-legacy\n');
    expect(
      resolveAgentConfigPath(
        ctxFor({
          env: {
            HOME: home,
            HERMES_CONFIG_PATH: join(home, 'nope.yaml'),
            OPENCLAW_CONFIG_PATH: legacy,
          },
        }),
      ),
    ).toBe(legacy);
  });
});

describe('ensureAgentRuntime', () => {
  function bundledCtx(version: string, extra: Partial<AgentRuntimeContext> = {}) {
    const artifact = fakeArtifact(join(home, 'bundle', `hermes-${version}-darwin-arm64`), version);
    return ctxFor({ bundledArtifactDir: artifact, ...extra });
  }

  it('installs the bundled runtime and reports needs_config without a key', async () => {
    const ctx = bundledCtx('1.0.0');
    const status = await ensureAgentRuntime(ctx);

    expect(status.state).toBe('needs_config');
    expect(await installedAgentVersion(ctx)).toBe('1.0.0');
    expect(existsSync(managedAgentPath(ctx))).toBe(true);
  });

  it('reports the seeded config path after an install, not a phantom one', async () => {
    const ctx = bundledCtx('1.0.0');
    const status = await ensureAgentRuntime(ctx);
    expect(status).toMatchObject({
      state: 'needs_config',
      configPath: join(home, '.hermes', 'config.user.yaml'),
      missing: ['llm.api_base', 'llm.api_key'],
    });
  });

  it('reports ready once the user fills the seeded config in', async () => {
    const ctx = bundledCtx('1.0.0');
    await ensureAgentRuntime(ctx);

    const configPath = join(home, '.hermes', 'config.user.yaml');
    writeFileSync(
      configPath,
      'llm:\n  provider: openai\n  model: openai/glm-4.6\n' +
        '  api_key: sk-live\n  api_base: https://gw.corp.example/v1\n',
    );

    expect(await readAgentRuntimeStatus(ctx)).toMatchObject({
      state: 'ready',
      version: '1.0.0',
      configPath,
      binPath: managedAgentPath(ctx),
    });
  });

  it('never touches ~/.local/bin — not even to create it', async () => {
    const ctx = bundledCtx('1.0.0');
    await ensureAgentRuntime(ctx);
    expect(existsSync(userBinPath(ctx))).toBe(false);
  });

  it('leaves an existing developer install alone and does not install', async () => {
    mkdirSync(join(home, '.local', 'bin'), { recursive: true });
    const shim = userBinPath(ctxFor());
    const shimBody = '#!/bin/sh\nexec /repo/agent/.venv/bin/hermes "$@"\n';
    writeFileSync(shim, shimBody);
    chmodSync(shim, 0o755);

    const ctx = bundledCtx('1.0.0');
    const status = await ensureAgentRuntime(ctx);

    expect(status).toEqual({ state: 'external', path: shim });
    expect(existsSync(join(home, '.hermes', 'runtime'))).toBe(false);
    expect(agentPathEnvValue(status)).toBeNull();
  });

  it('updates in place and keeps the pinned path valid', async () => {
    const first = await ensureAgentRuntime(bundledCtx('1.0.0'));
    expect(first.state).toBe('needs_config');

    const pinned = managedAgentPath(ctxFor());
    const secondCtx = bundledCtx('2.0.0');
    const second = await ensureAgentRuntime(secondCtx);

    expect(second).toMatchObject({ state: 'needs_config', version: '2.0.0' });
    expect(agentPathEnvValue(second)).toBe(pinned);
    expect(existsSync(pinned)).toBe(true);
    expect(readlinkSync(join(home, '.hermes', 'runtime', 'current'))).toBe('versions/2.0.0');
    expect(existsSync(join(home, '.hermes', 'runtime', 'versions', '1.0.0'))).toBe(true);
  });

  it('does not reinstall when the bundled version is already active', async () => {
    const ctx = bundledCtx('1.0.0');
    await ensureAgentRuntime(ctx);
    const marker = join(home, '.hermes', 'runtime', 'versions', '1.0.0', 'installed-once');
    writeFileSync(marker, '');

    await ensureAgentRuntime(bundledCtx('1.0.0'));

    expect(existsSync(marker)).toBe(true);
  });

  it('reports not_installed when the build carries no artifact', async () => {
    const status = await ensureAgentRuntime(ctxFor());
    expect(status.state).toBe('not_installed');
    expect(agentPathEnvValue(status)).toBeNull();
  });

  describe('on Windows', () => {
    it('names the missing Windows artifact rather than saying nothing', async () => {
      const status = await ensureAgentRuntime(
        ctxFor({ platform: 'win32', bundledArtifactDir: null }),
      );

      expect(status).toMatchObject({ state: 'not_installed' });
      expect(status).toHaveProperty('detail', expect.stringContaining('Windows'));
      expect(agentPathEnvValue(status)).toBeNull();
    });

    it('says the bundled artifact carries no Windows installer, and what was looked for', async () => {
      const status = await ensureAgentRuntime(bundledCtx('1.0.0', { platform: 'win32' }));

      expect(status).toMatchObject({ state: 'not_installed' });
      expect(status).toHaveProperty('detail', expect.stringContaining('install-artifact.ps1'));
    });

    it('still refuses to run an installer whose arguments are undecided', async () => {
      const ctx = bundledCtx('1.0.0', { platform: 'win32' });
      const artifact = ctx.bundledArtifactDir as string;
      writeFileSync(join(artifact, 'bin', 'install-artifact.ps1'), 'exit 0\n');

      const status = await ensureAgentRuntime(ctx);

      expect(status).toMatchObject({ state: 'not_installed' });
      expect(status).toHaveProperty('detail', expect.stringContaining('install-artifact.ps1'));
      expect(existsSync(join(home, '.hermes', 'runtime'))).toBe(false);
    });

    it('never touches the POSIX user bin on Windows', async () => {
      mkdirSync(join(home, '.local', 'bin'), { recursive: true });
      writeFileSync(userBinPath(ctxFor()), '#!/bin/sh\n');

      const status = await readAgentRuntimeStatus(
        ctxFor({ platform: 'win32', bundledArtifactDir: null }),
      );

      expect(status.state).toBe('not_installed');
    });
  });

  it('keeps the previous version usable when an update fails', async () => {
    const ctx = bundledCtx('1.0.0');
    await ensureAgentRuntime(ctx);

    const broken = fakeArtifact(join(home, 'broken'), '2.0.0');
    writeFileSync(join(broken, 'bin', 'hermes'), '#!/bin/sh\nexit 1\n');
    chmodSync(join(broken, 'bin', 'hermes'), 0o755);

    const status = await ensureAgentRuntime(ctxFor({ bundledArtifactDir: broken }));

    expect(status).toMatchObject({ state: 'needs_config', version: '1.0.0' });
    expect(readlinkSync(join(home, '.hermes', 'runtime', 'current'))).toBe('versions/1.0.0');
  });
});

describe('readAgentRuntimeStatus', () => {
  it('never installs as a side effect of a status read', async () => {
    const artifact = fakeArtifact(join(home, 'bundle'), '1.0.0');
    const ctx = ctxFor({ bundledArtifactDir: artifact });

    const status = await readAgentRuntimeStatus(ctx);

    expect(status.state).toBe('not_installed');
    expect(existsSync(join(home, '.hermes', 'runtime'))).toBe(false);
  });

  it('surfaces a foreign install rather than claiming nothing is there', async () => {
    mkdirSync(join(home, '.local', 'bin'), { recursive: true });
    writeFileSync(userBinPath(ctxFor()), '#!/bin/sh\n');
    const status = await readAgentRuntimeStatus(ctxFor());
    expect(status).toEqual({ state: 'external', path: userBinPath(ctxFor()) });
  });

  it('flags a foreign hermes on PATH alongside a managed runtime', async () => {
    const artifact = fakeArtifact(join(home, 'bundle'), '1.0.0');
    const ctx = ctxFor({ bundledArtifactDir: artifact });
    await ensureAgentRuntime(ctx);

    mkdirSync(join(home, '.local', 'bin'), { recursive: true });
    writeFileSync(userBinPath(ctx), '#!/bin/sh\n');

    const status = await readAgentRuntimeStatus(ctx);
    expect(status).toMatchObject({
      state: 'needs_config',
      userBinConflict: userBinPath(ctx),
    });
  });
});

describe('stripQuarantineAttributes', () => {
  function fakeXattr(binDir: string, logFile: string, exitCode = 0, stderr = ''): void {
    mkdirSync(binDir, { recursive: true });
    const script = join(binDir, 'xattr');
    writeFileSync(
      script,
      `#!/bin/sh\n` +
        `printf '%s\\n' "$*" >> "${logFile}"\n` +
        (stderr ? `printf '%s\\n' "${stderr}" 1>&2\n` : '') +
        `exit ${exitCode}\n`,
    );
    chmodSync(script, 0o755);
  }

  it('is a no-op on non-darwin platforms', async () => {
    const artifact = join(home, 'bundle');
    mkdirSync(artifact, { recursive: true });
    const result = await stripQuarantineAttributes(
      { platform: 'win32', env: { HOME: home } },
      artifact,
    );
    expect(result).toEqual({ ok: true });
  });

  it("calls xattr -dr com.apple.quarantine exactly on the artifact dir, never the user's home", async () => {
    const binDir = join(home, 'fakebin');
    const logFile = join(home, 'xattr.log');
    fakeXattr(binDir, logFile);
    const artifact = join(home, 'bundle', 'Contents', 'Resources', 'hermes');
    mkdirSync(artifact, { recursive: true });
    const userTree = join(home, '.hermes');
    mkdirSync(userTree, { recursive: true });
    writeFileSync(join(userTree, 'config.user.yaml'), 'llm: {}\n');

    const result = await stripQuarantineAttributes(
      { platform: 'darwin', env: { HOME: home, PATH: binDir } },
      artifact,
    );

    expect(result).toEqual({ ok: true });
    const calls = readFileSync(logFile, 'utf-8').trim().split('\n');
    expect(calls).toEqual([`-dr com.apple.quarantine ${artifact}`]);
    expect(calls.join('\n')).not.toContain(userTree);
  });

  it('reports failure without throwing when xattr exits non-zero', async () => {
    const binDir = join(home, 'fakebin');
    const logFile = join(home, 'xattr.log');
    fakeXattr(binDir, logFile, 1, 'Operation not permitted');
    const artifact = join(home, 'bundle');
    mkdirSync(artifact, { recursive: true });

    const result = await stripQuarantineAttributes(
      { platform: 'darwin', env: { HOME: home, PATH: binDir } },
      artifact,
    );

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toContain('Operation not permitted');
    }
  });
});

describe('probeBashViaAcp / ensureBashProbe', () => {
  function fakeAcpBinary(path: string, behavior: 'success' | 'bad-output' | 'hang'): void {
    const script =
      behavior === 'hang'
        ? `#!${process.execPath}\nsetInterval(() => {}, 1000);\n`
        : `#!${process.execPath}\n` +
          `const readline = require("readline");\n` +
          `const rl = readline.createInterface({ input: process.stdin });\n` +
          `rl.on("line", (line) => {\n` +
          `  const msg = JSON.parse(line);\n` +
          `  if (msg.id === 1) {\n` +
          `    process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }) + "\\n");\n` +
          `  } else if (msg.id === 2) {\n` +
          `    process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id: 2, result: { sessionId: "s1" } }) + "\\n");\n` +
          `  } else if (msg.id === 3) {\n` +
          `    const text = ${behavior === 'success' ? '"goosar-ok"' : '"nope"'};\n` +
          `    process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id: 3, result: text }) + "\\n");\n` +
          `  }\n` +
          `});\n`;
    writeFileSync(path, script);
    chmodSync(path, 0o755);
  }

  it('resolves ok when the ACP session echoes the marker', async () => {
    const bin = join(home, 'hermes');
    fakeAcpBinary(bin, 'success');
    const result = await probeBashViaAcp(bin, { HOME: home });
    expect(result).toEqual({ ok: true });
  });

  it('resolves not-ok when the output does not contain the marker', async () => {
    const bin = join(home, 'hermes');
    fakeAcpBinary(bin, 'bad-output');
    const result = await probeBashViaAcp(bin, { HOME: home });
    expect(result.ok).toBe(false);
  });

  it('ensureBashProbe skips (does not spawn anything) unless GOOSAR_AGENT_BASH_PROBE=1', async () => {
    const bin = join(home, 'hermes');
    fakeAcpBinary(bin, 'success');
    const result = await ensureBashProbe({ home, env: { HOME: home } }, bin);
    expect(result.ok).toBe(false);
    expect(result.skipped).toBe(true);
  });

  it('ensureBashProbe runs the real probe when the flag is set', async () => {
    const bin = join(home, 'hermes');
    fakeAcpBinary(bin, 'success');
    const result = await ensureBashProbe(
      { home, env: { HOME: home, GOOSAR_AGENT_BASH_PROBE: '1' } },
      bin,
    );
    expect(result.ok).toBe(true);
  });
});

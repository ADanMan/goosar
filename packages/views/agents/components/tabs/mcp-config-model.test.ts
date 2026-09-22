import { describe, expect, it } from 'vitest';
import {
  listManagedMcpServers,
  removeManagedMcpServer,
  upsertManagedMcpServer,
  validateMcpServerConfig,
} from './mcp-config-model';

describe('mcp config compatibility model', () => {
  it('lists a name present in both containers as two rows, not one', () => {
    const servers = listManagedMcpServers({
      mcpServers: { shared: { command: 'canonical' } },
      mcp: {
        shared: { type: 'local', command: ['native'] },
        legacy: { type: 'remote', url: 'https://example.test/mcp' },
      },
    });

    expect(servers.map(({ name, container }) => ({ name, container }))).toEqual([
      { name: 'legacy', container: 'mcp' },
      { name: 'shared', container: 'mcp' },
      { name: 'shared', container: 'mcpServers' },
    ]);
  });

  it('removes only the copy in the container the row came from', () => {
    const value = {
      mcpServers: { shared: { command: 'canonical' } },
      mcp: { shared: { type: 'local', command: ['native'] } },
    };
    const native = listManagedMcpServers(value).find((server) => server.container === 'mcp');
    expect(native).toBeDefined();

    expect(removeManagedMcpServer(value, native!)).toEqual({
      mcpServers: { shared: { command: 'canonical' } },
      mcp: {},
    });
  });

  it('updates a single legacy entry while preserving unknown document fields', () => {
    const value = {
      provider: 'opencode',
      mcp: {
        legacy: { type: 'remote', url: 'https://old.test/mcp' },
        sibling: { type: 'local', command: ['node', 'server.js'] },
      },
    };
    const legacy = listManagedMcpServers(value).find((server) => server.name === 'legacy');
    expect(legacy).toBeDefined();

    expect(
      upsertManagedMcpServer(value, legacy!, 'legacy', {
        type: 'remote',
        url: 'https://new.test/mcp',
      }),
    ).toEqual({
      provider: 'opencode',
      mcp: {
        legacy: { type: 'remote', url: 'https://new.test/mcp' },
        sibling: { type: 'local', command: ['node', 'server.js'] },
      },
    });
  });

  it('never collapses a delete into null, so unseen top-level keys survive', () => {
    const value = { mcpServers: { fetch: { command: 'uvx' } } };
    const [fetch] = listManagedMcpServers(value);
    expect(removeManagedMcpServer(value, fetch!)).toEqual({ mcpServers: {} });

    const withMetadata = { version: 1, ...value };
    const [withMetadataFetch] = listManagedMcpServers(withMetadata);
    expect(removeManagedMcpServer(withMetadata, withMetadataFetch!)).toEqual({
      version: 1,
      mcpServers: {},
    });
  });
});

describe('mcp server config validation', () => {
  it('rejects a configuration that is not a JSON object', () => {
    expect(validateMcpServerConfig(['uvx']).errors).toEqual(['not_object']);
    expect(validateMcpServerConfig(null).errors).toEqual(['not_object']);
  });

  it('rejects a configuration with neither a command nor a URL', () => {
    expect(validateMcpServerConfig({ timeout: 30 }).errors).toEqual(['missing_target']);
  });

  it('rejects an empty or non-textual stdio command', () => {
    expect(validateMcpServerConfig({ command: '   ' }).errors).toEqual(['command_required']);
    expect(validateMcpServerConfig({ command: [] }).errors).toEqual(['command_required']);
  });

  it('rejects a bare command that carries its own arguments', () => {
    expect(
      validateMcpServerConfig({
        command: 'npx -y @modelcontextprotocol/server-filesystem',
      }).errors,
    ).toEqual(['command_has_args']);
  });

  it('rejects shell syntax the runtime never expands', () => {
    expect(validateMcpServerConfig({ command: '$HOME/bin/server' }).errors).toEqual([
      'command_shell_syntax',
    ]);
    expect(validateMcpServerConfig({ command: '/usr/bin/server | tee log' }).errors).toEqual([
      'command_shell_syntax',
    ]);
  });

  it('accepts an absolute path that legitimately contains spaces', () => {
    expect(
      validateMcpServerConfig({
        command: '/Applications/My Tools/bin/mcp-server',
      }),
    ).toEqual({ errors: [], warnings: [] });
  });

  it('accepts a Windows absolute path', () => {
    expect(
      validateMcpServerConfig({
        command: 'C:\\Program Files\\mcp\\server.exe',
      }),
    ).toEqual({ errors: [], warnings: [] });
  });

  it('accepts a valid stdio command with arguments and no warnings', () => {
    expect(
      validateMcpServerConfig({
        command: '/usr/local/bin/mcp-server',
        args: ['--root', '/srv'],
        env: { TOKEN: 'x' },
      }),
    ).toEqual({ errors: [], warnings: [] });
  });

  it('accepts the provider-native array command form', () => {
    expect(
      validateMcpServerConfig({
        type: 'local',
        command: ['/opt/mcp/bin/server', '--stdio'],
      }),
    ).toEqual({ errors: [], warnings: [] });
  });

  it('warns without blocking when a bare command cannot be verified', () => {
    expect(validateMcpServerConfig({ command: 'mcp-server' })).toEqual({
      errors: [],
      warnings: ['command_unverified'],
    });
  });

  it('warns without blocking when the command fetches from a public registry', () => {
    for (const runner of ['npx', 'uvx', 'bunx', 'pipx']) {
      expect(validateMcpServerConfig({ command: runner })).toEqual({
        errors: [],
        warnings: ['command_registry_runner'],
      });
    }
  });

  it('reports the registry warning for an absolute path to a package runner', () => {
    expect(validateMcpServerConfig({ command: '/opt/homebrew/bin/npx' }).warnings).toEqual([
      'command_registry_runner',
    ]);
  });

  it('accepts a valid streamable HTTP configuration', () => {
    expect(
      validateMcpServerConfig({
        type: 'http',
        url: 'https://mcp.example.com/mcp',
        headers: { Authorization: 'Bearer x' },
      }),
    ).toEqual({ errors: [], warnings: [] });
  });

  it('accepts a valid SSE configuration', () => {
    expect(
      validateMcpServerConfig({
        type: 'sse',
        url: 'http://127.0.0.1:8931/sse',
      }),
    ).toEqual({ errors: [], warnings: [] });
  });

  it('rejects a URL without a usable http scheme', () => {
    expect(validateMcpServerConfig({ url: 'mcp.example.com' }).errors).toEqual(['url_invalid']);
    expect(validateMcpServerConfig({ url: 'ftp://mcp.example.com' }).errors).toEqual([
      'url_invalid',
    ]);
    expect(validateMcpServerConfig({ type: 'http', url: '  ' }).errors).toEqual(['url_required']);
  });
});

export type McpConfigContainer = 'mcpServers' | 'mcp';

export type ManagedMcpServer = {
  name: string;
  config: Record<string, unknown>;
  container: McpConfigContainer;
  transport: string;
  enabled: boolean;
  masked: boolean;
};

export const MCP_MASKED_MARKER_KEY = '__goosar_masked__';

export function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

export function isMaskedMcpServerConfig(config: unknown): boolean {
  if (!isRecord(config)) return false;
  const keys = Object.keys(config);
  return keys.length === 1 && config[MCP_MASKED_MARKER_KEY] === true;
}

export function mcpTransport(config: Record<string, unknown>): string {
  const type = typeof config.type === 'string' ? config.type.toLowerCase() : '';
  if (config.command || type === 'local' || type === 'stdio') return 'stdio';
  if (type === 'sse') return 'sse';
  if (config.url || type === 'remote' || type === 'http' || type === 'streamable-http') {
    return 'http';
  }
  return 'unknown';
}

export type McpConfigErrorCode =
  | 'not_object'
  | 'missing_target'
  | 'command_required'
  | 'command_has_args'
  | 'command_shell_syntax'
  | 'url_required'
  | 'url_invalid';

export type McpConfigWarningCode = 'command_unverified' | 'command_registry_runner';

export type McpConfigValidation = {
  errors: McpConfigErrorCode[];
  warnings: McpConfigWarningCode[];
};

const REGISTRY_RUNNER_COMMANDS = new Set([
  'npx',
  'npx.cmd',
  'pnpx',
  'bunx',
  'yarn',
  'dlx',
  'uvx',
  'pipx',
]);

const SHELL_SYNTAX = /[|&;<>$`\n\r]/;

function isAbsoluteCommandPath(command: string): boolean {
  return command.startsWith('/') || command.startsWith('\\\\') || /^[A-Za-z]:[\\/]/.test(command);
}

function commandBasename(command: string): string {
  const separator = Math.max(command.lastIndexOf('/'), command.lastIndexOf('\\'));
  return (separator === -1 ? command : command.slice(separator + 1)).toLowerCase();
}

function commandTokens(value: unknown): string[] | null {
  if (typeof value === 'string') return [value.trim()];
  if (Array.isArray(value)) {
    const tokens = value.filter((item): item is string => typeof item === 'string');
    return tokens.length > 0 ? tokens : [];
  }
  return null;
}

function validateStdio(config: Record<string, unknown>): McpConfigValidation {
  const tokens = commandTokens(config.command);
  const command = tokens?.[0]?.trim() ?? '';
  if (command === '') return { errors: ['command_required'], warnings: [] };

  if (SHELL_SYNTAX.test(command)) {
    return { errors: ['command_shell_syntax'], warnings: [] };
  }

  const absolute = isAbsoluteCommandPath(command);
  if (!absolute && /\s/.test(command)) {
    return { errors: ['command_has_args'], warnings: [] };
  }

  if (REGISTRY_RUNNER_COMMANDS.has(commandBasename(command))) {
    return { errors: [], warnings: ['command_registry_runner'] };
  }
  return { errors: [], warnings: absolute ? [] : ['command_unverified'] };
}

function validateUrl(config: Record<string, unknown>): McpConfigValidation {
  const url = typeof config.url === 'string' ? config.url.trim() : '';
  if (url === '') return { errors: ['url_required'], warnings: [] };

  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return { errors: ['url_invalid'], warnings: [] };
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    return { errors: ['url_invalid'], warnings: [] };
  }
  return { errors: [], warnings: [] };
}

export function validateMcpServerConfig(value: unknown): McpConfigValidation {
  if (!isRecord(value)) return { errors: ['not_object'], warnings: [] };

  switch (mcpTransport(value)) {
    case 'stdio':
      return validateStdio(value);
    case 'http':
    case 'sse':
      return validateUrl(value);
    default:
      return { errors: ['missing_target'], warnings: [] };
  }
}

export function listManagedMcpServers(value: unknown): ManagedMcpServer[] {
  if (!isRecord(value)) return [];

  const out: ManagedMcpServer[] = [];
  const seen = new Set<string>();
  for (const container of ['mcpServers', 'mcp'] as const) {
    const raw = value[container];
    if (!isRecord(raw)) continue;
    for (const [name, entry] of Object.entries(raw)) {
      const identity = `${container}:${name}`;
      if (seen.has(identity) || !isRecord(entry)) continue;
      seen.add(identity);
      const masked = isMaskedMcpServerConfig(entry);
      out.push({
        name,
        config: entry,
        container,
        transport: masked ? '' : mcpTransport(entry),
        enabled: masked || (entry.enabled !== false && entry.disabled !== true),
        masked,
      });
    }
  }
  return out.sort((a, b) => a.name.localeCompare(b.name) || a.container.localeCompare(b.container));
}

export function upsertManagedMcpServer(
  value: unknown,
  previous: ManagedMcpServer | null,
  name: string,
  config: Record<string, unknown>,
): Record<string, unknown> {
  const document = isRecord(value) ? { ...value } : {};
  const container: McpConfigContainer = previous?.container ?? 'mcpServers';

  if (previous) {
    const previousMap = isRecord(document[previous.container])
      ? { ...(document[previous.container] as Record<string, unknown>) }
      : {};
    delete previousMap[previous.name];
    document[previous.container] = previousMap;
  }

  const target = isRecord(document[container])
    ? { ...(document[container] as Record<string, unknown>) }
    : {};
  target[name] = config;
  document[container] = target;
  return document;
}

export function removeManagedMcpServer(
  value: unknown,
  server: ManagedMcpServer,
): Record<string, unknown> {
  const document = isRecord(value) ? { ...value } : {};
  const container = isRecord(document[server.container])
    ? { ...(document[server.container] as Record<string, unknown>) }
    : {};
  delete container[server.name];
  document[server.container] = container;
  return document;
}

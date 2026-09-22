// Набор кодов рантаймов, backend которых читает `agent.mcp_config`.
const MCP_SUPPORTED_PROVIDERS = new Set([
  'runtime-c',
  'runtime-d',
  'runtime-e',
  'runtime-g',
  'runtime-i',
  'runtime-j',
  'runtime-k',
  'runtime-l',
  'runtime-m',
  'runtime-n',
  'runtime-p',
  'runtime-q',
  'runtime-r',
]);

export function providerSupportsMcpConfig(provider: string | undefined | null): boolean {
  if (!provider) return false;
  return MCP_SUPPORTED_PROVIDERS.has(provider);
}

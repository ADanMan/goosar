/**
 * Types for the shared MCP contract probe. The module itself is plain ESM so
 * both `node scripts/bundle-agent.mjs` and the bundled main process can use it
 * (see mcp-contract.mjs for why that constraint exists).
 */

export interface McpProbeAnswer {
  state?: string;
  version?: string;
  detail?: string;
  tool_fields?: string[];
}

export type McpClientVerdict =
  | { ok: true; state: "ok"; version?: string }
  | { ok: true; state: "absent"; detail?: string }
  | { ok: false; state: "incompatible"; version?: string; reason: string }
  | { ok: false; state: "unprobeable"; reason: string };

export const MCP_CONTRACT_PROBE: string;
export function incompatibleMcpClientReason(answer: McpProbeAnswer): string;
export function classifyMcpProbeAnswer(
  answer: McpProbeAnswer | undefined,
): McpClientVerdict;

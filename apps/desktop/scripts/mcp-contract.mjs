// Контракт MCP-клиента в одном месте. Один и тот же вопрос Python-runtime задают
// bundle-agent.mjs (артефакту при сборке) и agent-bootstrap.ts (установленному
// runtime на машине).

export const MCP_CONTRACT_PROBE = [
  'import json',
  'import importlib.metadata as m',
  'try:',
  '    import mcp.types as t',
  'except Exception as e:',
  "    print(json.dumps({'state': 'absent', 'detail': str(e)}))",
  '    raise SystemExit(0)',
  "tool = set(getattr(t.Tool, 'model_fields', {}))",
  "res = set(getattr(t.CallToolResult, 'model_fields', {}))",
  "ok = 'inputSchema' in tool and 'isError' in res",
  'print(json.dumps({',
  "    'state': 'ok' if ok else 'incompatible',",
  "    'version': m.version('mcp'),",
  "    'tool_fields': sorted(tool),",
  '}))',
].join('\n');

export function incompatibleMcpClientReason(answer) {
  return (
    `the runtime carries mcp ${answer?.version ?? 'of an unknown version'}, ` +
    `whose Tool model has ${JSON.stringify(answer?.tool_fields ?? [])} — the ` +
    'agent reads Tool.inputSchema and CallToolResult.isError, so every MCP ' +
    'server would start, answer correctly and expose no tools at all.'
  );
}

export function classifyMcpProbeAnswer(answer) {
  if (answer?.state === 'absent') {
    return { ok: true, state: 'absent', detail: answer.detail };
  }
  if (answer?.state === 'ok') {
    return { ok: true, state: 'ok', version: answer.version };
  }
  if (answer?.state === 'incompatible') {
    return {
      ok: false,
      state: 'incompatible',
      version: answer.version,
      reason: incompatibleMcpClientReason(answer),
    };
  }
  return {
    ok: false,
    state: 'unprobeable',
    reason: `unreadable answer from the MCP client probe: ${JSON.stringify(answer)}`,
  };
}

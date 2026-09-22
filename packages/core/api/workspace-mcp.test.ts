import { describe, expect, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  EMPTY_WORKSPACE_MCP_SERVERS,
  WorkspaceMcpServerListSchema,
  type WorkspaceMcpServer,
} from './workspace-mcp';

function parseList(data: unknown): WorkspaceMcpServer[] {
  return parseWithFallback(data, WorkspaceMcpServerListSchema, EMPTY_WORKSPACE_MCP_SERVERS, {
    endpoint: 'GET /api/workspace-mcp-servers',
  });
}

describe('WorkspaceMcpServerListSchema', () => {
  test('parses the workspace library listing', () => {
    const parsed = parseList([
      {
        id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
        workspace_id: '0e9b1e2c-1111-2222-3333-444455556666',
        name: 'jira',
        transport: 'http',
        created_at: '2026-09-01T12:00:00Z',
        updated_at: '2026-09-01T12:00:00Z',
      },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.name).toBe('jira');
    expect(parsed[0]?.enabled).toBeUndefined();
  });

  test('keeps the assignment toggle on the agent-scoped listing', () => {
    const parsed = parseList([
      {
        id: 's1',
        workspace_id: 'w1',
        name: 'jira',
        transport: 'http',
        enabled: false,
      },
    ]);
    expect(parsed[0]?.enabled).toBe(false);
  });

  test('passes an unknown transport through verbatim', () => {
    expect(parseList([{ id: 's1', name: 'ws', transport: 'websocket' }])[0]).toMatchObject({
      transport: 'websocket',
    });
  });

  test('defaults a missing transport to unknown rather than failing', () => {
    const parsed = parseList([{ id: 's1', name: 'bare' }]);
    expect(parsed[0]?.transport).toBe('unknown');
    expect(parsed[0]?.workspace_id).toBe('');
  });

  test.each([
    ['an envelope instead of an array', { servers: [] }],
    ['null', null],
    ['a string', 'nope'],
    ['a row with no id', [{ name: 'jira', transport: 'http' }]],
    ['a row with no name', [{ id: 's1', transport: 'http' }]],
    ['a row whose name is not a string', [{ id: 's1', name: 42 }]],
  ])('falls back to an empty list on %s', (_label, body) => {
    expect(parseList(body)).toEqual([]);
  });
});

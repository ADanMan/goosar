import { describe, expect, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  DeploymentMcpServerListSchema,
  EMPTY_DEPLOYMENT_MCP_SERVERS,
  type DeploymentMcpServer,
} from './deployment-mcp';

function parseList(data: unknown): DeploymentMcpServer[] {
  return parseWithFallback(data, DeploymentMcpServerListSchema, EMPTY_DEPLOYMENT_MCP_SERVERS, {
    endpoint: 'GET /api/deployment/mcp-servers',
  });
}

describe('DeploymentMcpServerListSchema', () => {
  test('parses the deployment-admin listing with its reach count', () => {
    const parsed = parseList([
      {
        id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
        name: 'corp-jira',
        transport: 'http',
        enabled_workspaces: 3,
        created_at: '2026-09-01T12:00:00Z',
        updated_at: '2026-09-01T12:00:00Z',
      },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.enabled_workspaces).toBe(3);
    expect(parsed[0]?.enabled).toBeUndefined();
  });

  test("keeps this workspace's opt-in on the workspace listing", () => {
    const parsed = parseList([{ id: 's1', name: 'corp-jira', transport: 'http', enabled: false }]);
    expect(parsed[0]?.enabled).toBe(false);
  });

  test('does not invent an opt-in the server did not state', () => {
    expect(parseList([{ id: 's1', name: 'bare' }])[0]?.enabled).toBeUndefined();
  });

  test('passes an unknown transport through verbatim', () => {
    expect(parseList([{ id: 's1', name: 'ws', transport: 'websocket' }])[0]).toMatchObject({
      transport: 'websocket',
    });
  });

  test('defaults a missing transport to a label rather than failing', () => {
    expect(parseList([{ id: 's1', name: 'bare' }])[0]?.transport).toBe('unknown');
  });

  test.each([
    ['an envelope instead of an array', { servers: [] }],
    ['null', null],
    ['a string', 'nope'],
    ['a record with no id', [{ name: 'corp-jira', transport: 'http' }]],
    ['a record with no name', [{ id: 's1', transport: 'http' }]],
    ['a record whose name is not a string', [{ id: 's1', name: 42 }]],
  ])('falls back to an empty list on %s', (_label, body) => {
    expect(parseList(body)).toEqual(EMPTY_DEPLOYMENT_MCP_SERVERS);
  });
});

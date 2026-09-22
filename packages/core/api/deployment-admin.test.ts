import { describe, expect, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  DeploymentAdminListSchema,
  DeploymentAdminPendingSchema,
  DeploymentPolicyViewSchema,
  DeploymentWorkspaceListSchema,
  DeploymentWorkspaceMemberListSchema,
  EMPTY_DEPLOYMENT_ADMIN_LIST,
  EMPTY_DEPLOYMENT_ADMIN_PENDING,
  EMPTY_DEPLOYMENT_POLICY_VIEW,
  EMPTY_DEPLOYMENT_WORKSPACE_LIST,
  EMPTY_DEPLOYMENT_WORKSPACE_MEMBER_LIST,
  isMcpKillSwitchActive,
  type DeploymentAdminEntry,
  type DeploymentAdminPending,
  type DeploymentPolicyView,
  type DeploymentWorkspaceEntry,
  type DeploymentWorkspaceMemberEntry,
} from './deployment-admin';

function parseAdmins(data: unknown): DeploymentAdminEntry[] {
  return parseWithFallback(data, DeploymentAdminListSchema, EMPTY_DEPLOYMENT_ADMIN_LIST, {
    endpoint: 'GET /api/deployment/admins',
  });
}

describe('DeploymentAdminListSchema', () => {
  test('parses a role-holder list', () => {
    const parsed = parseAdmins([
      {
        user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
        email: 'root@corp.example',
        name: 'Root',
        granted_by: '6ba7b811-9dad-11d1-80b4-00c04fd430c8',
        granted_at: '2026-08-01T12:00:00Z',
      },
      { user_id: '6ba7b812-9dad-11d1-80b4-00c04fd430c8' },
    ]);
    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.email).toBe('root@corp.example');
    expect(parsed[1]?.email).toBeUndefined();
  });

  test('defaults a missing user_id to an empty string', () => {
    const parsed = parseAdmins([{ email: 'root@corp.example' }]);
    expect(parsed[0]?.user_id).toBe('');
  });

  test('falls back on a malformed response (object body)', () => {
    expect(parseAdmins({ admins: [] })).toEqual(EMPTY_DEPLOYMENT_ADMIN_LIST);
  });
});

function parsePending(data: unknown): DeploymentAdminPending {
  return parseWithFallback(data, DeploymentAdminPendingSchema, EMPTY_DEPLOYMENT_ADMIN_PENDING, {
    endpoint: 'POST /api/deployment/admins',
  });
}

describe('DeploymentAdminPendingSchema', () => {
  test('parses a filed grant with its confirm hint verbatim', () => {
    const hint =
      'run on the server: `go run ./cmd/goosar_admin confirm 1b4e28ba-2fa1-11d2-883f-0016d3cca427`';
    const parsed = parsePending({
      status: 'pending',
      request_id: '1b4e28ba-2fa1-11d2-883f-0016d3cca427',
      action: 'grant',
      target_user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
      target_email: 'new-admin@corp.example',
      requested_by: '6ba7b811-9dad-11d1-80b4-00c04fd430c8',
      requested_at: '2026-08-27T10:00:00Z',
      confirm_hint: hint,
    });
    expect(parsed.status).toBe('pending');
    expect(parsed.action).toBe('grant');
    expect(parsed.confirm_hint).toBe(hint);
  });

  test('defaults dropped fields instead of failing the whole answer', () => {
    const parsed = parsePending({ request_id: 'r1', action: 'revoke' });
    expect(parsed.status).toBe('pending');
    expect(parsed.confirm_hint).toBe('');
    expect(parsed.target_email).toBeUndefined();
  });

  test('falls back on a malformed response (string body)', () => {
    expect(parsePending('accepted')).toEqual(EMPTY_DEPLOYMENT_ADMIN_PENDING);
  });
});

function parsePolicy(data: unknown): DeploymentPolicyView {
  return parseWithFallback(data, DeploymentPolicyViewSchema, EMPTY_DEPLOYMENT_POLICY_VIEW, {
    endpoint: 'GET /api/deployment/policy',
  });
}

describe('DeploymentPolicyViewSchema', () => {
  test('parses the full strict document', () => {
    const parsed = parsePolicy({
      policy: {
        llm: {
          base_url: 'https://gw.corp.example/v1',
          model: 'openai/coding-medium',
          locked: true,
        },
        mcp: {
          '*': { enabled: false, locked: true },
          github: { enabled: true },
        },
      },
      updated_at: '2026-08-27T10:00:00Z',
    });
    expect(parsed.policy.llm?.base_url).toBe('https://gw.corp.example/v1');
    expect(parsed.policy.llm?.locked).toBe(true);
    expect(parsed.policy.mcp?.['*']?.enabled).toBe(false);
    expect(parsed.policy.mcp?.github?.locked).toBeUndefined();
  });

  test('reads a missing or null policy as the empty document', () => {
    expect(parsePolicy({}).policy).toEqual({});
    expect(parsePolicy({ policy: null }).policy).toEqual({});
  });

  test('falls back on a malformed response (llm is a string)', () => {
    const parsed = parsePolicy({ policy: { llm: 'gateway' } });
    expect(parsed).toEqual(EMPTY_DEPLOYMENT_POLICY_VIEW);
  });

  test('strips unknown policy blocks a newer backend may add', () => {
    const parsed = parsePolicy({
      policy: { llm: { model: 'm' }, future_block: { x: 1 } },
    });
    expect(parsed.policy.llm?.model).toBe('m');
    expect('future_block' in parsed.policy).toBe(false);
  });
});

describe('isMcpKillSwitchActive', () => {
  test('active only in the one wildcard form the server admits', () => {
    expect(isMcpKillSwitchActive({ mcp: { '*': { enabled: false, locked: true } } })).toBe(true);
  });

  test('a drifted wildcard shape never reads as active', () => {
    expect(isMcpKillSwitchActive({})).toBe(false);
    expect(isMcpKillSwitchActive({ mcp: {} })).toBe(false);
    expect(isMcpKillSwitchActive({ mcp: { '*': { enabled: false } } })).toBe(false);
    expect(isMcpKillSwitchActive({ mcp: { '*': { locked: true } } })).toBe(false);
    expect(isMcpKillSwitchActive({ mcp: { '*': { enabled: true, locked: true } } })).toBe(false);
    expect(isMcpKillSwitchActive({ mcp: { github: { enabled: false, locked: true } } })).toBe(
      false,
    );
  });
});

function parseWorkspaces(data: unknown): DeploymentWorkspaceEntry[] {
  return parseWithFallback(data, DeploymentWorkspaceListSchema, EMPTY_DEPLOYMENT_WORKSPACE_LIST, {
    endpoint: 'GET /api/deployment/workspaces',
  });
}

describe('DeploymentWorkspaceListSchema', () => {
  test('parses the directory with member counts', () => {
    const parsed = parseWorkspaces([
      {
        id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
        name: 'Acme Team',
        slug: 'acme',
        member_count: 3,
      },
      {
        id: '6ba7b811-9dad-11d1-80b4-00c04fd430c8',
        name: 'Beta Lab',
        slug: 'beta-lab',
        member_count: 1,
      },
    ]);
    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.name).toBe('Acme Team');
    expect(parsed[1]?.member_count).toBe(1);
  });

  test('defaults dropped fields instead of failing the row', () => {
    const parsed = parseWorkspaces([{ name: 'Nameless' }]);
    expect(parsed[0]?.id).toBe('');
    expect(parsed[0]?.slug).toBe('');
    expect(parsed[0]?.member_count).toBe(0);
  });

  test('falls back on a malformed response (envelope object)', () => {
    expect(parseWorkspaces({ workspaces: [] })).toEqual(EMPTY_DEPLOYMENT_WORKSPACE_LIST);
    expect(parseWorkspaces('nope')).toEqual(EMPTY_DEPLOYMENT_WORKSPACE_LIST);
  });
});

function parseMembers(data: unknown): DeploymentWorkspaceMemberEntry[] {
  return parseWithFallback(
    data,
    DeploymentWorkspaceMemberListSchema,
    EMPTY_DEPLOYMENT_WORKSPACE_MEMBER_LIST,
    { endpoint: 'GET /api/deployment/workspaces/{workspaceId}/members' },
  );
}

describe('DeploymentWorkspaceMemberListSchema', () => {
  test('parses a member roster for the card', () => {
    const parsed = parseMembers([
      {
        user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
        name: 'Root',
        email: 'root@corp.example',
        role: 'owner',
      },
      { user_id: '6ba7b811-9dad-11d1-80b4-00c04fd430c8', role: 'member' },
    ]);
    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.email).toBe('root@corp.example');
    expect(parsed[0]?.role).toBe('owner');
    expect(parsed[1]?.name).toBeUndefined();
  });

  test('defaults a dropped role to an empty string, never a privilege', () => {
    const parsed = parseMembers([{ user_id: 'u1' }]);
    expect(parsed[0]?.role).toBe('');
  });

  test('falls back on a malformed response (object body)', () => {
    expect(parseMembers({ members: [] })).toEqual(EMPTY_DEPLOYMENT_WORKSPACE_MEMBER_LIST);
  });
});

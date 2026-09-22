import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiClient } from './client';

const BASE = 'https://api.example.test';

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubJson(body: unknown, status = 200): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(status === 204 ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function calledUrl(fetchMock: ReturnType<typeof vi.fn>): string {
  return String(fetchMock.mock.calls[0]?.[0]);
}

function calledInit(fetchMock: ReturnType<typeof vi.fn>): RequestInit | undefined {
  return fetchMock.mock.calls[0]?.[1] as RequestInit | undefined;
}

describe('ApiClient.listDeploymentWorkspaces', () => {
  it('GETs the directory and parses the rows', async () => {
    const fetchMock = stubJson([
      { id: 'ws-1', name: 'Acme', slug: 'acme', member_count: 3 },
      { id: 'ws-2', name: 'Beta' },
    ]);

    const result = await new ApiClient(BASE).listDeploymentWorkspaces();

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/workspaces`);
    expect(result).toEqual([
      { id: 'ws-1', name: 'Acme', slug: 'acme', member_count: 3 },
      { id: 'ws-2', name: 'Beta', slug: '', member_count: 0 },
    ]);
  });

  it('falls back to an empty list when the response is not an array', async () => {
    stubJson({ workspaces: [] });
    await expect(new ApiClient(BASE).listDeploymentWorkspaces()).resolves.toEqual([]);
  });
});

describe('ApiClient.listDeploymentWorkspaceMembers', () => {
  it("GETs one workspace's roster", async () => {
    const fetchMock = stubJson([
      { user_id: 'u-1', name: 'Root', email: 'root@corp.example', role: 'owner' },
    ]);

    const result = await new ApiClient(BASE).listDeploymentWorkspaceMembers('ws-1');

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/workspaces/ws-1/members`);
    expect(result[0]?.role).toBe('owner');
    expect(result[0]?.deactivated).toBe(false);
  });

  it('carries the account block through so the roster can show it (#455)', async () => {
    stubJson([
      {
        user_id: 'u-1',
        name: 'Leaver',
        email: 'leaver@corp.example',
        role: 'member',
        deactivated: true,
      },
    ]);
    const result = await new ApiClient(BASE).listDeploymentWorkspaceMembers('ws-1');
    expect(result[0]?.deactivated).toBe(true);
  });

  it("defaults a drifted role to '' rather than to a privilege", async () => {
    stubJson([{ user_id: 'u-1' }]);
    const result = await new ApiClient(BASE).listDeploymentWorkspaceMembers('ws-1');
    expect(result[0]).toEqual({ user_id: 'u-1', role: '', deactivated: false });
  });

  it('falls back to an empty roster on a malformed response', async () => {
    stubJson({ members: [{ user_id: 'u-1' }] });
    await expect(new ApiClient(BASE).listDeploymentWorkspaceMembers('ws-1')).resolves.toEqual([]);
  });
});

describe('ApiClient deployment workspace config', () => {
  it('GETs the masked config of the TARGET workspace', async () => {
    const fetchMock = stubJson({
      llm_base_url: 'https://gw.example/v1',
      llm_model: 'coding-medium',
      has_llm_api_key: true,
    });

    const result = await new ApiClient(BASE).getDeploymentWorkspaceConfig('ws-1');

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/workspaces/ws-1/config`);
    expect(result.has_llm_api_key).toBe(true);
  });

  it('refuses to turn a malformed answer into a keyless, empty configuration', async () => {
    stubJson([{ llm_model: 'x' }]);
    await expect(new ApiClient(BASE).getDeploymentWorkspaceConfig('ws-1')).rejects.toThrow(
      /schema/i,
    );
  });

  it('PUTs the patch verbatim and parses the canonical answer', async () => {
    const fetchMock = stubJson({
      llm_model: 'corp-model',
      has_llm_api_key: false,
    });

    const result = await new ApiClient(BASE).putDeploymentWorkspaceConfig('ws-1', {
      llm_model: 'corp-model',
    });

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/workspaces/ws-1/config`);
    const init = calledInit(fetchMock);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(String(init?.body))).toEqual({ llm_model: 'corp-model' });
    expect(result.llm_model).toBe('corp-model');
  });
});

describe('ApiClient deployment user config override', () => {
  it("GETs one member's override", async () => {
    const fetchMock = stubJson({
      user_id: 'u-2',
      llm_model: 'personal-model',
      has_llm_api_key: false,
    });

    const result = await new ApiClient(BASE).getDeploymentWorkspaceConfigOverride('ws-1', 'u-2');

    expect(calledUrl(fetchMock)).toBe(
      `${BASE}/api/deployment/workspaces/ws-1/config/overrides/u-2`,
    );
    expect(result?.llm_model).toBe('personal-model');
  });

  it("turns the server's 404 into null instead of throwing", async () => {
    stubJson({ error: 'no configuration override for this user' }, 404);
    await expect(
      new ApiClient(BASE).getDeploymentWorkspaceConfigOverride('ws-1', 'u-2'),
    ).resolves.toBeNull();
  });

  it("rethrows non-404 failures — a 500 must never read as 'no override'", async () => {
    stubJson({ error: 'failed to journal configuration access' }, 500);
    await expect(
      new ApiClient(BASE).getDeploymentWorkspaceConfigOverride('ws-1', 'u-2'),
    ).rejects.toThrow();
  });

  it("PUTs the override patch to the member's path", async () => {
    const fetchMock = stubJson({ user_id: 'u-2', has_llm_api_key: false });

    await new ApiClient(BASE).putDeploymentWorkspaceConfigOverride('ws-1', 'u-2', {
      llm_base_url: 'https://personal.example/v1',
    });

    expect(calledUrl(fetchMock)).toBe(
      `${BASE}/api/deployment/workspaces/ws-1/config/overrides/u-2`,
    );
    const init = calledInit(fetchMock);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(String(init?.body))).toEqual({
      llm_base_url: 'https://personal.example/v1',
    });
  });

  it('falls back to the empty override view on a malformed PUT answer', async () => {
    stubJson('not-an-object');
    await expect(
      new ApiClient(BASE).putDeploymentWorkspaceConfigOverride('ws-1', 'u-2', {
        llm_model: 'm',
      }),
    ).resolves.toEqual({ user_id: '', has_llm_api_key: false });
  });

  it("DELETEs the override at the member's path", async () => {
    const fetchMock = stubJson(null, 204);

    await new ApiClient(BASE).deleteDeploymentWorkspaceConfigOverride('ws-1', 'u-2');

    expect(calledUrl(fetchMock)).toBe(
      `${BASE}/api/deployment/workspaces/ws-1/config/overrides/u-2`,
    );
    expect(calledInit(fetchMock)?.method).toBe('DELETE');
  });
});

describe('ApiClient.listDeploymentWorkspaceConfigOverrides', () => {
  it('GETs every override of a workspace in one call', async () => {
    const fetchMock = stubJson([
      { user_id: 'u-2', llm_model: 'personal-model', has_llm_api_key: false },
      { user_id: 'u-3', has_llm_api_key: true },
    ]);

    const result = await new ApiClient(BASE).listDeploymentWorkspaceConfigOverrides('ws-1');

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/workspaces/ws-1/config/overrides`);
    expect(result.map((o) => o.user_id)).toEqual(['u-2', 'u-3']);
    expect(result[1]?.has_llm_api_key).toBe(true);
  });

  it('answers with the empty listing the server actually sent', async () => {
    stubJson([]);
    await expect(
      new ApiClient(BASE).listDeploymentWorkspaceConfigOverrides('ws-1'),
    ).resolves.toEqual([]);
  });

  describe("refuses to turn an unparsed answer into 'no overrides'", () => {
    const drifted: Array<[string, unknown]> = [
      ['an envelope around the array', { overrides: [{ user_id: 'u-2' }] }],
      ['a null body from a nil slice', null],
      ['a row with a field of the wrong type', [{ user_id: 'u-2', llm_model: 42 }]],
    ];
    for (const [name, body] of drifted) {
      it(`rejects on ${name}`, async () => {
        stubJson(body);
        await expect(
          new ApiClient(BASE).listDeploymentWorkspaceConfigOverrides('ws-1'),
        ).rejects.toThrow(/schema/i);
      });
    }
  });
});

describe('drift is a failed read, never an empty answer', () => {
  it('refuses an envelope around the workspace config', async () => {
    stubJson({
      config: {
        llm_base_url: 'https://gw.corp.example/v1',
        has_llm_api_key: true,
      },
    });
    const api = new ApiClient(BASE);
    await expect(api.getDeploymentWorkspaceConfig('ws-1')).rejects.toThrow();
  });

  it('refuses a body missing the field the server always sends', async () => {
    stubJson({ llm_base_url: 'https://gw.corp.example/v1' });
    const api = new ApiClient(BASE);
    await expect(api.getDeploymentWorkspaceConfig('ws-1')).rejects.toThrow();
  });

  it('refuses an unparsed per-member override instead of claiming there is none', async () => {
    stubJson({ override: { user_id: 'u-1', has_llm_api_key: true } });
    const api = new ApiClient(BASE);
    await expect(api.getDeploymentWorkspaceConfigOverride('ws-1', 'u-1')).rejects.toThrow();
  });

  it('still accepts an answer that grew a field this client does not know', async () => {
    stubJson({
      has_llm_api_key: false,
      llm_model: 'openai/gpt-5',
      some_future_field: 'ignored',
    });
    const api = new ApiClient(BASE);
    const view = await api.getDeploymentWorkspaceConfig('ws-1');
    expect(view.llm_model).toBe('openai/gpt-5');
  });
});

describe('ApiClient.listDeploymentAdminPending', () => {
  it('GETs the filed-request queue and parses the rows', async () => {
    const fetchMock = stubJson([
      {
        status: 'pending',
        request_id: 'req-1',
        action: 'grant',
        target_user_id: 'u-9',
        target_email: 'new@corp.example',
        confirm_hint: 'docker compose exec backend ./goosar_admin confirm req-1',
      },
    ]);

    const result = await new ApiClient(BASE).listDeploymentAdminPending();

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/admins/pending`);
    expect(result[0]?.request_id).toBe('req-1');
    expect(result[0]?.confirm_hint).toContain('goosar_admin confirm req-1');
  });

  it('defaults a drifted row instead of dropping the filed request', async () => {
    stubJson([{ request_id: 'req-2' }]);
    const result = await new ApiClient(BASE).listDeploymentAdminPending();
    expect(result).toHaveLength(1);
    expect(result[0]?.action).toBe('');
  });

  it('falls back to an empty queue when the response is not an array', async () => {
    stubJson({ pending: [] });
    await expect(new ApiClient(BASE).listDeploymentAdminPending()).resolves.toEqual([]);
  });
});

describe('ApiClient.listDeploymentAudit', () => {
  it('GETs the journal and passes the limit through', async () => {
    const fetchMock = stubJson([
      {
        id: 'a-1',
        action: 'deployment_admin.grant.confirmed',
        target_type: 'user',
        target_id: 'u-9',
        created_at: '2026-09-07T10:00:00Z',
      },
    ]);

    const result = await new ApiClient(BASE).listDeploymentAudit(10);

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/audit?limit=10`);
    expect(result[0]?.action).toBe('deployment_admin.grant.confirmed');
    expect(result[0]?.actor_user_id).toBeUndefined();
  });

  it('omits the query string when no limit is asked for', async () => {
    const fetchMock = stubJson([]);
    await new ApiClient(BASE).listDeploymentAudit();
    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/audit`);
  });

  it('falls back to an empty journal on a malformed response', async () => {
    stubJson({ entries: [{ id: 'a-1' }] });
    await expect(new ApiClient(BASE).listDeploymentAudit()).resolves.toEqual([]);
  });

  it('reads auth events with outcome, reason and client identity', async () => {
    stubJson([
      {
        id: 'e-1',
        source: 'auth',
        action: 'auth.login_code.failed',
        actor_type: 'anonymous',
        actor_id: '9f86d081',
        target_type: 'email',
        outcome: 'failure',
        reason: 'invalid_code',
        client_ip: '198.51.100.9',
        user_agent: 'Mozilla/5.0',
        request_id: 'req-7',
        created_at: '2026-09-07T10:00:00Z',
        cursor: 'MjAyNg',
      },
    ]);

    const result = await new ApiClient(BASE).listDeploymentAudit();

    expect(result[0]?.outcome).toBe('failure');
    expect(result[0]?.reason).toBe('invalid_code');
    expect(result[0]?.client_ip).toBe('198.51.100.9');
    expect(result[0]?.source).toBe('auth');
    expect(result[0]?.cursor).toBe('MjAyNg');
  });

  it('sends every filter, and an EMPTY cursor as a real parameter', async () => {
    const fetchMock = stubJson([]);
    await new ApiClient(BASE).listDeploymentAudit({
      limit: 100,
      action: 'auth.login_code.failed',
      actor: '9f86d081',
      since: '2026-09-01T00:00:00Z',
      source: 'auth',
      cursor: '',
    });

    const url = calledUrl(fetchMock);
    expect(url).toContain('limit=100');
    expect(url).toContain('action=auth.login_code.failed');
    expect(url).toContain('actor=9f86d081');
    expect(url).toContain('source=auth');
    expect(url).toContain('cursor=');
  });

  it('still parses a pre-#389 entry with none of the new fields', async () => {
    stubJson([
      {
        id: 'a-1',
        action: 'deployment_policy.set',
        target_type: 'deployment',
        created_at: '2026-09-07T10:00:00Z',
      },
    ]);

    const result = await new ApiClient(BASE).listDeploymentAudit();

    expect(result).toHaveLength(1);
    expect(result[0]?.source).toBeUndefined();
    expect(result[0]?.outcome).toBeUndefined();
  });

  it('falls back to an empty journal when an auth entry is malformed', async () => {
    stubJson([{ id: 'e-1', action: 'auth.logout', outcome: 42 }]);
    await expect(new ApiClient(BASE).listDeploymentAudit()).resolves.toEqual([]);
  });
});

describe('ApiClient deployment user offboarding', () => {
  it('POSTs the deactivation and parses the evidence counters', async () => {
    const fetchMock = stubJson({
      user_id: 'u-1',
      email: 'leaver@corp.example',
      name: 'Leaver',
      deactivated: true,
      deactivated_at: '2026-09-08T09:00:00Z',
      token_version: 3,
      revoked_tokens: 2,
      closed_connections: 1,
    });

    const result = await new ApiClient(BASE).deactivateDeploymentUser('u-1');

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/users/u-1/deactivate`);
    expect(calledInit(fetchMock)?.method).toBe('POST');
    expect(result.deactivated).toBe(true);
    expect(result.revoked_tokens).toBe(2);
  });

  it('POSTs the reactivation', async () => {
    const fetchMock = stubJson({
      user_id: 'u-1',
      email: 'back@corp.example',
      name: 'Back',
      deactivated: false,
      deactivated_at: null,
      token_version: 3,
    });

    const result = await new ApiClient(BASE).reactivateDeploymentUser('u-1');

    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/users/u-1/reactivate`);
    expect(calledInit(fetchMock)?.method).toBe('POST');
    expect(result.deactivated).toBe(false);
    expect(result.deactivated_at).toBeNull();
  });

  it('falls back to the empty user on a malformed response', async () => {
    stubJson([{ user_id: 'u-1', deactivated: true }]);
    await expect(new ApiClient(BASE).deactivateDeploymentUser('u-1')).resolves.toEqual({
      user_id: '',
      email: '',
      name: '',
      deactivated: false,
      deactivated_at: null,
      token_version: 0,
      revoked_tokens: 0,
      closed_connections: 0,
    });
  });

  it('escapes the user id in the path', async () => {
    const fetchMock = stubJson({ user_id: 'a/b' });
    await new ApiClient(BASE).deactivateDeploymentUser('a/b');
    expect(calledUrl(fetchMock)).toBe(`${BASE}/api/deployment/users/a%2Fb/deactivate`);
  });
});

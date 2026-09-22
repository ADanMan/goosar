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

describe('ApiClient.listJoinTargets', () => {
  it('GETs the picker list and parses the rows', async () => {
    const fetchMock = stubJson([
      {
        id: 'ws-1',
        slug: 'hr',
        name: 'HR',
        description: 'Подбор и адаптация',
        template_key: 'hr',
        member_count: 4,
      },
    ]);

    const result = await new ApiClient(BASE).listJoinTargets();

    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(`${BASE}/api/deployment/join-targets`);
    expect(result).toEqual([
      {
        id: 'ws-1',
        slug: 'hr',
        name: 'HR',
        description: 'Подбор и адаптация',
        template_key: 'hr',
        member_count: 4,
      },
    ]);
  });

  it('defaults a drifted row instead of dropping it', async () => {
    stubJson([{ id: 'ws-2', slug: 'legal' }]);

    const [row] = await new ApiClient(BASE).listJoinTargets();

    expect(row?.id).toBe('ws-2');
    expect(row?.name).toBe('');
    expect(row?.member_count).toBe(0);
  });

  it('falls back to an empty list on a malformed response', async () => {
    stubJson({ targets: 'not a list' });

    await expect(new ApiClient(BASE).listJoinTargets()).resolves.toEqual([]);
  });
});

describe('ApiClient.joinTarget', () => {
  it('POSTs to the join endpoint and parses the result', async () => {
    const fetchMock = stubJson({
      id: 'ws-1',
      slug: 'hr',
      name: 'HR',
      already_member: false,
    });

    const result = await new ApiClient(BASE).joinTarget('ws-1');

    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(
      `${BASE}/api/deployment/join-targets/ws-1/join`,
    );
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit | undefined)?.method).toBe('POST');
    expect(result.slug).toBe('hr');
    expect(result.already_member).toBe(false);
  });

  it('reads the idempotent repeat', async () => {
    stubJson({ id: 'ws-1', slug: 'hr', name: 'HR', already_member: true });

    await expect(new ApiClient(BASE).joinTarget('ws-1')).resolves.toMatchObject({
      already_member: true,
    });
  });

  it('falls back to a slug-less result on a malformed response', async () => {
    stubJson('joined!');

    const result = await new ApiClient(BASE).joinTarget('ws-1');

    expect(result.slug).toBe('');
    expect(result.already_member).toBe(false);
  });
});

describe('ApiClient.setDeploymentWorkspaceOpenJoin', () => {
  it('PATCHes the flag', async () => {
    const fetchMock = stubJson({
      id: 'ws-1',
      name: 'HR',
      slug: 'hr',
      open_join: false,
    });

    await new ApiClient(BASE).setDeploymentWorkspaceOpenJoin('ws-1', false);

    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(`${BASE}/api/deployment/workspaces/ws-1`);
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit | undefined;
    expect(init?.method).toBe('PATCH');
    expect(JSON.parse(String(init?.body))).toEqual({ open_join: false });
  });
});

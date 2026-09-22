import { describe, expect, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  EMPTY_WORKSPACE_CONTENT_SYNC_REQUEST,
  WorkspaceContentSyncRequestSchema,
  type WorkspaceContentSyncRequest,
} from './workspace-content';

const ENDPOINT = { endpoint: 'GET /api/runtimes/{runtimeId}/workspace-content/{requestId}' };

function parse(data: unknown): WorkspaceContentSyncRequest {
  return parseWithFallback(
    data,
    WorkspaceContentSyncRequestSchema,
    EMPTY_WORKSPACE_CONTENT_SYNC_REQUEST,
    ENDPOINT,
  );
}

describe('WorkspaceContentSyncRequestSchema', () => {
  test('parses a completed sync with conflicts', () => {
    const parsed = parse({
      id: 'req-1',
      runtime_id: 'rt-1',
      status: 'completed',
      written: ['agents/a/AGENT.md'],
      unchanged: ['skills/deploy/SKILL.md'],
      conflicts: [{ path: 'agents/b/AGENT.md', reason: 'locally_modified' }],
      created_at: '2026-08-14T00:00:00Z',
      updated_at: '2026-08-14T00:00:01Z',
    });

    expect(parsed.status).toBe('completed');
    expect(parsed.written).toEqual(['agents/a/AGENT.md']);
    expect(parsed.conflicts?.[0]?.reason).toBe('locally_modified');
  });

  test('parses a minimal pending sync with every optional field omitted', () => {
    const parsed = parse({
      id: 'req-2',
      runtime_id: 'rt-1',
      status: 'pending',
      created_at: '2026-08-14T00:00:00Z',
      updated_at: '2026-08-14T00:00:00Z',
    });

    expect(parsed.id).toBe('req-2');
    expect(parsed.written).toBeUndefined();
    expect(parsed.conflicts).toBeUndefined();
  });

  test('keeps a status this build has never heard of', () => {
    const parsed = parse({
      id: 'req-3',
      runtime_id: 'rt-1',
      status: 'queued_behind_update',
      created_at: '2026-08-14T00:00:00Z',
      updated_at: '2026-08-14T00:00:00Z',
    });

    expect(parsed.status).toBe('queued_behind_update');
  });

  test('keeps a conflict reason this build has never heard of', () => {
    const parsed = parse({
      id: 'req-4',
      runtime_id: 'rt-1',
      status: 'completed',
      conflicts: [{ path: 'skills/x/SKILL.md', reason: 'some_future_reason' }],
      created_at: '2026-08-14T00:00:00Z',
      updated_at: '2026-08-14T00:00:00Z',
    });

    expect(parsed.conflicts?.[0]?.reason).toBe('some_future_reason');
  });

  test.each([
    ['null', null],
    ['a string', 'not an object'],
    ['an array', []],
    [
      'an object missing id',
      { runtime_id: 'rt-1', status: 'completed', created_at: '', updated_at: '' },
    ],
    [
      'written as a string instead of an array',
      {
        id: 'req-5',
        runtime_id: 'rt-1',
        status: 'completed',
        written: 'agents/a/AGENT.md',
        created_at: '',
        updated_at: '',
      },
    ],
    [
      'a conflict entry missing its reason',
      {
        id: 'req-6',
        runtime_id: 'rt-1',
        status: 'completed',
        conflicts: [{ path: 'skills/x/SKILL.md' }],
        created_at: '',
        updated_at: '',
      },
    ],
  ])('falls back without throwing when the response is %s', (_label, malformed) => {
    const parsed = parse(malformed);

    expect(parsed).toEqual(EMPTY_WORKSPACE_CONTENT_SYNC_REQUEST);
    expect(parsed.status).toBe('failed');
  });
});

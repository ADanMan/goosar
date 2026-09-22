import { describe, expect, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  EMPTY_EXPORT_JOB,
  EMPTY_USER_ERASURE,
  ExportJobSchema,
  UserErasureSchema,
  type ExportJob,
  type UserErasure,
} from './data-export';

function parseJob(data: unknown): ExportJob {
  return parseWithFallback(data, ExportJobSchema, EMPTY_EXPORT_JOB, {
    endpoint: 'GET /api/workspaces/{id}/export/{jobId}',
  });
}

function parseErasure(data: unknown): UserErasure {
  return parseWithFallback(data, UserErasureSchema, EMPTY_USER_ERASURE, {
    endpoint: 'DELETE /api/deployment/users/{userId}',
  });
}

describe('ExportJobSchema', () => {
  test('parses a completed job with its manifest and download URL', () => {
    const parsed = parseJob({
      id: '55555555-5555-5555-5555-555555555555',
      workspace_id: '44444444-4444-4444-4444-444444444444',
      status: 'completed',
      error: null,
      size_bytes: 8421,
      created_at: '2026-09-08T10:00:00Z',
      completed_at: '2026-09-08T10:00:05Z',
      manifest: {
        schema_version: 1,
        kind: 'workspace',
        generated_at: '2026-09-08T10:00:05Z',
        counts: { issue: 12, comment: 40 },
        attachments: { count: 3, bytes: 2048, skipped: [] },
        redacted: ['workspace_mcp.sealed_env'],
        truncated: false,
        notes: [],
      },
      download_url:
        '/api/workspaces/44444444-4444-4444-4444-444444444444/export/55555555-5555-5555-5555-555555555555/download',
    });
    expect(parsed.status).toBe('completed');
    expect(parsed.size_bytes).toBe(8421);
    expect(parsed.download_url).toContain('/download');
    expect(parsed.manifest?.counts.issue).toBe(12);
    expect(parsed.manifest?.attachments.count).toBe(3);
    expect(parsed.manifest?.redacted).toContain('workspace_mcp.sealed_env');
  });

  test('keeps a pending job usable while the manifest does not exist yet', () => {
    const parsed = parseJob({
      id: '55555555-5555-5555-5555-555555555555',
      workspace_id: '44444444-4444-4444-4444-444444444444',
      status: 'pending',
      error: null,
      size_bytes: 0,
      created_at: '2026-09-08T10:00:00Z',
      completed_at: null,
      manifest: null,
      download_url: null,
    });
    expect(parsed.status).toBe('pending');
    expect(parsed.manifest).toBeNull();
    expect(parsed.download_url).toBeNull();
  });

  test('keeps a status the frontend has never heard of', () => {
    const parsed = parseJob({
      id: '55555555-5555-5555-5555-555555555555',
      workspace_id: '44444444-4444-4444-4444-444444444444',
      status: 'cancelled',
      created_at: '2026-09-08T10:00:00Z',
    });
    expect(parsed.status).toBe('cancelled');
  });

  test("carries the server's reason on a failed job", () => {
    const parsed = parseJob({
      id: '55555555-5555-5555-5555-555555555555',
      workspace_id: '44444444-4444-4444-4444-444444444444',
      status: 'failed',
      error: 'export exceeded GOOSAR_EXPORT_MAX_BYTES',
      size_bytes: 0,
      created_at: '2026-09-08T10:00:00Z',
    });
    expect(parsed.error).toBe('export exceeded GOOSAR_EXPORT_MAX_BYTES');
  });

  test('drops a manifest that drifted rather than the whole job', () => {
    const parsed = parseJob({
      id: '55555555-5555-5555-5555-555555555555',
      workspace_id: '44444444-4444-4444-4444-444444444444',
      status: 'completed',
      created_at: '2026-09-08T10:00:00Z',
      manifest: { schema_version: 'one', counts: 'lots' },
      download_url: '/download',
    });
    expect(parsed.status).toBe('completed');
    expect(parsed.download_url).toBe('/download');
    expect(parsed.manifest).toBeNull();
  });

  test('falls back on a malformed response', () => {
    expect(parseJob({ status: 7 })).toEqual(EMPTY_EXPORT_JOB);
    expect(parseJob('completed')).toEqual(EMPTY_EXPORT_JOB);
    expect(parseJob(null)).toEqual(EMPTY_EXPORT_JOB);
  });

  test('the fallback never looks like a downloadable export', () => {
    expect(EMPTY_EXPORT_JOB.download_url).toBeNull();
    expect(EMPTY_EXPORT_JOB.status === 'completed').toBe(false);
  });
});

describe('UserErasureSchema', () => {
  test('parses an erasure result', () => {
    const parsed = parseErasure({
      user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
      email: 'deleted+6ba7b810@invalid',
      name: 'Удалённый пользователь',
      removed_memberships: 2,
      revoked_tokens: 3,
      closed_connections: 1,
    });
    expect(parsed.name).toBe('Удалённый пользователь');
    expect(parsed.removed_memberships).toBe(2);
    expect(parsed.revoked_tokens).toBe(3);
  });

  test('defaults the counters when the backend omits them', () => {
    const parsed = parseErasure({
      user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
      email: 'deleted+6ba7b810@invalid',
      name: 'Удалённый пользователь',
    });
    expect(parsed.removed_memberships).toBe(0);
    expect(parsed.revoked_tokens).toBe(0);
    expect(parsed.closed_connections).toBe(0);
  });

  test('falls back on a malformed response', () => {
    expect(parseErasure({ revoked_tokens: 'three' })).toEqual(EMPTY_USER_ERASURE);
    expect(parseErasure(null)).toEqual(EMPTY_USER_ERASURE);
  });

  test('the fallback never claims an erasure happened', () => {
    expect(EMPTY_USER_ERASURE.user_id).toBe('');
    expect(EMPTY_USER_ERASURE.removed_memberships).toBe(0);
  });
});

import { describe, expect, it } from 'vitest';
import type { z } from 'zod';
import {
  AppConfigSchema,
  AuthMethodsResponseSchema,
  EMPTY_AUTH_METHODS,
  AgentTaskListSchema,
  AutopilotRunSchema,
  FALLBACK_AUTOPILOT_RUN,
  CommentTriggerPreviewSchema,
  DashboardAgentRunTimeListSchema,
  DashboardFailureByAgentListSchema,
  DashboardFailureDailyListSchema,
  DashboardUsageByAgentListSchema,
  DashboardUsageDailyListSchema,
  ChatDraftRestoresResponseSchema,
  CommentSchema,
  CommentsListSchema,
  CreateFeedbackResponseSchema,
  DuplicateIssueErrorBodySchema,
  EMPTY_CHAT_DRAFT_RESTORES,
  EMPTY_CREATE_FEEDBACK_RESPONSE,
  EMPTY_INBOX_ITEMS,
  EMPTY_INBOX_UNREAD_SUMMARY,
  EMPTY_SEARCH_PROJECTS_RESPONSE,
  EMPTY_USER,
  InboxItemListSchema,
  InboxUnreadSummarySchema,
  IssueTriggerPreviewSchema,
  ListIssuesResponseSchema,
  ListPropertiesResponseSchema,
  SearchProjectsResponseSchema,
  RuntimeHourlyActivityListSchema,
  RuntimeUsageByAgentListSchema,
  RuntimeUsageByHourListSchema,
  RuntimeUsageListSchema,
  EMPTY_MEMBER_WITH_USER,
  EMPTY_MEMBER_WITH_USER_LIST,
  MemberWithUserListSchema,
  MemberWithUserSchema,
  SquadListSchema,
  SquadSchema,
  TimelineEntriesSchema,
  UserSchema,
  WorkspaceTemplateListSchema,
  EMPTY_WORKSPACE_TEMPLATE_LIST,
  WorkspaceCapabilitiesSchema,
  EMPTY_WORKSPACE_CAPABILITIES,
} from './schemas';
import { parseWithFallback } from './schema';

const baseIssue = {
  id: '11111111-1111-1111-1111-111111111111',
  workspace_id: 'ws-1',
  number: 1,
  identifier: 'MUL-1',
  title: 'Test',
  description: null,
  status: 'todo',
  priority: 'medium',
  assignee_type: null,
  assignee_id: null,
  creator_type: 'member',
  creator_id: 'user-1',
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

describe('IssueSchema (via ListIssuesResponseSchema)', () => {
  it('accepts a primitive metadata KV map', () => {
    const payload = {
      issues: [
        {
          ...baseIssue,
          metadata: { pipeline_status: 'waiting', pr_number: 3, is_blocked: true },
        },
      ],
      total: 1,
    };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.metadata).toEqual({
      pipeline_status: 'waiting',
      pr_number: 3,
      is_blocked: true,
    });
  });

  it('defaults metadata to {} when the server omits it (older backend)', () => {
    const { metadata: _omit, ...issueWithoutMetadata } = baseIssue;
    const payload = { issues: [issueWithoutMetadata], total: 1 };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.metadata).toEqual({});
  });

  it('rejects metadata with non-primitive values (nested object)', () => {
    const payload = {
      issues: [{ ...baseIssue, metadata: { nested: { x: 1 } } }],
      total: 1,
    };
    expect(ListIssuesResponseSchema.safeParse(payload).success).toBe(false);
  });

  it('accepts a numeric stage', () => {
    const payload = { issues: [{ ...baseIssue, stage: 2 }], total: 1 };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.stage).toBe(2);
  });

  it('defaults stage to null when the server omits it (older backend)', () => {
    const { stage: _omit, ...issueWithoutStage } = baseIssue;
    const payload = { issues: [issueWithoutStage], total: 1 };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.stage).toBeNull();
  });

  it('accepts custom property values including multi_select arrays', () => {
    const payload = {
      issues: [
        {
          ...baseIssue,
          properties: {
            'def-1': 'opt-a',
            'def-2': ['opt-x', 'opt-y'],
            'def-3': 3.5,
            'def-4': true,
          },
        },
      ],
      total: 1,
    };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.properties).toEqual({
      'def-1': 'opt-a',
      'def-2': ['opt-x', 'opt-y'],
      'def-3': 3.5,
      'def-4': true,
    });
  });

  it('defaults properties to {} when the server omits it (older backend)', () => {
    const parsed = ListIssuesResponseSchema.parse({ issues: [baseIssue], total: 1 });
    expect(parsed.issues[0]?.properties).toEqual({});
  });

  it('drops unknown-shaped property values instead of failing the issue parse', () => {
    const payload = {
      issues: [
        {
          ...baseIssue,
          properties: { 'def-1': { nested: 1 }, 'def-2': 'opt-a' },
        },
      ],
      total: 1,
    };
    const parsed = ListIssuesResponseSchema.parse(payload);
    expect(parsed.issues[0]?.properties).toEqual({ 'def-2': 'opt-a' });
  });
});

describe('IssuePropertySchema (via ListPropertiesResponseSchema)', () => {
  const baseProperty = {
    id: '22222222-2222-2222-2222-222222222222',
    workspace_id: 'ws-1',
    name: 'Severity',
    type: 'select',
    description: '',
    icon: 'flag',
    config: { options: [{ id: 'opt-1', name: 'Critical', color: '#ef4444' }] },
    position: 1,
    archived: false,
    archived_at: null,
    usage_count: 2,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };

  it('parses a full definition', () => {
    const parsed = ListPropertiesResponseSchema.parse({ properties: [baseProperty], total: 1 });
    expect(parsed.properties[0]?.config.options?.[0]?.name).toBe('Critical');
    expect(parsed.properties[0]?.icon).toBe('flag');
  });

  it('survives a malformed response by defaulting the list', () => {
    const parsed = ListPropertiesResponseSchema.parse({});
    expect(parsed.properties).toEqual([]);
    expect(parsed.total).toBe(0);
  });

  it('keeps unknown property types as strings (forward compat)', () => {
    const parsed = ListPropertiesResponseSchema.parse({
      properties: [{ ...baseProperty, type: 'relation', config: {} }],
      total: 1,
    });
    expect(parsed.properties[0]?.type).toBe('relation');
  });

  it('defaults config when the server sends none', () => {
    const { config: _omit, ...withoutConfig } = baseProperty;
    const parsed = ListPropertiesResponseSchema.parse({ properties: [withoutConfig], total: 1 });
    expect(parsed.properties[0]?.config).toEqual({});
  });

  it('defaults icon for an older server response', () => {
    const { icon: _omit, ...withoutIcon } = baseProperty;
    const parsed = ListPropertiesResponseSchema.parse({ properties: [withoutIcon], total: 1 });
    expect(parsed.properties[0]?.icon).toBe('');
  });
});

const PREVIEW_FALLBACK = { triggers: [], total_count: 0 };
const PREVIEW_ENDPOINT = { endpoint: 'POST /api/issues/preview-trigger' };

describe('IssueTriggerPreviewSchema', () => {
  it('parses a well-formed response', () => {
    const parsed = IssueTriggerPreviewSchema.parse({
      triggers: [
        { issue_id: 'i1', agent_id: 'a1', source: 'assign', handoff_supported: true },
        { issue_id: 'i2', agent_id: 'a2', source: 'status', handoff_supported: false },
      ],
      total_count: 2,
    });
    expect(parsed.total_count).toBe(2);
    expect(parsed.triggers).toHaveLength(2);
    expect(parsed.triggers[0]).toMatchObject({
      issue_id: 'i1',
      agent_id: 'a1',
      source: 'assign',
      handoff_supported: true,
    });
  });

  it('defaults missing top-level fields (empty / older backend)', () => {
    const parsed = IssueTriggerPreviewSchema.parse({});
    expect(parsed.triggers).toEqual([]);
    expect(parsed.total_count).toBe(0);
  });

  it('defaults missing optional item fields, keeping required issue_id', () => {
    const parsed = IssueTriggerPreviewSchema.parse({
      triggers: [{ issue_id: 'i1' }],
      total_count: 1,
    });
    expect(parsed.triggers[0]).toEqual({
      issue_id: 'i1',
      agent_id: '',
      source: '',
      handoff_supported: false,
    });
  });

  it('parseWithFallback returns the fallback for a malformed shape (triggers not an array)', () => {
    const parsed = parseWithFallback(
      { triggers: 'nope', total_count: 1 },
      IssueTriggerPreviewSchema,
      PREVIEW_FALLBACK,
      PREVIEW_ENDPOINT,
    );
    expect(parsed).toEqual(PREVIEW_FALLBACK);
  });

  it('parseWithFallback returns the fallback when an item drops the required issue_id', () => {
    const parsed = parseWithFallback(
      { triggers: [{ agent_id: 'a1', source: 'assign' }], total_count: 1 },
      IssueTriggerPreviewSchema,
      PREVIEW_FALLBACK,
      PREVIEW_ENDPOINT,
    );
    expect(parsed).toEqual(PREVIEW_FALLBACK);
  });

  it('parseWithFallback returns the fallback for a wrong-typed total_count', () => {
    const parsed = parseWithFallback(
      { triggers: [], total_count: '5' },
      IssueTriggerPreviewSchema,
      PREVIEW_FALLBACK,
      PREVIEW_ENDPOINT,
    );
    expect(parsed).toEqual(PREVIEW_FALLBACK);
  });

  it('parseWithFallback returns the fallback for null / non-object bodies', () => {
    expect(
      parseWithFallback(null, IssueTriggerPreviewSchema, PREVIEW_FALLBACK, PREVIEW_ENDPOINT),
    ).toEqual(PREVIEW_FALLBACK);
    expect(
      parseWithFallback('oops', IssueTriggerPreviewSchema, PREVIEW_FALLBACK, PREVIEW_ENDPOINT),
    ).toEqual(PREVIEW_FALLBACK);
  });
});

describe('CommentSchema source_task_id (#167 / H4)', () => {
  const baseComment = {
    id: 'comment-1',
    issue_id: 'issue-1',
    author_type: 'agent',
    author_id: 'agent-1',
    content: 'hello',
    type: 'comment',
    parent_id: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };

  it('parses a comment with source_task_id omitted (server now redacts it for regular agent comments)', () => {
    const parsed = CommentSchema.parse(baseComment);
    expect(parsed.source_task_id).toBeUndefined();
  });

  it('still accepts source_task_id when present (agent system/failure comments keep it for retry)', () => {
    const parsed = CommentSchema.parse({
      ...baseComment,
      type: 'system',
      source_task_id: 'task-1',
    });
    expect(parsed.source_task_id).toBe('task-1');
  });

  it('parseWithFallback returns the list even when items drop source_task_id', () => {
    const parsed = parseWithFallback<z.infer<typeof CommentsListSchema>>(
      [baseComment, { ...baseComment, id: 'comment-2' }],
      CommentsListSchema,
      [],
      { endpoint: 'GET /api/issues/:id/comments' },
    );
    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.source_task_id).toBeUndefined();
  });
});

describe('TimelineEntriesSchema', () => {
  it('preserves source_task_id for agent failure comments', () => {
    const parsed = TimelineEntriesSchema.parse([
      {
        type: 'comment',
        id: 'comment-1',
        actor_type: 'agent',
        actor_id: 'agent-1',
        created_at: '2026-01-01T00:00:00Z',
        content: 'API Error: 500 Internal server error',
        comment_type: 'system',
        source_task_id: 'task-1',
      },
    ]);

    expect(parsed[0]?.source_task_id).toBe('task-1');
  });
});

describe('AgentTaskListSchema', () => {
  const task = {
    id: 'task-1',
    agent_id: 'agent-1',
    runtime_id: 'runtime-1',
    issue_id: 'issue-1',
    status: 'queued',
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: '2026-07-10T00:00:00Z',
    trigger_comment_id: 'comment-3',
  };

  it('preserves planned and delivered comment IDs for a task run', () => {
    const parsed = AgentTaskListSchema.parse([
      {
        ...task,
        coalesced_comment_ids: ['comment-1', 'comment-2'],
        delivered_comment_ids: ['comment-1', 'comment-2', 'comment-3'],
      },
    ]);

    expect(parsed[0]?.trigger_comment_id).toBe('comment-3');
    expect(parsed[0]?.coalesced_comment_ids).toEqual(['comment-1', 'comment-2']);
    expect(parsed[0]?.delivered_comment_ids).toEqual(['comment-1', 'comment-2', 'comment-3']);
  });

  it('accepts task payloads from older backends without comment coverage', () => {
    const parsed = AgentTaskListSchema.parse([task]);
    expect(parsed[0]?.coalesced_comment_ids).toBeUndefined();
    expect(parsed[0]?.delivered_comment_ids).toBeUndefined();
  });

  it('degrades malformed optional coverage without dropping task rows', () => {
    const parsed = AgentTaskListSchema.parse([
      {
        ...task,
        coalesced_comment_ids: ['comment-1', 2],
        delivered_comment_ids: 'not-an-array',
      },
      {
        ...task,
        id: 'task-2',
        delivered_comment_ids: ['comment-2', 'comment-3'],
      },
    ]);

    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.coalesced_comment_ids).toBeUndefined();
    expect(parsed[0]?.delivered_comment_ids).toBeUndefined();
    expect(parsed[1]?.delivered_comment_ids).toEqual(['comment-2', 'comment-3']);
  });
});

describe('ChatDraftRestoresResponseSchema', () => {
  it('parses a well-formed response with attachments', () => {
    const parsed = parseWithFallback(
      {
        restores: [
          {
            id: 'msg-1',
            chat_session_id: 's-1',
            task_id: 't-1',
            content: 'run the thing',
            attachments: [{ id: 'att-1', filename: 'notes.txt' }],
            created_at: '2026-07-01T00:00:00Z',
          },
        ],
      },
      ChatDraftRestoresResponseSchema,
      EMPTY_CHAT_DRAFT_RESTORES,
      { endpoint: 'test' },
    );
    expect(parsed.restores).toHaveLength(1);
    expect(parsed.restores[0]?.content).toBe('run the thing');
    expect(parsed.restores[0]?.attachments?.[0]?.id).toBe('att-1');
  });

  it('defaults a missing restores array instead of crashing the composer', () => {
    const parsed = parseWithFallback(
      {},
      ChatDraftRestoresResponseSchema,
      EMPTY_CHAT_DRAFT_RESTORES,
      { endpoint: 'test' },
    );
    expect(parsed.restores).toEqual([]);
  });

  it('falls back to the empty response on a malformed row', () => {
    const parsed = parseWithFallback(
      { restores: [{ chat_session_id: 's-1', content: 42 }] },
      ChatDraftRestoresResponseSchema,
      EMPTY_CHAT_DRAFT_RESTORES,
      { endpoint: 'test' },
    );
    expect(parsed).toEqual(EMPTY_CHAT_DRAFT_RESTORES);
  });
});

describe('CreateFeedbackResponseSchema', () => {
  const ENDPOINT = { endpoint: 'POST /api/feedback' };

  it('parses a well-formed response and preserves extra fields', () => {
    const parsed = parseWithFallback(
      { id: 'feedback-1', created_at: '2026-06-26T00:00:00Z', future_field: true },
      CreateFeedbackResponseSchema,
      EMPTY_CREATE_FEEDBACK_RESPONSE,
      ENDPOINT,
    );
    expect(parsed).toMatchObject({
      id: 'feedback-1',
      created_at: '2026-06-26T00:00:00Z',
      future_field: true,
    });
  });

  it('returns the empty fallback for malformed feedback responses', () => {
    expect(
      parseWithFallback(
        { id: 123, created_at: '2026-06-26T00:00:00Z' },
        CreateFeedbackResponseSchema,
        EMPTY_CREATE_FEEDBACK_RESPONSE,
        ENDPOINT,
      ),
    ).toBe(EMPTY_CREATE_FEEDBACK_RESPONSE);
    expect(
      parseWithFallback(
        null,
        CreateFeedbackResponseSchema,
        EMPTY_CREATE_FEEDBACK_RESPONSE,
        ENDPOINT,
      ),
    ).toBe(EMPTY_CREATE_FEEDBACK_RESPONSE);
  });
});

describe('DuplicateIssueErrorBodySchema', () => {
  const valid = {
    code: 'active_duplicate_issue',
    error: 'An active issue with this title already exists: MUL-12 – Login bug',
    issue: {
      id: '11111111-1111-1111-1111-111111111111',
      identifier: 'MUL-12',
      title: 'Login bug',
    },
  };

  it('accepts a well-formed body', () => {
    expect(DuplicateIssueErrorBodySchema.safeParse(valid).success).toBe(true);
  });

  it('accepts unknown extra fields via .loose()', () => {
    const forwardCompat = {
      ...valid,
      hint: 'Try a different title',
      issue: { ...valid.issue, workspace_id: 'ws-1', status: 'todo' },
    };
    expect(DuplicateIssueErrorBodySchema.safeParse(forwardCompat).success).toBe(true);
  });

  it('rejects a renamed code (so renames degrade to the generic toast)', () => {
    const renamed = { ...valid, code: 'duplicate_issue' };
    expect(DuplicateIssueErrorBodySchema.safeParse(renamed).success).toBe(false);
  });

  it('rejects a missing issue object', () => {
    const { issue: _omit, ...without } = valid;
    expect(DuplicateIssueErrorBodySchema.safeParse(without).success).toBe(false);
  });

  it('rejects a non-string issue.id', () => {
    const broken = { ...valid, issue: { ...valid.issue, id: 42 } };
    expect(DuplicateIssueErrorBodySchema.safeParse(broken).success).toBe(false);
  });

  it('accepts a missing error field (it is optional)', () => {
    const { error: _omit, ...without } = valid;
    expect(DuplicateIssueErrorBodySchema.safeParse(without).success).toBe(true);
  });
});

describe('UserSchema timezone drift', () => {
  const base = {
    id: '11111111-1111-1111-1111-111111111111',
    name: 'Ada',
    email: 'ada@example.com',
  };

  it('defaults timezone to null when the field is absent', () => {
    const parsed = UserSchema.parse(base);
    expect(parsed.timezone).toBe(null);
  });

  it('preserves an explicit IANA timezone', () => {
    const parsed = UserSchema.parse({ ...base, timezone: 'Asia/Tokyo' });
    expect(parsed.timezone).toBe('Asia/Tokyo');
  });

  it('accepts an explicit null timezone', () => {
    const parsed = UserSchema.parse({ ...base, timezone: null });
    expect(parsed.timezone).toBe(null);
  });

  it('falls back to EMPTY_USER when timezone is the wrong type', () => {
    const parsed = parseWithFallback({ ...base, timezone: 42 }, UserSchema, EMPTY_USER, {
      endpoint: 'GET /api/me',
    });
    expect(parsed).toBe(EMPTY_USER);
  });
});

describe('SquadListSchema member preview drift', () => {
  const baseSquad = {
    id: 'squad-1',
    workspace_id: 'ws-1',
    name: 'Frontend Squad',
    description: '',
    instructions: '',
    avatar_url: null,
    leader_id: 'agent-1',
    creator_id: 'user-1',
    created_at: '2026-05-01T00:00:00Z',
    updated_at: '2026-05-01T00:00:00Z',
    archived_at: null,
    archived_by: null,
  };

  it('defaults preview fields when an older backend omits them', () => {
    const parsed = SquadListSchema.parse([baseSquad]);
    expect(parsed[0]?.member_count).toBe(0);
    expect(parsed[0]?.member_preview).toEqual([]);
  });

  it('defaults preview fields on a single squad response', () => {
    const parsed = SquadSchema.parse(baseSquad);
    expect(parsed.member_count).toBe(0);
    expect(parsed.member_preview).toEqual([]);
  });

  it('preserves lightweight member preview rows', () => {
    const parsed = SquadListSchema.parse([
      {
        ...baseSquad,
        member_count: 2,
        member_preview: [
          { member_type: 'agent', member_id: 'agent-1', role: 'leader' },
          { member_type: 'member', member_id: 'user-2', role: 'member' },
        ],
      },
    ]);
    expect(parsed[0]?.member_count).toBe(2);
    expect(parsed[0]?.member_preview).toHaveLength(2);
    expect(parsed[0]?.member_preview?.[0]?.role).toBe('leader');
  });
});

describe('dashboard + runtime usage schema drift', () => {
  it('coerces a missing numeric field to 0 instead of dropping the array', () => {
    const parsed = DashboardUsageDailyListSchema.parse([
      { date: '2026-05-19', model: 'claude-opus-4-7', input_tokens: 100 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.output_tokens).toBe(0);
    expect(parsed[0]?.cache_read_tokens).toBe(0);
    expect(parsed[0]?.cache_write_tokens).toBe(0);
  });

  it('coerces a missing date key to "" so the rest of the series survives', () => {
    const parsed = DashboardUsageDailyListSchema.parse([
      { model: 'claude-opus-4-7', input_tokens: 5 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.date).toBe('');
  });

  it('coerces a missing agent_id key to "" for the agent-runtime panel', () => {
    const parsed = DashboardAgentRunTimeListSchema.parse([
      { total_seconds: 42, task_count: 3, failed_count: 0 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.agent_id).toBe('');
  });

  it('coerces a missing agent_id key to "" for the usage-by-agent panel', () => {
    const parsed = DashboardUsageByAgentListSchema.parse([
      { model: 'claude-opus-4-7', input_tokens: 7 },
    ]);
    expect(parsed[0]?.agent_id).toBe('');
  });

  it('coerces missing fields on every runtime usage schema', () => {
    expect(RuntimeUsageListSchema.parse([{ date: '2026-05-19' }])[0]?.input_tokens).toBe(0);
    expect(RuntimeHourlyActivityListSchema.parse([{ hour: 9 }])[0]?.count).toBe(0);
    expect(RuntimeUsageByAgentListSchema.parse([{ model: 'x' }])[0]?.agent_id).toBe('');
    expect(RuntimeUsageByHourListSchema.parse([{ hour: 9 }])[0]?.model).toBe('');
  });

  it('defaults a missing provider to "" so an older server\'s rows still price by bare model', () => {
    expect(
      DashboardUsageDailyListSchema.parse([{ date: '2026-05-19', model: 'claude-opus-4-7' }])[0]
        ?.provider,
    ).toBe('');
    expect(DashboardUsageByAgentListSchema.parse([{ model: 'claude-opus-4-7' }])[0]?.provider).toBe(
      '',
    );
    expect(RuntimeUsageByAgentListSchema.parse([{ model: 'x' }])[0]?.provider).toBe('');
  });

  it('rejects a non-array body so parseWithFallback can return its fallback', () => {
    expect(DashboardUsageDailyListSchema.safeParse(null).success).toBe(false);
    expect(DashboardFailureDailyListSchema.safeParse(null).success).toBe(false);
    expect(DashboardFailureByAgentListSchema.safeParse({ rows: [] }).success).toBe(false);
    expect(RuntimeUsageListSchema.safeParse({ rows: [] }).success).toBe(false);
  });

  it('keeps a failure_reason the client build has never heard of', () => {
    const parsed = DashboardFailureDailyListSchema.parse([
      { date: '2026-05-19', failure_reason: 'agent_error.brand_new', task_count: 3 },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.failure_reason).toBe('agent_error.brand_new');
    expect(parsed[0]?.task_count).toBe(3);
  });

  it('coerces a missing failure row field without dropping the array', () => {
    const daily = DashboardFailureDailyListSchema.parse([{ date: '2026-05-19' }]);
    expect(daily).toHaveLength(1);
    expect(daily[0]?.failure_reason).toBe('');
    expect(daily[0]?.task_count).toBe(0);

    const byAgent = DashboardFailureByAgentListSchema.parse([
      { failure_reason: 'timeout', task_count: 2 },
    ]);
    expect(byAgent[0]?.agent_id).toBe('');
  });

  it('keeps unknown server-side fields via .loose()', () => {
    const parsed = RuntimeUsageListSchema.parse([{ date: '2026-05-19', region: 'us-east' }]);
    expect((parsed[0] as Record<string, unknown>).region).toBe('us-east');
  });
});

describe('AppConfigSchema cdn_signed drift', () => {
  it('defaults cdn_signed to false when the server omits it (pre-MUL-3254 servers)', () => {
    const parsed = AppConfigSchema.parse({ cdn_domain: 'cdn.example.com' });
    expect(parsed.cdn_signed).toBe(false);
  });

  it('coerces a malformed cdn_signed to false instead of failing the whole config', () => {
    const parsed = AppConfigSchema.parse({
      cdn_domain: 'cdn.example.com',
      cdn_signed: 'yes',
    });
    expect(parsed.cdn_signed).toBe(false);
    expect(parsed.cdn_domain).toBe('cdn.example.com');
  });

  it('keeps cdn_signed=true from a signing-enabled server', () => {
    const parsed = AppConfigSchema.parse({ cdn_signed: true });
    expect(parsed.cdn_signed).toBe(true);
  });

  it('parses frontend feature flag decisions', () => {
    const parsed = AppConfigSchema.parse({
      feature_flags: {
        composio_mcp_apps: true,
        malformed_future_flag: 'yes',
      },
    });
    expect(parsed.feature_flags).toEqual({
      composio_mcp_apps: true,
      malformed_future_flag: false,
    });
  });

  it('defaults malformed feature_flags to an empty object', () => {
    const parsed = AppConfigSchema.parse({ feature_flags: ['not', 'an', 'object'] });
    expect(parsed.feature_flags).toEqual({});
  });

  it('parses server_version and leaves it undefined when the server omits it', () => {
    expect(AppConfigSchema.parse({ server_version: '1.2.3' }).server_version).toBe('1.2.3');
    expect(AppConfigSchema.parse({}).server_version).toBeUndefined();
  });

  it('parses email_transport and leaves it undefined on older servers', () => {
    expect(AppConfigSchema.parse({ email_transport: 'none' }).email_transport).toBe('none');
    expect(AppConfigSchema.parse({ email_transport: 'dev' }).email_transport).toBe('dev');
    expect(AppConfigSchema.parse({}).email_transport).toBeUndefined();
  });

  it('coerces a malformed email_transport to undefined instead of failing the whole config', () => {
    const parsed = AppConfigSchema.parse({
      email_transport: { mode: 'smtp' },
      cdn_domain: 'cdn.example.com',
    });
    expect(parsed.email_transport).toBeUndefined();
    expect(parsed.cdn_domain).toBe('cdn.example.com');
  });

  it('parses delivery_profile from a perimeter server', () => {
    expect(AppConfigSchema.parse({ delivery_profile: 'perimeter' }).delivery_profile).toBe(
      'perimeter',
    );
  });

  it('leaves delivery_profile undefined when the server omits it (managed cloud, older servers)', () => {
    expect(AppConfigSchema.parse({}).delivery_profile).toBeUndefined();
  });

  it('coerces a malformed delivery_profile to undefined instead of failing the whole config', () => {
    const parsed = AppConfigSchema.parse({
      delivery_profile: 42,
      cdn_domain: 'cdn.example.com',
    });
    expect(parsed.delivery_profile).toBeUndefined();
    expect(parsed.cdn_domain).toBe('cdn.example.com');
  });

  it('parses min_daemon_version advertised by the server (#439)', () => {
    expect(AppConfigSchema.parse({ min_daemon_version: '1.0.0' }).min_daemon_version).toBe('1.0.0');
  });

  it('leaves min_daemon_version undefined when the server omits it (gate off, older servers)', () => {
    expect(AppConfigSchema.parse({}).min_daemon_version).toBeUndefined();
  });

  it('coerces a malformed min_daemon_version to undefined instead of failing the whole config', () => {
    const parsed = AppConfigSchema.parse({
      min_daemon_version: { major: 1 },
      cdn_domain: 'cdn.example.com',
    });
    expect(parsed.min_daemon_version).toBeUndefined();
    expect(parsed.cdn_domain).toBe('cdn.example.com');
  });

  it('passes deployment_hosts through as an opaque object', () => {
    expect(
      AppConfigSchema.parse({
        deployment_hosts: { jiraUrl: 'https://jira.example.test' },
      }).deployment_hosts,
    ).toEqual({ jiraUrl: 'https://jira.example.test' });
  });

  it('coerces a malformed deployment_hosts to undefined instead of failing', () => {
    expect(
      AppConfigSchema.parse({
        deployment_hosts: 'https://jira.example.test',
        allow_signup: false,
      }).deployment_hosts,
    ).toBeUndefined();
    expect(AppConfigSchema.parse({ deployment_hosts: [] }).deployment_hosts).toBeUndefined();
    expect(AppConfigSchema.parse({}).deployment_hosts).toBeUndefined();
    expect(AppConfigSchema.parse({ deployment_hosts: 7, allow_signup: false }).allow_signup).toBe(
      false,
    );
  });

  it('parses skill_sources, keeping [] distinct from absent (issue #66)', () => {
    expect(AppConfigSchema.parse({ skill_sources: ['clawhub', 'github'] }).skill_sources).toEqual([
      'clawhub',
      'github',
    ]);
    expect(AppConfigSchema.parse({ skill_sources: [] }).skill_sources).toEqual([]);
    expect(AppConfigSchema.parse({}).skill_sources).toBeUndefined();
  });

  it('coerces a malformed skill_sources to undefined (all-enabled default) instead of failing the whole config', () => {
    const parsed = AppConfigSchema.parse({
      skill_sources: 'clawhub',
      cdn_domain: 'cdn.example.com',
    });
    expect(parsed.skill_sources).toBeUndefined();
    expect(parsed.cdn_domain).toBe('cdn.example.com');
    expect(AppConfigSchema.parse({ skill_sources: ['clawhub', 42, null] }).skill_sources).toEqual([
      'clawhub',
    ]);
  });

  it('parses allowed_providers from a policy-restricted server (issue #69)', () => {
    expect(
      AppConfigSchema.parse({ allowed_providers: ['runtime-j', 'runtime-c'] }).allowed_providers,
    ).toEqual(['runtime-j', 'runtime-c']);
  });

  it('keeps allowed_providers undefined when the server omits it (absent = unrestricted, NOT an empty list)', () => {
    expect(AppConfigSchema.parse({}).allowed_providers).toBeUndefined();
  });

  it('coerces a malformed allowed_providers to undefined instead of failing the whole config', () => {
    const parsed = AppConfigSchema.parse({
      allowed_providers: 'runtime-j',
      cdn_domain: 'cdn.example.com',
    });
    expect(parsed.allowed_providers).toBeUndefined();
    expect(parsed.cdn_domain).toBe('cdn.example.com');
  });

  it('drops non-string allowed_providers entries instead of failing the whole config', () => {
    expect(
      AppConfigSchema.parse({ allowed_providers: ['runtime-j', 7, null] }).allowed_providers,
    ).toEqual(['runtime-j']);
  });
});

describe('InboxUnreadSummarySchema', () => {
  const ENDPOINT = { endpoint: 'GET /api/inbox/unread-summary' };

  it('parses a well-formed summary and tolerates extra fields', () => {
    const parsed = parseWithFallback(
      [
        { workspace_id: 'ws-1', count: 2 },
        { workspace_id: 'ws-2', count: 0, future_field: 'ignored' },
      ],
      InboxUnreadSummarySchema,
      EMPTY_INBOX_UNREAD_SUMMARY,
      ENDPOINT,
    );
    expect(parsed).toEqual([
      { workspace_id: 'ws-1', count: 2 },
      { workspace_id: 'ws-2', count: 0, future_field: 'ignored' },
    ]);
  });

  it('returns the empty fallback (dot hidden) for a non-array body', () => {
    expect(
      parseWithFallback(
        { rows: [] },
        InboxUnreadSummarySchema,
        EMPTY_INBOX_UNREAD_SUMMARY,
        ENDPOINT,
      ),
    ).toBe(EMPTY_INBOX_UNREAD_SUMMARY);
    expect(
      parseWithFallback(null, InboxUnreadSummarySchema, EMPTY_INBOX_UNREAD_SUMMARY, ENDPOINT),
    ).toBe(EMPTY_INBOX_UNREAD_SUMMARY);
  });

  it('returns the empty fallback when an entry has a wrong-typed count', () => {
    expect(
      parseWithFallback(
        [{ workspace_id: 'ws-1', count: 'lots' }],
        InboxUnreadSummarySchema,
        EMPTY_INBOX_UNREAD_SUMMARY,
        ENDPOINT,
      ),
    ).toBe(EMPTY_INBOX_UNREAD_SUMMARY);
  });
});

describe('InboxItemListSchema', () => {
  const ENDPOINT = { endpoint: 'GET /api/inbox/archived' };

  const row = (overrides: Record<string, unknown> = {}) => ({
    id: 'inbox-1',
    workspace_id: 'ws-1',
    recipient_type: 'member',
    recipient_id: 'member-1',
    type: 'new_comment',
    severity: 'info',
    issue_id: 'issue-1',
    title: 'Issue title',
    body: null,
    read: false,
    archived: true,
    created_at: '2026-06-15T08:00:00Z',
    ...overrides,
  });

  it('parses a well-formed archived list and tolerates extra fields', () => {
    const parsed = parseWithFallback(
      [row({ issue_status: 'in_progress', details: { comment_id: 'c-1' }, future_field: 1 })],
      InboxItemListSchema,
      EMPTY_INBOX_ITEMS,
      ENDPOINT,
    );
    expect(parsed).toHaveLength(1);
    expect(parsed[0]).toMatchObject({ id: 'inbox-1', archived: true });
  });

  it("keeps a notification type this client doesn't know yet", () => {
    const parsed = parseWithFallback(
      [row({ type: 'some_future_type', severity: 'future_severity' })],
      InboxItemListSchema,
      EMPTY_INBOX_ITEMS,
      ENDPOINT,
    );
    expect(parsed).toHaveLength(1);
  });

  it('accepts rows that omit the nullable optional fields', () => {
    const { body, issue_id, ...withoutOptionals } = row();
    void body;
    void issue_id;
    expect(
      parseWithFallback([withoutOptionals], InboxItemListSchema, EMPTY_INBOX_ITEMS, ENDPOINT),
    ).toHaveLength(1);
  });

  it('returns the empty fallback for a non-array body', () => {
    expect(parseWithFallback({ items: [] }, InboxItemListSchema, EMPTY_INBOX_ITEMS, ENDPOINT)).toBe(
      EMPTY_INBOX_ITEMS,
    );
    expect(parseWithFallback(null, InboxItemListSchema, EMPTY_INBOX_ITEMS, ENDPOINT)).toBe(
      EMPTY_INBOX_ITEMS,
    );
  });

  it('returns the empty fallback when a row is missing a required field', () => {
    const { id, ...withoutId } = row();
    void id;
    expect(parseWithFallback([withoutId], InboxItemListSchema, EMPTY_INBOX_ITEMS, ENDPOINT)).toBe(
      EMPTY_INBOX_ITEMS,
    );
  });

  it('returns the empty fallback when `archived` is wrong-typed', () => {
    expect(
      parseWithFallback(
        [row({ archived: 'yes' })],
        InboxItemListSchema,
        EMPTY_INBOX_ITEMS,
        ENDPOINT,
      ),
    ).toBe(EMPTY_INBOX_ITEMS);
  });
});

describe('SearchProjectsResponseSchema date drift', () => {
  const ENDPOINT = { endpoint: 'GET /api/projects/search' };

  const baseProject = {
    id: 'p-1',
    workspace_id: 'ws-1',
    title: 'Launch',
    description: null,
    icon: null,
    status: 'in_progress',
    priority: 'high',
    lead_type: null,
    lead_id: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
    match_source: 'title',
  };

  it('parses start_date / due_date when the backend returns them', () => {
    const parsed = parseWithFallback(
      {
        projects: [{ ...baseProject, start_date: '2026-03-01', due_date: '2026-03-31' }],
        total: 1,
      },
      SearchProjectsResponseSchema,
      EMPTY_SEARCH_PROJECTS_RESPONSE,
      ENDPOINT,
    );
    expect(parsed.projects[0]?.start_date).toBe('2026-03-01');
    expect(parsed.projects[0]?.due_date).toBe('2026-03-31');
  });

  it('defaults missing start_date / due_date to null without dropping results', () => {
    const parsed = parseWithFallback(
      { projects: [baseProject], total: 1 },
      SearchProjectsResponseSchema,
      EMPTY_SEARCH_PROJECTS_RESPONSE,
      ENDPOINT,
    );
    expect(parsed).not.toBe(EMPTY_SEARCH_PROJECTS_RESPONSE);
    expect(parsed.projects).toHaveLength(1);
    expect(parsed.projects[0]?.start_date).toBeNull();
    expect(parsed.projects[0]?.due_date).toBeNull();
  });
});

describe('AutopilotRunSchema', () => {
  const ENDPOINT = { endpoint: 'POST /api/autopilots/:id/trigger' };
  const baseRun = {
    id: 'run-1',
    autopilot_id: 'ap-1',
    trigger_id: null,
    source: 'manual',
    status: 'issue_created',
    issue_id: 'issue-1',
    task_id: null,
    triggered_at: '2026-07-14T00:00:00Z',
    completed_at: null,
    failure_reason: null,
    trigger_payload: null,
    result: null,
    created_at: '2026-07-14T00:00:00Z',
  };

  it("preserves a blocked run's status and reason_code", () => {
    const parsed = parseWithFallback(
      {
        ...baseRun,
        status: 'skipped',
        failure_reason: "you are not allowed to trigger this autopilot's assignee agent",
        reason_code: 'invocation_not_allowed',
      },
      AutopilotRunSchema,
      FALLBACK_AUTOPILOT_RUN,
      ENDPOINT,
    );
    expect(parsed.status).toBe('skipped');
    expect(parsed.reason_code).toBe('invocation_not_allowed');
  });

  it('tolerates an older server omitting reason_code', () => {
    const parsed = parseWithFallback(baseRun, AutopilotRunSchema, FALLBACK_AUTOPILOT_RUN, ENDPOINT);
    expect(parsed.status).toBe('issue_created');
    expect(parsed.reason_code).toBeUndefined();
  });

  it('degrades a malformed response to a non-success fallback (never a false success)', () => {
    const parsed = parseWithFallback(
      'not-an-object',
      AutopilotRunSchema,
      FALLBACK_AUTOPILOT_RUN,
      ENDPOINT,
    );
    expect(parsed).toBe(FALLBACK_AUTOPILOT_RUN);
    expect(parsed.status).toBe('failed');
  });
});

describe('CommentTriggerPreviewSchema.blocked', () => {
  it('parses blocked mention outcomes alongside agents', () => {
    const parsed = CommentTriggerPreviewSchema.parse({
      agents: [{ id: 'a1', source: 'mention_agent', reason: '' }],
      blocked: [
        {
          target_type: 'squad',
          target_id: 's1',
          status: 'blocked',
          reason_code: 'invocation_not_allowed',
        },
      ],
    });
    expect(parsed.agents).toHaveLength(1);
    expect(parsed.blocked).toEqual([
      {
        target_type: 'squad',
        target_id: 's1',
        status: 'blocked',
        reason_code: 'invocation_not_allowed',
      },
    ]);
  });

  it('defaults blocked to [] when an older server omits it', () => {
    const parsed = CommentTriggerPreviewSchema.parse({ agents: [] });
    expect(parsed.blocked).toEqual([]);
  });

  it('degrades a malformed blocked field to [] without dropping agents', () => {
    const parsed = CommentTriggerPreviewSchema.parse({
      agents: [{ id: 'a1', source: 'mention_agent', reason: '' }],
      blocked: 'nope',
    });
    expect(parsed.agents).toHaveLength(1);
    expect(parsed.blocked).toEqual([]);
  });

  it('drops a single malformed blocked entry without discarding the valid ones', () => {
    const parsed = CommentTriggerPreviewSchema.parse({
      agents: [],
      blocked: [
        {
          target_type: 'squad',
          target_id: 's1',
          status: 'blocked',
          reason_code: 'invocation_not_allowed',
        },
        { status: 'blocked' }, // missing target_id → dropped individually
        {
          target_type: 'agent',
          target_id: 'a1',
          status: 'blocked',
          reason_code: 'runtime_offline',
        },
      ],
    });
    expect(parsed.blocked.map((b) => b.target_id)).toEqual(['s1', 'a1']);
  });
});

describe('MemberWithUserSchema', () => {
  it('parses a well-formed member and carries perimeter_access', () => {
    const parsed = MemberWithUserSchema.parse({
      id: 'm1',
      workspace_id: 'w1',
      user_id: 'u1',
      role: 'admin',
      created_at: '2026-01-01T00:00:00Z',
      name: 'Alice',
      email: 'alice@example.com',
      avatar_url: null,
      perimeter_access: true,
    });
    expect(parsed.perimeter_access).toBe(true);
    expect(parsed.role).toBe('admin');
  });

  it('defaults perimeter_access to false for older backends that omit it', () => {
    const parsed = MemberWithUserSchema.parse({
      id: 'm1',
      workspace_id: 'w1',
      user_id: 'u1',
      role: 'member',
      created_at: '',
      name: 'Bob',
      email: 'bob@example.com',
      avatar_url: null,
    });
    expect(parsed.perimeter_access).toBe(false);
  });

  it('keeps parsing an unknown future role (lenient enum rule)', () => {
    const parsed = MemberWithUserSchema.parse({
      id: 'm1',
      role: 'superadmin',
    });
    expect(parsed.role).toBe('superadmin');
  });

  it('parseWithFallback returns the empty list for a malformed list shape', () => {
    const parsed = parseWithFallback(
      { members: 'nope' },
      MemberWithUserListSchema,
      EMPTY_MEMBER_WITH_USER_LIST,
      { endpoint: 'GET /api/workspaces/{id}/members' },
    );
    expect(parsed).toEqual([]);
  });

  it('parseWithFallback returns the empty member for a malformed PATCH response', () => {
    const parsed = parseWithFallback(
      { id: 42, perimeter_access: 'yes' },
      MemberWithUserSchema,
      EMPTY_MEMBER_WITH_USER,
      { endpoint: 'PATCH /api/workspaces/{id}/members/{memberId}' },
    );
    expect(parsed).toEqual(EMPTY_MEMBER_WITH_USER);
    expect(parsed.perimeter_access).toBe(false);
  });
});

describe('WorkspaceTemplateListSchema', () => {
  it('parses the bare-array shape the server returns today', () => {
    const parsed = parseWithFallback(
      [
        { key: 'hr', name: 'HR', description: 'People stuff' },
        { key: 'sales', name: 'Sales', description: '' },
      ],
      WorkspaceTemplateListSchema,
      EMPTY_WORKSPACE_TEMPLATE_LIST,
      { endpoint: 'GET /api/workspace-templates' },
    );
    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.key).toBe('hr');
    expect(parsed[1]?.name).toBe('Sales');
  });

  it('accepts a future {templates: [...]} envelope', () => {
    const parsed = parseWithFallback(
      { templates: [{ key: 'legal', name: 'Legal' }] },
      WorkspaceTemplateListSchema,
      EMPTY_WORKSPACE_TEMPLATE_LIST,
      { endpoint: 'GET /api/workspace-templates' },
    );
    expect(parsed).toHaveLength(1);
    expect(parsed[0]?.key).toBe('legal');
    expect(parsed[0]?.description).toBe('');
  });

  it('defaults missing name/description on an otherwise valid entry', () => {
    const parsed = parseWithFallback(
      [{ key: 'finance' }],
      WorkspaceTemplateListSchema,
      EMPTY_WORKSPACE_TEMPLATE_LIST,
      { endpoint: 'GET /api/workspace-templates' },
    );
    expect(parsed).toEqual([{ key: 'finance', name: '', description: '' }]);
  });

  it('parseWithFallback returns the empty list for a malformed response', () => {
    const parsed = parseWithFallback(
      { nonsense: true },
      WorkspaceTemplateListSchema,
      EMPTY_WORKSPACE_TEMPLATE_LIST,
      { endpoint: 'GET /api/workspace-templates' },
    );
    expect(parsed).toEqual([]);
  });

  it('parseWithFallback returns the empty list when an entry has no key', () => {
    const parsed = parseWithFallback(
      [{ name: 'No key' }],
      WorkspaceTemplateListSchema,
      EMPTY_WORKSPACE_TEMPLATE_LIST,
      { endpoint: 'GET /api/workspace-templates' },
    );
    expect(parsed).toEqual([]);
  });
});

describe('WorkspaceCapabilitiesSchema', () => {
  it('parses the shape the server returns today', () => {
    const parsed = parseWithFallback(
      {
        template_key: 'hr',
        role_name: 'HR',
        role_summary: 'Люди и документы',
        capabilities: [{ key: 'mail', title: 'Разбор почты', body: 'Прочитает входящие.' }],
      },
      WorkspaceCapabilitiesSchema,
      EMPTY_WORKSPACE_CAPABILITIES,
      { endpoint: 'GET /api/workspaces/{id}/capabilities' },
    );
    expect(parsed.template_key).toBe('hr');
    expect(parsed.capabilities).toHaveLength(1);
    expect(parsed.capabilities[0]?.title).toBe('Разбор почты');
  });

  it('treats a role-less workspace as a normal empty answer', () => {
    const parsed = parseWithFallback(
      { template_key: '', role_name: '', role_summary: '', capabilities: [] },
      WorkspaceCapabilitiesSchema,
      EMPTY_WORKSPACE_CAPABILITIES,
      { endpoint: 'GET /api/workspaces/{id}/capabilities' },
    );
    expect(parsed).toEqual(EMPTY_WORKSPACE_CAPABILITIES);
  });

  it('drops capability entries that carry no title', () => {
    const parsed = parseWithFallback(
      {
        template_key: 'legal',
        capabilities: [
          { key: 'a', title: 'Договоры', body: 'Подготовит проект.' },
          { key: 'b', title: '   ', body: 'Осиротевшая запись' },
          { key: 'c', body: 'Без заголовка' },
        ],
      },
      WorkspaceCapabilitiesSchema,
      EMPTY_WORKSPACE_CAPABILITIES,
      { endpoint: 'GET /api/workspaces/{id}/capabilities' },
    );
    expect(parsed.capabilities.map((c) => c.key)).toEqual(['a']);
  });

  it('keeps sample tasks that carry both halves and drops the rest (#349)', () => {
    const parsed = parseWithFallback(
      {
        template_key: 'finance',
        sample_tasks: [
          { key: 'a', title: 'Свести таблицу', prompt: 'Сведи данные.' },
          { key: 'b', title: 'Без промпта', prompt: '  ' },
          { key: 'c', prompt: 'Без заголовка' },
        ],
      },
      WorkspaceCapabilitiesSchema,
      EMPTY_WORKSPACE_CAPABILITIES,
      { endpoint: 'GET /api/workspaces/{id}/capabilities' },
    );
    expect(parsed.sample_tasks.map((task) => task.key)).toEqual(['a']);
    expect(parsed.sample_tasks[0]?.requires).toEqual([]);
  });

  it('survives a backend that sends sample_tasks: null', () => {
    const parsed = parseWithFallback(
      { template_key: 'hr', role_name: 'HR', sample_tasks: null },
      WorkspaceCapabilitiesSchema,
      EMPTY_WORKSPACE_CAPABILITIES,
      { endpoint: 'GET /api/workspaces/{id}/capabilities' },
    );
    expect(parsed.role_name).toBe('HR');
    expect(parsed.sample_tasks).toEqual([]);
  });

  it('tolerates an older backend that omits the whole payload', () => {
    const parsed = parseWithFallback(
      {},
      WorkspaceCapabilitiesSchema,
      EMPTY_WORKSPACE_CAPABILITIES,
      { endpoint: 'GET /api/workspaces/{id}/capabilities' },
    );
    expect(parsed).toEqual(EMPTY_WORKSPACE_CAPABILITIES);
  });

  it('parseWithFallback returns the empty view for a malformed response', () => {
    const parsed = parseWithFallback(
      { capabilities: 'not-a-list' },
      WorkspaceCapabilitiesSchema,
      EMPTY_WORKSPACE_CAPABILITIES,
      { endpoint: 'GET /api/workspaces/{id}/capabilities' },
    );
    expect(parsed).toEqual(EMPTY_WORKSPACE_CAPABILITIES);
  });

  it('keeps the role when a backend sends a null capability list', () => {
    const parsed = parseWithFallback(
      {
        template_key: 'hr',
        role_name: 'Персонал',
        role_summary: 'Люди и документы',
        capabilities: null,
      },
      WorkspaceCapabilitiesSchema,
      EMPTY_WORKSPACE_CAPABILITIES,
      { endpoint: 'GET /api/workspaces/{id}/capabilities' },
    );
    expect(parsed.template_key).toBe('hr');
    expect(parsed.role_name).toBe('Персонал');
    expect(parsed.capabilities).toEqual([]);
  });
});

describe('AuthMethodsResponseSchema (#394)', () => {
  const parse = (data: unknown) =>
    parseWithFallback(data, AuthMethodsResponseSchema, EMPTY_AUTH_METHODS, {
      endpoint: 'GET /api/auth/methods',
    });

  it('reads a deployment that offers all three methods', () => {
    const parsed = parse({
      methods: ['email', 'oidc', 'ldap'],
      oidc_display_name: 'Вход через Keycloak',
      ldap_display_name: 'Учётная запись домена',
    });
    expect(parsed.methods).toEqual(['email', 'oidc', 'ldap']);
    expect(parsed.oidc_display_name).toBe('Вход через Keycloak');
    expect(parsed.ldap_display_name).toBe('Учётная запись домена');
  });

  it('defaults the display names a server may omit', () => {
    const parsed = parse({ methods: ['email'] });
    expect(parsed.methods).toEqual(['email']);
    expect(parsed.oidc_display_name).toBe('');
    expect(parsed.ldap_display_name).toBe('');
  });

  it('drops a method it does not know and keeps the rest', () => {
    const parsed = parse({ methods: ['email', 'saml', 'oidc'] });
    expect(parsed.methods).toEqual(['email', 'oidc']);
  });

  it('survives a field growing a shape it did not have', () => {
    const parsed = parse({
      methods: ['oidc'],
      oidc_display_name: 'SSO',
      mfa_required: true,
    });
    expect(parsed.methods).toEqual(['oidc']);
  });

  it.each([
    ['not an object', 'email,oidc'],
    ['null', null],
    ['methods is not a list', { methods: 'email' }],
    ['methods is a number', { methods: 42 }],
  ])('falls back to the email form when the body is %s', (_name, body) => {
    expect(parse(body)).toEqual(EMPTY_AUTH_METHODS);
    expect(EMPTY_AUTH_METHODS.methods).toEqual(['email']);
  });

  it('parses an empty method list without inventing one', () => {
    expect(parse({ methods: [] }).methods).toEqual([]);
  });
});

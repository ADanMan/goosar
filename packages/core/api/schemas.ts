import { z } from 'zod';
import type {
  Agent,
  AgentTemplate,
  AgentTemplateSummary,
  AgentBuilderRuntimeSwitch,
  AgentBuilderSession,
  Attachment,
  AutopilotRun,
  BillingBalance,
  BillingBatchesPage,
  BillingCheckoutSessionStatus,
  BillingPriceTier,
  BillingTopupsPage,
  BillingTransactionsPage,
  CancelTaskResponse,
  ChatDraftRestoresResponse,
  CreateAgentFromTemplateResponse,
  CreateBillingCheckoutSessionResponse,
  CreateBillingPortalSessionResponse,
  CronPreviewResponse,
  GroupedIssuesResponse,
  GitHubConnectResponse,
  GitHubPullRequest,
  InboxItem,
  InboxWorkspaceUnread,
  Label,
  IssueProperty,
  ListPropertiesResponse,
  IssuePropertiesResponse,
  IssueMetadataResponse,
  IssueTableGroupDescriptor,
  IssueTableFacetsResponse,
  IssueTableGroupsResponse,
  IssueTableRowsResponse,
  ListIssuesResponse,
  MemberWithUser,
  WorkspaceTemplate,
  WorkspaceCapabilities,
  ListGitHubInstallationsResponse,
  ListGitHubRepositoriesResponse,
  ListLabelsResponse,
  ListWebhookDeliveriesResponse,
  NotificationPreferenceResponse,
  ResourceLabelsResponse,
  SearchIssuesResponse,
  SearchProjectsResponse,
  Squad,
  TimelineEntry,
  User,
  WebhookDelivery,
} from '../types';
import type { CloudRuntimeNode } from '../runtimes/cloud-runtime';
import type { CreateFeedbackResponse } from '../feedback/types';

export const GitHubInstallationSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    installation_id: z.number().optional(),
    account_login: z.string(),
    account_type: z.string(),
    account_avatar_url: z.string().nullable(),
    created_at: z.string(),
    connected_by: z.string().optional(),
  })
  .loose();

export const ListGitHubInstallationsResponseSchema = z
  .object({
    installations: z.array(GitHubInstallationSchema).default([]),
    configured: z.boolean().optional().default(false),
    repository_browse_configured: z.boolean().optional().default(false),
    can_manage: z.boolean().optional().default(false),
  })
  .loose();

export const EMPTY_LIST_GITHUB_INSTALLATIONS_RESPONSE: ListGitHubInstallationsResponse = {
  installations: [],
  configured: false,
  repository_browse_configured: false,
  can_manage: false,
};

export const GitHubConnectResponseSchema = z
  .object({
    url: z.string().optional(),
    configured: z.boolean().optional().default(false),
  })
  .loose();

export const EMPTY_GITHUB_CONNECT_RESPONSE: GitHubConnectResponse = {
  configured: false,
};

export const GitHubRepositorySchema = z
  .object({
    id: z.number(),
    full_name: z.string(),
    html_url: z.string(),
    clone_url: z.string(),
    description: z.string().nullable(),
    private: z.boolean(),
    archived: z.boolean(),
    default_branch: z.string(),
  })
  .loose();

export const ListGitHubRepositoriesResponseSchema = z
  .object({
    repositories: z.array(GitHubRepositorySchema).default([]),
    total_count: z.number().optional().default(0),
    next_page: z.number().nullable().optional().default(null),
  })
  .loose();

export const EMPTY_LIST_GITHUB_REPOSITORIES_RESPONSE: ListGitHubRepositoriesResponse = {
  repositories: [],
  total_count: 0,
  next_page: null,
};

export const GitHubPullRequestSchema = z
  .object({
    id: z.string(),
    provider: z.string().optional().default('github'),
    workspace_id: z.string(),
    repo_owner: z.string(),
    repo_name: z.string(),
    number: z.number(),
    title: z.string(),
    state: z.string(),
    html_url: z.string(),
    branch: z.string().nullable(),
    author_login: z.string().nullable(),
    author_avatar_url: z.string().nullable(),
    merged_at: z.string().nullable(),
    closed_at: z.string().nullable(),
    pr_created_at: z.string(),
    pr_updated_at: z.string(),
    mergeable: z.string().nullable().optional(),
    merge_state_status: z.string().nullable().optional(),
    snapshot_available: z.boolean().optional(),
    checks_rollup: z.string().nullable().optional(),
    checks_conclusion: z.string().nullable().optional(),
    checks_total: z.number().optional().default(0),
    checks_passed: z.number().optional().default(0),
    checks_failed: z.number().optional().default(0),
    checks_running: z.number().optional().default(0),
    checks_pending: z.number().optional().default(0),
    failed_check_names: z.array(z.string()).optional().default([]),
    snapshot_stale: z.boolean().optional().default(false),
    snapshot_fetched_at: z.string().nullable().optional(),
    mergeable_state: z.string().nullable().optional(),
    additions: z.number().optional().default(0),
    deletions: z.number().optional().default(0),
    changed_files: z.number().optional().default(0),
  })
  .loose();

export const IssuePullRequestsResponseSchema = z
  .object({
    pull_requests: z.array(GitHubPullRequestSchema).default([]),
  })
  .loose();

export const EMPTY_ISSUE_PULL_REQUESTS_RESPONSE: { pull_requests: GitHubPullRequest[] } = {
  pull_requests: [],
};

export const LabelSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    resource_type: z.string().optional().default('issue'),
    name: z.string(),
    description: z.string().optional().default(''),
    color: z.string(),
    usage_count: z.number().optional().default(0),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

export const EMPTY_LABEL: Label = {
  id: '',
  workspace_id: '',
  resource_type: 'issue',
  name: '',
  description: '',
  color: '#6b7280',
  usage_count: 0,
  created_at: '',
  updated_at: '',
};

export const ListLabelsResponseSchema = z
  .object({
    labels: z.array(LabelSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const EMPTY_LIST_LABELS_RESPONSE: ListLabelsResponse = {
  labels: [],
  total: 0,
};

export const ResourceLabelsResponseSchema = z
  .object({
    labels: z.array(LabelSchema).default([]),
  })
  .loose();

export const EMPTY_RESOURCE_LABELS_RESPONSE: ResourceLabelsResponse = {
  labels: [],
};

export const IssuePropertySchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    name: z.string(),
    type: z.string(),
    description: z.string().optional().default(''),
    icon: z.string().optional().default(''),
    config: z
      .object({
        options: z
          .array(
            z
              .object({
                id: z.string(),
                name: z.string(),
                color: z.string().optional().default('#6b7280'),
              })
              .loose(),
          )
          .optional(),
      })
      .loose()
      .default({}),
    position: z.number().optional().default(0),
    archived: z.boolean().optional().default(false),
    archived_at: z.string().nullable().optional(),
    usage_count: z.number().optional().default(0),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

export const EMPTY_ISSUE_PROPERTY: IssueProperty = {
  id: '',
  workspace_id: '',
  name: '',
  type: 'text',
  description: '',
  icon: '',
  config: {},
  position: 0,
  archived: false,
  usage_count: 0,
  created_at: '',
  updated_at: '',
};

export const ListPropertiesResponseSchema = z
  .object({
    properties: z.array(IssuePropertySchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const EMPTY_LIST_PROPERTIES_RESPONSE: ListPropertiesResponse = {
  properties: [],
  total: 0,
};

export const IssuePropertyValuesSchema = z.preprocess(
  (raw) => {
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) return {};
    const out: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(raw)) {
      const ok =
        typeof value === 'string' ||
        typeof value === 'number' ||
        typeof value === 'boolean' ||
        (Array.isArray(value) && value.every((item) => typeof item === 'string'));
      if (ok) out[key] = value;
    }
    return out;
  },
  z
    .record(z.string(), z.union([z.string(), z.number(), z.boolean(), z.array(z.string())]))
    .default({}),
);

export const IssuePropertiesResponseSchema = z
  .object({
    properties: IssuePropertyValuesSchema,
  })
  .loose();

export const EMPTY_ISSUE_PROPERTIES_RESPONSE: IssuePropertiesResponse = {
  properties: {},
};

export interface AppConfigResponse {
  cdn_domain: string;
  cdn_signed?: boolean;
  allow_signup: boolean;
  posthog_key?: string;
  posthog_host?: string;
  analytics_environment?: string;
  daemon_server_url?: string;
  daemon_app_url?: string;
  workspace_creation_disabled?: boolean;
  vcs_integration_available?: boolean;
  email_transport?: string;
  feature_flags?: Record<string, boolean>;
  server_version?: string;
  delivery_profile?: string;
  skill_sources?: string[];
  deployment_hosts?: Record<string, unknown>;
  allowed_providers?: string[];
  min_daemon_version?: string;
  external_images?: string;
  image_hosts?: string[];
}

const ReactionSchema = z.object({
  id: z.string(),
  comment_id: z.string(),
  actor_type: z.string(),
  actor_id: z.string(),
  emoji: z.string(),
  created_at: z.string(),
});

const AttachmentSchema = z
  .object({
    id: z.string(),
  })
  .loose();

export const AttachmentResponseSchema = z
  .object({
    id: z.string(),
    url: z.string(),
    download_url: z.string(),
    markdown_url: z.string().optional().default(''),
    download_ticket_url: z.string().optional().default(''),
    filename: z.string(),
    chat_session_id: z.string().nullable().optional(),
    chat_message_id: z.string().nullable().optional(),
  })
  .loose();

export const EMPTY_ATTACHMENT: Attachment = {
  id: '',
  workspace_id: '',
  issue_id: null,
  comment_id: null,
  chat_session_id: null,
  chat_message_id: null,
  uploader_type: '',
  uploader_id: '',
  filename: '',
  url: '',
  download_url: '',
  markdown_url: '',
  content_type: '',
  size_bytes: 0,
  created_at: '',
};

const TimelineEntrySchema = z
  .object({
    type: z.string(),
    id: z.string(),
    actor_type: z.string(),
    actor_id: z.string(),
    created_at: z.string(),
    action: z.string().optional(),
    details: z.record(z.string(), z.unknown()).optional(),
    content: z.string().optional(),
    parent_id: z.string().nullable().optional(),
    updated_at: z.string().optional(),
    comment_type: z.string().optional(),
    reactions: z.array(ReactionSchema).optional(),
    attachments: z.array(AttachmentSchema).optional(),
    source_task_id: z.string().nullable().optional(),
    coalesced_count: z.number().optional(),
  })
  .loose();

export const TimelineEntriesSchema = z.array(TimelineEntrySchema);

export const EMPTY_TIMELINE_ENTRIES: TimelineEntry[] = [];

const OptionalStringSchema = z.preprocess(
  (value) => (typeof value === 'string' ? value : undefined),
  z.string().optional(),
);

const BooleanWithDefaultSchema = (fallback: boolean) =>
  z.preprocess(
    (value) => (typeof value === 'boolean' ? value : undefined),
    z.boolean().default(fallback),
  );

const FeatureFlagsSchema = z.preprocess(
  (value) => (value && typeof value === 'object' && !Array.isArray(value) ? value : undefined),
  z.record(z.string(), BooleanWithDefaultSchema(false)).default({}),
);

export const AppConfigSchema = z
  .object({
    cdn_domain: z.string().default(''),
    cdn_signed: BooleanWithDefaultSchema(false),
    allow_signup: BooleanWithDefaultSchema(true),
    posthog_key: OptionalStringSchema,
    posthog_host: OptionalStringSchema,
    analytics_environment: OptionalStringSchema,
    daemon_server_url: OptionalStringSchema,
    daemon_app_url: OptionalStringSchema,
    workspace_creation_disabled: BooleanWithDefaultSchema(false).optional(),
    vcs_integration_available: BooleanWithDefaultSchema(false).optional(),
    feature_flags: FeatureFlagsSchema,
    server_version: OptionalStringSchema,
    delivery_profile: OptionalStringSchema,
    skill_sources: z.preprocess(
      (value) => (Array.isArray(value) ? value.filter((v) => typeof v === 'string') : undefined),
      z.array(z.string()).optional(),
    ),
    allowed_providers: z.preprocess(
      (value) => (Array.isArray(value) ? value.filter((v) => typeof v === 'string') : undefined),
      z.array(z.string()).optional(),
    ),
    min_daemon_version: OptionalStringSchema,
    external_images: OptionalStringSchema,
    email_transport: OptionalStringSchema,
    image_hosts: z.preprocess(
      (value) => (Array.isArray(value) ? value.filter((v) => typeof v === 'string') : undefined),
      z.array(z.string()).default([]),
    ),
    deployment_hosts: z.preprocess(
      (value) => (value && typeof value === 'object' && !Array.isArray(value) ? value : undefined),
      z.record(z.string(), z.unknown()).optional(),
    ),
  })
  .loose();

export const EMPTY_APP_CONFIG: AppConfigResponse = {
  cdn_domain: '',
  cdn_signed: false,
  allow_signup: true,
  daemon_server_url: '',
  daemon_app_url: '',
  workspace_creation_disabled: false,
  vcs_integration_available: false,
  feature_flags: {},
  external_images: '',
  image_hosts: [],
};

export const NotificationPreferenceResponseSchema = z
  .object({
    workspace_id: z.string(),
    preferences: z.record(z.string(), z.string()).default({}),
  })
  .loose();

export const EMPTY_NOTIFICATION_PREFERENCE_RESPONSE: NotificationPreferenceResponse = {
  workspace_id: '',
  preferences: {},
};

export const CreateFeedbackResponseSchema = z
  .object({
    id: z.string(),
    created_at: z.string(),
  })
  .loose();

export const EMPTY_CREATE_FEEDBACK_RESPONSE: CreateFeedbackResponse = {
  id: '',
  created_at: '',
};

export const CommentSchema = z
  .object({
    id: z.string(),
    issue_id: z.string(),
    author_type: z.string(),
    author_id: z.string(),
    content: z.string(),
    type: z.string(),
    parent_id: z.string().nullable(),
    reactions: z.array(ReactionSchema).default([]),
    attachments: z.array(AttachmentSchema).default([]),
    created_at: z.string(),
    updated_at: z.string(),
    source_task_id: z.string().nullable().optional(),
  })
  .loose();

export const CommentsListSchema = z.array(CommentSchema);

const CommentTriggerPreviewAgentSchema = z
  .object({
    id: z.string(),
    name: z.string().default(''),
    avatar_url: z.string().optional(),
    source: z.string().default(''),
    reason: z.string().default(''),
  })
  .loose();

export const CommentTriggerOutcomeSchema = z
  .object({
    target_type: z.string().default(''),
    target_id: z.string(),
    status: z.string().default(''),
    reason_code: z.string().default(''),
  })
  .loose();

export const CommentTriggerPreviewSchema = z
  .object({
    agents: z.array(CommentTriggerPreviewAgentSchema).default([]),
    blocked: z
      .array(z.unknown())
      .catch([])
      .default([])
      .transform((items) =>
        items.flatMap((item) => {
          const parsed = CommentTriggerOutcomeSchema.safeParse(item);
          return parsed.success ? [parsed.data] : [];
        }),
      ),
  })
  .loose();

const IssueTriggerPreviewItemSchema = z
  .object({
    issue_id: z.string(),
    agent_id: z.string().default(''),
    source: z.string().default(''),
    handoff_supported: z.boolean().default(false),
  })
  .loose();

export const IssueTriggerPreviewSchema = z
  .object({
    triggers: z.array(IssueTriggerPreviewItemSchema).default([]),
    total_count: z.number().default(0),
  })
  .loose();

const IssueMetadataSchema = z
  .record(z.string(), z.union([z.string(), z.number(), z.boolean()]))
  .default({});

export const IssueMetadataResponseSchema = z
  .object({
    metadata: IssueMetadataSchema,
  })
  .loose();

export const EMPTY_ISSUE_METADATA_RESPONSE: IssueMetadataResponse = {
  metadata: {},
};

export const IssueSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    number: z.number(),
    identifier: z.string(),
    title: z.string(),
    description: z.string().nullable(),
    status: z.string(),
    priority: z.string(),
    assignee_type: z.string().nullable(),
    assignee_id: z.string().nullable(),
    creator_type: z.string(),
    creator_id: z.string(),
    parent_issue_id: z.string().nullable(),
    project_id: z.string().nullable(),
    position: z.number(),
    stage: z.number().nullable().default(null),
    start_date: z.string().nullable(),
    due_date: z.string().nullable(),
    metadata: IssueMetadataSchema,
    properties: IssuePropertyValuesSchema,
    reactions: z.array(z.unknown()).optional(),
    labels: z.array(z.unknown()).optional(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

export const ListIssuesResponseSchema = z
  .object({
    issues: z.array(IssueSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const CreateIssueResponseSchema = IssueSchema.extend({
  id: z.string().min(1),
  labels: z.array(LabelSchema).optional().catch(undefined),
}).loose();

export const EMPTY_LIST_ISSUES_RESPONSE: ListIssuesResponse = {
  issues: [],
  total: 0,
};

const SearchIssueResultSchema = IssueSchema.extend({
  match_source: z.string(),
  matched_snippet: z.string().optional(),
  matched_description_snippet: z.string().optional(),
  matched_comment_snippet: z.string().optional(),
}).loose();

export const SearchIssuesResponseSchema = z
  .object({
    issues: z.array(SearchIssueResultSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const EMPTY_SEARCH_ISSUES_RESPONSE: SearchIssuesResponse = {
  issues: [],
  total: 0,
};

const ProjectSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    title: z.string(),
    description: z.string().nullable(),
    icon: z.string().nullable(),
    status: z.string(),
    priority: z.string(),
    lead_type: z.string().nullable(),
    lead_id: z.string().nullable(),
    start_date: z.string().nullable().default(null),
    due_date: z.string().nullable().default(null),
    created_at: z.string(),
    updated_at: z.string(),
    issue_count: z.number().default(0),
    done_count: z.number().default(0),
    resource_count: z.number().default(0),
  })
  .loose();

const SearchProjectResultSchema = ProjectSchema.extend({
  match_source: z.string(),
  matched_snippet: z.string().optional(),
}).loose();

export const SearchProjectsResponseSchema = z
  .object({
    projects: z.array(SearchProjectResultSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const EMPTY_SEARCH_PROJECTS_RESPONSE: SearchProjectsResponse = {
  projects: [],
  total: 0,
};

const IssueAssigneeGroupSchema = z
  .object({
    id: z.string(),
    assignee_type: z.string().nullable(),
    assignee_id: z.string().nullable(),
    issues: z.array(IssueSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const GroupedIssuesResponseSchema = z
  .object({
    groups: z.array(IssueAssigneeGroupSchema).default([]),
  })
  .loose();

export const EMPTY_GROUPED_ISSUES_RESPONSE: GroupedIssuesResponse = {
  groups: [],
};

const IssueTableActorRefSchema = z
  .object({
    type: z.string(),
    id: z.string(),
  })
  .loose();

const IssueTableParentRefSchema = z
  .object({
    id: z.string(),
    number: z.number(),
    identifier: z.string(),
    title: z.string(),
    status: z.string(),
  })
  .loose();

const IssueTableGroupValueSchema = z.discriminatedUnion('kind', [
  z
    .object({
      kind: z.literal('status'),
      status: z.string(),
    })
    .loose(),
  z
    .object({
      kind: z.literal('assignee'),
      actor: IssueTableActorRefSchema.nullable(),
    })
    .loose(),
  z
    .object({
      kind: z.literal('project'),
      project_id: z.string().nullable().optional().default(null),
    })
    .loose(),
  z
    .object({
      kind: z.literal('parent'),
      parent_id: z.string().nullable().optional().default(null),
      parent: IssueTableParentRefSchema.nullable().optional().default(null),
      value_state: z.enum(['value', 'unavailable', 'unset']),
    })
    .loose(),
  z
    .object({
      kind: z.literal('property'),
      property_id: z.string(),
      value: z.union([z.string(), z.boolean(), z.null()]).optional(),
      value_state: z.enum(['value', 'unavailable', 'unset']),
    })
    .loose(),
]);

const IssueTableGroupDescriptorSchema: z.ZodType<IssueTableGroupDescriptor> = z.lazy(() =>
  z
    .object({
      key: z.string(),
      value: IssueTableGroupValueSchema,
      count: z.number(),
      secondary_groups: z.array(IssueTableGroupDescriptorSchema).optional(),
    })
    .loose(),
);

export const IssueTableGroupsResponseSchema = z
  .object({
    query_fingerprint: z.string(),
    total: z.number(),
    groups: z.array(IssueTableGroupDescriptorSchema).default([]),
    next_cursor: z.string().nullable().default(null),
  })
  .loose();

export const EMPTY_ISSUE_TABLE_GROUPS_RESPONSE: IssueTableGroupsResponse = {
  query_fingerprint: '',
  total: 0,
  groups: [],
  next_cursor: null,
};

const IssueTableRowSchema = z
  .object({
    issue: IssueSchema,
    direct_child_count: z.number().default(0),
  })
  .loose();

export const IssueTableRowsResponseSchema = z
  .object({
    query_fingerprint: z.string(),
    group_key: z.string().nullable().default(null),
    parent_id: z.string().nullable().default(null),
    total: z.number(),
    rows: z.array(IssueTableRowSchema).default([]),
    branch_total: z.number(),
    next_cursor: z.string().nullable().default(null),
  })
  .loose();

export const EMPTY_ISSUE_TABLE_ROWS_RESPONSE: IssueTableRowsResponse = {
  query_fingerprint: '',
  group_key: null,
  parent_id: null,
  total: 0,
  rows: [],
  branch_total: 0,
  next_cursor: null,
};

const IssueTableFacetValueSchema = z
  .object({
    key: z.string(),
    count: z.number(),
  })
  .loose();

const IssueTableFacetSchema = z
  .object({
    kind: z.enum(['status', 'priority', 'assignee', 'creator', 'project', 'label', 'property']),
    property_id: z.string().optional(),
    values: z.array(IssueTableFacetValueSchema).default([]),
  })
  .loose();

export const IssueTableFacetsResponseSchema = z
  .object({
    query_fingerprint: z.string(),
    total: z.number(),
    facets: z.array(IssueTableFacetSchema).default([]),
  })
  .loose();

export const EMPTY_ISSUE_TABLE_FACETS_RESPONSE: IssueTableFacetsResponse = {
  query_fingerprint: '',
  total: 0,
  facets: [],
};

const SubscriberSchema = z
  .object({
    issue_id: z.string(),
    user_type: z.string(),
    user_id: z.string(),
    reason: z.string(),
    created_at: z.string(),
  })
  .loose();

export const SubscribersListSchema = z.array(SubscriberSchema);

export const ChildIssuesResponseSchema = z
  .object({
    issues: z.array(IssueSchema).default([]),
  })
  .loose();

export const CloudRuntimeNodeSchema = z
  .object({
    id: z.string(),
    owner_id: z.string(),
    instance_id: z.string(),
    region: z.string(),
    instance_type: z.string(),
    image_id: z.string(),
    subnet_id: z.string(),
    name: z.string(),
    status: z.string(),
    tags: z.record(z.string(), z.string()).default({}),
    metadata: z.record(z.string(), z.unknown()).default({}),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

export const CloudRuntimeNodeListSchema = z.array(CloudRuntimeNodeSchema);

export const EMPTY_CLOUD_RUNTIME_NODE_LIST: CloudRuntimeNode[] = [];

export const EMPTY_CLOUD_RUNTIME_NODE: CloudRuntimeNode = {
  id: '',
  owner_id: '',
  instance_id: '',
  region: '',
  instance_type: '',
  image_id: '',
  subnet_id: '',
  name: '',
  status: '',
  tags: {},
  metadata: {},
  created_at: '',
  updated_at: '',
};

const CostSplitShape = {
  cost_usd_ticks: z.number().optional(),
  uncosted_input_tokens: z.number().optional(),
  uncosted_output_tokens: z.number().optional(),
  uncosted_cache_read_tokens: z.number().optional(),
  uncosted_cache_write_tokens: z.number().optional(),
};

const DashboardUsageDailySchema = z
  .object({
    date: z.string().default(''),
    provider: z.string().default(''),
    model: z.string().default(''),
    input_tokens: z.number().default(0),
    output_tokens: z.number().default(0),
    cache_read_tokens: z.number().default(0),
    cache_write_tokens: z.number().default(0),
    ...CostSplitShape,
    task_count: z.number().default(0),
  })
  .loose();

export const DashboardUsageDailyListSchema = z.array(DashboardUsageDailySchema);

const DashboardUsageByAgentSchema = z
  .object({
    agent_id: z.string().default(''),
    provider: z.string().default(''),
    model: z.string().default(''),
    input_tokens: z.number().default(0),
    output_tokens: z.number().default(0),
    cache_read_tokens: z.number().default(0),
    cache_write_tokens: z.number().default(0),
    ...CostSplitShape,
    task_count: z.number().default(0),
  })
  .loose();

export const DashboardUsageByAgentListSchema = z.array(DashboardUsageByAgentSchema);

const DashboardAgentRunTimeSchema = z
  .object({
    agent_id: z.string().default(''),
    total_seconds: z.number().default(0),
    task_count: z.number().default(0),
    failed_count: z.number().default(0),
  })
  .loose();

export const DashboardAgentRunTimeListSchema = z.array(DashboardAgentRunTimeSchema);

const DashboardRunTimeDailySchema = z
  .object({
    date: z.string().default(''),
    total_seconds: z.number().default(0),
    task_count: z.number().default(0),
    failed_count: z.number().default(0),
  })
  .loose();

export const DashboardRunTimeDailyListSchema = z.array(DashboardRunTimeDailySchema);

const DashboardFailureDailySchema = z
  .object({
    date: z.string().default(''),
    failure_reason: z.string().default(''),
    task_count: z.number().default(0),
  })
  .loose();

export const DashboardFailureDailyListSchema = z.array(DashboardFailureDailySchema);

const DashboardFailureByAgentSchema = z
  .object({
    agent_id: z.string().default(''),
    failure_reason: z.string().default(''),
    task_count: z.number().default(0),
  })
  .loose();

export const DashboardFailureByAgentListSchema = z.array(DashboardFailureByAgentSchema);

const RuntimeUsageSchema = z
  .object({
    runtime_id: z.string().default(''),
    date: z.string().default(''),
    provider: z.string().default(''),
    model: z.string().default(''),
    input_tokens: z.number().default(0),
    output_tokens: z.number().default(0),
    cache_read_tokens: z.number().default(0),
    cache_write_tokens: z.number().default(0),
    ...CostSplitShape,
  })
  .loose();

export const RuntimeUsageListSchema = z.array(RuntimeUsageSchema);

const RuntimeHourlyActivitySchema = z
  .object({
    hour: z.number().default(0),
    count: z.number().default(0),
  })
  .loose();

export const RuntimeHourlyActivityListSchema = z.array(RuntimeHourlyActivitySchema);

const RuntimeUsageByAgentSchema = z
  .object({
    agent_id: z.string().default(''),
    provider: z.string().default(''),
    model: z.string().default(''),
    input_tokens: z.number().default(0),
    output_tokens: z.number().default(0),
    cache_read_tokens: z.number().default(0),
    cache_write_tokens: z.number().default(0),
    ...CostSplitShape,
    task_count: z.number().default(0),
  })
  .loose();

export const RuntimeUsageByAgentListSchema = z.array(RuntimeUsageByAgentSchema);

const RuntimeUsageByHourSchema = z
  .object({
    hour: z.number().default(0),
    model: z.string().default(''),
    input_tokens: z.number().default(0),
    output_tokens: z.number().default(0),
    cache_read_tokens: z.number().default(0),
    cache_write_tokens: z.number().default(0),
    ...CostSplitShape,
    task_count: z.number().default(0),
  })
  .loose();

export const RuntimeUsageByHourListSchema = z.array(RuntimeUsageByHourSchema);

const AttributionUserSchema = z
  .object({
    id: z.string().default(''),
    name: z.string().optional(),
    email: z.string().optional(),
    avatar_url: z.string().optional(),
  })
  .loose();

const TaskEvidenceSchema = z
  .object({
    kind: z.string().default(''),
    ref_id: z.string().default(''),
  })
  .loose();

const TaskAttributionSchema = z
  .object({
    source: z.string().default('unattributed'),
    precise: z.boolean().default(false),
    initiator: AttributionUserSchema.optional(),
    originator: AttributionUserSchema.optional(),
    evidence: TaskEvidenceSchema.optional(),
    rule_version_id: z.string().optional(),
    delegated_from_task_id: z.string().optional(),
    retry_of_task_id: z.string().optional(),
    rerun_of_task_id: z.string().optional(),
  })
  .loose();

const OptionalStringArraySchema = z.preprocess(
  (value) =>
    Array.isArray(value) && value.every((item) => typeof item === 'string') ? value : undefined,
  z.array(z.string()).optional(),
);

export const AgentTaskSchema = z
  .object({
    id: z.string(),
    agent_id: z.string().default(''),
    runtime_id: z.string().default(''),
    issue_id: z.string().default(''),
    status: z.string().default('cancelled'),
    priority: z.number().default(0),
    dispatched_at: z.string().nullable().default(null),
    started_at: z.string().nullable().default(null),
    completed_at: z.string().nullable().default(null),
    result: z.unknown().default(null),
    error: z.string().nullable().default(null),
    failure_reason: z.string().optional(),
    created_at: z.string().default(''),
    chat_session_id: z.string().optional(),
    autopilot_run_id: z.string().optional(),
    parent_task_id: z.string().optional(),
    attempt: z.number().optional(),
    trigger_comment_id: z.string().optional(),
    coalesced_comment_ids: OptionalStringArraySchema,
    delivered_comment_ids: OptionalStringArraySchema,
    trigger_summary: z.string().optional(),
    handoff_note: z.string().optional(),
    kind: z.string().optional(),
    work_dir: z.string().optional(),
    relative_work_dir: z.string().optional(),
    attribution: TaskAttributionSchema.optional(),
  })
  .loose();

export const AgentTaskListSchema = z.array(AgentTaskSchema);

export const AgentEnvResponseSchema = z
  .object({
    agent_id: z.string(),
    custom_env: z.record(z.string(), z.string()),
    values_masked: z.boolean().optional(),
  })
  .loose();

const CancelledChatMessageSchema = z
  .object({
    chat_session_id: z.string(),
    message_id: z.string(),
    content: z.string(),
    restore_to_input: z.boolean().default(false),
    attachments: z.array(AttachmentSchema).optional(),
  })
  .loose();

export const CancelTaskResponseSchema = AgentTaskSchema.extend({
  cancelled_chat_message: CancelledChatMessageSchema.nullish().transform(
    (value) => value ?? undefined,
  ),
}).loose();

const ChatDraftRestoreSchema = z
  .object({
    id: z.string(),
    chat_session_id: z.string(),
    task_id: z.string().optional(),
    content: z.string().default(''),
    attachments: z.array(AttachmentSchema).optional(),
    created_at: z.string().optional(),
  })
  .loose();

export const ChatDraftRestoresResponseSchema = z
  .object({
    restores: z.array(ChatDraftRestoreSchema).default([]),
  })
  .loose();

export const EMPTY_CHAT_DRAFT_RESTORES: ChatDraftRestoresResponse = {
  restores: [],
};

export const EMPTY_CANCEL_TASK_RESPONSE: CancelTaskResponse = {
  id: '',
  agent_id: '',
  runtime_id: '',
  issue_id: '',
  status: 'cancelled',
  priority: 0,
  dispatched_at: null,
  started_at: null,
  completed_at: null,
  result: null,
  error: null,
  created_at: '',
};

const AgentTemplateSkillRefSchema = z
  .object({
    source_url: z.string(),
    cached_name: z.string().default(''),
    cached_description: z.string().default(''),
  })
  .loose();

const AgentTemplateSummarySchemaBase = z
  .object({
    slug: z.string(),
    name: z.string(),
    description: z.string().default(''),
    category: z.string().optional(),
    icon: z.string().optional(),
    accent: z.string().optional(),
    skills: z.array(AgentTemplateSkillRefSchema).default([]),
  })
  .loose();

export const AgentTemplateSummarySchema = AgentTemplateSummarySchemaBase;

export const AgentTemplateSummaryListSchema = z.union([
  z.array(AgentTemplateSummarySchemaBase),
  z
    .object({ templates: z.array(AgentTemplateSummarySchemaBase).default([]) })
    .loose()
    .transform((v) => v.templates),
]);

export const EMPTY_AGENT_TEMPLATE_SUMMARY_LIST: AgentTemplateSummary[] = [];

export const AgentTemplateSchema = AgentTemplateSummarySchemaBase.extend({
  instructions: z.string().default(''),
}).loose();

export const EMPTY_AGENT_TEMPLATE_DETAIL: AgentTemplate = {
  slug: '',
  name: '',
  description: '',
  skills: [],
  instructions: '',
};

export const AgentPermissionModeSchema = z.enum(['private', 'public_to']).catch('private');

export const AgentInvocationTargetSchema = z
  .object({
    target_type: z.string(),
    target_id: z
      .string()
      .nullable()
      .optional()
      .transform((v) => v ?? null),
  })
  .loose();

export const AgentInvocationTargetsSchema = z.array(AgentInvocationTargetSchema).default([]);

const MinimalAgentSchema = z
  .object({
    id: z.string(),
    permission_mode: AgentPermissionModeSchema.optional(),
    invocation_targets: AgentInvocationTargetsSchema.optional(),
  })
  .loose();

export const CreateAgentFromTemplateResponseSchema = z
  .object({
    agent: MinimalAgentSchema,
    imported_skill_ids: z.array(z.string()).default([]),
    reused_skill_ids: z.array(z.string()).default([]),
  })
  .loose();

export const EMPTY_CREATE_AGENT_FROM_TEMPLATE_RESPONSE: CreateAgentFromTemplateResponse = {
  agent: { id: '' } as Agent,
  imported_skill_ids: [],
  reused_skill_ids: [],
};

export const AgentBuilderSessionSchema = z
  .object({
    session_id: z.string(),
    builder_agent_id: z.string(),
    runtime_id: z.string(),
  })
  .loose();

export const EMPTY_AGENT_BUILDER_SESSION: AgentBuilderSession = {
  session_id: '',
  builder_agent_id: '',
  runtime_id: '',
};

export const AgentBuilderRuntimeSwitchSchema = z
  .object({
    runtime_id: z.string(),
  })
  .loose();

export const agentBuilderRuntimeSwitchFallback = (
  requestedRuntimeID: string,
): AgentBuilderRuntimeSwitch => ({ runtime_id: requestedRuntimeID });

const SquadMemberPreviewSchema = z
  .object({
    member_type: z.string(),
    member_id: z.string(),
    role: z.string().default(''),
  })
  .loose();

export const SquadSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    name: z.string(),
    description: z.string().default(''),
    instructions: z.string().default(''),
    avatar_url: z
      .string()
      .nullable()
      .optional()
      .transform((v) => v ?? null),
    leader_id: z.string(),
    creator_id: z.string(),
    created_at: z.string(),
    updated_at: z.string(),
    archived_at: z
      .string()
      .nullable()
      .optional()
      .transform((v) => v ?? null),
    archived_by: z
      .string()
      .nullable()
      .optional()
      .transform((v) => v ?? null),
    member_count: z.number().default(0),
    member_preview: z.array(SquadMemberPreviewSchema).default([]),
  })
  .loose();

export const SquadListSchema = z.array(SquadSchema);
export const EMPTY_SQUAD_LIST: Squad[] = [];
export const EMPTY_SQUAD: Squad = {
  id: '',
  workspace_id: '',
  name: '',
  description: '',
  instructions: '',
  avatar_url: null,
  leader_id: '',
  creator_id: '',
  created_at: '',
  updated_at: '',
  archived_at: null,
  archived_by: null,
  member_count: 0,
  member_preview: [],
};

const SquadActiveIssueBriefSchema = z
  .object({
    issue_id: z.string(),
    identifier: z.string(),
    title: z.string(),
    issue_status: z.string(),
  })
  .loose();

const SquadMemberStatusSchema = z
  .object({
    member_type: z.string(),
    member_id: z.string(),
    status: z
      .string()
      .nullable()
      .optional()
      .transform((v) => v ?? null),
    active_issues: z.array(SquadActiveIssueBriefSchema).default([]),
    last_active_at: z
      .string()
      .nullable()
      .optional()
      .transform((v) => v ?? null),
  })
  .loose();

export const SquadMemberStatusListResponseSchema = z
  .object({
    members: z.array(SquadMemberStatusSchema).default([]),
  })
  .loose();

export const EMPTY_SQUAD_MEMBER_STATUS_LIST = { members: [] };

export const DuplicateIssueErrorBodySchema = z
  .object({
    code: z.literal('active_duplicate_issue'),
    error: z.string().optional(),
    issue: z
      .object({
        id: z.string(),
        identifier: z.string(),
        title: z.string(),
      })
      .loose(),
  })
  .loose();

export interface DuplicateIssueErrorBody {
  code: 'active_duplicate_issue';
  error?: string;
  issue: {
    id: string;
    identifier: string;
    title: string;
  };
}

const WebhookDeliverySchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    autopilot_id: z.string(),
    trigger_id: z.string(),
    provider: z.string(),
    event: z.string(),
    dedupe_key: z.string().nullable(),
    dedupe_source: z.string().nullable(),
    signature_status: z.string(),
    status: z.string(),
    attempt_count: z.number().default(0),
    dispatch_attempts: z.number().default(0),
    available_at: z.string().default(''),
    content_type: z.string().nullable(),
    response_status: z.number().nullable(),
    autopilot_run_id: z.string().nullable(),
    replayed_from_delivery_id: z.string().nullable(),
    error: z.string().nullable(),
    received_at: z.string(),
    last_attempt_at: z.string(),
    created_at: z.string(),
    selected_headers: z.record(z.string(), z.unknown()).nullable().optional(),
    raw_body: z.string().nullable().optional(),
    response_body: z.string().nullable().optional(),
  })
  .loose();

export const ListWebhookDeliveriesResponseSchema = z
  .object({
    deliveries: z.array(WebhookDeliverySchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const WebhookDeliveryResponseSchema = WebhookDeliverySchema;

export const EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE: ListWebhookDeliveriesResponse = {
  deliveries: [],
  total: 0,
};

const AutopilotListItemSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    title: z.string(),
    description: z.string().nullable().optional(),
    project_id: z.string().nullable().optional(),
    assignee_type: z.string().default('agent'),
    assignee_id: z.string(),
    status: z.string(),
    execution_mode: z.string(),
    issue_title_template: z.string().nullable().optional(),
    created_by_type: z.string(),
    created_by_id: z.string(),
    last_run_at: z.string().nullable().optional(),
    created_at: z.string(),
    updated_at: z.string(),
    trigger_kinds: z.array(z.string()).optional(),
    next_run_at: z.string().nullable().optional(),
    last_run_status: z.string().nullable().optional(),
    is_template: z.boolean().optional().catch(undefined),
    can_write: z.boolean().optional(),
    can_manage_access: z.boolean().optional(),
  })
  .loose();

export const ListAutopilotsResponseSchema = z
  .object({
    autopilots: z.array(AutopilotListItemSchema).default([]),
    total: z.number().default(0),
  })
  .loose();

export const EMPTY_LIST_AUTOPILOTS_RESPONSE = {
  autopilots: [],
  total: 0,
};

export const AutopilotRunSchema = z
  .object({
    id: z.string().default(''),
    autopilot_id: z.string().default(''),
    trigger_id: z.string().nullable().default(null),
    source: z.string().default('manual'),
    status: z.string().default('failed'),
    issue_id: z.string().nullable().default(null),
    task_id: z.string().nullable().default(null),
    triggered_at: z.string().default(''),
    completed_at: z.string().nullable().default(null),
    failure_reason: z.string().nullable().default(null),
    reason_code: z.string().optional(),
    trigger_payload: z.unknown().default(null),
    result: z.unknown().default(null),
    created_at: z.string().default(''),
  })
  .loose();

export const FALLBACK_AUTOPILOT_RUN: AutopilotRun = {
  id: '',
  autopilot_id: '',
  trigger_id: null,
  source: 'manual',
  status: 'failed',
  issue_id: null,
  task_id: null,
  triggered_at: '',
  completed_at: null,
  failure_reason: null,
  trigger_payload: null,
  result: null,
  created_at: '',
};

export const CronPreviewResponseSchema = z
  .object({
    next_runs: z.array(z.string()),
  })
  .loose();

export const UNREADABLE_CRON_PREVIEW_RESPONSE: CronPreviewResponse = {
  next_runs: null,
};

export const EMPTY_WEBHOOK_DELIVERY: WebhookDelivery = {
  id: '',
  workspace_id: '',
  autopilot_id: '',
  trigger_id: '',
  provider: '',
  event: '',
  dedupe_key: null,
  dedupe_source: null,
  signature_status: 'not_required',
  status: 'queued',
  attempt_count: 0,
  dispatch_attempts: 0,
  available_at: '',
  content_type: null,
  response_status: null,
  autopilot_run_id: null,
  replayed_from_delivery_id: null,
  error: null,
  received_at: '',
  last_attempt_at: '',
  created_at: '',
};

export const UserSchema = z
  .object({
    id: z.string(),
    name: z.string().default(''),
    email: z.string().default(''),
    avatar_url: z.string().nullable().default(null),
    onboarded_at: z.string().nullable().default(null),
    onboarding_questionnaire: z.record(z.string(), z.unknown()).default({}),
    starter_content_state: z.string().nullable().default(null),
    language: z.string().nullable().default(null),
    profile_description: z.string().default(''),
    timezone: z.string().nullable().default(null),
    created_at: z.string().default(''),
    updated_at: z.string().default(''),
  })
  .loose();

export const EMPTY_USER: User = {
  id: '',
  name: '',
  email: '',
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  starter_content_state: null,
  language: null,
  profile_description: '',
  timezone: null,
  created_at: '',
  updated_at: '',
};

export const LoginResponseSchema = z
  .object({
    token: z.string().default(''),
    user: UserSchema,
  })
  .loose();

export const EMPTY_LOGIN_RESPONSE = {
  token: '',
  user: EMPTY_USER,
};

export const AUTH_METHODS = ['email', 'oidc', 'ldap'] as const;
export type AuthMethod = (typeof AUTH_METHODS)[number];

export const AuthMethodsResponseSchema = z
  .object({
    methods: z
      .array(z.enum(AUTH_METHODS).nullable().catch(null))
      .default([])
      .transform((list) => list.filter((m): m is AuthMethod => m !== null)),
    oidc_display_name: z.string().default(''),
    ldap_display_name: z.string().default(''),
  })
  .loose();

export type AuthMethodsResponse = z.infer<typeof AuthMethodsResponseSchema>;

export const EMPTY_AUTH_METHODS: AuthMethodsResponse = {
  methods: ['email'],
  oidc_display_name: '',
  ldap_display_name: '',
};

export const InboxUnreadSummarySchema = z.array(
  z
    .object({
      workspace_id: z.string(),
      count: z.number(),
    })
    .loose(),
);

export const EMPTY_INBOX_UNREAD_SUMMARY: InboxWorkspaceUnread[] = [];

export const InboxItemListSchema = z.array(
  z
    .object({
      id: z.string(),
      workspace_id: z.string(),
      recipient_type: z.string(),
      recipient_id: z.string(),
      type: z.string(),
      severity: z.string(),
      issue_id: z.string().nullish(),
      title: z.string(),
      body: z.string().nullish(),
      read: z.boolean(),
      archived: z.boolean(),
      created_at: z.string(),
    })
    .loose(),
);

export const EMPTY_INBOX_ITEMS: InboxItem[] = [];

export const BillingBalanceSchema = z
  .object({
    owner_id: z.string(),
    balance_micro: z.number(),
    balance_credit: z.number(),
    updated_at: z.string(),
  })
  .loose();

export const EMPTY_BILLING_BALANCE: BillingBalance = {
  owner_id: '',
  balance_micro: 0,
  balance_credit: 0,
  updated_at: '',
};

export const BillingTransactionSchema = z
  .object({
    id: z.string(),
    owner_id: z.string(),
    idempotency_key: z.string().default(''),
    tx_type: z.string(),
    source: z.string(),
    amount_micro: z.number(),
    balance_after: z.number(),
    reference_id: z.string().default(''),
    description: z.string().default(''),
    metadata: z.record(z.string(), z.unknown()).default({}),
    created_at: z.string(),
  })
  .loose();

export const BillingTransactionsPageSchema = z
  .object({
    items: z.array(BillingTransactionSchema).default([]),
    total: z.number().default(0),
    page: z.number().default(1),
    page_size: z.number().default(20),
  })
  .loose();

export const EMPTY_BILLING_TRANSACTIONS_PAGE: BillingTransactionsPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingBatchSchema = z
  .object({
    id: z.string(),
    owner_id: z.string(),
    source_tx_id: z.string().default(''),
    source_type: z.string(),
    total_micro: z.number(),
    remaining_micro: z.number(),
    expires_at: z.string().nullable().optional(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

export const BillingBatchesPageSchema = z
  .object({
    items: z.array(BillingBatchSchema).default([]),
    total: z.number().default(0),
    page: z.number().default(1),
    page_size: z.number().default(20),
  })
  .loose();

export const EMPTY_BILLING_BATCHES_PAGE: BillingBatchesPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingTopupSchema = z
  .object({
    id: z.string(),
    owner_id: z.string(),
    amount_cents: z.number(),
    currency: z.string().default('usd'),
    credits: z.number(),
    bonus_credits: z.number().default(0),
    status: z.string(),
    tier_id: z.string().default(''),
    stripe_checkout_id: z.string().default(''),
    purchase_batch_id: z.string().optional(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

export const BillingTopupsPageSchema = z
  .object({
    items: z.array(BillingTopupSchema).default([]),
    total: z.number().default(0),
    page: z.number().default(1),
    page_size: z.number().default(20),
  })
  .loose();

export const EMPTY_BILLING_TOPUPS_PAGE: BillingTopupsPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingPriceTierSchema = z
  .object({
    id: z.string(),
    display_name: z.string().default(''),
    amount_cents: z.number(),
    credits: z.number(),
    bonus_credits: z.number().optional(),
    bonus_expires_in: z.string().optional(),
  })
  .loose();

export const BillingPriceTierListSchema = z.array(BillingPriceTierSchema);

export const EMPTY_BILLING_PRICE_TIER_LIST: BillingPriceTier[] = [];

export const CreateBillingCheckoutSessionResponseSchema = z
  .object({
    order_id: z.string(),
    session_id: z.string(),
    url: z.string(),
  })
  .loose();

export const EMPTY_CREATE_BILLING_CHECKOUT_SESSION_RESPONSE: CreateBillingCheckoutSessionResponse =
  {
    order_id: '',
    session_id: '',
    url: '',
  };

export const BillingCheckoutSessionStatusSchema = z
  .object({
    order_id: z.string(),
    status: z.string(),
    amount_cents: z.number(),
    credits: z.number(),
    bonus_credits: z.number().default(0),
    currency: z.string().default('usd'),
    tier_id: z.string().default(''),
  })
  .loose();

export const EMPTY_BILLING_CHECKOUT_SESSION_STATUS: BillingCheckoutSessionStatus = {
  order_id: '',
  status: 'pending',
  amount_cents: 0,
  credits: 0,
  bonus_credits: 0,
  currency: 'usd',
  tier_id: '',
};

export const CreateBillingPortalSessionResponseSchema = z
  .object({
    url: z.string(),
  })
  .loose();

export const EMPTY_CREATE_BILLING_PORTAL_SESSION_RESPONSE: CreateBillingPortalSessionResponse = {
  url: '',
};

export const MemberWithUserSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string().default(''),
    user_id: z.string().default(''),
    role: z.string().default('member'),
    created_at: z.string().default(''),
    name: z.string().default(''),
    email: z.string().default(''),
    avatar_url: z.string().nullable().default(null),
    perimeter_access: z.boolean().default(false),
  })
  .loose();

export const MemberWithUserListSchema = z.array(MemberWithUserSchema);

export const EMPTY_MEMBER_WITH_USER: MemberWithUser = {
  id: '',
  workspace_id: '',
  user_id: '',
  role: 'member',
  created_at: '',
  name: '',
  email: '',
  avatar_url: null,
  perimeter_access: false,
};

export const EMPTY_MEMBER_WITH_USER_LIST: MemberWithUser[] = [];

export const WorkspaceTemplateSchema = z
  .object({
    key: z.string(),
    name: z.string().default(''),
    description: z.string().default(''),
  })
  .loose();

export const WorkspaceTemplateListSchema = z.union([
  z.array(WorkspaceTemplateSchema),
  z
    .object({ templates: z.array(WorkspaceTemplateSchema).default([]) })
    .loose()
    .transform((v) => v.templates),
]);

export const EMPTY_WORKSPACE_TEMPLATE_LIST: WorkspaceTemplate[] = [];

export const WorkspaceCapabilitySchema = z
  .object({
    key: z.string().default(''),
    title: z.string().default(''),
    body: z.string().default(''),
  })
  .loose();

export const WorkspaceSampleTaskSchema = z
  .object({
    key: z.string().default(''),
    title: z.string().default(''),
    prompt: z.string().default(''),
    requires: z
      .array(z.string())
      .nullish()
      .transform((list) => list ?? []),
  })
  .loose();

export const WorkspaceCapabilitiesSchema = z
  .object({
    template_key: z.string().default(''),
    role_name: z.string().default(''),
    role_summary: z.string().default(''),
    capabilities: z
      .array(WorkspaceCapabilitySchema)
      .nullish()
      .transform((list) => (list ?? []).filter((c) => c.title.trim() !== '')),
    sample_tasks: z
      .array(WorkspaceSampleTaskSchema)
      .nullish()
      .transform((list) =>
        (list ?? []).filter((task) => task.title.trim() !== '' && task.prompt.trim() !== ''),
      ),
  })
  .loose();

export const EMPTY_WORKSPACE_CAPABILITIES: WorkspaceCapabilities = {
  template_key: '',
  role_name: '',
  role_summary: '',
  capabilities: [],
  sample_tasks: [],
};

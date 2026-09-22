// Локальные zod-схемы и fallback-значения для эндпоинтов, ещё не описанных
// в @goosar/core/api/schemas. Намеренно нестрогие.
import { z } from "zod";
import type {
  Agent,
  AgentInvocationTarget,
  AgentTask,
  Attachment,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  Comment,
  InboxItem,
  IssueLabelsResponse,
  Label,
  ListLabelsResponse,
  ListProjectResourcesResponse,
  ListProjectsResponse,
  MemberWithUser,
  PinnedItem,
  Project,
  ProjectResource,
  RuntimeDevice,
  SearchIssuesResponse,
  SearchProjectsResponse,
  SendChatMessageResponse,
  Squad,
  TaskMessagePayload,
  User,
  Workspace,
} from "@goosar/core/types";
import { IssueSchema } from "@goosar/core/api/schemas";

export const AttachmentSchema: z.ZodType<Attachment> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  issue_id: z.string().nullable().default(null),
  comment_id: z.string().nullable().default(null),
  chat_session_id: z.string().nullable().default(null),
  chat_message_id: z.string().nullable().default(null),
  uploader_type: z.string().default(""),
  uploader_id: z.string().default(""),
  filename: z.string(),
  url: z.string(),
  download_url: z.string().default(""),
  markdown_url: z.string().default(""),
  content_type: z.string().default(""),
  size_bytes: z.number().default(0),
  created_at: z.string().default(""),
}).loose();

export const AttachmentListSchema = z.array(AttachmentSchema).default([]);
export const EMPTY_ATTACHMENT_LIST: Attachment[] = [];

export const CommentSchema = z.object({
  id: z.string(),
  issue_id: z.string().default(""),
  author_type: z.string().default("member"),
  author_id: z.string().default(""),
  content: z.string().default(""),
  type: z.string().default("comment"),
  parent_id: z.string().nullable().default(null),
  reactions: z.array(z.unknown()).default([]),
  attachments: z.array(z.unknown()).default([]),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  resolved_at: z.string().nullable().default(null),
  resolved_by_type: z.string().nullable().default(null),
  resolved_by_id: z.string().nullable().default(null),
  source_task_id: z.string().nullable().optional(),
}).loose() as unknown as z.ZodType<Comment>;

export const EMPTY_COMMENT: Comment = {
  id: "",
  issue_id: "",
  author_type: "member",
  author_id: "",
  content: "",
  type: "comment",
  parent_id: null,
  reactions: [],
  attachments: [],
  created_at: "",
  updated_at: "",
  resolved_at: null,
  resolved_by_type: null,
  resolved_by_id: null,
};

export const NotificationPreferenceResponseSchema = z.object({
  workspace_id: z.string().default(""),
  preferences: z.record(z.string(), z.string()).default({}),
}).loose();
export const EMPTY_NOTIFICATION_PREFERENCES = {
  workspace_id: "",
  preferences: {},
} as const;

const LabelSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  color: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const ListLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_LABELS_RESPONSE: ListLabelsResponse = {
  labels: [],
  total: 0,
};

export const IssueLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
}).loose();

export const EMPTY_ISSUE_LABELS_RESPONSE: IssueLabelsResponse = {
  labels: [],
};

export const ProjectSchema = z.object({
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
}).loose();

export const ListProjectsResponseSchema = z.object({
  projects: z.array(ProjectSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROJECTS_RESPONSE: ListProjectsResponse = {
  projects: [],
  total: 0,
};

export const EMPTY_PROJECT: Project = {
  id: "",
  workspace_id: "",
  title: "",
  description: null,
  icon: null,
  status: "planned",
  priority: "none",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: null,
  created_at: "",
  updated_at: "",
  issue_count: 0,
  done_count: 0,
  resource_count: 0,
};

const ProjectResourceSchema = z.object({
  id: z.string(),
  project_id: z.string(),
  workspace_id: z.string(),
  resource_type: z.string(),
  resource_ref: z.unknown(),
  label: z.string().nullable(),
  position: z.number().default(0),
  created_at: z.string(),
  created_by: z.string().nullable(),
}).loose();

export const ListProjectResourcesResponseSchema = z.object({
  resources: z.array(ProjectResourceSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROJECT_RESOURCES_RESPONSE: ListProjectResourcesResponse = {
  resources: [],
  total: 0,
};

export const ChatSessionSchema: z.ZodType<ChatSession> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  creator_id: z.string().default(""),
  title: z.string().default(""),
  status: z.enum(["active", "archived"]).catch("active"),
  has_unread: z.boolean().default(false),
  unread_count: z.number().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const ChatSessionListSchema = z.array(ChatSessionSchema).default([]);

export const EMPTY_CHAT_SESSION_LIST: ChatSession[] = [];

export const ChatMessageSchema: z.ZodType<ChatMessage> = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  role: z.enum(["user", "assistant"]).catch("assistant"),
  content: z.string().default(""),
  task_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  failure_reason: z.string().nullable().optional(),
  elapsed_ms: z.number().nullable().optional(),
}).loose();

export const ChatMessageListSchema = z.array(ChatMessageSchema).default([]);

export const EMPTY_CHAT_MESSAGE_LIST: ChatMessage[] = [];

export const ChatPendingTaskSchema: z.ZodType<ChatPendingTask> = z.object({
  task_id: z.string().optional(),
  status: z.string().optional(),
  created_at: z.string().optional(),
}).loose();

export const EMPTY_CHAT_PENDING_TASK: ChatPendingTask = {};

export const SendChatMessageResponseSchema: z.ZodType<SendChatMessageResponse> = z.object({
  message_id: z.string(),
  task_id: z.string(),
  created_at: z.string().default(""),
}).loose();

export const TaskMessagePayloadSchema: z.ZodType<TaskMessagePayload> = z.object({
  task_id: z.string(),
  issue_id: z.string().default(""),
  chat_session_id: z.string().optional(),
  seq: z.number().default(0),
  type: z
    .enum(["text", "thinking", "tool_use", "tool_result", "error"])
    .catch("text"),
  tool: z.string().optional(),
  content: z.string().optional(),
  input: z.record(z.string(), z.unknown()).optional(),
  output: z.string().optional(),
  created_at: z.string().optional(),
}).loose();

export const TaskMessageListSchema = z.array(TaskMessagePayloadSchema).default([]);

export const EMPTY_TASK_MESSAGE_LIST: TaskMessagePayload[] = [];

const SearchIssueResultSchema = IssueSchema.safeExtend({
  match_source: z.enum(["title", "description", "comment"]).catch("title"),
  matched_snippet: z.string().optional(),
});

export const SearchIssuesResponseSchema = z.object({
  issues: z.array(SearchIssueResultSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_SEARCH_ISSUES_RESPONSE: SearchIssuesResponse = {
  issues: [],
  total: 0,
};

const SearchProjectResultSchema = ProjectSchema.safeExtend({
  match_source: z.enum(["title", "description"]).catch("title"),
  matched_snippet: z.string().optional(),
});

export const SearchProjectsResponseSchema = z.object({
  projects: z.array(SearchProjectResultSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_SEARCH_PROJECTS_RESPONSE: SearchProjectsResponse = {
  projects: [],
  total: 0,
};

export const AgentTaskSchema: z.ZodType<AgentTask> = z.object({
  id: z.string(),
  agent_id: z.string().default(""),
  runtime_id: z.string().default(""),
  issue_id: z.string().default(""),
  status: z
    .enum(["queued", "dispatched", "running", "completed", "failed", "cancelled"])
    .catch("queued"),
  priority: z.number().default(0),
  dispatched_at: z.string().nullable().default(null),
  started_at: z.string().nullable().default(null),
  completed_at: z.string().nullable().default(null),
  result: z.unknown().default(null),
  error: z.string().nullable().default(null),
  failure_reason: z
    .enum(["agent_error", "timeout", "runtime_offline", "runtime_recovery", "manual", ""])
    .optional()
    .catch("")
    .transform((v) => (v === "" ? undefined : v)),
  created_at: z.string().default(""),
  chat_session_id: z.string().optional(),
  autopilot_run_id: z.string().optional(),
  parent_task_id: z.string().optional(),
  attempt: z.number().optional(),
  trigger_comment_id: z.string().optional(),
  trigger_summary: z.string().optional(),
  kind: z.enum(["comment", "autopilot", "chat", "quick_create", "direct"]).optional().catch("direct"),
  work_dir: z.string().optional(),
}).loose();

export const AgentTaskListSchema = z.array(AgentTaskSchema).default([]);

export const ActiveTasksResponseSchema = z.object({
  tasks: z.array(AgentTaskSchema).default([]),
}).loose();

export interface ActiveTasksResponse {
  tasks: AgentTask[];
}

export const EMPTY_AGENT_TASK_LIST: AgentTask[] = [];
export const EMPTY_ACTIVE_TASKS_RESPONSE: ActiveTasksResponse = { tasks: [] };

export const UserSchema: z.ZodType<User> = z.object({
  id: z.string(),
  name: z.string().default(""),
  email: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  onboarded_at: z.string().nullable().default(null),
  onboarding_questionnaire: z.record(z.string(), z.unknown()).default({}),
  starter_content_state: z.string().nullable().default(null),
  language: z.string().nullable().default(null),
  profile_description: z.string().default(""),
  timezone: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_USER: User = {
  id: "",
  name: "",
  email: "",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  starter_content_state: null,
  language: null,
  profile_description: "",
  timezone: null,
  created_at: "",
  updated_at: "",
};

export const WorkspaceSchema: z.ZodType<Workspace> = z.object({
  id: z.string(),
  name: z.string().default(""),
  slug: z.string().default(""),
  description: z.string().nullable().default(null),
  context: z.string().nullable().default(null),
  settings: z.record(z.string(), z.unknown()).default({}),
  repos: z.array(z.object({ url: z.string() }).loose()).default([]),
  issue_prefix: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const WorkspaceListSchema = z.array(WorkspaceSchema).default([]);
export const EMPTY_WORKSPACE_LIST: Workspace[] = [];

export const PinnedItemSchema: z.ZodType<PinnedItem> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  user_id: z.string().default(""),
  item_type: z.enum(["issue", "project"]).catch("issue"),
  item_id: z.string(),
  position: z.number().default(0),
  created_at: z.string().default(""),
}).loose();

export const PinListSchema = z.array(PinnedItemSchema).default([]);
export const EMPTY_PIN_LIST: PinnedItem[] = [];

const InboxItemSchema: z.ZodType<InboxItem> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  recipient_type: z.enum(["member", "agent"]).catch("member"),
  recipient_id: z.string().default(""),
  actor_type: z
    .enum(["member", "agent", "system"])
    .nullable()
    .catch(null),
  actor_id: z.string().nullable().default(null),
  type: z.string() as unknown as z.ZodType<InboxItem["type"]>,
  severity: z
    .enum(["action_required", "attention", "info"])
    .catch("info"),
  issue_id: z.string().nullable().default(null),
  title: z.string().default(""),
  body: z.string().nullable().default(null),
  issue_status: z.string().nullable().default(null) as unknown as z.ZodType<
    InboxItem["issue_status"]
  >,
  read: z.boolean().default(false),
  archived: z.boolean().default(false),
  created_at: z.string().default(""),
  details: z.record(z.string(), z.string()).nullable().default(null),
}).loose();

export const InboxListSchema = z.array(InboxItemSchema).default([]);
export const EMPTY_INBOX_LIST: InboxItem[] = [];

export const MemberWithUserSchema: z.ZodType<MemberWithUser> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  user_id: z.string().default(""),
  role: z.enum(["owner", "admin", "member"]).catch("member"),
  created_at: z.string().default(""),
  name: z.string().default(""),
  email: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
}).loose();

export const MemberListSchema = z.array(MemberWithUserSchema).default([]);
export const EMPTY_MEMBER_LIST: MemberWithUser[] = [];

const AgentInvocationTargetSchema: z.ZodType<AgentInvocationTarget> = z
  .object({
    target_type: z.enum(["workspace", "member", "team"]).catch("team"),
    target_id: z
      .string()
      .nullable()
      .optional()
      .catch(null)
      .transform((v) => v ?? null),
  })
  .loose();

export const AgentSchema: z.ZodType<Agent> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  runtime_id: z.string().default(""),
  name: z.string().default(""),
  description: z.string().default(""),
  instructions: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  runtime_mode: z.string().catch("daemon") as unknown as z.ZodType<
    Agent["runtime_mode"]
  >,
  runtime_config: z.record(z.string(), z.unknown()).default({}),
  custom_args: z.array(z.string()).default([]),
  has_custom_env: z.boolean().optional(),
  custom_env_key_count: z.number().optional(),
  visibility: z.string().catch("workspace") as unknown as z.ZodType<
    Agent["visibility"]
  >,
  permission_mode: z.enum(["private", "public_to"]).catch("private"),
  invocation_targets: z.array(AgentInvocationTargetSchema).default([]),
  status: z.string().catch("active") as unknown as z.ZodType<Agent["status"]>,
  max_concurrent_tasks: z.number().default(1),
  model: z.string().default(""),
  owner_id: z.string().nullable().default(null),
  skills: z.array(z.unknown()).default([]) as unknown as z.ZodType<
    Agent["skills"]
  >,
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  archived_at: z.string().nullable().default(null),
  archived_by: z.string().nullable().default(null),
}).loose();

export const AgentListSchema = z.array(AgentSchema).default([]);
export const EMPTY_AGENT_LIST: Agent[] = [];

export const RuntimeSchema: z.ZodType<RuntimeDevice> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  daemon_id: z.string().nullable().default(null),
  name: z.string().default(""),
  runtime_mode: z.string().catch("local") as unknown as z.ZodType<
    RuntimeDevice["runtime_mode"]
  >,
  provider: z.string().default(""),
  launch_header: z.string().default(""),
  status: z.enum(["online", "offline"]).catch("offline"),
  last_seen_at: z.string().nullable().default(null),
  device_info: z.string().default(""),
  metadata: z.record(z.string(), z.unknown()).default({}),
  owner_id: z.string().nullable().default(null),
  visibility: z.string().catch("private") as unknown as z.ZodType<
    RuntimeDevice["visibility"]
  >,
  timezone: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const RuntimeListSchema = z.array(RuntimeSchema).default([]);
export const EMPTY_RUNTIME_LIST: RuntimeDevice[] = [];

export const SquadSchema: z.ZodType<Squad> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  name: z.string().default(""),
  description: z.string().default(""),
  instructions: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  leader_id: z.string().default(""),
  creator_id: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  archived_at: z.string().nullable().default(null),
  archived_by: z.string().nullable().default(null),
}).loose();

export const SquadListSchema = z.array(SquadSchema).default([]);
export const EMPTY_SQUAD_LIST: Squad[] = [];

export const EMPTY_ISSUE_FALLBACK: import("@goosar/core/types").Issue = {
  id: "",
  workspace_id: "",
  number: 0,
  identifier: "",
  title: "",
  description: null,
  status: "backlog",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  created_at: "",
  updated_at: "",
};

export type { Label, Project, ProjectResource };

export type AgentStatus = 'idle' | 'working' | 'blocked' | 'error' | 'offline';

export type AgentRuntimeMode = 'local' | 'cloud';

export type AgentVisibility = 'workspace' | 'private';

export type AgentPermissionMode = 'private' | 'public_to';

export interface AgentInvocationTarget {
  target_type: 'workspace' | 'member' | 'team';
  target_id: string | null;
}

export interface AgentInvocationTargetInput {
  target_type: 'workspace' | 'member' | 'team';
  target_id?: string;
}

export type RuntimeVisibility = 'private' | 'public';

export interface RuntimeDevice {
  id: string;
  workspace_id: string;
  daemon_id: string | null;
  name: string;
  custom_name?: string | null;
  runtime_mode: AgentRuntimeMode;
  provider: string;
  launch_header: string;
  status: 'online' | 'offline';
  device_info: string;
  metadata: Record<string, unknown>;
  owner_id: string | null;
  visibility: RuntimeVisibility;
  profile_id?: string | null;
  last_seen_at: string | null;
  created_at: string;
  updated_at: string;
}

export type AgentRuntime = RuntimeDevice;

export const RUNTIME_PROFILE_PROTOCOL_FAMILIES = [
  'runtime-a',
  'runtime-c',
  'runtime-d',
  'runtime-e',
  'runtime-f',
  'runtime-g',
  'runtime-h',
  'runtime-i',
  'runtime-j',
  'runtime-k',
  'runtime-l',
  'runtime-m',
  'runtime-n',
  'runtime-o',
  'runtime-p',
  'runtime-q',
  'runtime-r',
] as const;

export type RuntimeProtocolFamily = (typeof RUNTIME_PROFILE_PROTOCOL_FAMILIES)[number];

export type RuntimeProfileVisibility = 'workspace' | 'private';

export interface RuntimeProfile {
  id: string;
  workspace_id: string;
  display_name: string;
  protocol_family: RuntimeProtocolFamily;
  command_name: string;
  description: string | null;
  fixed_args: string[];
  visibility: RuntimeProfileVisibility;
  created_by: string | null;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateRuntimeProfileRequest {
  display_name: string;
  protocol_family: RuntimeProtocolFamily;
  command_name: string;
  description?: string;
  fixed_args?: string[];
  visibility?: RuntimeProfileVisibility;
  enabled?: boolean;
}

export interface UpdateRuntimeProfileRequest {
  display_name?: string;
  command_name?: string;
  description?: string | null;
  fixed_args?: string[];
  visibility?: RuntimeProfileVisibility;
  enabled?: boolean;
}

export type TaskFailureReason =
  | 'agent_error'
  | 'timeout'
  | 'codex_semantic_inactivity'
  | 'runtime_offline'
  | 'runtime_reconnect_timeout'
  | 'runtime_recovery'
  | 'manual';

export interface AgentActivityBucket {
  agent_id: string;
  bucket_at: string;
  task_count: number;
  failed_count: number;
}

export interface AgentRunCount {
  agent_id: string;
  run_count: number;
}

export interface WorkspaceWorkingAgent {
  id: string;
  name: string;
  avatar_url: string | null;
  running_task_count: number;
  issue_ids: string[];
}

export type WorkspaceWorkingAgentType = 'issue' | 'autopilot' | 'chat';

export type WorkspaceWorkingAgentMineRelation = 'assigned' | 'created' | 'involved' | 'any';

export interface AttributionUser {
  id: string;
  name?: string;
  email?: string;
  avatar_url?: string;
}

export interface TaskEvidence {
  kind: string;
  ref_id: string;
}

export interface TaskAttribution {
  source: string;
  precise: boolean;
  initiator?: AttributionUser;
  originator?: AttributionUser;
  evidence?: TaskEvidence;
  rule_version_id?: string;
  delegated_from_task_id?: string;
  retry_of_task_id?: string;
  rerun_of_task_id?: string;
}

export interface AgentTask {
  id: string;
  agent_id: string;
  runtime_id: string;
  issue_id: string;
  status:
    | 'queued'
    | 'dispatched'
    | 'waiting_local_directory'
    | 'running'
    | 'completed'
    | 'failed'
    | 'cancelled';
  priority: number;
  dispatched_at: string | null;
  started_at: string | null;
  completed_at: string | null;
  result: unknown;
  error: string | null;
  failure_reason?: TaskFailureReason | '';
  created_at: string;
  chat_session_id?: string;
  autopilot_run_id?: string;
  parent_task_id?: string;
  attempt?: number;
  trigger_comment_id?: string;
  coalesced_comment_ids?: string[];
  delivered_comment_ids?: string[];
  trigger_summary?: string;
  handoff_note?: string;
  kind?: 'comment' | 'autopilot' | 'chat' | 'quick_create' | 'direct';
  work_dir?: string;
  relative_work_dir?: string;
  attribution?: TaskAttribution;
}

export interface Agent {
  id: string;
  workspace_id: string;
  runtime_id: string;
  name: string;
  description: string;
  instructions: string;
  avatar_url: string | null;
  runtime_mode: AgentRuntimeMode;
  runtime_config: Record<string, unknown>;
  custom_args: string[];
  has_custom_env?: boolean;
  custom_env_key_count?: number;
  mcp_config?: unknown | null;
  mcp_config_redacted?: boolean;
  mcp_config_encrypted?: boolean;
  composio_toolkit_allowlist?: string[];
  composio_toolkit_allowlist_redacted?: boolean;
  visibility: AgentVisibility;
  permission_mode: AgentPermissionMode;
  invocation_targets: AgentInvocationTarget[];
  status: AgentStatus;
  max_concurrent_tasks: number;
  model: string;
  thinking_level?: string;
  service_tier?: string;
  owner_id: string | null;
  system_key?: string;
  skills: AgentSkillSummary[];
  disabled_runtime_skills?: DisabledRuntimeSkill[];
  created_at: string;
  updated_at: string;
  archived_at: string | null;
  archived_by: string | null;
}

export interface DisabledRuntimeSkill {
  runtime_id: string;
  provider: string;
  root: 'provider' | 'universal' | 'plugin';
  key: string;
  name?: string;
  plugin?: string;
}

export interface SetAgentRuntimeSkillEnabledRequest {
  runtime_id: string;
  root: 'provider' | 'universal' | 'plugin';
  key: string;
  name: string;
  plugin?: string;
  enabled: boolean;
}

export interface AgentSkillSummary {
  id: string;
  name: string;
  description: string;
  enabled?: boolean;
}

export interface CreateAgentRequest {
  name: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  runtime_id: string;
  runtime_config?: Record<string, unknown>;
  custom_env?: Record<string, string>;
  custom_args?: string[];
  visibility?: AgentVisibility;
  permission_mode?: AgentPermissionMode;
  invocation_targets?: AgentInvocationTargetInput[];
  max_concurrent_tasks?: number;
  model?: string;
  thinking_level?: string;
  service_tier?: string;
  template?: string;
  mcp_config?: unknown;
  skill_ids?: string[];
}

export interface AgentBuilderSession {
  session_id: string;
  builder_agent_id: string;
  runtime_id: string;
}

export interface AgentBuilderRuntimeSwitch {
  runtime_id: string;
}

export interface AgentTemplateSummary {
  slug: string;
  name: string;
  description: string;
  category?: string;
  icon?: string;
  accent?: string;
  skills: AgentTemplateSkillRef[];
}

export interface AgentTemplate extends AgentTemplateSummary {
  instructions: string;
}

export interface AgentTemplateSkillRef {
  source_url: string;
  cached_name: string;
  cached_description: string;
}

export interface CreateAgentFromTemplateRequest {
  template_slug: string;
  name: string;
  runtime_id: string;
  model?: string;
  visibility?: AgentVisibility;
  permission_mode?: AgentPermissionMode;
  invocation_targets?: AgentInvocationTargetInput[];
  max_concurrent_tasks?: number;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  extra_skill_ids?: string[];
}

export interface CreateAgentFromTemplateResponse {
  agent: Agent;
  imported_skill_ids: string[];
  reused_skill_ids: string[];
}

export interface CreateAgentFromTemplateFailure {
  error: string;
  failed_urls: string[];
}

export interface UpdateAgentRequest {
  name?: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  runtime_id?: string;
  runtime_config?: Record<string, unknown>;
  custom_args?: string[];
  mcp_config?: unknown | null;
  composio_toolkit_allowlist?: string[] | null;
  visibility?: AgentVisibility;
  permission_mode?: AgentPermissionMode;
  invocation_targets?: AgentInvocationTargetInput[];
  status?: AgentStatus;
  max_concurrent_tasks?: number;
  model?: string;
  thinking_level?: string;
  service_tier?: string;
}

export interface AgentEnvResponse {
  agent_id: string;
  custom_env: Record<string, string>;
  values_masked?: boolean;
}

export const AGENT_ENV_MASKED_VALUE = '****';

export interface UpdateAgentEnvRequest {
  custom_env: Record<string, string>;
}

export interface SkillSummary {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  config: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  enabled?: boolean;
}

export interface Skill extends SkillSummary {
  content: string;
  files: SkillFile[];
}

export interface SkillFile {
  id: string;
  skill_id: string;
  path: string;
  content: string;
  created_at: string;
  updated_at: string;
}

export interface CreateSkillRequest {
  name: string;
  description?: string;
  content?: string;
  config?: Record<string, unknown>;
  files?: { path: string; content: string }[];
}

export interface UpdateSkillRequest {
  name?: string;
  description?: string;
  content?: string;
  config?: Record<string, unknown>;
  files?: { path: string; content: string }[];
}

export interface SetAgentSkillsRequest {
  skill_ids: string[];
}

export interface IssueUsageSummary {
  total_input_tokens: number;
  total_output_tokens: number;
  total_cache_read_tokens: number;
  total_cache_write_tokens: number;
  cost_usd_ticks?: number;
  uncosted_input_tokens?: number;
  uncosted_output_tokens?: number;
  uncosted_cache_read_tokens?: number;
  uncosted_cache_write_tokens?: number;
  task_count: number;
}

export interface RuntimeUsage {
  runtime_id: string;
  date: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  cost_usd_ticks?: number;
  uncosted_input_tokens?: number;
  uncosted_output_tokens?: number;
  uncosted_cache_read_tokens?: number;
  uncosted_cache_write_tokens?: number;
}

export interface RuntimeHourlyActivity {
  hour: number;
  count: number;
}

export interface RuntimeUsageByAgent {
  agent_id: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  cost_usd_ticks?: number;
  uncosted_input_tokens?: number;
  uncosted_output_tokens?: number;
  uncosted_cache_read_tokens?: number;
  uncosted_cache_write_tokens?: number;
  task_count: number;
}

export interface RuntimeUsageByHour {
  hour: number;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  cost_usd_ticks?: number;
  uncosted_input_tokens?: number;
  uncosted_output_tokens?: number;
  uncosted_cache_read_tokens?: number;
  uncosted_cache_write_tokens?: number;
  task_count: number;
}

export interface DashboardUsageDaily {
  date: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  cost_usd_ticks?: number;
  uncosted_input_tokens?: number;
  uncosted_output_tokens?: number;
  uncosted_cache_read_tokens?: number;
  uncosted_cache_write_tokens?: number;
  task_count: number;
}

export interface DashboardUsageByAgent {
  agent_id: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  cost_usd_ticks?: number;
  uncosted_input_tokens?: number;
  uncosted_output_tokens?: number;
  uncosted_cache_read_tokens?: number;
  uncosted_cache_write_tokens?: number;
  task_count: number;
}

export interface DashboardAgentRunTime {
  agent_id: string;
  total_seconds: number;
  task_count: number;
  failed_count: number;
}

export interface DashboardRunTimeDaily {
  date: string;
  total_seconds: number;
  task_count: number;
  failed_count: number;
}

export interface DashboardFailureDaily {
  date: string;
  failure_reason: string;
  task_count: number;
}

export interface DashboardFailureByAgent {
  agent_id: string;
  failure_reason: string;
  task_count: number;
}

export type RuntimeUpdateStatus = 'pending' | 'running' | 'completed' | 'failed' | 'timeout';

export interface RuntimeUpdate {
  id: string;
  runtime_id: string;
  status: RuntimeUpdateStatus;
  target_version: string;
  output?: string;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface RuntimeModel {
  id: string;
  label: string;
  provider?: string;
  default?: boolean;
  thinking?: RuntimeModelThinking;
  service_tiers?: RuntimeModelServiceTier[];
}

export interface RuntimeModelServiceTier {
  id: string;
  name: string;
  description?: string;
}

export interface RuntimeModelThinking {
  supported_levels: RuntimeModelThinkingLevel[];
  default_level?: string;
}

export interface RuntimeModelThinkingLevel {
  value: string;
  label: string;
  description?: string;
}

export type RuntimeModelListStatus = 'pending' | 'running' | 'completed' | 'failed' | 'timeout';

export interface RuntimeModelListRequest {
  id: string;
  runtime_id: string;
  status: RuntimeModelListStatus;
  models?: RuntimeModel[];
  supported: boolean;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface RuntimeModelsResult {
  models: RuntimeModel[];
  supported: boolean;
}

export type RuntimeLocalSkillStatus =
  'pending' | 'running' | 'completed' | 'conflict' | 'failed' | 'timeout';

export type RuntimeLocalSkillImportAction = 'overwrite';

export interface RuntimeLocalSkillImportConflict {
  existing_skill_id: string;
  existing_created_by?: string;
  can_overwrite: boolean;
}

export interface RuntimeLocalSkillSummary {
  key: string;
  name: string;
  description?: string;
  source_path: string;
  provider: string;
  root?: 'provider' | 'universal' | 'plugin';
  plugin?: string;
  can_disable?: boolean;
  file_count: number;
}

export interface RuntimeLocalMcpServerSummary {
  name: string;
  transport?: 'stdio' | 'http' | 'sse' | 'unknown';
  source?: string;
  enabled: boolean;
}

export interface RuntimeLocalSkillListRequest {
  id: string;
  runtime_id: string;
  status: RuntimeLocalSkillStatus;
  skills?: RuntimeLocalSkillSummary[];
  supported: boolean;
  mcp_servers?: RuntimeLocalMcpServerSummary[];
  mcp_supported?: boolean;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateRuntimeLocalSkillImportRequest {
  skill_key: string;
  name?: string;
  description?: string;
  action?: RuntimeLocalSkillImportAction;
  target_skill_id?: string;
  supports_conflict?: boolean;
}

export interface RuntimeLocalSkillImportRequest {
  id: string;
  runtime_id: string;
  skill_key: string;
  name?: string;
  description?: string;
  action?: RuntimeLocalSkillImportAction;
  target_skill_id?: string;
  supports_conflict?: boolean;
  status: RuntimeLocalSkillStatus;
  skill?: Skill;
  conflict?: RuntimeLocalSkillImportConflict;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface RuntimeLocalSkillsResult {
  skills: RuntimeLocalSkillSummary[];
  supported: boolean;
  mcpServers: RuntimeLocalMcpServerSummary[];
  mcpSupported: boolean;
}

export interface RuntimeLocalSkillImportResult {
  status: 'created' | 'updated' | 'conflict';
  skill?: Skill;
  conflict?: RuntimeLocalSkillImportConflict;
}

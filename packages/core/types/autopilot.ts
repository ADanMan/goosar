export type AutopilotStatus = 'active' | 'paused' | 'archived';

export type AutopilotExecutionMode = 'create_issue' | 'run_only';

export type AutopilotAssigneeType = 'agent' | 'squad';

export type AutopilotTriggerKind = 'schedule' | 'webhook' | 'api';

export type AutopilotRunStatus = 'issue_created' | 'running' | 'completed' | 'failed' | 'skipped';

export type AutopilotRunSource = 'schedule' | 'manual' | 'webhook' | 'api';

export interface Autopilot {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  project_id?: string | null;
  assignee_type: AutopilotAssigneeType;
  assignee_id: string;
  status: AutopilotStatus;
  execution_mode: AutopilotExecutionMode;
  issue_title_template: string | null;
  created_by_type: string;
  created_by_id: string;
  last_run_at: string | null;
  created_at: string;
  updated_at: string;
  trigger_kinds?: string[];
  next_run_at?: string | null;
  last_run_status?: string | null;
  subscribers?: AutopilotSubscriber[];
  is_template?: boolean;
  can_write?: boolean;
  can_manage_access?: boolean;
}

export interface WebhookEventFilter {
  event: string;
  actions?: string[];
}

export interface AutopilotSubscriber {
  user_type: 'member';
  user_id: string;
  created_at: string;
}

export interface AutopilotCollaborator {
  user_type: 'member';
  user_id: string;
  granted_by: string;
  created_at: string;
}

export interface AutopilotCollaboratorsResponse {
  collaborators: AutopilotCollaborator[];
}

export interface AutopilotTrigger {
  id: string;
  autopilot_id: string;
  kind: AutopilotTriggerKind;
  enabled: boolean;
  cron_expression: string | null;
  timezone: string | null;
  next_run_at: string | null;
  webhook_token: string | null;
  webhook_path?: string | null;
  webhook_url?: string | null;
  label: string | null;
  event_filters?: WebhookEventFilter[] | null;
  last_fired_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface AutopilotRun {
  id: string;
  autopilot_id: string;
  trigger_id: string | null;
  source: AutopilotRunSource;
  status: AutopilotRunStatus;
  issue_id: string | null;
  task_id: string | null;
  triggered_at: string;
  completed_at: string | null;
  failure_reason: string | null;
  reason_code?: string;
  trigger_payload: unknown;
  result: unknown;
  created_at: string;
}

export interface AutopilotSubscriberInput {
  user_type: 'member';
  user_id: string;
}

export interface CreateAutopilotRequest {
  title: string;
  description?: string;
  project_id?: string | null;
  assignee_type?: AutopilotAssigneeType;
  assignee_id: string;
  execution_mode: AutopilotExecutionMode;
  issue_title_template?: string;
  subscribers?: AutopilotSubscriberInput[];
}

export interface UpdateAutopilotRequest {
  title?: string;
  description?: string | null;
  project_id?: string | null;
  assignee_type?: AutopilotAssigneeType;
  assignee_id?: string;
  status?: AutopilotStatus;
  execution_mode?: AutopilotExecutionMode;
  issue_title_template?: string | null;
  subscribers?: AutopilotSubscriberInput[];
}

export interface CreateAutopilotTriggerRequest {
  kind: AutopilotTriggerKind;
  cron_expression?: string;
  timezone?: string;
  label?: string;
  event_filters?: WebhookEventFilter[];
}

export interface UpdateAutopilotTriggerRequest {
  enabled?: boolean;
  cron_expression?: string;
  timezone?: string;
  label?: string;
  event_filters?: WebhookEventFilter[] | null;
}

export interface CronPreviewResponse {
  next_runs: string[] | null;
}

export interface ListAutopilotsResponse {
  autopilots: Autopilot[];
  total: number;
}

export interface GetAutopilotResponse {
  autopilot: Autopilot;
  triggers: AutopilotTrigger[];
  collaborators?: AutopilotCollaborator[];
}

export interface ListAutopilotRunsResponse {
  runs: AutopilotRun[];
  total: number;
}

export type WebhookDeliveryStatus = 'queued' | 'dispatched' | 'rejected' | 'ignored' | 'failed';

export type WebhookSignatureStatus = 'not_required' | 'valid' | 'invalid' | 'missing';

export interface WebhookDelivery {
  id: string;
  workspace_id: string;
  autopilot_id: string;
  trigger_id: string;
  provider: string;
  event: string;
  dedupe_key: string | null;
  dedupe_source: string | null;
  signature_status: WebhookSignatureStatus;
  status: WebhookDeliveryStatus;
  attempt_count: number;
  dispatch_attempts: number;
  available_at: string;
  content_type: string | null;
  response_status: number | null;
  autopilot_run_id: string | null;
  replayed_from_delivery_id: string | null;
  error: string | null;
  received_at: string;
  last_attempt_at: string;
  created_at: string;
  selected_headers?: Record<string, unknown> | null;
  raw_body?: string | null;
  response_body?: string | null;
}

export interface ListWebhookDeliveriesResponse {
  deliveries: WebhookDelivery[];
  total: number;
}

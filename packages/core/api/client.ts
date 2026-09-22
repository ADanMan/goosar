import type {
  Issue,
  IssuePriority,
  IssueMetadataValue,
  IssueMetadataResponse,
  CreateIssueRequest,
  MoveIssueRequest,
  UpdateIssueRequest,
  GroupedIssuesResponse,
  ListIssuesResponse,
  SearchIssuesResponse,
  SearchProjectsResponse,
  UpdateMeRequest,
  CreateMemberRequest,
  UpdateMemberRequest,
  ListIssuesParams,
  ListGroupedIssuesParams,
  IssueTableFacetsRequest,
  IssueTableFacetsResponse,
  IssueTableGroupsRequest,
  IssueTableGroupsResponse,
  IssueTableRowsRequest,
  IssueTableRowsResponse,
  Agent,
  CreateAgentRequest,
  AgentTemplate,
  AgentTemplateSummary,
  CreateAgentFromTemplateRequest,
  CreateAgentFromTemplateResponse,
  AgentBuilderRuntimeSwitch,
  AgentBuilderSession,
  UpdateAgentRequest,
  AgentEnvResponse,
  UpdateAgentEnvRequest,
  AgentTask,
  AgentActivityBucket,
  AgentRunCount,
  WorkspaceWorkingAgent,
  WorkspaceWorkingAgentMineRelation,
  WorkspaceWorkingAgentType,
  AgentRuntime,
  RuntimeProfile,
  CreateRuntimeProfileRequest,
  UpdateRuntimeProfileRequest,
  InboxItem,
  InboxWorkspaceUnread,
  IssueSubscriber,
  Comment,
  CommentTriggerPreview,
  IssueTriggerPreview,
  IssueTriggerPreviewParams,
  Reaction,
  IssueReaction,
  Workspace,
  WorkspaceRepo,
  WorkspaceTemplate,
  WorkspaceCapabilities,
  MemberWithUser,
  User,
  Skill,
  SkillSummary,
  CreateSkillRequest,
  UpdateSkillRequest,
  SetAgentSkillsRequest,
  SetAgentRuntimeSkillEnabledRequest,
  PersonalAccessToken,
  CreatePersonalAccessTokenRequest,
  CreatePersonalAccessTokenResponse,
  RuntimeUsage,
  IssueUsageSummary,
  RuntimeHourlyActivity,
  RuntimeUsageByAgent,
  RuntimeUsageByHour,
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
  DashboardFailureDaily,
  DashboardFailureByAgent,
  RuntimeUpdate,
  RuntimeModelListRequest,
  RuntimeLocalSkillListRequest,
  CreateRuntimeLocalSkillImportRequest,
  RuntimeLocalSkillImportRequest,
  TimelineEntry,
  AssigneeFrequencyEntry,
  TaskMessagePayload,
  Attachment,
  ChatSession,
  ChatPinnedAgent,
  ChatMessage,
  ChatMessagesPage,
  ChatDraftRestoresResponse,
  ChatPendingTask,
  PendingChatTasksResponse,
  HasPendingChatTasksResponse,
  SendChatMessageResponse,
  CancelTaskResponse,
  Project,
  CreateProjectRequest,
  UpdateProjectRequest,
  ListProjectsResponse,
  ProjectResource,
  CreateProjectResourceRequest,
  UpdateProjectResourceRequest,
  ListProjectResourcesResponse,
  Label,
  IssueProperty,
  IssuePropertyValue,
  CreatePropertyRequest,
  UpdatePropertyRequest,
  ListPropertiesResponse,
  IssuePropertiesResponse,
  CreateLabelRequest,
  UpdateLabelRequest,
  ListLabelsResponse,
  IssueLabelsResponse,
  LabelResourceType,
  ResourceLabelsResponse,
  PinnedItem,
  CreatePinRequest,
  PinnedItemType,
  ReorderPinsRequest,
  Invitation,
  Autopilot,
  AutopilotTrigger,
  AutopilotRun,
  CreateAutopilotRequest,
  UpdateAutopilotRequest,
  CreateAutopilotTriggerRequest,
  UpdateAutopilotTriggerRequest,
  ListAutopilotsResponse,
  CronPreviewResponse,
  GetAutopilotResponse,
  AutopilotCollaboratorsResponse,
  ListAutopilotRunsResponse,
  ListWebhookDeliveriesResponse,
  WebhookDelivery,
  NotificationPreferenceResponse,
  NotificationPreferences,
  GitHubPullRequest,
  ListGitHubInstallationsResponse,
  ListGitHubRepositoriesResponse,
  GitHubConnectResponse,
  ListVCSConnectionsResponse,
  ConnectVCSRequest,
  ConnectVCSResponse,
  ComposioToolkit,
  ComposioConnection,
  ComposioConnectInitResponse,
  SlackInstallation,
  ListSlackInstallationsResponse,
  RegisterSlackBYORequest,
  RedeemSlackBindingTokenResponse,
  Squad,
  SquadMember,
  SquadMemberStatusListResponse,
  BillingBalance,
  BillingTransactionsPage,
  BillingBatchesPage,
  BillingTopupsPage,
  BillingPriceTier,
  CreateBillingCheckoutSessionRequest,
  CreateBillingCheckoutSessionResponse,
  BillingCheckoutSessionStatus,
  CreateBillingPortalSessionResponse,
} from '../types';
import type { AuthMethodsResponse } from './schemas';
import type { OnboardingCompletionPath } from '../onboarding/types';
import type { CreateFeedbackResponse, FeedbackKind } from '../feedback/types';
import type {
  CloudRuntimeNode,
  CreateCloudRuntimeNodeRequest,
  ListCloudRuntimeNodesParams,
} from '../runtimes/cloud-runtime';
import { type Logger, noopLogger } from '../logger';
import { createRequestId } from '../utils';
import { getCurrentSlug } from '../platform/workspace-storage';
import { parseStrict, parseWithFallback } from './schema';
import {
  EMPTY_LOGIN_RESULT,
  EMPTY_MFA_CONFIRMATION,
  EMPTY_MFA_ENROLLMENT,
  EMPTY_MFA_STATUS,
  EMPTY_SESSION_LIST,
  EMPTY_SESSION_REVOKE,
  LoginResultSchema,
  MFAConfirmSchema,
  MFAEnrollSchema,
  MFAStatusSchema,
  SessionListSchema,
  SessionRevokeSchema,
  type LoginResult,
  type MFAConfirmation,
  type MFAEnrollment,
  type MFAStatus,
  type SessionEntry,
  type SessionRevokeResult,
} from './mfa';
import {
  EMPTY_WORKSPACE_CONTENT_SYNC_REQUEST,
  WorkspaceContentSyncRequestSchema,
  type WorkspaceContentSyncRequest,
} from './workspace-content';
import {
  EMPTY_EFFECTIVE_CONFIG_VIEW,
  EffectiveConfigViewSchema,
  type EffectiveConfigView,
} from './effective-config';
import {
  EMPTY_PROVISIONING_CATALOG_VIEW,
  EMPTY_PROVISIONING_PINS_VIEW,
  EMPTY_USER_CONFIG_OVERRIDE_VIEW,
  EMPTY_WORKSPACE_CONFIG_VIEW,
  ProvisioningCatalogViewSchema,
  ProvisioningPinsViewSchema,
  UserConfigOverrideViewSchema,
  WorkspaceConfigViewSchema,
  type ProvisioningCatalogView,
  type ProvisioningPinInput,
  type ProvisioningPinsView,
  type UserConfigOverridePatch,
  type UserConfigOverrideView,
  type WorkspaceConfigPatch,
  type WorkspaceConfigView,
} from './workspace-admin';
import {
  EMPTY_WORKSPACE_MCP_SERVERS,
  WorkspaceMcpServerListSchema,
  type WorkspaceMcpServer,
  type WorkspaceMcpServerInput,
} from './workspace-mcp';
import {
  EMPTY_DEPLOYMENT_MCP_SERVERS,
  DeploymentMcpServerListSchema,
  type DeploymentMcpServer,
  type DeploymentMcpServerInput,
} from './deployment-mcp';
import {
  DeploymentUserSchema,
  EMPTY_DEPLOYMENT_USER,
  type DeploymentUser,
} from './deployment-user';
import {
  AdminAuditListSchema,
  DeploymentAdminEntrySchema,
  DeploymentAdminListSchema,
  DeploymentAdminPendingListSchema,
  DeploymentAdminPendingSchema,
  DeploymentPolicyViewSchema,
  DeploymentUserConfigOverrideListSchema,
  DeploymentWorkspaceListSchema,
  DeploymentWorkspaceMemberListSchema,
  EMPTY_ADMIN_AUDIT_LIST,
  EMPTY_DEPLOYMENT_ADMIN_LIST,
  EMPTY_DEPLOYMENT_ADMIN_PENDING,
  EMPTY_DEPLOYMENT_ADMIN_PENDING_LIST,
  EMPTY_DEPLOYMENT_POLICY_VIEW,
  EMPTY_DEPLOYMENT_WORKSPACE_LIST,
  EMPTY_DEPLOYMENT_WORKSPACE_MEMBER_LIST,
  type DeploymentAuditQuery,
  type AdminAuditEntry,
  type DeploymentAdminAddResult,
  type DeploymentAdminEntry,
  type DeploymentAdminPending,
  type DeploymentPolicyDoc,
  type DeploymentPolicyView,
  type DeploymentWorkspaceEntry,
  type DeploymentWorkspaceMemberEntry,
} from './deployment-admin';
import {
  EMPTY_JOIN_TARGET_LIST,
  EMPTY_JOIN_TARGET_RESULT,
  JoinTargetListSchema,
  JoinTargetResultSchema,
  type JoinTarget,
  type JoinTargetResult,
} from './deployment-join';
import {
  DeploymentFleetSchema,
  EMPTY_DEPLOYMENT_FLEET,
  type DeploymentFleet,
} from './deployment-fleet';
import {
  AgentEnvResponseSchema,
  AgentTaskListSchema,
  AgentTemplateSchema,
  AgentTemplateSummaryListSchema,
  AttachmentResponseSchema,
  CancelTaskResponseSchema,
  ChatDraftRestoresResponseSchema,
  ChildIssuesResponseSchema,
  CommentsListSchema,
  CommentTriggerPreviewSchema,
  IssueTriggerPreviewSchema,
  CloudRuntimeNodeListSchema,
  CloudRuntimeNodeSchema,
  CreateAgentFromTemplateResponseSchema,
  AgentBuilderRuntimeSwitchSchema,
  AgentBuilderSessionSchema,
  agentBuilderRuntimeSwitchFallback,
  DashboardAgentRunTimeListSchema,
  DashboardRunTimeDailyListSchema,
  DashboardFailureDailyListSchema,
  DashboardFailureByAgentListSchema,
  DashboardUsageByAgentListSchema,
  DashboardUsageDailyListSchema,
  EMPTY_AGENT_TEMPLATE_DETAIL,
  EMPTY_AGENT_TEMPLATE_SUMMARY_LIST,
  EMPTY_APP_CONFIG,
  EMPTY_ATTACHMENT,
  EMPTY_CLOUD_RUNTIME_NODE,
  EMPTY_CLOUD_RUNTIME_NODE_LIST,
  EMPTY_CREATE_AGENT_FROM_TEMPLATE_RESPONSE,
  EMPTY_AGENT_BUILDER_SESSION,
  EMPTY_GROUPED_ISSUES_RESPONSE,
  EMPTY_ISSUE_TABLE_FACETS_RESPONSE,
  EMPTY_ISSUE_TABLE_GROUPS_RESPONSE,
  EMPTY_ISSUE_TABLE_ROWS_RESPONSE,
  EMPTY_LIST_ISSUES_RESPONSE,
  EMPTY_SEARCH_ISSUES_RESPONSE,
  EMPTY_SEARCH_PROJECTS_RESPONSE,
  EMPTY_SQUAD,
  EMPTY_SQUAD_LIST,
  EMPTY_SQUAD_MEMBER_STATUS_LIST,
  EMPTY_MEMBER_WITH_USER,
  EMPTY_MEMBER_WITH_USER_LIST,
  EMPTY_TIMELINE_ENTRIES,
  EMPTY_USER,
  EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE,
  EMPTY_WEBHOOK_DELIVERY,
  AppConfigSchema,
  type AppConfigResponse,
  GroupedIssuesResponseSchema,
  IssueTableFacetsResponseSchema,
  IssueTableGroupsResponseSchema,
  IssueTableRowsResponseSchema,
  ListAutopilotsResponseSchema,
  EMPTY_LIST_AUTOPILOTS_RESPONSE,
  AutopilotRunSchema,
  FALLBACK_AUTOPILOT_RUN,
  CronPreviewResponseSchema,
  UNREADABLE_CRON_PREVIEW_RESPONSE,
  ListIssuesResponseSchema,
  CreateIssueResponseSchema,
  MemberWithUserSchema,
  MemberWithUserListSchema,
  WorkspaceTemplateListSchema,
  EMPTY_WORKSPACE_TEMPLATE_LIST,
  WorkspaceCapabilitiesSchema,
  EMPTY_WORKSPACE_CAPABILITIES,
  ListWebhookDeliveriesResponseSchema,
  RuntimeHourlyActivityListSchema,
  RuntimeUsageByAgentListSchema,
  RuntimeUsageByHourListSchema,
  RuntimeUsageListSchema,
  SearchIssuesResponseSchema,
  SearchProjectsResponseSchema,
  SquadSchema,
  SquadListSchema,
  SquadMemberStatusListResponseSchema,
  SubscribersListSchema,
  TimelineEntriesSchema,
  UserSchema,
  AuthMethodsResponseSchema,
  EMPTY_AUTH_METHODS,
  WebhookDeliveryResponseSchema,
  BillingBalanceSchema,
  BillingTransactionsPageSchema,
  BillingBatchesPageSchema,
  BillingTopupsPageSchema,
  BillingPriceTierListSchema,
  CreateBillingCheckoutSessionResponseSchema,
  BillingCheckoutSessionStatusSchema,
  CreateBillingPortalSessionResponseSchema,
  EMPTY_BILLING_BALANCE,
  EMPTY_BILLING_TRANSACTIONS_PAGE,
  EMPTY_BILLING_BATCHES_PAGE,
  EMPTY_BILLING_TOPUPS_PAGE,
  EMPTY_BILLING_PRICE_TIER_LIST,
  EMPTY_CREATE_BILLING_CHECKOUT_SESSION_RESPONSE,
  EMPTY_BILLING_CHECKOUT_SESSION_STATUS,
  EMPTY_CREATE_BILLING_PORTAL_SESSION_RESPONSE,
  EMPTY_CANCEL_TASK_RESPONSE,
  EMPTY_CHAT_DRAFT_RESTORES,
  CreateFeedbackResponseSchema,
  EMPTY_CREATE_FEEDBACK_RESPONSE,
  InboxUnreadSummarySchema,
  EMPTY_INBOX_UNREAD_SUMMARY,
  InboxItemListSchema,
  EMPTY_INBOX_ITEMS,
  NotificationPreferenceResponseSchema,
  EMPTY_NOTIFICATION_PREFERENCE_RESPONSE,
  LabelSchema,
  ListLabelsResponseSchema,
  IssuePropertySchema,
  ListPropertiesResponseSchema,
  IssuePropertiesResponseSchema,
  IssueMetadataResponseSchema,
  EMPTY_ISSUE_PROPERTY,
  EMPTY_LIST_PROPERTIES_RESPONSE,
  EMPTY_ISSUE_PROPERTIES_RESPONSE,
  EMPTY_ISSUE_METADATA_RESPONSE,
  EMPTY_ISSUE_PULL_REQUESTS_RESPONSE,
  IssuePullRequestsResponseSchema,
  ResourceLabelsResponseSchema,
  EMPTY_LABEL,
  EMPTY_LIST_LABELS_RESPONSE,
  EMPTY_RESOURCE_LABELS_RESPONSE,
  GitHubConnectResponseSchema,
  ListGitHubInstallationsResponseSchema,
  ListGitHubRepositoriesResponseSchema,
  EMPTY_GITHUB_CONNECT_RESPONSE,
  EMPTY_LIST_GITHUB_INSTALLATIONS_RESPONSE,
  EMPTY_LIST_GITHUB_REPOSITORIES_RESPONSE,
} from './schemas';

export interface ApiClientIdentity {
  platform?: string;
  version?: string;
  os?: string;
}

export interface ApiClientOptions {
  logger?: Logger;
  onUnauthorized?: () => void;
  identity?: ApiClientIdentity;
}

export interface ClientRuntimeSnapshot {
  probe_result: 'success' | 'error';
  runtime_count?: number;
  provider_summary?: Record<string, number>;
  online_count?: number;
  offline_count?: number;
}

export interface ClientUsageRequest {
  install_id: string;
  runtime?: ClientRuntimeSnapshot;
}

export interface LoginResponse {
  token: string;
  user: User;
}

export class ApiError extends Error {
  readonly status: number;
  readonly statusText: string;
  readonly body?: unknown;
  readonly serverMessage?: string;

  constructor(
    message: string,
    status: number,
    statusText: string,
    body?: unknown,
    serverMessage?: string,
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.statusText = statusText;
    this.body = body;
    this.serverMessage = serverMessage;
  }
}

export function dispatchReasonCode(err: unknown): string | undefined {
  if (err instanceof ApiError && err.body && typeof err.body === 'object') {
    const code = (err.body as { reason_code?: unknown }).reason_code;
    if (typeof code === 'string' && code.length > 0) return code;
  }
  return undefined;
}

export function apiErrorCode(err: unknown): string | undefined {
  if (err instanceof ApiError && err.body && typeof err.body === 'object') {
    const code = (err.body as { code?: unknown }).code;
    if (typeof code === 'string' && code.length > 0) return code;
  }
  return undefined;
}

export class PreviewTooLargeError extends Error {
  constructor() {
    super('attachment too large for inline preview');
    this.name = 'PreviewTooLargeError';
  }
}

export class PreviewUnsupportedError extends Error {
  constructor() {
    super('attachment type not supported for inline preview');
    this.name = 'PreviewUnsupportedError';
  }
}

export const CHAT_DRAFT_RESTORE_CAPABILITY = 'chat-draft-restore-v1';

export class ApiClient {
  private baseUrl: string;
  private token: string | null = null;
  private logger: Logger;
  private options: ApiClientOptions;

  constructor(baseUrl: string, options?: ApiClientOptions) {
    this.baseUrl = baseUrl;
    this.options = options ?? {};
    this.logger = options?.logger ?? noopLogger;
  }

  getBaseUrl(): string {
    return this.baseUrl;
  }

  setToken(token: string | null) {
    this.token = token;
  }

  private readCsrfToken(): string | null {
    if (typeof document === 'undefined') return null;
    const match = document.cookie.split('; ').find((c) => c.startsWith('goosar_csrf='));
    return match ? (match.split('=')[1] ?? null) : null;
  }

  private authHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};
    if (this.token) headers['Authorization'] = `Bearer ${this.token}`;
    const slug = getCurrentSlug();
    if (slug) headers['X-Workspace-Slug'] = slug;
    const csrf = this.readCsrfToken();
    if (csrf) headers['X-CSRF-Token'] = csrf;
    const id = this.options.identity;
    if (id?.platform) headers['X-Client-Platform'] = id.platform;
    if (id?.version) headers['X-Client-Version'] = id.version;
    if (id?.os) headers['X-Client-OS'] = id.os;
    return headers;
  }

  private handleUnauthorized() {
    this.token = null;
    this.options.onUnauthorized?.();
  }

  private async parseErrorMessage(res: Response, fallback: string): Promise<string> {
    try {
      const data = (await res.json()) as { error?: string };
      if (typeof data.error === 'string' && data.error) return data.error;
    } catch {
      // Ignore non-JSON error bodies.
    }
    return fallback;
  }

  private async parseErrorBody(
    res: Response,
    fallback: string,
  ): Promise<{ message: string; body: unknown; serverMessage?: string }> {
    try {
      const data = (await res.json()) as { error?: string };
      const serverMessage = typeof data.error === 'string' && data.error ? data.error : undefined;
      return { message: serverMessage ?? fallback, body: data, serverMessage };
    } catch {
      return { message: fallback, body: undefined };
    }
  }

  private async fetchRaw(
    path: string,
    init?: RequestInit & { extraHeaders?: Record<string, string> },
  ): Promise<Response> {
    const rid = createRequestId();
    const start = Date.now();
    const method = init?.method ?? 'GET';

    const headers: Record<string, string> = {
      'X-Request-ID': rid,
      ...this.authHeaders(),
      ...(init?.extraHeaders ?? {}),
      ...((init?.headers as Record<string, string>) ?? {}),
    };
    const hadAuth = headers['Authorization'] !== undefined;

    this.logger.info(`→ ${method} ${path}`, { rid });

    const res = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers,
      credentials: 'include',
    });

    if (!res.ok) {
      if (res.status === 401 && hadAuth) this.handleUnauthorized();
      const { message, body, serverMessage } = await this.parseErrorBody(
        res,
        `API error: ${res.status} ${res.statusText}`,
      );
      const logLevel = res.status === 404 ? 'warn' : 'error';
      this.logger[logLevel](`← ${res.status} ${path}`, {
        rid,
        duration: `${Date.now() - start}ms`,
        error: message,
      });
      throw new ApiError(message, res.status, res.statusText, body, serverMessage);
    }

    this.logger.info(`← ${res.status} ${path}`, { rid, duration: `${Date.now() - start}ms` });
    return res;
  }

  private async fetch<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await this.fetchRaw(path, {
      ...init,
      extraHeaders: { 'Content-Type': 'application/json' },
    });
    if (res.status === 204) {
      return undefined as T;
    }
    return res.json() as Promise<T>;
  }

  async sendCode(email: string): Promise<void> {
    await this.fetch('/auth/send-code', {
      method: 'POST',
      body: JSON.stringify({ email }),
    });
  }

  async verifyCode(email: string, code: string): Promise<LoginResult> {
    const raw = await this.fetch<unknown>('/auth/verify-code', {
      method: 'POST',
      body: JSON.stringify({ email, code }),
    });
    return parseWithFallback(raw, LoginResultSchema, EMPTY_LOGIN_RESULT, {
      endpoint: 'POST /auth/verify-code',
    });
  }

  async verifyLink(linkToken: string): Promise<LoginResult> {
    const raw = await this.fetch<unknown>('/auth/verify-link', {
      method: 'POST',
      body: JSON.stringify({ link_token: linkToken }),
    });
    return parseWithFallback(raw, LoginResultSchema, EMPTY_LOGIN_RESULT, {
      endpoint: 'POST /auth/verify-link',
    });
  }

  async getAuthMethods(): Promise<AuthMethodsResponse> {
    const raw = await this.fetch<unknown>('/api/auth/methods');
    return parseWithFallback(raw, AuthMethodsResponseSchema, EMPTY_AUTH_METHODS, {
      endpoint: 'GET /api/auth/methods',
    });
  }

  oidcStartURL(client?: 'desktop'): string {
    const base = `${this.baseUrl}/api/auth/oidc/start`;
    return client ? `${base}?client=${encodeURIComponent(client)}` : base;
  }

  async ldapLogin(username: string, password: string): Promise<LoginResult> {
    const raw = await this.fetch<unknown>('/api/auth/ldap/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
    return parseWithFallback(raw, LoginResultSchema, EMPTY_LOGIN_RESULT, {
      endpoint: 'POST /api/auth/ldap/login',
    });
  }

  async verifyMFA(payload: {
    mfa_token: string;
    code?: string;
    recovery_code?: string;
  }): Promise<LoginResult> {
    const raw = await this.fetch<unknown>('/api/auth/mfa/verify', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
    return parseWithFallback(raw, LoginResultSchema, EMPTY_LOGIN_RESULT, {
      endpoint: 'POST /api/auth/mfa/verify',
    });
  }

  async getMFAStatus(): Promise<MFAStatus> {
    const raw = await this.fetch<unknown>('/api/auth/mfa');
    return parseWithFallback(raw, MFAStatusSchema, EMPTY_MFA_STATUS, {
      endpoint: 'GET /api/auth/mfa',
    });
  }

  async enrollTOTP(): Promise<MFAEnrollment> {
    const raw = await this.fetch<unknown>('/api/auth/mfa/totp/enroll', {
      method: 'POST',
    });
    return parseWithFallback(raw, MFAEnrollSchema, EMPTY_MFA_ENROLLMENT, {
      endpoint: 'POST /api/auth/mfa/totp/enroll',
      omitReceivedFromLog: true,
    });
  }

  async confirmTOTP(code: string): Promise<MFAConfirmation> {
    const raw = await this.fetch<unknown>('/api/auth/mfa/totp/confirm', {
      method: 'POST',
      body: JSON.stringify({ code }),
    });
    return parseWithFallback(raw, MFAConfirmSchema, EMPTY_MFA_CONFIRMATION, {
      endpoint: 'POST /api/auth/mfa/totp/confirm',
      omitReceivedFromLog: true,
    });
  }

  async disableTOTP(code: string): Promise<void> {
    await this.fetch('/api/auth/mfa/totp/disable', {
      method: 'POST',
      body: JSON.stringify({ code }),
    });
  }

  async regenerateRecoveryCodes(code: string): Promise<MFAConfirmation> {
    const raw = await this.fetch<unknown>('/api/auth/mfa/recovery-codes', {
      method: 'POST',
      body: JSON.stringify({ code }),
    });
    return parseWithFallback(raw, MFAConfirmSchema, EMPTY_MFA_CONFIRMATION, {
      endpoint: 'POST /api/auth/mfa/recovery-codes',
      omitReceivedFromLog: true,
    });
  }

  async listSessions(): Promise<SessionEntry[]> {
    const raw = await this.fetch<unknown>('/api/auth/sessions');
    return parseWithFallback(raw, SessionListSchema, EMPTY_SESSION_LIST, {
      endpoint: 'GET /api/auth/sessions',
    });
  }

  async revokeSession(sessionId: string): Promise<void> {
    await this.fetch(`/api/auth/sessions/${encodeURIComponent(sessionId)}`, {
      method: 'DELETE',
    });
  }

  async revokeAllSessions(): Promise<SessionRevokeResult> {
    const raw = await this.fetch<unknown>('/api/auth/sessions/revoke-all', {
      method: 'POST',
    });
    return parseWithFallback(raw, SessionRevokeSchema, EMPTY_SESSION_REVOKE, {
      endpoint: 'POST /api/auth/sessions/revoke-all',
    });
  }

  async revokeDeploymentUserSessions(userId: string): Promise<SessionRevokeResult> {
    const raw = await this.fetch<unknown>(
      `/api/deployment/users/${encodeURIComponent(userId)}/revoke-sessions`,
      { method: 'POST' },
    );
    return parseWithFallback(raw, SessionRevokeSchema, EMPTY_SESSION_REVOKE, {
      endpoint: 'POST /api/deployment/users/{id}/revoke-sessions',
    });
  }

  async logout(): Promise<void> {
    await this.fetch('/auth/logout', { method: 'POST' });
  }

  async issueCliToken(): Promise<{ token: string }> {
    return this.fetch('/api/cli-token', { method: 'POST' });
  }

  async getMe(): Promise<User> {
    const raw = await this.fetch<unknown>('/api/me');
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: 'GET /api/me',
    });
  }

  async markOnboardingComplete(payload?: {
    completion_path?: OnboardingCompletionPath;
    workspace_id?: string;
  }): Promise<User> {
    const raw = await this.fetch<unknown>('/api/me/onboarding/complete', {
      method: 'POST',
      body: payload ? JSON.stringify(payload) : undefined,
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: 'POST /api/me/onboarding/complete',
    });
  }

  async joinCloudWaitlist(payload: { email: string; reason?: string }): Promise<User> {
    const raw = await this.fetch<unknown>('/api/me/onboarding/cloud-waitlist', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: 'POST /api/me/onboarding/cloud-waitlist',
    });
  }

  async patchOnboarding(payload: { questionnaire?: Record<string, unknown> }): Promise<User> {
    const raw = await this.fetch<unknown>('/api/me/onboarding', {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: 'PATCH /api/me/onboarding',
    });
  }

  async updateMe(data: UpdateMeRequest): Promise<User> {
    const raw = await this.fetch<unknown>('/api/me', {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, UserSchema, EMPTY_USER, {
      endpoint: 'PATCH /api/me',
    });
  }

  async listIssues(params?: ListIssuesParams): Promise<ListIssuesResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set('limit', String(params.limit));
    if (params?.offset) search.set('offset', String(params.offset));
    if (params?.workspace_id) search.set('workspace_id', params.workspace_id);
    if (params?.q?.trim()) search.set('q', params.q.trim());
    if (params?.status) search.set('status', params.status);
    if (params?.statuses?.length) search.set('statuses', params.statuses.join(','));
    if (params?.priority) search.set('priority', params.priority);
    if (params?.priorities?.length) search.set('priorities', params.priorities.join(','));
    if (params?.assignee_id) search.set('assignee_id', params.assignee_id);
    if (params?.assignee_ids?.length) search.set('assignee_ids', params.assignee_ids.join(','));
    if (params?.assignee_types?.length)
      search.set('assignee_types', params.assignee_types.join(','));
    if (params?.creator_id) search.set('creator_id', params.creator_id);
    if (params?.project_id) search.set('project_id', params.project_id);
    if (params?.assignee_filters?.length) {
      search.set(
        'assignee_filters',
        params.assignee_filters.map((f) => `${f.type}:${f.id}`).join(','),
      );
    }
    if (params?.include_no_assignee) search.set('include_no_assignee', 'true');
    if (params?.creator_filters?.length) {
      search.set(
        'creator_filters',
        params.creator_filters.map((f) => `${f.type}:${f.id}`).join(','),
      );
    }
    if (params?.project_ids?.length) search.set('project_ids', params.project_ids.join(','));
    if (params?.include_no_project) search.set('include_no_project', 'true');
    if (params?.label_ids?.length) search.set('label_ids', params.label_ids.join(','));
    if (params?.top_level_only) search.set('top_level_only', 'true');
    if (params?.ids) search.set('ids', params.ids.join(','));
    if (params?.involves_user_id) search.set('involves_user_id', params.involves_user_id);
    if (params?.metadata && Object.keys(params.metadata).length > 0) {
      search.set('metadata', JSON.stringify(params.metadata));
    }
    if (params?.properties && Object.keys(params.properties).length > 0) {
      search.set('properties', JSON.stringify(params.properties));
    }
    if (params?.open_only) search.set('open_only', 'true');
    if (params?.scheduled) search.set('scheduled', 'true');
    if (params?.date_field) search.set('date_field', params.date_field);
    if (params?.date_start) search.set('date_start', params.date_start);
    if (params?.date_end) search.set('date_end', params.date_end);
    if (params?.sort_by) search.set('sort', params.sort_by);
    if (params?.sort_direction) search.set('direction', params.sort_direction);
    if (params?.ids) {
      const raw = await this.fetch<unknown>('/api/issues/query', {
        method: 'POST',
        body: JSON.stringify(Object.fromEntries(search)),
      });
      return parseWithFallback(raw, ListIssuesResponseSchema, EMPTY_LIST_ISSUES_RESPONSE, {
        endpoint: 'POST /api/issues/query',
      });
    }
    const path = `/api/issues?${search}`;
    const raw = await this.fetch<unknown>(path);
    return parseWithFallback(raw, ListIssuesResponseSchema, EMPTY_LIST_ISSUES_RESPONSE, {
      endpoint: 'GET /api/issues',
    });
  }

  async listGroupedIssues(params: ListGroupedIssuesParams): Promise<GroupedIssuesResponse> {
    const search = new URLSearchParams({ group_by: params.group_by });
    if (params.limit) search.set('limit', String(params.limit));
    if (params.offset) search.set('offset', String(params.offset));
    if (params.workspace_id) search.set('workspace_id', params.workspace_id);
    if (params.statuses?.length) search.set('statuses', params.statuses.join(','));
    if (params.priorities?.length) search.set('priorities', params.priorities.join(','));
    if (params.assignee_types?.length)
      search.set('assignee_types', params.assignee_types.join(','));
    if (params.assignee_id) search.set('assignee_id', params.assignee_id);
    if (params.assignee_ids?.length) search.set('assignee_ids', params.assignee_ids.join(','));
    if (params.creator_id) search.set('creator_id', params.creator_id);
    if (params.project_id) search.set('project_id', params.project_id);
    if (params.involves_user_id) search.set('involves_user_id', params.involves_user_id);
    if (params.metadata && Object.keys(params.metadata).length > 0) {
      search.set('metadata', JSON.stringify(params.metadata));
    }
    if (params.properties && Object.keys(params.properties).length > 0) {
      search.set('properties', JSON.stringify(params.properties));
    }
    if (params.assignee_filters?.length) {
      search.set(
        'assignee_filters',
        params.assignee_filters.map((f) => `${f.type}:${f.id}`).join(','),
      );
    }
    if (params.include_no_assignee) search.set('include_no_assignee', 'true');
    if (params.creator_filters?.length) {
      search.set(
        'creator_filters',
        params.creator_filters.map((f) => `${f.type}:${f.id}`).join(','),
      );
    }
    if (params.project_ids?.length) search.set('project_ids', params.project_ids.join(','));
    if (params.include_no_project) search.set('include_no_project', 'true');
    if (params.label_ids?.length) search.set('label_ids', params.label_ids.join(','));
    if (params.group_assignee_type) search.set('group_assignee_type', params.group_assignee_type);
    if (params.group_assignee_id) search.set('group_assignee_id', params.group_assignee_id);
    if (params.date_field) search.set('date_field', params.date_field);
    if (params.date_start) search.set('date_start', params.date_start);
    if (params.date_end) search.set('date_end', params.date_end);
    if (params.sort_by) search.set('sort', params.sort_by);
    if (params.sort_direction) search.set('direction', params.sort_direction);
    const raw = await this.fetch<unknown>(`/api/issues/grouped?${search}`);
    return parseWithFallback(raw, GroupedIssuesResponseSchema, EMPTY_GROUPED_ISSUES_RESPONSE, {
      endpoint: 'GET /api/issues/grouped',
    });
  }

  async listIssueTableGroups(params: IssueTableGroupsRequest): Promise<IssueTableGroupsResponse> {
    const raw = await this.fetch<unknown>('/api/issues/table/groups', {
      method: 'POST',
      body: JSON.stringify(params),
    });
    return parseWithFallback(
      raw,
      IssueTableGroupsResponseSchema,
      EMPTY_ISSUE_TABLE_GROUPS_RESPONSE,
      { endpoint: 'POST /api/issues/table/groups' },
    );
  }

  async listIssueTableRows(params: IssueTableRowsRequest): Promise<IssueTableRowsResponse> {
    const raw = await this.fetch<unknown>('/api/issues/table/rows', {
      method: 'POST',
      body: JSON.stringify(params),
    });
    return parseWithFallback(raw, IssueTableRowsResponseSchema, EMPTY_ISSUE_TABLE_ROWS_RESPONSE, {
      endpoint: 'POST /api/issues/table/rows',
    });
  }

  async listIssueTableFacets(params: IssueTableFacetsRequest): Promise<IssueTableFacetsResponse> {
    const raw = await this.fetch<unknown>('/api/issues/table/facets', {
      method: 'POST',
      body: JSON.stringify(params),
    });
    return parseWithFallback(
      raw,
      IssueTableFacetsResponseSchema,
      EMPTY_ISSUE_TABLE_FACETS_RESPONSE,
      { endpoint: 'POST /api/issues/table/facets' },
    );
  }

  async searchIssues(params: {
    q: string;
    limit?: number;
    offset?: number;
    include_closed?: boolean;
    signal?: AbortSignal;
  }): Promise<SearchIssuesResponse> {
    const search = new URLSearchParams({ q: params.q });
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    if (params.include_closed) search.set('include_closed', 'true');
    const raw = await this.fetch<unknown>(
      `/api/issues/search?${search}`,
      params.signal ? { signal: params.signal } : undefined,
    );
    return parseWithFallback(raw, SearchIssuesResponseSchema, EMPTY_SEARCH_ISSUES_RESPONSE, {
      endpoint: 'GET /api/issues/search',
    });
  }

  async searchProjects(params: {
    q: string;
    limit?: number;
    offset?: number;
    include_closed?: boolean;
    signal?: AbortSignal;
  }): Promise<SearchProjectsResponse> {
    const search = new URLSearchParams({ q: params.q });
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    if (params.include_closed) search.set('include_closed', 'true');
    const raw = await this.fetch<unknown>(
      `/api/projects/search?${search}`,
      params.signal ? { signal: params.signal } : undefined,
    );
    return parseWithFallback(raw, SearchProjectsResponseSchema, EMPTY_SEARCH_PROJECTS_RESPONSE, {
      endpoint: 'GET /api/projects/search',
    });
  }

  async getIssue(id: string): Promise<Issue> {
    return this.fetch(`/api/issues/${id}`);
  }

  async createIssue(data: CreateIssueRequest): Promise<Issue> {
    const raw = await this.fetch<unknown>('/api/issues', {
      method: 'POST',
      body: JSON.stringify(data),
    });
    const issue = parseWithFallback<Issue | null>(raw, CreateIssueResponseSchema, null, {
      endpoint: 'POST /api/issues',
    });
    if (!issue) {
      throw new Error();
    }
    return issue;
  }

  async quickCreateIssue(data: {
    agent_id?: string;
    squad_id?: string;
    prompt: string;
    priority?: IssuePriority;
    due_date?: string;
    project_id?: string | null;
    parent_issue_id?: string | null;
    attachment_ids?: string[];
  }): Promise<{ task_id: string }> {
    return this.fetch('/api/issues/quick-create', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async createFeedback(data: {
    message: string;
    url?: string;
    workspace_id?: string;
    kind?: FeedbackKind;
  }): Promise<CreateFeedbackResponse> {
    const raw = await this.fetch<unknown>('/api/feedback', {
      method: 'POST',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, CreateFeedbackResponseSchema, EMPTY_CREATE_FEEDBACK_RESPONSE, {
      endpoint: 'POST /api/feedback',
    });
  }

  async upsertClientUsage(data: ClientUsageRequest): Promise<void> {
    await this.fetch('/api/client-usage', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateIssue(id: string, data: UpdateIssueRequest): Promise<Issue> {
    return this.fetch(`/api/issues/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async setIssueMetadataKey(
    issueId: string,
    key: string,
    value: IssueMetadataValue,
  ): Promise<IssueMetadataResponse> {
    const raw = await this.fetch<unknown>(
      `/api/issues/${issueId}/metadata/${encodeURIComponent(key)}`,
      {
        method: 'PUT',
        body: JSON.stringify({ value }),
      },
    );
    return parseWithFallback(raw, IssueMetadataResponseSchema, EMPTY_ISSUE_METADATA_RESPONSE, {
      endpoint: 'PUT /api/issues/{id}/metadata/{key}',
    });
  }

  async moveIssue(id: string, data: MoveIssueRequest): Promise<Issue> {
    return this.fetch(`/api/issues/${id}/move`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async listChildIssues(id: string): Promise<{ issues: Issue[] }> {
    const raw = await this.fetch<unknown>(`/api/issues/${id}/children`);
    return parseWithFallback(
      raw,
      ChildIssuesResponseSchema,
      { issues: [] },
      {
        endpoint: 'GET /api/issues/:id/children',
      },
    );
  }

  async listChildrenByParents(parentIds: string[]): Promise<{ issues: Issue[] }> {
    const raw = await this.fetch<unknown>(`/api/issues/children?parent_ids=${parentIds.join(',')}`);
    return parseWithFallback(
      raw,
      ChildIssuesResponseSchema,
      { issues: [] },
      {
        endpoint: 'GET /api/issues/children',
      },
    );
  }

  async getChildIssueProgress(): Promise<{
    progress: { parent_issue_id: string; total: number; done: number }[];
  }> {
    return this.fetch('/api/issues/child-progress');
  }

  async deleteIssue(id: string): Promise<void> {
    await this.fetch(`/api/issues/${id}`, { method: 'DELETE' });
  }

  async batchUpdateIssues(
    issueIds: string[],
    updates: UpdateIssueRequest,
  ): Promise<{ updated: number }> {
    return this.fetch('/api/issues/batch-update', {
      method: 'POST',
      body: JSON.stringify({ issue_ids: issueIds, updates }),
    });
  }

  async batchDeleteIssues(issueIds: string[]): Promise<{ deleted: number }> {
    return this.fetch('/api/issues/batch-delete', {
      method: 'POST',
      body: JSON.stringify({ issue_ids: issueIds }),
    });
  }

  async listComments(issueId: string): Promise<Comment[]> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/comments`);
    return parseWithFallback(raw, CommentsListSchema, [], {
      endpoint: 'GET /api/issues/:id/comments',
    });
  }

  async createComment(
    issueId: string,
    content: string,
    type?: string,
    parentId?: string,
    attachmentIds?: string[],
    suppressAgentIds?: string[],
  ): Promise<Comment> {
    return this.fetch(`/api/issues/${issueId}/comments`, {
      method: 'POST',
      body: JSON.stringify({
        content,
        type: type ?? 'comment',
        ...(parentId ? { parent_id: parentId } : {}),
        ...(attachmentIds?.length ? { attachment_ids: attachmentIds } : {}),
        ...(suppressAgentIds?.length ? { suppress_agent_ids: suppressAgentIds } : {}),
      }),
    });
  }

  async previewCommentTriggers(
    issueId: string,
    content: string,
    parentId?: string,
    editingCommentId?: string,
  ): Promise<CommentTriggerPreview> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/comments/trigger-preview`, {
      method: 'POST',
      body: JSON.stringify({
        content,
        ...(parentId ? { parent_id: parentId } : {}),
        ...(editingCommentId ? { editing_comment_id: editingCommentId } : {}),
      }),
    });
    return parseWithFallback(
      raw,
      CommentTriggerPreviewSchema,
      { agents: [] },
      {
        endpoint: 'POST /api/issues/:id/comments/trigger-preview',
      },
    );
  }

  async previewIssueTrigger(params: IssueTriggerPreviewParams): Promise<IssueTriggerPreview> {
    const raw = await this.fetch<unknown>('/api/issues/preview-trigger', {
      method: 'POST',
      body: JSON.stringify({
        ...(params.issueIds?.length ? { issue_ids: params.issueIds } : {}),
        ...(params.isCreate ? { is_create: true } : {}),
        ...(params.assigneeType ? { assignee_type: params.assigneeType } : {}),
        ...(params.assigneeId ? { assignee_id: params.assigneeId } : {}),
        ...(params.status ? { status: params.status } : {}),
      }),
    });
    return parseWithFallback(
      raw,
      IssueTriggerPreviewSchema,
      { triggers: [], total_count: 0 },
      {
        endpoint: 'POST /api/issues/preview-trigger',
      },
    );
  }

  async listTimeline(issueId: string): Promise<TimelineEntry[]> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/timeline`);
    return parseWithFallback(raw, TimelineEntriesSchema, EMPTY_TIMELINE_ENTRIES, {
      endpoint: 'GET /api/issues/:id/timeline',
    });
  }

  async getAssigneeFrequency(): Promise<AssigneeFrequencyEntry[]> {
    return this.fetch('/api/assignee-frequency');
  }

  async updateComment(
    commentId: string,
    content: string,
    attachmentIds?: string[],
    suppressAgentIds?: string[],
  ): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}`, {
      method: 'PUT',
      body: JSON.stringify({
        content,
        attachment_ids: attachmentIds,
        ...(suppressAgentIds?.length ? { suppress_agent_ids: suppressAgentIds } : {}),
      }),
    });
  }

  async deleteComment(commentId: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}`, { method: 'DELETE' });
  }

  async resolveComment(commentId: string): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}/resolve`, { method: 'POST' });
  }

  async unresolveComment(commentId: string): Promise<Comment> {
    return this.fetch(`/api/comments/${commentId}/resolve`, { method: 'DELETE' });
  }

  async addReaction(commentId: string, emoji: string): Promise<Reaction> {
    return this.fetch(`/api/comments/${commentId}/reactions`, {
      method: 'POST',
      body: JSON.stringify({ emoji }),
    });
  }

  async removeReaction(commentId: string, emoji: string): Promise<void> {
    await this.fetch(`/api/comments/${commentId}/reactions`, {
      method: 'DELETE',
      body: JSON.stringify({ emoji }),
    });
  }

  async addIssueReaction(issueId: string, emoji: string): Promise<IssueReaction> {
    return this.fetch(`/api/issues/${issueId}/reactions`, {
      method: 'POST',
      body: JSON.stringify({ emoji }),
    });
  }

  async removeIssueReaction(issueId: string, emoji: string): Promise<void> {
    await this.fetch(`/api/issues/${issueId}/reactions`, {
      method: 'DELETE',
      body: JSON.stringify({ emoji }),
    });
  }

  async listIssueSubscribers(issueId: string): Promise<IssueSubscriber[]> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/subscribers`);
    return parseWithFallback(raw, SubscribersListSchema, [], {
      endpoint: 'GET /api/issues/:id/subscribers',
    });
  }

  async subscribeToIssue(issueId: string, userId?: string, userType?: string): Promise<void> {
    const body: Record<string, string> = {};
    if (userId) body.user_id = userId;
    if (userType) body.user_type = userType;
    await this.fetch(`/api/issues/${issueId}/subscribe`, {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  async unsubscribeFromIssue(issueId: string, userId?: string, userType?: string): Promise<void> {
    const body: Record<string, string> = {};
    if (userId) body.user_id = userId;
    if (userType) body.user_type = userType;
    await this.fetch(`/api/issues/${issueId}/unsubscribe`, {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  async listAgents(params?: {
    workspace_id?: string;
    include_archived?: boolean;
  }): Promise<Agent[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set('workspace_id', params.workspace_id);
    if (params?.include_archived) search.set('include_archived', 'true');
    return this.fetch(`/api/agents?${search}`);
  }

  async getAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}`);
  }

  async createAgent(data: CreateAgentRequest): Promise<Agent> {
    return this.fetch('/api/agents', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async createAgentBuilderSession(data: {
    runtime_id: string;
    model?: string;
  }): Promise<AgentBuilderSession> {
    const raw = await this.fetch<unknown>('/api/agent-builder/sessions', {
      method: 'POST',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, AgentBuilderSessionSchema, EMPTY_AGENT_BUILDER_SESSION, {
      endpoint: 'POST /api/agent-builder/sessions',
    });
  }

  async switchAgentBuilderRuntime(
    sessionId: string,
    data: { runtime_id: string },
  ): Promise<AgentBuilderRuntimeSwitch> {
    const raw = await this.fetch<unknown>(`/api/agent-builder/sessions/${sessionId}/runtime`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
    return parseWithFallback(
      raw,
      AgentBuilderRuntimeSwitchSchema,
      agentBuilderRuntimeSwitchFallback(data.runtime_id),
      { endpoint: 'PATCH /api/agent-builder/sessions/{id}/runtime' },
    );
  }

  async listAgentTemplates(): Promise<AgentTemplateSummary[]> {
    const raw = await this.fetch<unknown>('/api/agent-templates');
    return parseWithFallback(
      raw,
      AgentTemplateSummaryListSchema,
      EMPTY_AGENT_TEMPLATE_SUMMARY_LIST,
      { endpoint: 'GET /api/agent-templates' },
    );
  }

  async getAgentTemplate(slug: string): Promise<AgentTemplate> {
    const raw = await this.fetch<unknown>(`/api/agent-templates/${encodeURIComponent(slug)}`);
    return parseWithFallback(
      raw,
      AgentTemplateSchema,
      { ...EMPTY_AGENT_TEMPLATE_DETAIL, slug },
      { endpoint: 'GET /api/agent-templates/:slug' },
    );
  }

  async createAgentFromTemplate(
    data: CreateAgentFromTemplateRequest,
  ): Promise<CreateAgentFromTemplateResponse> {
    const raw = await this.fetch<unknown>('/api/agents/from-template', {
      method: 'POST',
      body: JSON.stringify(data),
    });
    return parseWithFallback(
      raw,
      CreateAgentFromTemplateResponseSchema,
      EMPTY_CREATE_AGENT_FROM_TEMPLATE_RESPONSE,
      { endpoint: 'POST /api/agents/from-template' },
    );
  }

  async updateAgent(id: string, data: UpdateAgentRequest): Promise<Agent> {
    return this.fetch(`/api/agents/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async archiveAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}/archive`, { method: 'POST' });
  }

  async getAgentEnv(id: string): Promise<AgentEnvResponse> {
    const raw = await this.fetch<unknown>(`/api/agents/${id}/env`);
    return parseStrict<AgentEnvResponse>(raw, AgentEnvResponseSchema, {
      endpoint: 'GET /api/agents/{id}/env',
      omitReceivedFromLog: true,
    });
  }

  async updateAgentEnv(id: string, data: UpdateAgentEnvRequest): Promise<AgentEnvResponse> {
    const raw = await this.fetch<unknown>(`/api/agents/${id}/env`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
    return parseStrict<AgentEnvResponse>(raw, AgentEnvResponseSchema, {
      endpoint: 'PUT /api/agents/{id}/env',
      omitReceivedFromLog: true,
    });
  }

  async restoreAgent(id: string): Promise<Agent> {
    return this.fetch(`/api/agents/${id}/restore`, { method: 'POST' });
  }

  async cancelAgentTasks(id: string): Promise<{ cancelled: number }> {
    return this.fetch(`/api/agents/${id}/cancel-tasks`, { method: 'POST' });
  }

  async listRuntimes(params?: { workspace_id?: string; owner?: 'me' }): Promise<AgentRuntime[]> {
    const search = new URLSearchParams();
    if (params?.workspace_id) search.set('workspace_id', params.workspace_id);
    if (params?.owner) search.set('owner', params.owner);
    return this.fetch(`/api/runtimes?${search}`);
  }

  async listCloudRuntimeNodes(params?: ListCloudRuntimeNodesParams): Promise<CloudRuntimeNode[]> {
    const search = new URLSearchParams();
    if (params?.limit !== undefined) search.set('limit', String(params.limit));
    if (params?.offset !== undefined) search.set('offset', String(params.offset));
    const query = search.toString();
    const raw = await this.fetch<unknown>(`/api/cloud-runtime/nodes${query ? `?${query}` : ''}`);
    return parseWithFallback(raw, CloudRuntimeNodeListSchema, EMPTY_CLOUD_RUNTIME_NODE_LIST, {
      endpoint: 'GET /api/cloud-runtime/nodes',
    });
  }

  async createCloudRuntimeNode(data: CreateCloudRuntimeNodeRequest): Promise<CloudRuntimeNode> {
    const res = await this.fetchRaw('/api/cloud-runtime/nodes', {
      method: 'POST',
      body: JSON.stringify(data),
      extraHeaders: { 'Content-Type': 'application/json' },
    });
    const raw = (await res.json()) as unknown;
    return parseWithFallback(raw, CloudRuntimeNodeSchema, EMPTY_CLOUD_RUNTIME_NODE, {
      endpoint: 'POST /api/cloud-runtime/nodes',
    });
  }

  async deleteCloudRuntimeNode(instanceId: string): Promise<void> {
    await this.fetchRaw('/api/cloud-runtime/nodes', {
      method: 'DELETE',
      body: JSON.stringify({ instance_id: instanceId }),
      extraHeaders: { 'Content-Type': 'application/json' },
    });
  }

  async getCloudBillingBalance(): Promise<BillingBalance> {
    const raw = await this.fetch<unknown>('/api/cloud-billing/balance');
    return parseWithFallback(raw, BillingBalanceSchema, EMPTY_BILLING_BALANCE, {
      endpoint: 'GET /api/cloud-billing/balance',
    });
  }

  async listCloudBillingTransactions(params?: {
    page?: number;
    page_size?: number;
  }): Promise<BillingTransactionsPage> {
    const search = new URLSearchParams();
    if (params?.page !== undefined) search.set('page', String(params.page));
    if (params?.page_size !== undefined) search.set('page_size', String(params.page_size));
    const query = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/cloud-billing/transactions${query ? `?${query}` : ''}`,
    );
    return parseWithFallback(raw, BillingTransactionsPageSchema, EMPTY_BILLING_TRANSACTIONS_PAGE, {
      endpoint: 'GET /api/cloud-billing/transactions',
    });
  }

  async listCloudBillingBatches(params?: {
    page?: number;
    page_size?: number;
  }): Promise<BillingBatchesPage> {
    const search = new URLSearchParams();
    if (params?.page !== undefined) search.set('page', String(params.page));
    if (params?.page_size !== undefined) search.set('page_size', String(params.page_size));
    const query = search.toString();
    const raw = await this.fetch<unknown>(`/api/cloud-billing/batches${query ? `?${query}` : ''}`);
    return parseWithFallback(raw, BillingBatchesPageSchema, EMPTY_BILLING_BATCHES_PAGE, {
      endpoint: 'GET /api/cloud-billing/batches',
    });
  }

  async listCloudBillingTopups(params?: {
    page?: number;
    page_size?: number;
  }): Promise<BillingTopupsPage> {
    const search = new URLSearchParams();
    if (params?.page !== undefined) search.set('page', String(params.page));
    if (params?.page_size !== undefined) search.set('page_size', String(params.page_size));
    const query = search.toString();
    const raw = await this.fetch<unknown>(`/api/cloud-billing/topups${query ? `?${query}` : ''}`);
    return parseWithFallback(raw, BillingTopupsPageSchema, EMPTY_BILLING_TOPUPS_PAGE, {
      endpoint: 'GET /api/cloud-billing/topups',
    });
  }

  async listCloudBillingPriceTiers(): Promise<BillingPriceTier[]> {
    const raw = await this.fetch<unknown>('/api/cloud-billing/price-tiers');
    return parseWithFallback(raw, BillingPriceTierListSchema, EMPTY_BILLING_PRICE_TIER_LIST, {
      endpoint: 'GET /api/cloud-billing/price-tiers',
    });
  }

  async createCloudBillingCheckoutSession(
    data: CreateBillingCheckoutSessionRequest,
  ): Promise<CreateBillingCheckoutSessionResponse> {
    const res = await this.fetchRaw('/api/cloud-billing/checkout-sessions', {
      method: 'POST',
      body: JSON.stringify(data),
      extraHeaders: { 'Content-Type': 'application/json' },
    });
    const raw = (await res.json()) as unknown;
    return parseWithFallback(
      raw,
      CreateBillingCheckoutSessionResponseSchema,
      EMPTY_CREATE_BILLING_CHECKOUT_SESSION_RESPONSE,
      { endpoint: 'POST /api/cloud-billing/checkout-sessions' },
    );
  }

  async getCloudBillingCheckoutSession(sessionId: string): Promise<BillingCheckoutSessionStatus> {
    const raw = await this.fetch<unknown>(
      `/api/cloud-billing/checkout-sessions/${encodeURIComponent(sessionId)}`,
    );
    return parseWithFallback(
      raw,
      BillingCheckoutSessionStatusSchema,
      EMPTY_BILLING_CHECKOUT_SESSION_STATUS,
      { endpoint: 'GET /api/cloud-billing/checkout-sessions/{sessionId}' },
    );
  }

  async createCloudBillingPortalSession(): Promise<CreateBillingPortalSessionResponse> {
    const res = await this.fetchRaw('/api/cloud-billing/portal-sessions', {
      method: 'POST',
      // Body is intentionally absent — the upstream endpoint requires no
      // payload today. fetchRaw with no body skips the Content-Type
      // default; that's fine because there's nothing to declare.
    });
    const raw = (await res.json()) as unknown;
    return parseWithFallback(
      raw,
      CreateBillingPortalSessionResponseSchema,
      EMPTY_CREATE_BILLING_PORTAL_SESSION_RESPONSE,
      { endpoint: 'POST /api/cloud-billing/portal-sessions' },
    );
  }

  async deleteRuntime(runtimeId: string): Promise<void> {
    await this.fetch(`/api/runtimes/${runtimeId}`, { method: 'DELETE' });
  }

  async archiveAgentsAndDeleteRuntime(
    runtimeId: string,
    expectedActiveAgentIds: string[],
  ): Promise<{ status: string; agents_archived: number; tasks_cancelled: number }> {
    return this.fetch(`/api/runtimes/${runtimeId}/archive-agents-and-delete`, {
      method: 'POST',
      body: JSON.stringify({ expected_active_agent_ids: expectedActiveAgentIds }),
    });
  }

  async updateRuntime(
    runtimeId: string,
    patch: {
      visibility?: 'private' | 'public';
      custom_name?: string;
      apply_to_machine?: boolean;
    },
  ): Promise<AgentRuntime> {
    return this.fetch(`/api/runtimes/${runtimeId}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    });
  }

  async listRuntimeProfiles(workspaceId: string): Promise<RuntimeProfile[]> {
    const res = await this.fetch<{ runtime_profiles?: RuntimeProfile[] }>(
      `/api/workspaces/${workspaceId}/runtime-profiles`,
    );
    return res.runtime_profiles ?? [];
  }

  async getRuntimeProfile(workspaceId: string, profileId: string): Promise<RuntimeProfile> {
    return this.fetch(`/api/workspaces/${workspaceId}/runtime-profiles/${profileId}`);
  }

  async createRuntimeProfile(
    workspaceId: string,
    body: CreateRuntimeProfileRequest,
  ): Promise<RuntimeProfile> {
    return this.fetch(`/api/workspaces/${workspaceId}/runtime-profiles`, {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  async updateRuntimeProfile(
    workspaceId: string,
    profileId: string,
    patch: UpdateRuntimeProfileRequest,
  ): Promise<RuntimeProfile> {
    return this.fetch(`/api/workspaces/${workspaceId}/runtime-profiles/${profileId}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    });
  }

  async deleteRuntimeProfile(workspaceId: string, profileId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/runtime-profiles/${profileId}`, {
      method: 'DELETE',
    });
  }

  async getRuntimeUsage(
    runtimeId: string,
    params?: { days?: number; tz?: string },
  ): Promise<RuntimeUsage[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set('days', String(params.days));
    if (params?.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/runtimes/${runtimeId}/usage?${search}`);
    return parseWithFallback<RuntimeUsage[]>(raw, RuntimeUsageListSchema, [], {
      endpoint: 'GET /api/runtimes/:id/usage',
    });
  }

  async getRuntimeTaskActivity(
    runtimeId: string,
    params?: { tz?: string },
  ): Promise<RuntimeHourlyActivity[]> {
    const search = new URLSearchParams();
    if (params?.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/runtimes/${runtimeId}/activity?${search}`);
    return parseWithFallback<RuntimeHourlyActivity[]>(raw, RuntimeHourlyActivityListSchema, [], {
      endpoint: 'GET /api/runtimes/:id/activity',
    });
  }

  async getRuntimeUsageByAgent(
    runtimeId: string,
    params?: { days?: number; tz?: string },
  ): Promise<RuntimeUsageByAgent[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set('days', String(params.days));
    if (params?.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/runtimes/${runtimeId}/usage/by-agent?${search}`);
    return parseWithFallback<RuntimeUsageByAgent[]>(raw, RuntimeUsageByAgentListSchema, [], {
      endpoint: 'GET /api/runtimes/:id/usage/by-agent',
    });
  }

  async getRuntimeUsageByHour(
    runtimeId: string,
    params?: { days?: number; tz?: string },
  ): Promise<RuntimeUsageByHour[]> {
    const search = new URLSearchParams();
    if (params?.days) search.set('days', String(params.days));
    if (params?.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/runtimes/${runtimeId}/usage/by-hour?${search}`);
    return parseWithFallback<RuntimeUsageByHour[]>(raw, RuntimeUsageByHourListSchema, [], {
      endpoint: 'GET /api/runtimes/:id/usage/by-hour',
    });
  }

  async getDashboardUsageDaily(params: {
    days?: number;
    project_id?: string | null;
    tz?: string;
  }): Promise<DashboardUsageDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set('days', String(params.days));
    if (params.project_id) search.set('project_id', params.project_id);
    if (params.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/usage/daily?${search}`);
    return parseWithFallback<DashboardUsageDaily[]>(raw, DashboardUsageDailyListSchema, [], {
      endpoint: 'GET /api/dashboard/usage/daily',
    });
  }

  async getDashboardUsageByAgent(params: {
    days?: number;
    project_id?: string | null;
    tz?: string;
  }): Promise<DashboardUsageByAgent[]> {
    const search = new URLSearchParams();
    if (params.days) search.set('days', String(params.days));
    if (params.project_id) search.set('project_id', params.project_id);
    if (params.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/usage/by-agent?${search}`);
    return parseWithFallback<DashboardUsageByAgent[]>(raw, DashboardUsageByAgentListSchema, [], {
      endpoint: 'GET /api/dashboard/usage/by-agent',
    });
  }

  async getDashboardAgentRunTime(params: {
    days?: number;
    project_id?: string | null;
    tz?: string;
  }): Promise<DashboardAgentRunTime[]> {
    const search = new URLSearchParams();
    if (params.days) search.set('days', String(params.days));
    if (params.project_id) search.set('project_id', params.project_id);
    if (params.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/agent-runtime?${search}`);
    return parseWithFallback<DashboardAgentRunTime[]>(raw, DashboardAgentRunTimeListSchema, [], {
      endpoint: 'GET /api/dashboard/agent-runtime',
    });
  }

  async getDashboardRunTimeDaily(params: {
    days?: number;
    project_id?: string | null;
    tz?: string;
  }): Promise<DashboardRunTimeDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set('days', String(params.days));
    if (params.project_id) search.set('project_id', params.project_id);
    if (params.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/runtime/daily?${search}`);
    return parseWithFallback<DashboardRunTimeDaily[]>(raw, DashboardRunTimeDailyListSchema, [], {
      endpoint: 'GET /api/dashboard/runtime/daily',
    });
  }

  async getDashboardFailuresDaily(params: {
    days?: number;
    project_id?: string | null;
    tz?: string;
  }): Promise<DashboardFailureDaily[]> {
    const search = new URLSearchParams();
    if (params.days) search.set('days', String(params.days));
    if (params.project_id) search.set('project_id', params.project_id);
    if (params.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/failures/daily?${search}`);
    return parseWithFallback<DashboardFailureDaily[]>(raw, DashboardFailureDailyListSchema, [], {
      endpoint: 'GET /api/dashboard/failures/daily',
    });
  }

  async getDashboardFailuresByAgent(params: {
    days?: number;
    project_id?: string | null;
    tz?: string;
  }): Promise<DashboardFailureByAgent[]> {
    const search = new URLSearchParams();
    if (params.days) search.set('days', String(params.days));
    if (params.project_id) search.set('project_id', params.project_id);
    if (params.tz) search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/dashboard/failures/by-agent?${search}`);
    return parseWithFallback<DashboardFailureByAgent[]>(
      raw,
      DashboardFailureByAgentListSchema,
      [],
      { endpoint: 'GET /api/dashboard/failures/by-agent' },
    );
  }

  async initiateUpdate(runtimeId: string, targetVersion: string): Promise<RuntimeUpdate> {
    return this.fetch(`/api/runtimes/${runtimeId}/update`, {
      method: 'POST',
      body: JSON.stringify({ target_version: targetVersion }),
    });
  }

  async getUpdateResult(runtimeId: string, updateId: string): Promise<RuntimeUpdate> {
    return this.fetch(`/api/runtimes/${runtimeId}/update/${updateId}`);
  }

  async initiateListModels(runtimeId: string): Promise<RuntimeModelListRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/models`, { method: 'POST' });
  }

  async getListModelsResult(
    runtimeId: string,
    requestId: string,
  ): Promise<RuntimeModelListRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/models/${requestId}`);
  }

  async initiateListLocalSkills(runtimeId: string): Promise<RuntimeLocalSkillListRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/local-skills`, {
      method: 'POST',
    });
  }

  async getListLocalSkillsResult(
    runtimeId: string,
    requestId: string,
  ): Promise<RuntimeLocalSkillListRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/local-skills/${requestId}`);
  }

  async initiateImportLocalSkill(
    runtimeId: string,
    data: CreateRuntimeLocalSkillImportRequest,
  ): Promise<RuntimeLocalSkillImportRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/local-skills/import`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async getImportLocalSkillResult(
    runtimeId: string,
    requestId: string,
  ): Promise<RuntimeLocalSkillImportRequest> {
    return this.fetch(`/api/runtimes/${runtimeId}/local-skills/import/${requestId}`);
  }

  async initiateWorkspaceContentSync(runtimeId: string): Promise<WorkspaceContentSyncRequest> {
    const raw = await this.fetch<unknown>(`/api/runtimes/${runtimeId}/workspace-content`, {
      method: 'POST',
    });
    return parseWithFallback(
      raw,
      WorkspaceContentSyncRequestSchema,
      EMPTY_WORKSPACE_CONTENT_SYNC_REQUEST,
      { endpoint: 'POST /api/runtimes/{runtimeId}/workspace-content' },
    );
  }

  async getWorkspaceContentSyncResult(
    runtimeId: string,
    requestId: string,
  ): Promise<WorkspaceContentSyncRequest> {
    const raw = await this.fetch<unknown>(
      `/api/runtimes/${runtimeId}/workspace-content/${requestId}`,
    );
    return parseWithFallback(
      raw,
      WorkspaceContentSyncRequestSchema,
      EMPTY_WORKSPACE_CONTENT_SYNC_REQUEST,
      { endpoint: 'GET /api/runtimes/{runtimeId}/workspace-content/{requestId}' },
    );
  }

  async getEffectiveConfig(wsId: string): Promise<EffectiveConfigView> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/effective-config?${search}`);
    return parseWithFallback(raw, EffectiveConfigViewSchema, EMPTY_EFFECTIVE_CONFIG_VIEW, {
      endpoint: 'GET /api/effective-config',
    });
  }

  async getWorkspaceConfig(wsId: string): Promise<WorkspaceConfigView> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/workspace-config?${search}`);
    return parseWithFallback(raw, WorkspaceConfigViewSchema, EMPTY_WORKSPACE_CONFIG_VIEW, {
      endpoint: 'GET /api/workspace-config',
    });
  }

  async putWorkspaceConfig(
    wsId: string,
    patch: WorkspaceConfigPatch,
  ): Promise<WorkspaceConfigView> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/workspace-config?${search}`, {
      method: 'PUT',
      body: JSON.stringify(patch),
    });
    return parseWithFallback(raw, WorkspaceConfigViewSchema, EMPTY_WORKSPACE_CONFIG_VIEW, {
      endpoint: 'PUT /api/workspace-config',
    });
  }

  async getWorkspaceConfigOverride(
    wsId: string,
    userId: string,
  ): Promise<UserConfigOverrideView | null> {
    const search = new URLSearchParams({ workspace_id: wsId });
    let raw: unknown;
    try {
      raw = await this.fetch<unknown>(`/api/workspace-config/overrides/${userId}?${search}`);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) return null;
      throw err;
    }
    return parseWithFallback(raw, UserConfigOverrideViewSchema, EMPTY_USER_CONFIG_OVERRIDE_VIEW, {
      endpoint: 'GET /api/workspace-config/overrides/{userId}',
    });
  }

  async putWorkspaceConfigOverride(
    wsId: string,
    userId: string,
    patch: UserConfigOverridePatch,
  ): Promise<UserConfigOverrideView> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/workspace-config/overrides/${userId}?${search}`, {
      method: 'PUT',
      body: JSON.stringify(patch),
    });
    return parseWithFallback(raw, UserConfigOverrideViewSchema, EMPTY_USER_CONFIG_OVERRIDE_VIEW, {
      endpoint: 'PUT /api/workspace-config/overrides/{userId}',
    });
  }

  async deleteWorkspaceConfigOverride(wsId: string, userId: string): Promise<void> {
    const search = new URLSearchParams({ workspace_id: wsId });
    await this.fetch<void>(`/api/workspace-config/overrides/${userId}?${search}`, {
      method: 'DELETE',
    });
  }

  async getProvisioningPins(wsId: string): Promise<ProvisioningPinsView> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/provisioning/pins?${search}`);
    return parseWithFallback(raw, ProvisioningPinsViewSchema, EMPTY_PROVISIONING_PINS_VIEW, {
      endpoint: 'GET /api/provisioning/pins',
    });
  }

  async putProvisioningPins(
    wsId: string,
    pins: ProvisioningPinInput[],
  ): Promise<ProvisioningPinsView> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/provisioning/pins?${search}`, {
      method: 'PUT',
      body: JSON.stringify({ pins }),
    });
    return parseWithFallback(raw, ProvisioningPinsViewSchema, EMPTY_PROVISIONING_PINS_VIEW, {
      endpoint: 'PUT /api/provisioning/pins',
    });
  }

  async getProvisioningCatalog(wsId: string): Promise<ProvisioningCatalogView> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/provisioning/catalog?${search}`);
    return parseWithFallback(raw, ProvisioningCatalogViewSchema, EMPTY_PROVISIONING_CATALOG_VIEW, {
      endpoint: 'GET /api/provisioning/catalog',
    });
  }

  async listWorkspaceMcpServers(wsId: string): Promise<WorkspaceMcpServer[]> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/workspace-mcp-servers?${search}`);
    return parseWithFallback(raw, WorkspaceMcpServerListSchema, EMPTY_WORKSPACE_MCP_SERVERS, {
      endpoint: 'GET /api/workspace-mcp-servers',
    });
  }

  async createWorkspaceMcpServer(wsId: string, input: WorkspaceMcpServerInput): Promise<void> {
    const search = new URLSearchParams({ workspace_id: wsId });
    await this.fetch<unknown>(`/api/workspace-mcp-servers?${search}`, {
      method: 'POST',
      body: JSON.stringify(input),
    });
  }

  async updateWorkspaceMcpServer(
    wsId: string,
    serverId: string,
    input: WorkspaceMcpServerInput,
  ): Promise<void> {
    const search = new URLSearchParams({ workspace_id: wsId });
    await this.fetch<unknown>(
      `/api/workspace-mcp-servers/${encodeURIComponent(serverId)}?${search}`,
      { method: 'PUT', body: JSON.stringify(input) },
    );
  }

  async deleteWorkspaceMcpServer(wsId: string, serverId: string): Promise<void> {
    const search = new URLSearchParams({ workspace_id: wsId });
    await this.fetch<unknown>(
      `/api/workspace-mcp-servers/${encodeURIComponent(serverId)}?${search}`,
      { method: 'DELETE' },
    );
  }

  async setWorkspaceMcpCredentials(
    wsId: string,
    serverId: string,
    values: Record<string, string>,
  ): Promise<void> {
    const search = new URLSearchParams({ workspace_id: wsId });
    await this.fetch<unknown>(
      `/api/workspace-mcp-servers/${encodeURIComponent(serverId)}/credentials?${search}`,
      { method: 'PUT', body: JSON.stringify({ values }) },
    );
  }

  async clearWorkspaceMcpCredentials(wsId: string, serverId: string): Promise<void> {
    const search = new URLSearchParams({ workspace_id: wsId });
    await this.fetch<unknown>(
      `/api/workspace-mcp-servers/${encodeURIComponent(serverId)}/credentials?${search}`,
      { method: 'DELETE' },
    );
  }

  async listAgentMcpServers(wsId: string, agentId: string): Promise<WorkspaceMcpServer[]> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(
      `/api/agents/${encodeURIComponent(agentId)}/mcp-servers?${search}`,
    );
    return parseWithFallback(raw, WorkspaceMcpServerListSchema, EMPTY_WORKSPACE_MCP_SERVERS, {
      endpoint: 'GET /api/agents/{id}/mcp-servers',
    });
  }

  async addAgentMcpServer(
    wsId: string,
    agentId: string,
    serverId: string,
  ): Promise<WorkspaceMcpServer[]> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(
      `/api/agents/${encodeURIComponent(agentId)}/mcp-servers?${search}`,
      { method: 'POST', body: JSON.stringify({ server_id: serverId }) },
    );
    return parseWithFallback(raw, WorkspaceMcpServerListSchema, EMPTY_WORKSPACE_MCP_SERVERS, {
      endpoint: 'POST /api/agents/{id}/mcp-servers',
    });
  }

  async setAgentMcpServerEnabled(
    wsId: string,
    agentId: string,
    serverId: string,
    enabled: boolean,
  ): Promise<WorkspaceMcpServer[]> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(
      `/api/agents/${encodeURIComponent(agentId)}/mcp-servers/${encodeURIComponent(serverId)}/enabled?${search}`,
      { method: 'PUT', body: JSON.stringify({ enabled }) },
    );
    return parseWithFallback(raw, WorkspaceMcpServerListSchema, EMPTY_WORKSPACE_MCP_SERVERS, {
      endpoint: 'PUT /api/agents/{id}/mcp-servers/{serverId}/enabled',
    });
  }

  async removeAgentMcpServer(
    wsId: string,
    agentId: string,
    serverId: string,
  ): Promise<WorkspaceMcpServer[]> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(
      `/api/agents/${encodeURIComponent(agentId)}/mcp-servers/${encodeURIComponent(serverId)}?${search}`,
      { method: 'DELETE' },
    );
    return parseWithFallback(raw, WorkspaceMcpServerListSchema, EMPTY_WORKSPACE_MCP_SERVERS, {
      endpoint: 'DELETE /api/agents/{id}/mcp-servers/{serverId}',
    });
  }

  async listDeploymentMcpServers(): Promise<DeploymentMcpServer[]> {
    const raw = await this.fetch<unknown>(`/api/deployment/mcp-servers`);
    return parseWithFallback(raw, DeploymentMcpServerListSchema, EMPTY_DEPLOYMENT_MCP_SERVERS, {
      endpoint: 'GET /api/deployment/mcp-servers',
    });
  }

  async createDeploymentMcpServer(input: DeploymentMcpServerInput): Promise<void> {
    await this.fetch<unknown>(`/api/deployment/mcp-servers`, {
      method: 'POST',
      body: JSON.stringify(input),
    });
  }

  async updateDeploymentMcpServer(
    serverId: string,
    input: DeploymentMcpServerInput,
  ): Promise<void> {
    await this.fetch<unknown>(`/api/deployment/mcp-servers/${encodeURIComponent(serverId)}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    });
  }

  async deleteDeploymentMcpServer(serverId: string): Promise<void> {
    await this.fetch<unknown>(`/api/deployment/mcp-servers/${encodeURIComponent(serverId)}`, {
      method: 'DELETE',
    });
  }

  async listWorkspaceDeploymentMcpServers(wsId: string): Promise<DeploymentMcpServer[]> {
    const search = new URLSearchParams({ workspace_id: wsId });
    const raw = await this.fetch<unknown>(`/api/deployment-mcp-servers?${search}`);
    return parseWithFallback(raw, DeploymentMcpServerListSchema, EMPTY_DEPLOYMENT_MCP_SERVERS, {
      endpoint: 'GET /api/deployment-mcp-servers',
    });
  }

  async setWorkspaceDeploymentMcpServerEnabled(
    wsId: string,
    serverId: string,
    enabled: boolean,
  ): Promise<void> {
    const search = new URLSearchParams({ workspace_id: wsId });
    await this.fetch<unknown>(
      `/api/deployment-mcp-servers/${encodeURIComponent(serverId)}/enabled?${search}`,
      { method: 'PUT', body: JSON.stringify({ enabled }) },
    );
  }

  async listDeploymentAdmins(): Promise<DeploymentAdminEntry[]> {
    const raw = await this.fetch<unknown>(`/api/deployment/admins`);
    return parseWithFallback(raw, DeploymentAdminListSchema, EMPTY_DEPLOYMENT_ADMIN_LIST, {
      endpoint: 'GET /api/deployment/admins',
    });
  }

  async addDeploymentAdmin(email: string): Promise<DeploymentAdminAddResult> {
    const raw = await this.fetch<unknown>(`/api/deployment/admins`, {
      method: 'POST',
      body: JSON.stringify({ email }),
    });
    const body = (raw ?? {}) as { status?: unknown; confirm_hint?: unknown };
    const looksPending =
      body.status === 'pending' ||
      (typeof body.confirm_hint === 'string' && body.confirm_hint.length > 0);
    if (looksPending) {
      return {
        kind: 'pending',
        pending: parseWithFallback(
          raw,
          DeploymentAdminPendingSchema,
          EMPTY_DEPLOYMENT_ADMIN_PENDING,
          { endpoint: 'POST /api/deployment/admins' },
        ),
      };
    }
    return {
      kind: 'already_admin',
      entry: parseWithFallback<DeploymentAdminEntry>(
        raw,
        DeploymentAdminEntrySchema,
        { user_id: '' },
        { endpoint: 'POST /api/deployment/admins' },
      ),
    };
  }

  async deactivateDeploymentUser(userId: string): Promise<DeploymentUser> {
    const raw = await this.fetch<unknown>(
      `/api/deployment/users/${encodeURIComponent(userId)}/deactivate`,
      { method: 'POST' },
    );
    return parseWithFallback(raw, DeploymentUserSchema, EMPTY_DEPLOYMENT_USER, {
      endpoint: 'POST /api/deployment/users/{userId}/deactivate',
    });
  }

  async reactivateDeploymentUser(userId: string): Promise<DeploymentUser> {
    const raw = await this.fetch<unknown>(
      `/api/deployment/users/${encodeURIComponent(userId)}/reactivate`,
      { method: 'POST' },
    );
    return parseWithFallback(raw, DeploymentUserSchema, EMPTY_DEPLOYMENT_USER, {
      endpoint: 'POST /api/deployment/users/{userId}/reactivate',
    });
  }

  async removeDeploymentAdmin(userId: string): Promise<DeploymentAdminPending> {
    const raw = await this.fetch<unknown>(`/api/deployment/admins/${userId}`, { method: 'DELETE' });
    return parseWithFallback(raw, DeploymentAdminPendingSchema, EMPTY_DEPLOYMENT_ADMIN_PENDING, {
      endpoint: 'DELETE /api/deployment/admins/{userId}',
    });
  }

  async listDeploymentAdminPending(): Promise<DeploymentAdminPending[]> {
    const raw = await this.fetch<unknown>(`/api/deployment/admins/pending`);
    return parseWithFallback(
      raw,
      DeploymentAdminPendingListSchema,
      EMPTY_DEPLOYMENT_ADMIN_PENDING_LIST,
      { endpoint: 'GET /api/deployment/admins/pending' },
    );
  }

  async listDeploymentAudit(query?: number | DeploymentAuditQuery): Promise<AdminAuditEntry[]> {
    const params = new URLSearchParams();
    if (typeof query === 'number') {
      params.set('limit', String(query));
    } else if (query) {
      if (query.limit !== undefined) params.set('limit', String(query.limit));
      if (query.action) params.set('action', query.action);
      if (query.actor) params.set('actor', query.actor);
      if (query.since) params.set('since', query.since);
      if (query.until) params.set('until', query.until);
      if (query.source) params.set('source', query.source);
      if (query.cursor !== undefined) params.set('cursor', query.cursor);
    }
    const search = params.toString() === '' ? '' : `?${params}`;
    const raw = await this.fetch<unknown>(`/api/deployment/audit${search}`);
    return parseWithFallback(raw, AdminAuditListSchema, EMPTY_ADMIN_AUDIT_LIST, {
      endpoint: 'GET /api/deployment/audit',
    });
  }

  async getDeploymentAdminPolicy(): Promise<DeploymentPolicyView> {
    const raw = await this.fetch<unknown>(`/api/deployment/policy`);
    return parseWithFallback(raw, DeploymentPolicyViewSchema, EMPTY_DEPLOYMENT_POLICY_VIEW, {
      endpoint: 'GET /api/deployment/policy',
    });
  }

  async putDeploymentAdminPolicy(policy: DeploymentPolicyDoc): Promise<DeploymentPolicyView> {
    const raw = await this.fetch<unknown>(`/api/deployment/policy`, {
      method: 'PUT',
      body: JSON.stringify({ policy }),
    });
    return parseWithFallback(raw, DeploymentPolicyViewSchema, EMPTY_DEPLOYMENT_POLICY_VIEW, {
      endpoint: 'PUT /api/deployment/policy',
    });
  }

  async getDeploymentFleet(): Promise<DeploymentFleet> {
    const raw = await this.fetch<unknown>(`/api/deployment/fleet`);
    return parseWithFallback(raw, DeploymentFleetSchema, EMPTY_DEPLOYMENT_FLEET, {
      endpoint: 'GET /api/deployment/fleet',
    });
  }

  async listDeploymentWorkspaces(): Promise<DeploymentWorkspaceEntry[]> {
    const raw = await this.fetch<unknown>(`/api/deployment/workspaces`);
    return parseWithFallback(raw, DeploymentWorkspaceListSchema, EMPTY_DEPLOYMENT_WORKSPACE_LIST, {
      endpoint: 'GET /api/deployment/workspaces',
    });
  }

  async listJoinTargets(): Promise<JoinTarget[]> {
    const raw = await this.fetch<unknown>(`/api/deployment/join-targets`);
    return parseWithFallback(raw, JoinTargetListSchema, EMPTY_JOIN_TARGET_LIST, {
      endpoint: 'GET /api/deployment/join-targets',
    });
  }

  async joinTarget(wsId: string): Promise<JoinTargetResult> {
    const raw = await this.fetch<unknown>(`/api/deployment/join-targets/${wsId}/join`, {
      method: 'POST',
    });
    return parseWithFallback(raw, JoinTargetResultSchema, EMPTY_JOIN_TARGET_RESULT, {
      endpoint: 'POST /api/deployment/join-targets/{id}/join',
    });
  }

  async setDeploymentWorkspaceOpenJoin(wsId: string, openJoin: boolean): Promise<void> {
    await this.fetch<unknown>(`/api/deployment/workspaces/${wsId}`, {
      method: 'PATCH',
      body: JSON.stringify({ open_join: openJoin }),
    });
  }

  async listDeploymentWorkspaceMembers(wsId: string): Promise<DeploymentWorkspaceMemberEntry[]> {
    const raw = await this.fetch<unknown>(`/api/deployment/workspaces/${wsId}/members`);
    return parseWithFallback(
      raw,
      DeploymentWorkspaceMemberListSchema,
      EMPTY_DEPLOYMENT_WORKSPACE_MEMBER_LIST,
      { endpoint: 'GET /api/deployment/workspaces/{workspaceId}/members' },
    );
  }

  async getDeploymentWorkspaceConfig(wsId: string): Promise<WorkspaceConfigView> {
    const raw = await this.fetch<unknown>(`/api/deployment/workspaces/${wsId}/config`);
    return parseStrict<WorkspaceConfigView>(raw, WorkspaceConfigViewSchema, {
      endpoint: 'GET /api/deployment/workspaces/{workspaceId}/config',
    });
  }

  async putDeploymentWorkspaceConfig(
    wsId: string,
    patch: WorkspaceConfigPatch,
  ): Promise<WorkspaceConfigView> {
    const raw = await this.fetch<unknown>(`/api/deployment/workspaces/${wsId}/config`, {
      method: 'PUT',
      body: JSON.stringify(patch),
    });
    return parseWithFallback(raw, WorkspaceConfigViewSchema, EMPTY_WORKSPACE_CONFIG_VIEW, {
      endpoint: 'PUT /api/deployment/workspaces/{workspaceId}/config',
    });
  }

  async getDeploymentWorkspaceConfigOverride(
    wsId: string,
    userId: string,
  ): Promise<UserConfigOverrideView | null> {
    let raw: unknown;
    try {
      raw = await this.fetch<unknown>(
        `/api/deployment/workspaces/${wsId}/config/overrides/${userId}`,
      );
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) return null;
      throw err;
    }
    return parseStrict<UserConfigOverrideView>(raw, UserConfigOverrideViewSchema, {
      endpoint: 'GET /api/deployment/workspaces/{workspaceId}/config/overrides/{userId}',
    });
  }

  async listDeploymentWorkspaceConfigOverrides(wsId: string): Promise<UserConfigOverrideView[]> {
    const raw = await this.fetch<unknown>(`/api/deployment/workspaces/${wsId}/config/overrides`);
    return parseStrict<UserConfigOverrideView[]>(raw, DeploymentUserConfigOverrideListSchema, {
      endpoint: 'GET /api/deployment/workspaces/{workspaceId}/config/overrides',
    });
  }

  async putDeploymentWorkspaceConfigOverride(
    wsId: string,
    userId: string,
    patch: UserConfigOverridePatch,
  ): Promise<UserConfigOverrideView> {
    const raw = await this.fetch<unknown>(
      `/api/deployment/workspaces/${wsId}/config/overrides/${userId}`,
      { method: 'PUT', body: JSON.stringify(patch) },
    );
    return parseWithFallback(raw, UserConfigOverrideViewSchema, EMPTY_USER_CONFIG_OVERRIDE_VIEW, {
      endpoint: 'PUT /api/deployment/workspaces/{workspaceId}/config/overrides/{userId}',
    });
  }

  async deleteDeploymentWorkspaceConfigOverride(wsId: string, userId: string): Promise<void> {
    await this.fetch<void>(`/api/deployment/workspaces/${wsId}/config/overrides/${userId}`, {
      method: 'DELETE',
    });
  }

  async listAgentTasks(agentId: string): Promise<AgentTask[]> {
    return this.fetch(`/api/agents/${agentId}/tasks`);
  }

  async getAgentTaskSnapshot(): Promise<AgentTask[]> {
    return this.fetch(`/api/agent-task-snapshot`);
  }

  async getWorkspaceWorkingAgents(
    type?: WorkspaceWorkingAgentType,
    mineRelation?: WorkspaceWorkingAgentMineRelation,
    parentIssueId?: string,
  ): Promise<WorkspaceWorkingAgent[]> {
    const search = new URLSearchParams();
    if (type) search.set('type', type);
    if (mineRelation) {
      search.set('scope', 'mine');
      search.set('relation', mineRelation);
    } else if (parentIssueId) {
      search.set('parent', parentIssueId);
    }
    const query = search.toString();
    return this.fetch(`/api/working-agents${query ? `?${query}` : ''}`);
  }

  async getWorkspaceAgentActivity30d(): Promise<AgentActivityBucket[]> {
    return this.fetch(`/api/agent-activity-30d`);
  }

  async getWorkspaceAgentRunCounts(): Promise<AgentRunCount[]> {
    return this.fetch(`/api/agent-run-counts`);
  }

  async getActiveTasksForIssue(issueId: string): Promise<{ tasks: AgentTask[] }> {
    return this.fetch(`/api/issues/${issueId}/active-task`);
  }

  async listTaskMessages(taskId: string, opts?: { since?: number }): Promise<TaskMessagePayload[]> {
    const query =
      opts?.since !== undefined ? `?since=${encodeURIComponent(String(opts.since))}` : '';
    return this.fetch(`/api/tasks/${taskId}/messages${query}`);
  }

  async listTasksByIssue(issueId: string): Promise<AgentTask[]> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/task-runs`);
    return parseWithFallback<AgentTask[]>(raw, AgentTaskListSchema, [], {
      endpoint: 'GET /api/issues/:id/task-runs',
    });
  }

  async getIssueUsage(issueId: string): Promise<IssueUsageSummary> {
    return this.fetch(`/api/issues/${issueId}/usage`);
  }

  async cancelTask(issueId: string, taskId: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/tasks/${taskId}/cancel`, {
      method: 'POST',
    });
  }

  async rerunIssue(issueId: string, taskId?: string): Promise<AgentTask> {
    return this.fetch(`/api/issues/${issueId}/rerun`, {
      method: 'POST',
      body: JSON.stringify(taskId ? { task_id: taskId } : {}),
    });
  }

  async listInbox(): Promise<InboxItem[]> {
    return this.fetch('/api/inbox');
  }

  async markInboxRead(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/read`, { method: 'POST' });
  }

  async archiveInbox(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/archive`, { method: 'POST' });
  }

  async listArchivedInbox(): Promise<InboxItem[]> {
    const raw = await this.fetch<unknown>('/api/inbox/archived');
    return parseWithFallback(raw, InboxItemListSchema, EMPTY_INBOX_ITEMS, {
      endpoint: 'GET /api/inbox/archived',
    });
  }

  async unarchiveInbox(id: string): Promise<InboxItem> {
    return this.fetch(`/api/inbox/${id}/unarchive`, { method: 'POST' });
  }

  async getUnreadInboxCount(): Promise<{ count: number }> {
    return this.fetch('/api/inbox/unread-count');
  }

  async getInboxUnreadSummary(): Promise<InboxWorkspaceUnread[]> {
    const raw = await this.fetch<unknown>('/api/inbox/unread-summary');
    return parseWithFallback(raw, InboxUnreadSummarySchema, EMPTY_INBOX_UNREAD_SUMMARY, {
      endpoint: 'GET /api/inbox/unread-summary',
    });
  }

  async markAllInboxRead(): Promise<{ count: number }> {
    return this.fetch('/api/inbox/mark-all-read', { method: 'POST' });
  }

  async archiveAllInbox(): Promise<{ count: number }> {
    return this.fetch('/api/inbox/archive-all', { method: 'POST' });
  }

  async archiveAllReadInbox(): Promise<{ count: number }> {
    return this.fetch('/api/inbox/archive-all-read', { method: 'POST' });
  }

  async archiveCompletedInbox(): Promise<{ count: number }> {
    return this.fetch('/api/inbox/archive-completed', { method: 'POST' });
  }

  async getNotificationPreferences(
    workspaceSlug?: string,
  ): Promise<NotificationPreferenceResponse> {
    const raw = await this.fetch<unknown>(
      '/api/notification-preferences',
      workspaceSlug ? { headers: { 'X-Workspace-Slug': workspaceSlug } } : undefined,
    );
    return parseWithFallback(
      raw,
      NotificationPreferenceResponseSchema,
      EMPTY_NOTIFICATION_PREFERENCE_RESPONSE,
      { endpoint: 'GET /api/notification-preferences' },
    );
  }

  async updateNotificationPreferences(
    preferences: NotificationPreferences,
    workspaceSlug?: string,
  ): Promise<NotificationPreferenceResponse> {
    const raw = await this.fetch<unknown>('/api/notification-preferences', {
      method: 'PATCH',
      headers: workspaceSlug ? { 'X-Workspace-Slug': workspaceSlug } : undefined,
      body: JSON.stringify({ preferences }),
    });
    return parseWithFallback(
      raw,
      NotificationPreferenceResponseSchema,
      EMPTY_NOTIFICATION_PREFERENCE_RESPONSE,
      { endpoint: 'PATCH /api/notification-preferences' },
    );
  }

  async getConfig(): Promise<AppConfigResponse> {
    const raw = await this.fetch<unknown>('/api/config');
    return parseWithFallback<AppConfigResponse>(raw, AppConfigSchema, EMPTY_APP_CONFIG, {
      endpoint: 'GET /api/config',
    });
  }

  async listWorkspaces(): Promise<Workspace[]> {
    return this.fetch('/api/workspaces');
  }

  async getWorkspace(id: string): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${id}`);
  }

  async createWorkspace(data: {
    name: string;
    slug: string;
    description?: string;
    context?: string;
    template_key?: string;
  }): Promise<Workspace> {
    return this.fetch('/api/workspaces', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async listWorkspaceTemplates(): Promise<WorkspaceTemplate[]> {
    const raw = await this.fetch<unknown>('/api/workspace-templates');
    return parseWithFallback(raw, WorkspaceTemplateListSchema, EMPTY_WORKSPACE_TEMPLATE_LIST, {
      endpoint: 'GET /api/workspace-templates',
    });
  }

  async getWorkspaceCapabilities(workspaceId: string): Promise<WorkspaceCapabilities> {
    const raw = await this.fetch<unknown>(`/api/workspaces/${workspaceId}/capabilities`);
    return parseWithFallback(raw, WorkspaceCapabilitiesSchema, EMPTY_WORKSPACE_CAPABILITIES, {
      endpoint: 'GET /api/workspaces/{id}/capabilities',
    });
  }

  async updateWorkspace(
    id: string,
    data: {
      name?: string;
      description?: string;
      context?: string;
      settings?: Record<string, unknown>;
      repos?: WorkspaceRepo[];
      issue_prefix?: string;
      avatar_url?: string;
    },
  ): Promise<Workspace> {
    return this.fetch(`/api/workspaces/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
  }

  async listMembers(workspaceId: string): Promise<MemberWithUser[]> {
    const raw = await this.fetch<unknown>(`/api/workspaces/${workspaceId}/members`);
    return parseWithFallback(raw, MemberWithUserListSchema, EMPTY_MEMBER_WITH_USER_LIST, {
      endpoint: 'GET /api/workspaces/{id}/members',
    });
  }

  async createMember(workspaceId: string, data: CreateMemberRequest): Promise<Invitation> {
    return this.fetch(`/api/workspaces/${workspaceId}/members`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateMember(
    workspaceId: string,
    memberId: string,
    data: UpdateMemberRequest,
  ): Promise<MemberWithUser> {
    const raw = await this.fetch<unknown>(`/api/workspaces/${workspaceId}/members/${memberId}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, MemberWithUserSchema, EMPTY_MEMBER_WITH_USER, {
      endpoint: 'PATCH /api/workspaces/{id}/members/{memberId}',
    });
  }

  async deleteMember(workspaceId: string, memberId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/members/${memberId}`, {
      method: 'DELETE',
    });
  }

  async leaveWorkspace(workspaceId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/leave`, {
      method: 'POST',
    });
  }

  async listWorkspaceInvitations(workspaceId: string): Promise<Invitation[]> {
    return this.fetch(`/api/workspaces/${workspaceId}/invitations`);
  }

  async revokeInvitation(workspaceId: string, invitationId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/invitations/${invitationId}`, {
      method: 'DELETE',
    });
  }

  async listMyInvitations(): Promise<Invitation[]> {
    return this.fetch('/api/invitations');
  }

  async getInvitation(invitationId: string): Promise<Invitation> {
    return this.fetch(`/api/invitations/${invitationId}`);
  }

  async acceptInvitation(invitationId: string): Promise<MemberWithUser> {
    return this.fetch(`/api/invitations/${invitationId}/accept`, {
      method: 'POST',
    });
  }

  async declineInvitation(invitationId: string): Promise<void> {
    await this.fetch(`/api/invitations/${invitationId}/decline`, {
      method: 'POST',
    });
  }

  async deleteWorkspace(workspaceId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}`, {
      method: 'DELETE',
    });
  }

  async listSkills(): Promise<SkillSummary[]> {
    return this.fetch('/api/skills');
  }

  async getSkill(id: string): Promise<Skill> {
    return this.fetch(`/api/skills/${id}`);
  }

  async createSkill(data: CreateSkillRequest): Promise<Skill> {
    return this.fetch('/api/skills', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateSkill(id: string, data: UpdateSkillRequest): Promise<Skill> {
    return this.fetch(`/api/skills/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async deleteSkill(id: string): Promise<void> {
    await this.fetch(`/api/skills/${id}`, { method: 'DELETE' });
  }

  async importSkill(data: { url: string }): Promise<Skill> {
    return this.fetch('/api/skills/import', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async listAgentSkills(agentId: string): Promise<SkillSummary[]> {
    return this.fetch(`/api/agents/${agentId}/skills`);
  }

  async setAgentSkills(agentId: string, data: SetAgentSkillsRequest): Promise<void> {
    await this.fetch(`/api/agents/${agentId}/skills`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async addAgentSkills(agentId: string, data: SetAgentSkillsRequest): Promise<void> {
    await this.fetch(`/api/agents/${agentId}/skills/add`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async setAgentSkillEnabled(agentId: string, skillId: string, enabled: boolean): Promise<void> {
    await this.fetch(`/api/agents/${agentId}/skills/${skillId}/enabled`, {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    });
  }

  async setAgentRuntimeSkillEnabled(
    agentId: string,
    data: SetAgentRuntimeSkillEnabledRequest,
  ): Promise<void> {
    await this.fetch(`/api/agents/${agentId}/runtime-skills/enabled`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async removeAgentSkill(agentId: string, skillId: string): Promise<void> {
    await this.fetch(`/api/agents/${agentId}/skills/${skillId}`, {
      method: 'DELETE',
    });
  }

  async listPersonalAccessTokens(): Promise<PersonalAccessToken[]> {
    return this.fetch('/api/tokens');
  }

  async createPersonalAccessToken(
    data: CreatePersonalAccessTokenRequest,
  ): Promise<CreatePersonalAccessTokenResponse> {
    return this.fetch('/api/tokens', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async revokePersonalAccessToken(id: string): Promise<void> {
    await this.fetch(`/api/tokens/${id}`, { method: 'DELETE' });
  }

  async uploadFile(
    file: File,
    opts?: { issueId?: string; commentId?: string; chatSessionId?: string },
    signal?: AbortSignal,
  ): Promise<Attachment> {
    const formData = new FormData();
    formData.append('file', file);
    if (opts?.issueId) formData.append('issue_id', opts.issueId);
    if (opts?.commentId) formData.append('comment_id', opts.commentId);
    if (opts?.chatSessionId) formData.append('chat_session_id', opts.chatSessionId);

    const rid = createRequestId();
    const start = Date.now();
    this.logger.info('→ POST /api/upload-file', { rid });

    const uploadHeaders = this.authHeaders();
    const hadAuth = uploadHeaders['Authorization'] !== undefined;
    const res = await fetch(`${this.baseUrl}/api/upload-file`, {
      method: 'POST',
      headers: uploadHeaders,
      body: formData,
      credentials: 'include',
      signal,
    });

    if (!res.ok) {
      if (res.status === 401 && hadAuth) this.handleUnauthorized();
      const message = await this.parseErrorMessage(res, `Upload failed: ${res.status}`);
      this.logger.error(`← ${res.status} /api/upload-file`, {
        rid,
        duration: `${Date.now() - start}ms`,
        error: message,
      });
      throw new Error(message);
    }

    this.logger.info(`← ${res.status} /api/upload-file`, {
      rid,
      duration: `${Date.now() - start}ms`,
    });
    const raw = (await res.json()) as unknown;
    return parseWithFallback(raw, AttachmentResponseSchema, EMPTY_ATTACHMENT, {
      endpoint: 'POST /api/upload-file',
    });
  }

  async listChatSessions(params?: { status?: string }): Promise<ChatSession[]> {
    const query = params?.status ? `?status=${params.status}` : '';
    return this.fetch(`/api/chat/sessions${query}`);
  }

  async getChatSession(id: string): Promise<ChatSession> {
    return this.fetch(`/api/chat/sessions/${id}`);
  }

  async createChatSession(data: {
    agent_id: string;
    title?: string;
    project_id?: string | null;
  }): Promise<ChatSession> {
    return this.fetch('/api/chat/sessions', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async deleteChatSession(id: string): Promise<void> {
    await this.fetch(`/api/chat/sessions/${id}`, { method: 'DELETE' });
  }

  async updateChatSession(
    id: string,
    data: { title: string } | { project_id: string | null },
  ): Promise<ChatSession> {
    return this.fetch(`/api/chat/sessions/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
  }

  async setChatSessionPinned(id: string, pinned: boolean): Promise<ChatSession> {
    return this.fetch(`/api/chat/sessions/${id}/pin`, {
      method: 'PATCH',
      body: JSON.stringify({ pinned }),
    });
  }

  async setChatSessionArchived(id: string, archived: boolean): Promise<ChatSession> {
    return this.fetch(`/api/chat/sessions/${id}/archive`, {
      method: 'PATCH',
      body: JSON.stringify({ archived }),
    });
  }

  async listChatPinnedAgents(): Promise<ChatPinnedAgent[]> {
    return this.fetch('/api/chat/pinned-agents');
  }

  async pinChatAgent(agentId: string): Promise<ChatPinnedAgent> {
    return this.fetch('/api/chat/pinned-agents', {
      method: 'POST',
      body: JSON.stringify({ agent_id: agentId }),
    });
  }

  async unpinChatAgent(agentId: string): Promise<void> {
    await this.fetch(`/api/chat/pinned-agents/${agentId}`, { method: 'DELETE' });
  }

  async listChatMessages(sessionId: string): Promise<ChatMessage[]> {
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`);
  }

  async listChatMessagesPage(
    sessionId: string,
    params: { before?: { created_at: string; id: string } | null; limit?: number } = {},
  ): Promise<ChatMessagesPage> {
    const limit = params.limit ?? 50;
    const query = new URLSearchParams({ limit: String(limit) });
    if (params.before) {
      query.set('before_created_at', params.before.created_at);
      query.set('before_id', params.before.id);
    }
    try {
      return await this.fetch(`/api/chat/sessions/${sessionId}/messages/page?${query.toString()}`);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404 && !params.before) {
        const messages = await this.listChatMessages(sessionId);
        return { messages, limit, has_more: false, next_cursor: null };
      }
      throw err;
    }
  }

  async sendChatMessage(
    sessionId: string,
    content: string,
    attachmentIds?: string[],
  ): Promise<SendChatMessageResponse> {
    const body: { content: string; attachment_ids?: string[] } = { content };
    if (attachmentIds && attachmentIds.length > 0) {
      body.attachment_ids = attachmentIds;
    }
    return this.fetch(`/api/chat/sessions/${sessionId}/messages`, {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  async getPendingChatTask(sessionId: string): Promise<ChatPendingTask> {
    return this.fetch(`/api/chat/sessions/${sessionId}/pending-task`);
  }

  async listChatDraftRestores(sessionId: string): Promise<ChatDraftRestoresResponse> {
    let raw: unknown;
    try {
      raw = await this.fetch<unknown>(`/api/chat/sessions/${sessionId}/draft-restores`);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        return { restores: [] };
      }
      throw err;
    }
    return parseWithFallback(raw, ChatDraftRestoresResponseSchema, EMPTY_CHAT_DRAFT_RESTORES, {
      endpoint: 'GET /api/chat/sessions/{id}/draft-restores',
    });
  }

  async consumeChatDraftRestore(sessionId: string, restoreId: string): Promise<void> {
    await this.fetch(`/api/chat/sessions/${sessionId}/draft-restores/${restoreId}`, {
      method: 'DELETE',
    });
  }

  async listPendingChatTasks(): Promise<PendingChatTasksResponse> {
    return this.fetch(`/api/chat/pending-tasks`);
  }

  async hasAnyPendingChatTasks(): Promise<HasPendingChatTasksResponse> {
    return this.fetch(`/api/chat/pending-tasks/has-any`);
  }

  async markChatSessionRead(sessionId: string): Promise<void> {
    await this.fetch(`/api/chat/sessions/${sessionId}/read`, { method: 'POST' });
  }

  async cancelTaskById(taskId: string): Promise<CancelTaskResponse> {
    const raw = await this.fetch<unknown>(`/api/tasks/${taskId}/cancel`, {
      method: 'POST',
      headers: { 'X-Client-Capabilities': CHAT_DRAFT_RESTORE_CAPABILITY },
    });
    return parseWithFallback(raw, CancelTaskResponseSchema, EMPTY_CANCEL_TASK_RESPONSE, {
      endpoint: 'POST /api/tasks/{taskId}/cancel',
    });
  }

  async listAttachments(issueId: string): Promise<Attachment[]> {
    return this.fetch(`/api/issues/${issueId}/attachments`);
  }

  async getAttachment(id: string): Promise<Attachment> {
    const raw = await this.fetch<unknown>(`/api/attachments/${id}`);
    return parseWithFallback(raw, AttachmentResponseSchema, EMPTY_ATTACHMENT, {
      endpoint: 'GET /api/attachments/{id}',
    });
  }

  async deleteAttachment(id: string): Promise<void> {
    await this.fetch(`/api/attachments/${id}`, { method: 'DELETE' });
  }

  async getAttachmentTextContent(
    id: string,
  ): Promise<{ text: string; originalContentType: string }> {
    let res: Response;
    try {
      res = await this.fetchRaw(`/api/attachments/${id}/content`);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 413) throw new PreviewTooLargeError();
        if (err.status === 415) throw new PreviewUnsupportedError();
      }
      throw err;
    }
    return {
      text: await res.text(),
      originalContentType: res.headers.get('X-Original-Content-Type') ?? '',
    };
  }

  async listProjects(params?: { status?: string }): Promise<ListProjectsResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set('status', params.status);
    return this.fetch(`/api/projects?${search}`);
  }

  async getProject(id: string): Promise<Project> {
    return this.fetch(`/api/projects/${id}`);
  }

  async createProject(data: CreateProjectRequest): Promise<Project> {
    return this.fetch('/api/projects', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateProject(id: string, data: UpdateProjectRequest): Promise<Project> {
    return this.fetch(`/api/projects/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async deleteProject(id: string): Promise<void> {
    await this.fetch(`/api/projects/${id}`, { method: 'DELETE' });
  }

  async listProjectResources(projectId: string): Promise<ListProjectResourcesResponse> {
    return this.fetch(`/api/projects/${projectId}/resources`);
  }

  async createProjectResource(
    projectId: string,
    data: CreateProjectResourceRequest,
  ): Promise<ProjectResource> {
    return this.fetch(`/api/projects/${projectId}/resources`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateProjectResource(
    projectId: string,
    resourceId: string,
    data: UpdateProjectResourceRequest,
  ): Promise<ProjectResource> {
    return this.fetch(`/api/projects/${projectId}/resources/${resourceId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async deleteProjectResource(projectId: string, resourceId: string): Promise<void> {
    await this.fetch(`/api/projects/${projectId}/resources/${resourceId}`, {
      method: 'DELETE',
    });
  }

  async listLabels(resourceType: LabelResourceType = 'issue'): Promise<ListLabelsResponse> {
    const raw = await this.fetch<unknown>(`/api/labels?resource_type=${resourceType}`);
    return parseWithFallback(raw, ListLabelsResponseSchema, EMPTY_LIST_LABELS_RESPONSE, {
      endpoint: 'GET /api/labels',
    });
  }

  async getLabel(id: string): Promise<Label> {
    const raw = await this.fetch<unknown>(`/api/labels/${id}`);
    return parseWithFallback(raw, LabelSchema, EMPTY_LABEL, {
      endpoint: 'GET /api/labels/{id}',
    });
  }

  async createLabel(data: CreateLabelRequest): Promise<Label> {
    const raw = await this.fetch<unknown>(`/api/labels`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, LabelSchema, EMPTY_LABEL, {
      endpoint: 'POST /api/labels',
    });
  }

  async updateLabel(id: string, data: UpdateLabelRequest): Promise<Label> {
    const raw = await this.fetch<unknown>(`/api/labels/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, LabelSchema, EMPTY_LABEL, {
      endpoint: 'PUT /api/labels/{id}',
    });
  }

  async deleteLabel(id: string): Promise<void> {
    await this.fetch(`/api/labels/${id}`, { method: 'DELETE' });
  }

  async listProperties(includeArchived = false): Promise<ListPropertiesResponse> {
    const suffix = includeArchived ? '?include_archived=true' : '';
    let raw: unknown;
    try {
      raw = await this.fetch<unknown>(`/api/properties${suffix}`);
    } catch (error) {
      if (
        error instanceof Error &&
        'status' in error &&
        (error as { status?: number }).status === 404
      ) {
        return EMPTY_LIST_PROPERTIES_RESPONSE;
      }
      throw error;
    }
    return parseWithFallback(raw, ListPropertiesResponseSchema, EMPTY_LIST_PROPERTIES_RESPONSE, {
      endpoint: 'GET /api/properties',
    });
  }

  async createProperty(data: CreatePropertyRequest): Promise<IssueProperty> {
    const raw = await this.fetch<unknown>(`/api/properties`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, IssuePropertySchema, EMPTY_ISSUE_PROPERTY, {
      endpoint: 'POST /api/properties',
    });
  }

  async updateProperty(id: string, data: UpdatePropertyRequest): Promise<IssueProperty> {
    const raw = await this.fetch<unknown>(`/api/properties/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, IssuePropertySchema, EMPTY_ISSUE_PROPERTY, {
      endpoint: 'PATCH /api/properties/{id}',
    });
  }

  async setIssueProperty(
    issueId: string,
    propertyId: string,
    value: IssuePropertyValue,
  ): Promise<IssuePropertiesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/properties/${propertyId}`, {
      method: 'PUT',
      body: JSON.stringify({ value }),
    });
    return parseWithFallback(raw, IssuePropertiesResponseSchema, EMPTY_ISSUE_PROPERTIES_RESPONSE, {
      endpoint: 'PUT /api/issues/{id}/properties/{propertyId}',
    });
  }

  async unsetIssueProperty(issueId: string, propertyId: string): Promise<IssuePropertiesResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/properties/${propertyId}`, {
      method: 'DELETE',
    });
    return parseWithFallback(raw, IssuePropertiesResponseSchema, EMPTY_ISSUE_PROPERTIES_RESPONSE, {
      endpoint: 'DELETE /api/issues/{id}/properties/{propertyId}',
    });
  }

  async listLabelsForIssue(issueId: string): Promise<IssueLabelsResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/labels`);
    return parseWithFallback(raw, ResourceLabelsResponseSchema, EMPTY_RESOURCE_LABELS_RESPONSE, {
      endpoint: 'GET /api/issues/{id}/labels',
    });
  }

  async attachLabel(issueId: string, labelId: string): Promise<IssueLabelsResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/labels`, {
      method: 'POST',
      body: JSON.stringify({ label_id: labelId }),
    });
    return parseWithFallback(raw, ResourceLabelsResponseSchema, EMPTY_RESOURCE_LABELS_RESPONSE, {
      endpoint: 'POST /api/issues/{id}/labels',
    });
  }

  async detachLabel(issueId: string, labelId: string): Promise<IssueLabelsResponse> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/labels/${labelId}`, {
      method: 'DELETE',
    });
    return parseWithFallback(raw, ResourceLabelsResponseSchema, EMPTY_RESOURCE_LABELS_RESPONSE, {
      endpoint: 'DELETE /api/issues/{id}/labels/{labelId}',
    });
  }

  async listLabelsForResource(
    resourceType: 'agent' | 'skill',
    resourceId: string,
  ): Promise<ResourceLabelsResponse> {
    const raw = await this.fetch<unknown>(
      `/api/${resourceType === 'agent' ? 'agents' : 'skills'}/${resourceId}/labels`,
    );
    return parseWithFallback(raw, ResourceLabelsResponseSchema, EMPTY_RESOURCE_LABELS_RESPONSE, {
      endpoint: `GET /api/${resourceType === 'agent' ? 'agents' : 'skills'}/{id}/labels`,
    });
  }

  async attachLabelToResource(
    resourceType: 'agent' | 'skill',
    resourceId: string,
    labelId: string,
  ): Promise<ResourceLabelsResponse> {
    const raw = await this.fetch<unknown>(
      `/api/${resourceType === 'agent' ? 'agents' : 'skills'}/${resourceId}/labels`,
      {
        method: 'POST',
        body: JSON.stringify({ label_id: labelId }),
      },
    );
    return parseWithFallback(raw, ResourceLabelsResponseSchema, EMPTY_RESOURCE_LABELS_RESPONSE, {
      endpoint: `POST /api/${resourceType === 'agent' ? 'agents' : 'skills'}/{id}/labels`,
    });
  }

  async detachLabelFromResource(
    resourceType: 'agent' | 'skill',
    resourceId: string,
    labelId: string,
  ): Promise<ResourceLabelsResponse> {
    const raw = await this.fetch<unknown>(
      `/api/${resourceType === 'agent' ? 'agents' : 'skills'}/${resourceId}/labels/${labelId}`,
      {
        method: 'DELETE',
      },
    );
    return parseWithFallback(raw, ResourceLabelsResponseSchema, EMPTY_RESOURCE_LABELS_RESPONSE, {
      endpoint: `DELETE /api/${resourceType === 'agent' ? 'agents' : 'skills'}/{id}/labels/{labelId}`,
    });
  }

  async listPins(): Promise<PinnedItem[]> {
    return this.fetch('/api/pins');
  }

  async createPin(data: CreatePinRequest): Promise<PinnedItem> {
    return this.fetch('/api/pins', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async deletePin(itemType: PinnedItemType, itemId: string): Promise<void> {
    await this.fetch(`/api/pins/${itemType}/${itemId}`, { method: 'DELETE' });
  }

  async reorderPins(data: ReorderPinsRequest): Promise<void> {
    await this.fetch('/api/pins/reorder', {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async listSquads(): Promise<Squad[]> {
    const raw = await this.fetch<unknown>(`/api/squads`);
    return parseWithFallback(raw, SquadListSchema, EMPTY_SQUAD_LIST, {
      endpoint: 'GET /api/squads',
    }) as Squad[];
  }

  async getSquad(id: string): Promise<Squad> {
    const raw = await this.fetch<unknown>(`/api/squads/${id}`);
    return parseWithFallback(raw, SquadSchema, EMPTY_SQUAD, {
      endpoint: 'GET /api/squads/:id',
    }) as Squad;
  }

  async createSquad(data: {
    name: string;
    description?: string;
    leader_id: string;
    avatar_url?: string;
  }): Promise<Squad> {
    const raw = await this.fetch<unknown>('/api/squads', {
      method: 'POST',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, SquadSchema, EMPTY_SQUAD, {
      endpoint: 'POST /api/squads',
    }) as Squad;
  }

  async updateSquad(
    id: string,
    data: {
      name?: string;
      description?: string;
      instructions?: string;
      leader_id?: string;
      avatar_url?: string;
    },
  ): Promise<Squad> {
    const raw = await this.fetch<unknown>(`/api/squads/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
    return parseWithFallback(raw, SquadSchema, EMPTY_SQUAD, {
      endpoint: 'PUT /api/squads/:id',
    }) as Squad;
  }

  async deleteSquad(id: string): Promise<void> {
    await this.fetch(`/api/squads/${id}`, { method: 'DELETE' });
  }

  async listSquadMembers(squadId: string): Promise<SquadMember[]> {
    return this.fetch(`/api/squads/${squadId}/members`);
  }

  async addSquadMember(
    squadId: string,
    data: { member_type: string; member_id: string; role?: string },
  ): Promise<SquadMember> {
    return this.fetch(`/api/squads/${squadId}/members`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async removeSquadMember(
    squadId: string,
    data: { member_type: string; member_id: string },
  ): Promise<void> {
    await this.fetch(`/api/squads/${squadId}/members`, {
      method: 'DELETE',
      body: JSON.stringify(data),
    });
  }

  async updateSquadMemberRole(
    squadId: string,
    data: { member_type: string; member_id: string; role: string },
  ): Promise<SquadMember> {
    return this.fetch(`/api/squads/${squadId}/members/role`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
  }

  async getSquadMemberStatus(squadId: string): Promise<SquadMemberStatusListResponse> {
    const raw = await this.fetch<unknown>(`/api/squads/${squadId}/members/status`);
    return parseWithFallback(
      raw,
      SquadMemberStatusListResponseSchema,
      EMPTY_SQUAD_MEMBER_STATUS_LIST,
      {
        endpoint: 'GET /api/squads/:id/members/status',
      },
    ) as SquadMemberStatusListResponse;
  }

  async listAutopilots(params?: { status?: string }): Promise<ListAutopilotsResponse> {
    const search = new URLSearchParams();
    if (params?.status) search.set('status', params.status);
    const raw = await this.fetch<unknown>(`/api/autopilots?${search}`);
    return parseWithFallback(
      raw,
      ListAutopilotsResponseSchema,
      EMPTY_LIST_AUTOPILOTS_RESPONSE as ListAutopilotsResponse,
      { endpoint: 'GET /api/autopilots' },
    );
  }

  async getAutopilot(id: string): Promise<GetAutopilotResponse> {
    return this.fetch(`/api/autopilots/${id}`);
  }

  async createAutopilot(data: CreateAutopilotRequest): Promise<Autopilot> {
    return this.fetch('/api/autopilots', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateAutopilot(id: string, data: UpdateAutopilotRequest): Promise<Autopilot> {
    return this.fetch(`/api/autopilots/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
  }

  async deleteAutopilot(id: string): Promise<void> {
    await this.fetch(`/api/autopilots/${id}`, { method: 'DELETE' });
  }

  async grantAutopilotAccess(id: string, userId: string): Promise<AutopilotCollaboratorsResponse> {
    return this.fetch(`/api/autopilots/${id}/collaborators`, {
      method: 'POST',
      body: JSON.stringify({ user_id: userId }),
    });
  }

  async revokeAutopilotAccess(id: string, userId: string): Promise<AutopilotCollaboratorsResponse> {
    return this.fetch(`/api/autopilots/${id}/collaborators/${userId}`, {
      method: 'DELETE',
    });
  }

  async triggerAutopilot(id: string): Promise<AutopilotRun> {
    const raw = await this.fetch<unknown>(`/api/autopilots/${id}/trigger`, { method: 'POST' });
    return parseWithFallback(raw, AutopilotRunSchema, FALLBACK_AUTOPILOT_RUN, {
      endpoint: 'POST /api/autopilots/:id/trigger',
    });
  }

  async listAutopilotRuns(
    id: string,
    params?: { limit?: number; offset?: number },
  ): Promise<ListAutopilotRunsResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set('limit', params.limit.toString());
    if (params?.offset) search.set('offset', params.offset.toString());
    return this.fetch(`/api/autopilots/${id}/runs?${search}`);
  }

  async getAutopilotRun(autopilotId: string, runId: string): Promise<AutopilotRun> {
    return this.fetch(`/api/autopilots/${autopilotId}/runs/${runId}`);
  }

  async createAutopilotTrigger(
    autopilotId: string,
    data: CreateAutopilotTriggerRequest,
  ): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateAutopilotTrigger(
    autopilotId: string,
    triggerId: string,
    data: UpdateAutopilotTriggerRequest,
  ): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
  }

  async deleteAutopilotTrigger(autopilotId: string, triggerId: string): Promise<void> {
    await this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}`, { method: 'DELETE' });
  }

  async cronPreview(params: { expr: string; tz: string }): Promise<CronPreviewResponse> {
    const search = new URLSearchParams();
    search.set('expr', params.expr);
    search.set('tz', params.tz);
    const raw = await this.fetch<unknown>(`/api/autopilots/cron-preview?${search}`);
    return parseWithFallback(raw, CronPreviewResponseSchema, UNREADABLE_CRON_PREVIEW_RESPONSE, {
      endpoint: 'GET /api/autopilots/cron-preview',
    });
  }

  async rotateAutopilotTriggerWebhookToken(
    autopilotId: string,
    triggerId: string,
  ): Promise<AutopilotTrigger> {
    return this.fetch(`/api/autopilots/${autopilotId}/triggers/${triggerId}/rotate-webhook-token`, {
      method: 'POST',
    });
  }

  async listAutopilotDeliveries(
    autopilotId: string,
    params?: { limit?: number; offset?: number },
  ): Promise<ListWebhookDeliveriesResponse> {
    const search = new URLSearchParams();
    if (params?.limit) search.set('limit', params.limit.toString());
    if (params?.offset) search.set('offset', params.offset.toString());
    const raw = await this.fetch<unknown>(`/api/autopilots/${autopilotId}/deliveries?${search}`);
    return parseWithFallback(
      raw,
      ListWebhookDeliveriesResponseSchema,
      EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE,
      { endpoint: 'GET /api/autopilots/:id/deliveries' },
    );
  }

  async getAutopilotDelivery(autopilotId: string, deliveryId: string): Promise<WebhookDelivery> {
    const raw = await this.fetch<unknown>(
      `/api/autopilots/${autopilotId}/deliveries/${deliveryId}`,
    );
    return parseWithFallback(
      raw,
      WebhookDeliveryResponseSchema,
      { ...EMPTY_WEBHOOK_DELIVERY, id: deliveryId, autopilot_id: autopilotId },
      { endpoint: 'GET /api/autopilots/:id/deliveries/:deliveryId' },
    );
  }

  async replayAutopilotDelivery(autopilotId: string, deliveryId: string): Promise<WebhookDelivery> {
    const raw = await this.fetch<unknown>(
      `/api/autopilots/${autopilotId}/deliveries/${deliveryId}/replay`,
      { method: 'POST' },
    );
    return parseWithFallback(
      raw,
      WebhookDeliveryResponseSchema,
      { ...EMPTY_WEBHOOK_DELIVERY, autopilot_id: autopilotId },
      { endpoint: 'POST /api/autopilots/:id/deliveries/:deliveryId/replay' },
    );
  }

  async getGitHubConnectURL(
    workspaceId: string,
    returnTo?: 'github' | 'repositories',
  ): Promise<GitHubConnectResponse> {
    const search = new URLSearchParams();
    if (returnTo) search.set('return_to', returnTo);
    const suffix = search.size > 0 ? `?${search.toString()}` : '';
    const raw = await this.fetch<unknown>(`/api/workspaces/${workspaceId}/github/connect${suffix}`);
    return parseWithFallback(raw, GitHubConnectResponseSchema, EMPTY_GITHUB_CONNECT_RESPONSE, {
      endpoint: 'GET /api/workspaces/:id/github/connect',
    });
  }

  async listGitHubInstallations(workspaceId: string): Promise<ListGitHubInstallationsResponse> {
    const raw = await this.fetch<unknown>(`/api/workspaces/${workspaceId}/github/installations`);
    return parseWithFallback(
      raw,
      ListGitHubInstallationsResponseSchema,
      EMPTY_LIST_GITHUB_INSTALLATIONS_RESPONSE,
      { endpoint: 'GET /api/workspaces/:id/github/installations' },
    );
  }

  async listGitHubInstallationRepositories(
    workspaceId: string,
    installationId: string,
    params: { page?: number; per_page?: number } = {},
  ): Promise<ListGitHubRepositoriesResponse> {
    const search = new URLSearchParams();
    if (params.page !== undefined) search.set('page', String(params.page));
    if (params.per_page !== undefined) search.set('per_page', String(params.per_page));
    const suffix = search.size > 0 ? `?${search.toString()}` : '';
    const raw = await this.fetch<unknown>(
      `/api/workspaces/${workspaceId}/github/installations/${installationId}/repositories${suffix}`,
    );
    return parseWithFallback(
      raw,
      ListGitHubRepositoriesResponseSchema,
      EMPTY_LIST_GITHUB_REPOSITORIES_RESPONSE,
      { endpoint: 'GET /api/workspaces/:id/github/installations/:installationId/repositories' },
    );
  }

  async deleteGitHubInstallation(workspaceId: string, installationId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/github/installations/${installationId}`, {
      method: 'DELETE',
    });
  }

  async listIssuePullRequests(issueId: string): Promise<{ pull_requests: GitHubPullRequest[] }> {
    const raw = await this.fetch<unknown>(`/api/issues/${issueId}/pull-requests`);
    return parseWithFallback(
      raw,
      IssuePullRequestsResponseSchema,
      EMPTY_ISSUE_PULL_REQUESTS_RESPONSE,
      { endpoint: 'GET /api/issues/:id/pull-requests' },
    );
  }

  async listVCSConnections(workspaceId: string): Promise<ListVCSConnectionsResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/vcs/connections`);
  }

  async connectVCS(workspaceId: string, body: ConnectVCSRequest): Promise<ConnectVCSResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/vcs/connections`, {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  async deleteVCSConnection(workspaceId: string, connectionId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/vcs/connections/${connectionId}`, {
      method: 'DELETE',
    });
  }

  async rotateVCSWebhook(workspaceId: string, connectionId: string): Promise<ConnectVCSResponse> {
    return this.fetch(
      `/api/workspaces/${workspaceId}/vcs/connections/${connectionId}/rotate-webhook`,
      { method: 'POST' },
    );
  }


  async listComposioToolkits(): Promise<ComposioToolkit[]> {
    return this.fetch(`/api/integrations/composio/toolkits`);
  }

  async listComposioConnections(): Promise<ComposioConnection[]> {
    return this.fetch(`/api/integrations/composio/connections`);
  }

  async beginComposioConnect(toolkitSlug: string): Promise<ComposioConnectInitResponse> {
    return this.fetch(`/api/integrations/composio/connect/init`, {
      method: 'POST',
      body: JSON.stringify({ toolkit_slug: toolkitSlug }),
    });
  }

  async deleteComposioConnection(connectionId: string): Promise<void> {
    await this.fetch(`/api/integrations/composio/connections/${connectionId}`, {
      method: 'DELETE',
    });
  }

  async listSlackInstallations(workspaceId: string): Promise<ListSlackInstallationsResponse> {
    return this.fetch(`/api/workspaces/${workspaceId}/slack/installations`);
  }

  async registerSlackBYO(
    workspaceId: string,
    agentId: string,
    body: RegisterSlackBYORequest,
  ): Promise<SlackInstallation> {
    const search = new URLSearchParams({ agent_id: agentId });
    return this.fetch(`/api/workspaces/${workspaceId}/slack/install/byo?${search.toString()}`, {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  async deleteSlackInstallation(workspaceId: string, installationId: string): Promise<void> {
    await this.fetch(`/api/workspaces/${workspaceId}/slack/installations/${installationId}`, {
      method: 'DELETE',
    });
  }

  async redeemSlackBindingToken(token: string): Promise<RedeemSlackBindingTokenResponse> {
    return this.fetch(`/api/slack/binding/redeem`, {
      method: 'POST',
      body: JSON.stringify({ token }),
    });
  }
}

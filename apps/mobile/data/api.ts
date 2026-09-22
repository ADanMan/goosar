/**
 * Собственная обёртка мобильного клиента над fetch — покрывает ту часть
 * API, которой пользуется приложение, но живёт отдельно, чтобы независимо
 * управлять ретраями, таймаутами и обработкой ошибок. Типы импортируются
 * только как `import type`, без рантайм-связи. Возможности: валидация Zod
 * с фолбэком, авто-разлогин на 401, X-Request-ID и логирование запросов,
 * авторизация через Bearer + X-Workspace-Slug (без cookie).
 */
import type {
  Agent,
  AgentTask,
  Attachment,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  Comment,
  CreateIssueRequest,
  CreateLabelRequest,
  CreateProjectRequest,
  CreateProjectResourceRequest,
  InboxItem,
  Issue,
  IssueLabelsResponse,
  Label,
  IssueReaction,
  ListIssuesParams,
  ListIssuesResponse,
  ListLabelsResponse,
  ListProjectResourcesResponse,
  ListProjectsResponse,
  MemberWithUser,
  PinnedItem,
  PinnedItemType,
  Project,
  ProjectResource,
  Reaction,
  ReorderPinsRequest,
  RuntimeDevice,
  SearchIssuesResponse,
  SearchProjectsResponse,
  SendChatMessageResponse,
  Squad,
  NotificationPreferenceResponse,
  NotificationPreferences,
  TaskMessagePayload,
  TimelineEntry,
  UpdateIssueRequest,
  UpdateMeRequest,
  UpdateProjectRequest,
  User,
  Workspace,
} from "@goosar/core/types";
import {
  AppConfigSchema,
  EMPTY_APP_CONFIG,
  EMPTY_LIST_ISSUES_RESPONSE,
  EMPTY_TIMELINE_ENTRIES,
  IssueSchema,
  ListIssuesResponseSchema,
  TimelineEntriesSchema,
  type AppConfigResponse,
} from "@goosar/core/api/schemas";
import {
  ActiveTasksResponseSchema,
  AgentListSchema,
  AgentTaskListSchema,
  AttachmentListSchema,
  AttachmentSchema,
  ChatMessageListSchema,
  CommentSchema,
  ChatPendingTaskSchema,
  ChatSessionListSchema,
  ChatSessionSchema,
  EMPTY_ACTIVE_TASKS_RESPONSE,
  EMPTY_AGENT_LIST,
  EMPTY_AGENT_TASK_LIST,
  EMPTY_ATTACHMENT_LIST,
  EMPTY_CHAT_MESSAGE_LIST,
  EMPTY_CHAT_PENDING_TASK,
  EMPTY_CHAT_SESSION_LIST,
  EMPTY_COMMENT,
  EMPTY_INBOX_LIST,
  EMPTY_ISSUE_FALLBACK,
  EMPTY_LIST_LABELS_RESPONSE,
  EMPTY_LIST_PROJECT_RESOURCES_RESPONSE,
  EMPTY_LIST_PROJECTS_RESPONSE,
  EMPTY_MEMBER_LIST,
  EMPTY_NOTIFICATION_PREFERENCES,
  EMPTY_PIN_LIST,
  EMPTY_PROJECT,
  EMPTY_RUNTIME_LIST,
  EMPTY_SEARCH_ISSUES_RESPONSE,
  EMPTY_SEARCH_PROJECTS_RESPONSE,
  EMPTY_SQUAD_LIST,
  EMPTY_USER,
  EMPTY_WORKSPACE_LIST,
  InboxListSchema,
  NotificationPreferenceResponseSchema,
  ListLabelsResponseSchema,
  ListProjectResourcesResponseSchema,
  ListProjectsResponseSchema,
  MemberListSchema,
  PinListSchema,
  PinnedItemSchema,
  ProjectSchema,
  RuntimeListSchema,
  SearchIssuesResponseSchema,
  SearchProjectsResponseSchema,
  SendChatMessageResponseSchema,
  SquadListSchema,
  TaskMessageListSchema,
  EMPTY_TASK_MESSAGE_LIST,
  UserSchema,
  WorkspaceListSchema,
} from "./schemas";
import type { ZodType } from "zod";
import { getCurrentSlug } from "./workspace-store";
import { parseWithFallback } from "@/lib/parse-response";
import { createRequestId } from "@/lib/request-id";

const API_URL = process.env.EXPO_PUBLIC_API_URL;

if (!API_URL) {
  throw new Error(
    "EXPO_PUBLIC_API_URL is not set. Add it to apps/mobile/.env.development.local " +
      "(see apps/mobile/.env.staging for an example).",
  );
}

export interface LoginResponse {
  token: string;
  user: User;
}

export interface FileAsset {
  uri: string;
  name: string;
  type: string;
}

const MAX_FILE_SIZE = 100 * 1024 * 1024;

const FETCH_TIMEOUT_MS = 30_000;

export class ApiError extends Error {
  readonly status: number;
  readonly body?: unknown;
  constructor(message: string, status: number, body?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

export interface ApiClientOptions {
  onUnauthorized?: () => void;
}

class ApiClient {
  private token: string | null = null;
  private options: ApiClientOptions = {};

  setToken(token: string | null) {
    this.token = token;
  }

  setOptions(options: ApiClientOptions) {
    this.options = { ...this.options, ...options };
  }

  private async fetch<T>(
    path: string,
    init: RequestInit & { signal?: AbortSignal } = {},
  ): Promise<T> {
    const rid = createRequestId();
    const start = Date.now();
    const method = init.method ?? "GET";

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "X-Client-Platform": "mobile",
      "X-Client-OS": "ios",
      "X-Client-Version": "0.1.0",
      "X-Request-ID": rid,
      ...((init.headers as Record<string, string>) ?? {}),
    };
    if (this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }
    const slug = getCurrentSlug();
    if (slug && !headers["X-Workspace-Slug"]) {
      headers["X-Workspace-Slug"] = slug;
    }

    const controller = new AbortController();
    const timeoutId = setTimeout(() => {
      controller.abort(new Error(`request timed out after ${FETCH_TIMEOUT_MS}ms`));
    }, FETCH_TIMEOUT_MS);
    const callerSignal = init.signal;
    const onCallerAbort = () => controller.abort(callerSignal?.reason);
    if (callerSignal) {
      if (callerSignal.aborted) controller.abort(callerSignal.reason);
      else callerSignal.addEventListener("abort", onCallerAbort);
    }

    console.log(`[api] → ${method} ${path}`, { rid });

    let res: Response;
    try {
      res = await fetch(`${API_URL}${path}`, {
        ...init,
        signal: controller.signal,
        headers,
      });
    } catch (err) {
      clearTimeout(timeoutId);
      callerSignal?.removeEventListener("abort", onCallerAbort);
      if (
        err instanceof Error &&
        err.name === "AbortError" &&
        !callerSignal?.aborted
      ) {
        const duration = Date.now() - start;
        console.warn(`[api] ← TIMEOUT ${path}`, {
          rid,
          duration: `${duration}ms`,
        });
        throw new ApiError(
          `Request timed out after ${FETCH_TIMEOUT_MS}ms`,
          0,
          undefined,
        );
      }
      throw err;
    }
    clearTimeout(timeoutId);
    callerSignal?.removeEventListener("abort", onCallerAbort);
    const duration = Date.now() - start;

    if (!res.ok) {
      if (res.status === 401) {
        this.options.onUnauthorized?.();
      }

      let body: unknown;
      try {
        body = await res.json();
      } catch {
        body = undefined;
      }
      const message =
        (body && typeof body === "object" && "message" in body
          ? String((body as { message: unknown }).message)
          : null) ?? `${res.status} ${res.statusText}`;

      const level = res.status === 404 ? "warn" : "error";
      console[level](`[api] ← ${res.status} ${path}`, {
        rid,
        duration: `${duration}ms`,
        error: message,
      });

      throw new ApiError(message, res.status, body);
    }

    console.log(`[api] ← ${res.status} ${path}`, {
      rid,
      duration: `${duration}ms`,
    });

    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  private async fetchValidated<T>(
    path: string,
    schema: ZodType,
    fallback: T,
    opts?: { signal?: AbortSignal; endpoint?: string },
  ): Promise<T> {
    const raw = await this.fetch<unknown>(path, { signal: opts?.signal });
    return parseWithFallback(raw, schema, fallback, {
      endpoint: opts?.endpoint ?? path,
    });
  }

  private async fetchValidatedWith<T>(
    path: string,
    schema: ZodType,
    fallback: T,
    init: RequestInit,
    opts?: { signal?: AbortSignal; endpoint?: string },
  ): Promise<T> {
    const raw = await this.fetch<unknown>(path, {
      ...init,
      signal: opts?.signal ?? init.signal ?? undefined,
    });
    return parseWithFallback(raw, schema, fallback, {
      endpoint: opts?.endpoint ?? `${init.method ?? "GET"} ${path}`,
    });
  }

  async sendCode(email: string): Promise<void> {
    await this.fetch<void>("/auth/send-code", {
      method: "POST",
      body: JSON.stringify({ email }),
    });
  }

  async verifyCode(email: string, code: string): Promise<LoginResponse> {
    return this.fetch<LoginResponse>("/auth/verify-code", {
      method: "POST",
      body: JSON.stringify({ email, code }),
    });
  }

  async getMe(opts?: { signal?: AbortSignal }): Promise<User> {
    return this.fetchValidated(
      "/api/me",
      UserSchema,
      EMPTY_USER,
      { ...opts, endpoint: "getMe" },
    );
  }

  async getAppConfig(opts?: { signal?: AbortSignal }): Promise<AppConfigResponse> {
    return this.fetchValidated(
      "/api/config",
      AppConfigSchema,
      EMPTY_APP_CONFIG,
      { ...opts, endpoint: "getAppConfig" },
    );
  }

  async updateMe(data: UpdateMeRequest): Promise<User> {
    return this.fetchValidatedWith(
      "/api/me",
      UserSchema,
      EMPTY_USER,
      { method: "PATCH", body: JSON.stringify(data) },
      { endpoint: "updateMe" },
    );
  }

  async getNotificationPreferences(
    opts?: { signal?: AbortSignal },
  ): Promise<NotificationPreferenceResponse> {
    return this.fetchValidated(
      "/api/notification-preferences",
      NotificationPreferenceResponseSchema,
      EMPTY_NOTIFICATION_PREFERENCES,
      { ...opts, endpoint: "getNotificationPreferences" },
    );
  }

  async updateNotificationPreferences(
    preferences: NotificationPreferences,
    workspaceSlug?: string,
  ): Promise<NotificationPreferenceResponse> {
    return this.fetchValidatedWith(
      "/api/notification-preferences",
      NotificationPreferenceResponseSchema,
      EMPTY_NOTIFICATION_PREFERENCES,
      {
        method: "PATCH",
        headers: workspaceSlug
          ? { "X-Workspace-Slug": workspaceSlug }
          : undefined,
        body: JSON.stringify({ preferences }),
      },
      { endpoint: "updateNotificationPreferences" },
    );
  }

  async listWorkspaces(opts?: {
    signal?: AbortSignal;
  }): Promise<Workspace[]> {
    const raw = await this.fetch<unknown>("/api/workspaces", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, WorkspaceListSchema, EMPTY_WORKSPACE_LIST, {
      endpoint: "listWorkspaces",
    });
  }

  async listInbox(opts?: { signal?: AbortSignal }): Promise<InboxItem[]> {
    const raw = await this.fetch<unknown>("/api/inbox", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, InboxListSchema, EMPTY_INBOX_LIST, {
      endpoint: "listInbox",
    });
  }

  async markInboxRead(id: string): Promise<InboxItem> {
    return this.fetch<InboxItem>(`/api/inbox/${id}/read`, { method: "POST" });
  }

  async archiveInbox(id: string): Promise<InboxItem> {
    return this.fetch<InboxItem>(`/api/inbox/${id}/archive`, { method: "POST" });
  }

  async markAllInboxRead(): Promise<{ count: number }> {
    return this.fetch<{ count: number }>("/api/inbox/mark-all-read", {
      method: "POST",
    });
  }

  async archiveAllInbox(): Promise<{ count: number }> {
    return this.fetch<{ count: number }>("/api/inbox/archive-all", {
      method: "POST",
    });
  }

  async archiveAllReadInbox(): Promise<{ count: number }> {
    return this.fetch<{ count: number }>("/api/inbox/archive-all-read", {
      method: "POST",
    });
  }

  async archiveCompletedInbox(): Promise<{ count: number }> {
    return this.fetch<{ count: number }>("/api/inbox/archive-completed", {
      method: "POST",
    });
  }

  async listMembers(
    workspaceId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<MemberWithUser[]> {
    const raw = await this.fetch<unknown>(
      `/api/workspaces/${workspaceId}/members`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, MemberListSchema, EMPTY_MEMBER_LIST, {
      endpoint: "listMembers",
    });
  }

  async listAgents(opts?: { signal?: AbortSignal }): Promise<Agent[]> {
    const raw = await this.fetch<unknown>("/api/agents", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, AgentListSchema, EMPTY_AGENT_LIST, {
      endpoint: "listAgents",
    });
  }

  async listRuntimes(opts?: { signal?: AbortSignal }): Promise<RuntimeDevice[]> {
    const raw = await this.fetch<unknown>("/api/runtimes", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, RuntimeListSchema, EMPTY_RUNTIME_LIST, {
      endpoint: "listRuntimes",
    });
  }

  async listAgentTaskSnapshot(
    opts?: { signal?: AbortSignal },
  ): Promise<AgentTask[]> {
    const raw = await this.fetch<unknown>("/api/agent-task-snapshot", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, AgentTaskListSchema, EMPTY_AGENT_TASK_LIST, {
      endpoint: "listAgentTaskSnapshot",
    });
  }

  async listSquads(opts?: { signal?: AbortSignal }): Promise<Squad[]> {
    const raw = await this.fetch<unknown>("/api/squads", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, SquadListSchema, EMPTY_SQUAD_LIST, {
      endpoint: "listSquads",
    });
  }

  async listIssues(
    params: ListIssuesParams = {},
    opts?: { signal?: AbortSignal },
  ): Promise<ListIssuesResponse> {
    const search = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v == null) continue;
      if (Array.isArray(v)) {
        if (v.length > 0) search.set(k, v.map(String).join(","));
      } else {
        search.set(k, String(v));
      }
    }
    const qs = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/issues${qs ? `?${qs}` : ""}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, ListIssuesResponseSchema, EMPTY_LIST_ISSUES_RESPONSE, {
      endpoint: "GET /api/issues",
    });
  }

  async searchIssues(
    params: { q: string; limit?: number; include_closed?: boolean; offset?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<SearchIssuesResponse> {
    const search = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v == null) continue;
      search.set(k, String(v));
    }
    const raw = await this.fetch<unknown>(
      `/api/issues/search?${search.toString()}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, SearchIssuesResponseSchema, EMPTY_SEARCH_ISSUES_RESPONSE, {
      endpoint: "GET /api/issues/search",
    });
  }

  async getIssue(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Issue> {
    return this.fetchValidated(
      `/api/issues/${id}`,
      IssueSchema,
      EMPTY_ISSUE_FALLBACK,
      { ...opts, endpoint: "getIssue" },
    );
  }

  async createIssue(body: CreateIssueRequest): Promise<Issue> {
    return this.fetch<Issue>("/api/issues", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async listTimeline(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<TimelineEntry[]> {
    return this.fetchValidated(
      `/api/issues/${issueId}/timeline`,
      TimelineEntriesSchema,
      EMPTY_TIMELINE_ENTRIES,
      { ...opts, endpoint: "GET /api/issues/:id/timeline" },
    );
  }

  async listAttachments(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Attachment[]> {
    return this.fetchValidated(
      `/api/issues/${issueId}/attachments`,
      AttachmentListSchema,
      EMPTY_ATTACHMENT_LIST,
      { ...opts, endpoint: "GET /api/issues/:id/attachments" },
    );
  }

  async listActiveTasksForIssue(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<AgentTask[]> {
    const parsed = await this.fetchValidated(
      `/api/issues/${issueId}/active-task`,
      ActiveTasksResponseSchema,
      EMPTY_ACTIVE_TASKS_RESPONSE,
      { ...opts, endpoint: "GET /api/issues/:id/active-task" },
    );
    return parsed.tasks;
  }

  async listTasksByIssue(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<AgentTask[]> {
    return this.fetchValidated(
      `/api/issues/${issueId}/task-runs`,
      AgentTaskListSchema,
      EMPTY_AGENT_TASK_LIST,
      { ...opts, endpoint: "GET /api/issues/:id/task-runs" },
    );
  }

  async createComment(
    issueId: string,
    content: string,
    opts?: { parentId?: string; type?: string; attachmentIds?: string[] },
  ): Promise<Comment> {
    return this.fetchValidatedWith(
      `/api/issues/${issueId}/comments`,
      CommentSchema,
      EMPTY_COMMENT,
      {
        method: "POST",
        body: JSON.stringify({
          content,
          type: opts?.type ?? "comment",
          ...(opts?.parentId ? { parent_id: opts.parentId } : {}),
          ...(opts?.attachmentIds ? { attachment_ids: opts.attachmentIds } : {}),
        }),
      },
      { endpoint: "createComment" },
    );
  }

  async updateComment(
    commentId: string,
    content: string,
    attachmentIds?: string[],
  ): Promise<Comment> {
    return this.fetchValidatedWith(
      `/api/comments/${commentId}`,
      CommentSchema,
      EMPTY_COMMENT,
      {
        method: "PUT",
        body: JSON.stringify({
          content,
          ...(attachmentIds ? { attachment_ids: attachmentIds } : {}),
        }),
      },
      { endpoint: "updateComment" },
    );
  }

  async deleteComment(commentId: string): Promise<void> {
    await this.fetch<void>(`/api/comments/${commentId}`, { method: "DELETE" });
  }

  async resolveComment(commentId: string): Promise<Comment> {
    return this.fetchValidatedWith(
      `/api/comments/${commentId}/resolve`,
      CommentSchema,
      EMPTY_COMMENT,
      { method: "POST" },
      { endpoint: "resolveComment" },
    );
  }

  async unresolveComment(commentId: string): Promise<Comment> {
    return this.fetchValidatedWith(
      `/api/comments/${commentId}/resolve`,
      CommentSchema,
      EMPTY_COMMENT,
      { method: "DELETE" },
      { endpoint: "unresolveComment" },
    );
  }

  async addReaction(commentId: string, emoji: string): Promise<Reaction> {
    return this.fetch<Reaction>(`/api/comments/${commentId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeReaction(commentId: string, emoji: string): Promise<void> {
    await this.fetch<void>(`/api/comments/${commentId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  async addIssueReaction(
    issueId: string,
    emoji: string,
  ): Promise<IssueReaction> {
    return this.fetch<IssueReaction>(`/api/issues/${issueId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeIssueReaction(issueId: string, emoji: string): Promise<void> {
    await this.fetch<void>(`/api/issues/${issueId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  async updateIssue(id: string, body: UpdateIssueRequest): Promise<Issue> {
    return this.fetch<Issue>(`/api/issues/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  async deleteIssue(id: string): Promise<void> {
    await this.fetch<void>(`/api/issues/${id}`, { method: "DELETE" });
  }

  async listLabels(opts?: {
    signal?: AbortSignal;
  }): Promise<ListLabelsResponse> {
    const raw = await this.fetch<unknown>("/api/labels", {
      signal: opts?.signal,
    });
    return parseWithFallback(
      raw,
      ListLabelsResponseSchema,
      EMPTY_LIST_LABELS_RESPONSE,
      { endpoint: "GET /api/labels" },
    );
  }

  async createLabel(body: CreateLabelRequest): Promise<Label> {
    return this.fetch<Label>("/api/labels", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async attachLabel(
    issueId: string,
    labelId: string,
  ): Promise<IssueLabelsResponse> {
    return this.fetch<IssueLabelsResponse>(
      `/api/issues/${issueId}/labels`,
      {
        method: "POST",
        body: JSON.stringify({ label_id: labelId }),
      },
    );
  }

  async detachLabel(
    issueId: string,
    labelId: string,
  ): Promise<IssueLabelsResponse> {
    return this.fetch<IssueLabelsResponse>(
      `/api/issues/${issueId}/labels/${labelId}`,
      { method: "DELETE" },
    );
  }

  async listProjects(opts?: {
    signal?: AbortSignal;
  }): Promise<ListProjectsResponse> {
    const raw = await this.fetch<unknown>("/api/projects", {
      signal: opts?.signal,
    });
    return parseWithFallback(
      raw,
      ListProjectsResponseSchema,
      EMPTY_LIST_PROJECTS_RESPONSE,
      { endpoint: "GET /api/projects" },
    );
  }

  async searchProjects(
    params: { q: string; limit?: number; include_closed?: boolean; offset?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<SearchProjectsResponse> {
    const search = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v == null) continue;
      search.set(k, String(v));
    }
    const raw = await this.fetch<unknown>(
      `/api/projects/search?${search.toString()}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(
      raw,
      SearchProjectsResponseSchema,
      EMPTY_SEARCH_PROJECTS_RESPONSE,
      { endpoint: "GET /api/projects/search" },
    );
  }

  async getProject(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Project> {
    const raw = await this.fetch<unknown>(`/api/projects/${id}`, {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, ProjectSchema, EMPTY_PROJECT, {
      endpoint: "GET /api/projects/:id",
    });
  }

  async createProject(body: CreateProjectRequest): Promise<Project> {
    return this.fetch<Project>("/api/projects", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async updateProject(
    id: string,
    body: UpdateProjectRequest,
  ): Promise<Project> {
    return this.fetch<Project>(`/api/projects/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  async deleteProject(id: string): Promise<void> {
    await this.fetch<void>(`/api/projects/${id}`, { method: "DELETE" });
  }

  async listProjectResources(
    projectId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ListProjectResourcesResponse> {
    const raw = await this.fetch<unknown>(
      `/api/projects/${projectId}/resources`,
      { signal: opts?.signal },
    );
    return parseWithFallback(
      raw,
      ListProjectResourcesResponseSchema,
      EMPTY_LIST_PROJECT_RESOURCES_RESPONSE,
      { endpoint: "GET /api/projects/:id/resources" },
    );
  }

  async createProjectResource(
    projectId: string,
    body: CreateProjectResourceRequest,
  ): Promise<ProjectResource> {
    return this.fetch<ProjectResource>(
      `/api/projects/${projectId}/resources`,
      {
        method: "POST",
        body: JSON.stringify(body),
      },
    );
  }

  async deleteProjectResource(
    projectId: string,
    resourceId: string,
  ): Promise<void> {
    await this.fetch<void>(
      `/api/projects/${projectId}/resources/${resourceId}`,
      { method: "DELETE" },
    );
  }

  async listChatSessions(
    opts?: { signal?: AbortSignal },
  ): Promise<ChatSession[]> {
    const raw = await this.fetch<unknown>("/api/chat/sessions", {
      signal: opts?.signal,
    });
    return parseWithFallback(
      raw,
      ChatSessionListSchema,
      EMPTY_CHAT_SESSION_LIST,
      { endpoint: "GET /api/chat/sessions" },
    );
  }

  async createChatSession(
    data: { agent_id: string; title?: string },
  ): Promise<ChatSession> {
    const raw = await this.fetch<unknown>("/api/chat/sessions", {
      method: "POST",
      body: JSON.stringify(data),
    });
    const parsed = ChatSessionSchema.safeParse(raw);
    if (!parsed.success) {
      console.error("[api] ← shape mismatch POST /api/chat/sessions", {
        issues: parsed.error.issues,
      });
      throw new ApiError("Create chat session response invalid", 0, raw);
    }
    return parsed.data;
  }

  async deleteChatSession(id: string): Promise<void> {
    await this.fetch<void>(`/api/chat/sessions/${id}`, { method: "DELETE" });
  }

  async listChatMessages(
    sessionId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ChatMessage[]> {
    const raw = await this.fetch<unknown>(
      `/api/chat/sessions/${sessionId}/messages`,
      { signal: opts?.signal },
    );
    return parseWithFallback(
      raw,
      ChatMessageListSchema,
      EMPTY_CHAT_MESSAGE_LIST,
      { endpoint: "GET /api/chat/sessions/:id/messages" },
    );
  }

  async sendChatMessage(
    sessionId: string,
    content: string,
    opts?: { attachmentIds?: string[] },
  ): Promise<SendChatMessageResponse> {
    const body: { content: string; attachment_ids?: string[] } = { content };
    if (opts?.attachmentIds && opts.attachmentIds.length > 0) {
      body.attachment_ids = opts.attachmentIds;
    }
    const raw = await this.fetch<unknown>(
      `/api/chat/sessions/${sessionId}/messages`,
      {
        method: "POST",
        body: JSON.stringify(body),
      },
    );
    const parsed = SendChatMessageResponseSchema.safeParse(raw);
    if (!parsed.success) {
      console.error("[api] ← shape mismatch POST /api/chat/sessions/:id/messages", {
        issues: parsed.error.issues,
      });
      throw new ApiError("Send message response invalid", 0, raw);
    }
    return parsed.data;
  }

  async getPendingChatTask(
    sessionId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ChatPendingTask> {
    const raw = await this.fetch<unknown>(
      `/api/chat/sessions/${sessionId}/pending-task`,
      { signal: opts?.signal },
    );
    return parseWithFallback(
      raw,
      ChatPendingTaskSchema,
      EMPTY_CHAT_PENDING_TASK,
      { endpoint: "GET /api/chat/sessions/:id/pending-task" },
    );
  }

  async markChatSessionRead(sessionId: string): Promise<void> {
    await this.fetch<void>(
      `/api/chat/sessions/${sessionId}/read`,
      { method: "POST" },
    );
  }

  async cancelTaskById(taskId: string): Promise<void> {
    await this.fetch<void>(`/api/tasks/${taskId}/cancel`, { method: "POST" });
  }

  async listTaskMessages(
    taskId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<TaskMessagePayload[]> {
    return this.fetchValidated(
      `/api/tasks/${taskId}/messages`,
      TaskMessageListSchema,
      EMPTY_TASK_MESSAGE_LIST,
      { ...opts, endpoint: "GET /api/tasks/:id/messages" },
    );
  }

  async listPins(opts?: { signal?: AbortSignal }): Promise<PinnedItem[]> {
    return this.fetchValidated(
      "/api/pins",
      PinListSchema,
      EMPTY_PIN_LIST,
      { ...opts, endpoint: "listPins" },
    );
  }

  async createPin(data: {
    item_type: PinnedItemType;
    item_id: string;
  }): Promise<PinnedItem> {
    return this.fetchValidatedWith(
      "/api/pins",
      PinnedItemSchema,
      {
        id: "",
        workspace_id: "",
        user_id: "",
        item_type: data.item_type,
        item_id: data.item_id,
        position: 0,
        created_at: "",
      },
      { method: "POST", body: JSON.stringify(data) },
      { endpoint: "createPin" },
    );
  }

  async deletePin(itemType: PinnedItemType, itemId: string): Promise<void> {
    await this.fetch<void>(`/api/pins/${itemType}/${itemId}`, {
      method: "DELETE",
    });
  }

  async reorderPins(data: ReorderPinsRequest): Promise<void> {
    await this.fetch<void>("/api/pins/reorder", {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async uploadFile(
    asset: FileAsset,
    opts?: { issueId?: string; commentId?: string },
  ): Promise<Attachment> {
    const rid = createRequestId();
    const start = Date.now();
    const path = "/api/upload-file";

    const headers: Record<string, string> = {
      "X-Client-Platform": "mobile",
      "X-Client-OS": "ios",
      "X-Client-Version": "0.1.0",
      "X-Request-ID": rid,
    };
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    const slug = getCurrentSlug();
    if (slug) headers["X-Workspace-Slug"] = slug;

    const formData = new FormData();
    formData.append(
      "file",
      { uri: asset.uri, name: asset.name, type: asset.type } as never,
    );
    if (opts?.issueId) formData.append("issue_id", opts.issueId);
    if (opts?.commentId) formData.append("comment_id", opts.commentId);

    console.log(`[api] → POST ${path}`, { rid, filename: asset.name });

    const res = await fetch(`${API_URL}${path}`, {
      method: "POST",
      headers,
      body: formData,
    });
    const duration = Date.now() - start;

    if (!res.ok) {
      if (res.status === 401) this.options.onUnauthorized?.();
      let body: unknown;
      try {
        body = await res.json();
      } catch {
        body = undefined;
      }
      const message =
        (body && typeof body === "object" && "message" in body
          ? String((body as { message: unknown }).message)
          : null) ?? `Upload failed: ${res.status}`;
      console.error(`[api] ← ${res.status} ${path}`, {
        rid,
        duration: `${duration}ms`,
        error: message,
      });
      throw new ApiError(message, res.status, body);
    }

    console.log(`[api] ← ${res.status} ${path}`, {
      rid,
      duration: `${duration}ms`,
    });

    const json: unknown = await res.json();
    const parsed = AttachmentSchema.safeParse(json);
    if (!parsed.success) {
      console.error(`[api] ← shape mismatch ${path}`, {
        rid,
        error: parsed.error.message,
      });
      throw new ApiError("Upload response invalid", res.status, json);
    }
    return parsed.data;
  }
}

export { MAX_FILE_SIZE };

export const api = new ApiClient();

// Устаревшие ручки онбординга, оставленные ради старых сборок desktop-клиента.
// Актуальный клиент создаёт Helper-агента и стартовые issue через общий
// фронтенд-хук и обычные CreateAgent/CreateIssue; этот файл — минимальная
// копия прежней реализации без сервисного слоя, нужная только пока не
// обновились все активные инсталляции. Поведение менять нельзя — контракт
// «то, чего ждёт старый клиент»; когда телеметрия подтвердит переход всех
// на новую версию, файл вместе с роутами и тестами можно удалить.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/issueguard"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const runtimeBootstrapBodyLimit = 8 * 1024

const maxStarterPromptLen = 2 * 1024

const (
	onboardingAssistantName = "Goosar Helper"
	onboardingIssueTitle    = "Start here: learn Goosar with Goosar Helper"
	onboardingAgentTemplate = "goosar_helper"

	noRuntimeIssueTitle = "Connect a runtime to start using agents"
)

const onboardingAssistantDescription = "Built-in workspace assistant. Answers Goosar questions and runs CLI operations."

const onboardingAssistantAvatarURL = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1024 1024'%3E%3Cdefs%3E%3ClinearGradient id='t' x1='0' y1='0' x2='0' y2='1'%3E%3Cstop offset='0%25' stop-color='%2323242C'/%3E%3Cstop offset='100%25' stop-color='%2313141A'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='1024' height='1024' rx='224' fill='url(%23t)'/%3E%3Ccircle cx='512' cy='512' r='300' fill='%23FFFFFF'/%3E%3Ccircle cx='512' cy='512' r='174' fill='%23F0BE32'/%3E%3C/svg%3E"

const onboardingAssistantInstructions = `You are Goosar Helper, the built-in AI assistant for this Goosar workspace. Your role is to help any member use Goosar better — answer questions, give advice, and execute workspace operations on their behalf.

## What Goosar is

Goosar is an open-source, AI-native team workspace (source: https://github.com/adanman/goosar). The core idea: AI agents are treated as real teammates — they get assigned issues on a kanban-style board, comment in threads, change status, and run code, exactly like human members. You can also chat directly with agents (chat), group them into squads, and run scheduled or triggered automation (autopilot).

For concept details (workspace / issue / project / agent / runtime / skill / squad / autopilot / inbox / chat session): fetch https://goosar.ru/docs via WebFetch — that's authoritative. For the "why" or implementation, fetch the GitHub repo above. Never paraphrase concepts from memory.

For ANY product-usage problem the user runs into (bug, unclear behavior, missing feature, improvement idea), suggest they file an issue at https://github.com/adanman/goosar/issues — that's the official feedback channel.

## What you can do

Your toolbox is the ` + "`goosar`" + ` CLI. It's already on your PATH and authenticated as the workspace owner.

Your full capability surface = whatever ` + "`goosar --help`" + ` shows. Run ` + "`goosar --help`" + ` first, then ` + "`goosar <command> --help`" + ` for any subcommand; use ` + "`--output json`" + ` for structured data. The CLI is your manifest — never invent commands or flags.

A few things you can actually do (non-exhaustive — ` + "`--help`" + ` is the source of truth):
- Create issues, post comments
- Create or iterate on agents
- Manage projects, squads, autopilots, skills, runtimes, etc.

## Tone

Be concise and direct, like a colleague. Respond in the user's language (Chinese in, Chinese out). When pointing at a UI location, name the exact path ("Settings → Agents → New"); when pointing at a doc, link to the specific page, not the homepage. Never fabricate URLs, flags, or file paths.`

const onboardingIssueDescription = `Welcome to Goosar.

This is your guided first run. Goosar Helper is assigned to this issue and will help you try the core workflow:

1. Read Goosar Helper's first comment.
2. Reply with something you want to build, fix, write, or plan.
3. @mention Goosar Helper when you want it to continue.
4. Open Agents and Runtimes later when you want to customize the teammate or the computer it runs on.

You can close this issue when the workflow makes sense.`

type bootstrapOnboardingRuntimeRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	RuntimeID     string `json:"runtime_id"`
	StarterPrompt string `json:"starter_prompt,omitempty"`
}

type bootstrapOnboardingRuntimeResponse struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	IssueID     string `json:"issue_id"`
}

type bootstrapOnboardingNoRuntimeRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type bootstrapOnboardingNoRuntimeResponse struct {
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
}

func (h *Handler) BootstrapOnboardingRuntime(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, runtimeBootstrapBodyLimit)
	var req bootstrapOnboardingRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	req.StarterPrompt = strings.TrimSpace(req.StarterPrompt)
	if utf8.RuneCountInString(req.StarterPrompt) > maxStarterPromptLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("starter_prompt exceeds %d characters", maxStarterPromptLen))
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start onboarding")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	member, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	runtime, err := qtx.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can create agents on it")
		return
	}

	agents, err := qtx.ListAgents(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	isFirstAgent := len(agents) == 0

	var assistant db.Agent
	assistantCreated := false
	for _, existing := range agents {
		if existing.Name == onboardingAssistantName && existing.Visibility == "workspace" {
			assistant = existing
			break
		}
	}
	if !assistant.ID.Valid {
		assistant, err = qtx.CreateAgent(r.Context(), db.CreateAgentParams{
			WorkspaceID:        wsUUID,
			Name:               onboardingAssistantName,
			Description:        onboardingAssistantDescription,
			AvatarUrl:          pgtype.Text{String: onboardingAssistantAvatarURL, Valid: true},
			RuntimeMode:        runtime.RuntimeMode,
			RuntimeConfig:      []byte("{}"),
			RuntimeID:          runtime.ID,
			Visibility:         "workspace",
			MaxConcurrentTasks: 6,
			OwnerID:            parseUUID(userID),
			Instructions:       onboardingAssistantInstructions,
			CustomEnv:          []byte("{}"),
			CustomArgs:         []byte("[]"),
			McpConfig:          nil,
			Model:              pgtype.Text{},
		})
		if err != nil {
			slog.Warn("bootstrap onboarding (shim): create assistant failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding assistant")
			return
		}
		assistantCreated = true
	}

	var emptyUUID pgtype.UUID
	issue, foundIssue, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(), qtx, wsUUID, emptyUUID, emptyUUID, onboardingIssueTitle, false,
	)
	if err != nil {
		slog.Warn("bootstrap onboarding (shim): duplicate issue check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
		return
	}
	issueCreated := false
	if !foundIssue {
		issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
			return
		}
		description := onboardingIssueDescription
		if req.StarterPrompt != "" {
			description = req.StarterPrompt
		}
		issue, err = qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:   wsUUID,
			Title:         onboardingIssueTitle,
			Description:   strOrNullText(description),
			Status:        "todo",
			Priority:      "high",
			AssigneeType:  pgtype.Text{String: "agent", Valid: true},
			AssigneeID:    assistant.ID,
			CreatorType:   "member",
			CreatorID:     parseUUID(userID),
			ParentIssueID: emptyUUID,
			Position:      0,
			Number:        issueNumber,
			ProjectID:     emptyUUID,
		})
		if err != nil {
			slog.Warn("bootstrap onboarding (shim): create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
			return
		}
		issueCreated = true
	}

	before, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	firstCompletion := !before.OnboardedAt.Valid
	updatedUser, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}

	if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), before.StarterContentState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record starter content state")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finish onboarding")
		return
	}

	if assistantCreated {

		resp := broadcastAgentResponse(h.agentToResponse(assistant))
		h.publish(protocol.EventAgentCreated, req.WorkspaceID, "member", userID, map[string]any{"agent": resp})
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
			userID, req.WorkspaceID, uuidToString(assistant.ID),
			runtime.Provider, runtime.RuntimeMode, onboardingAgentTemplate, isFirstAgent,
		))
	}
	if issueCreated {
		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": resp})
		platform, _, _ := middleware.ClientMetadataFromContext(r.Context())
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueCreated(
			userID, req.WorkspaceID, uuidToString(issue.ID),
			uuidToString(assistant.ID), "", "", analytics.SourceOnboarding,
			platform,
		))
		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}
	if firstCompletion {
		onboardedAt := ""
		if updatedUser.OnboardedAt.Valid {
			onboardedAt = updatedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID, req.WorkspaceID, analytics.OnboardingPathFull,
			onboardedAt, updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		AgentID:     uuidToString(assistant.ID),
		IssueID:     uuidToString(issue.ID),
	})
}

func (h *Handler) BootstrapOnboardingNoRuntime(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, runtimeBootstrapBodyLimit)
	var req bootstrapOnboardingNoRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start onboarding")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	userBefore, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	var emptyUUID pgtype.UUID
	existing, foundIssue, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(), qtx, wsUUID, emptyUUID, emptyUUID, noRuntimeIssueTitle, false,
	)
	if err != nil {
		slog.Warn("bootstrap no-runtime onboarding (shim): duplicate issue check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
		return
	}

	var issue db.Issue
	issueCreated := false
	if foundIssue {
		issue = existing
	} else {
		issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
			return
		}
		issue, err = qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:   wsUUID,
			Title:         noRuntimeIssueTitle,
			Description:   strOrNullText(noRuntimeIssueDescription(userBefore.Language)),
			Status:        "todo",
			Priority:      "high",
			AssigneeType:  pgtype.Text{String: "member", Valid: true},
			AssigneeID:    parseUUID(userID),
			CreatorType:   "member",
			CreatorID:     parseUUID(userID),
			ParentIssueID: emptyUUID,
			Position:      0,
			Number:        issueNumber,
			ProjectID:     emptyUUID,
		})
		if err != nil {
			slog.Warn("bootstrap no-runtime onboarding (shim): create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
			return
		}
		issueCreated = true
	}

	firstCompletion := !userBefore.OnboardedAt.Valid
	updatedUser, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}
	if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), userBefore.StarterContentState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record starter content state")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finish onboarding")
		return
	}

	if issueCreated {
		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": resp})
		platform2, _, _ := middleware.ClientMetadataFromContext(r.Context())
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueCreated(
			userID, req.WorkspaceID, uuidToString(issue.ID),
			"", "", "", analytics.SourceOnboarding,
			platform2,
		))
	}
	if firstCompletion {
		onboardedAt := ""
		if updatedUser.OnboardedAt.Valid {
			onboardedAt = updatedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID, req.WorkspaceID, analytics.OnboardingPathRuntimeSkipped,
			onboardedAt, updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingNoRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		IssueID:     uuidToString(issue.ID),
	})
}

func noRuntimeIssueDescription(language pgtype.Text) string {
	tag := strings.ToLower(strings.TrimSpace(language.String))
	switch {
	case strings.HasPrefix(tag, "zh"):
		return zhNoRuntimeIssueDescription()
	case strings.HasPrefix(tag, "en"):
		return enNoRuntimeIssueDescription()
	default:
		return ruNoRuntimeIssueDescription()
	}
}

func enNoRuntimeIssueDescription() string {
	return strings.Join([]string{
		"Welcome to Goosar.",
		"",
		"Agents need a runtime before they can execute work. You can still use Goosar as a lightweight project-management workspace while you install one.",
		"",
		"## Try Goosar first",
		"",
		"Before the runtime is ready, you can:",
		"",
		"1. Create a project for your current work.",
		"2. Create a few issues and move them across backlog, todo, in_progress, and done.",
		"3. Add priorities, labels, comments, and subscriptions.",
		"4. Use Inbox to track assignments and mentions.",
		"",
		"That gives you the project-management layer first. Once a runtime is connected, agents can start working from the same issues.",
		"",
		"## Install your first agent runtime",
		"",
		"Full guide: https://goosar.ru/docs/install-agent-runtime",
		"",
		"For English users, the fastest first path is Codex:",
		"",
		"1. Make sure Node.js is installed.",
		"2. Install Codex:",
		"   npm i -g @openai/codex",
		"3. Sign in:",
		"   codex",
		"4. Confirm your terminal can find it:",
		"   which codex",
		"   codex --version",
		"5. Restart the Goosar daemon:",
		"   goosar daemon restart",
		"   If you use the desktop app, restarting the app is enough.",
		"6. Return to Runtimes and refresh. You should see a Codex runtime online.",
		"7. Create your first agent from that runtime, then assign an issue to the agent and set status to todo.",
		"",
		"Codex reference: https://developers.openai.com/codex/cli",
		"",
		"When the runtime is connected, you can create Goosar Helper for a guided first run.",
	}, "\n")
}

func zhNoRuntimeIssueDescription() string {
	return strings.Join([]string{
		"欢迎来到 Goosar。",
		"",
		"智能体需要先连上运行时才能执行工作。运行时还没准备好时，你也可以先把 Goosar 当作轻量项目管理工具体验起来。",
		"",
		"## 先体验项目管理功能",
		"",
		"运行时安装前，你可以先做这些事：",
		"",
		"1. 为当前工作创建一个项目。",
		"2. 新建几个 issue，并在 backlog、todo、in_progress、done 之间流转。",
		"3. 给 issue 加优先级、标签、评论和订阅。",
		"4. 用收件箱追踪分配给你的事项和 @mention。",
		"",
		"这样你先熟悉项目管理层。连上运行时后，智能体会直接在这些 issue 上开始工作。",
		"",
		"## 安装第一个 Agent 运行时",
		"",
		"完整文档：https://goosar.ru/docs/install-agent-runtime",
		"",
		"中文用户建议先装 Kimi CLI：",
		"",
		"1. 在 macOS / Linux 终端安装 Kimi CLI：",
		"   curl -LsSf https://code.kimi.com/install.sh | bash",
		"   Windows PowerShell：",
		"   Invoke-RestMethod https://code.kimi.com/install.ps1 | Invoke-Expression",
		"2. 确认终端能找到 Kimi：",
		"   kimi --version",
		"3. 在你想让 Kimi 工作的项目目录里启动一次：",
		"   kimi",
		"4. 首次启动后输入 /login，按提示完成 Kimi Code 或 API key 配置。",
		"5. 重启 Goosar 守护进程：",
		"   goosar daemon restart",
		"   如果你用桌面端，重启 app 即可。",
		"6. 回到 Runtimes 页面刷新。你应该能看到一个在线的 Kimi 运行时。",
		"7. 用这个运行时创建第一个智能体，再把一个 issue 分配给它，并把状态切到 todo。",
		"",
		"Kimi CLI 官方文档：https://moonshotai.github.io/kimi-cli/zh/guides/getting-started.html",
		"",
		"运行时连上后，你就可以创建 Goosar Helper，开始一次有智能体参与的上手引导。",
	}, "\n")
}

func ruNoRuntimeIssueDescription() string {
	return strings.Join([]string{
		"Добро пожаловать в Goosar.",
		"",
		"Агентам нужна среда выполнения, чтобы выполнять работу. Пока вы её устанавливаете, Goosar можно использовать как лёгкий инструмент управления проектами.",
		"",
		"## Сначала осмотритесь в Goosar",
		"",
		"Пока среда выполнения не готова, можно:",
		"",
		"1. Создать проект для текущей работы.",
		"2. Создать несколько issue и провести их через backlog, todo, in_progress и done.",
		"3. Добавить приоритеты, метки, комментарии и подписки.",
		"4. Отслеживать назначения и упоминания во «Входящих».",
		"",
		"Так вы сначала освоите слой управления проектами. Когда среда выполнения подключится, агенты начнут работать с теми же issue.",
		"",
		"## Установите первую среду выполнения для агентов",
		"",
		"Полное руководство: https://goosar.ru/docs/install-agent-runtime",
		"",
		"Самый быстрый первый путь — Codex:",
		"",
		"1. Убедитесь, что установлен Node.js.",
		"2. Установите Codex:",
		"   npm i -g @openai/codex",
		"3. Войдите в аккаунт:",
		"   codex",
		"4. Проверьте, что терминал его находит:",
		"   which codex",
		"   codex --version",
		"5. Перезапустите демон Goosar:",
		"   goosar daemon restart",
		"   Если вы пользуетесь десктопным приложением, достаточно перезапустить его.",
		"6. Вернитесь в «Среды выполнения» и обновите страницу. Там должна появиться среда выполнения Codex со статусом online.",
		"7. Создайте из неё первого агента, затем назначьте ему issue и поставьте статус todo.",
		"",
		"Справочник Codex: https://developers.openai.com/codex/cli",
		"",
		"Когда среда выполнения подключена, создайте Goosar Helper — он проведёт вас через первый запуск.",
	}, "\n")
}

func strOrNullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func claimStarterContentStateIfUnset(
	ctx context.Context,
	q *db.Queries,
	userID pgtype.UUID,
	current pgtype.Text,
) error {
	if current.Valid {
		return nil
	}
	_, err := q.SetStarterContentState(ctx, db.SetStarterContentStateParams{
		ID:                  userID,
		StarterContentState: pgtype.Text{String: "imported", Valid: true},
	})
	return err
}

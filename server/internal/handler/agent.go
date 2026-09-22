package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/attribution"
	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/perimeterpolicy"
	"github.com/adanman/goosar/server/internal/runtimeapps"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	"github.com/adanman/goosar/server/pkg/agent"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const maxAgentDescriptionLength = 255

type AgentResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	RuntimeID     string          `json:"runtime_id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Instructions  string          `json:"instructions"`
	AvatarURL     *string         `json:"avatar_url"`
	RuntimeMode   string          `json:"runtime_mode"`
	RuntimeConfig any             `json:"runtime_config"`
	CustomArgs    []string        `json:"custom_args"`
	McpConfig     json.RawMessage `json:"mcp_config"`

	HasCustomEnv      bool `json:"has_custom_env"`
	CustomEnvKeyCount int  `json:"custom_env_key_count"`
	McpConfigRedacted bool `json:"mcp_config_redacted"`

	McpConfigEncrypted bool   `json:"mcp_config_encrypted"`
	Visibility         string `json:"visibility"`

	PermissionMode string `json:"permission_mode"`

	InvocationTargets  []AgentInvocationTargetDTO `json:"invocation_targets"`
	Status             string                     `json:"status"`
	MaxConcurrentTasks int32                      `json:"max_concurrent_tasks"`
	Model              string                     `json:"model"`

	ThinkingLevel string `json:"thinking_level"`

	ServiceTier string `json:"service_tier"`

	ComposioToolkitAllowlist         []string               `json:"composio_toolkit_allowlist,omitempty"`
	ComposioToolkitAllowlistRedacted bool                   `json:"composio_toolkit_allowlist_redacted,omitempty"`
	OwnerID                          *string                `json:"owner_id"`
	Skills                           []AgentSkillSummary    `json:"skills"`
	DisabledRuntimeSkills            []DisabledRuntimeSkill `json:"disabled_runtime_skills"`
	CreatedAt                        string                 `json:"created_at"`
	UpdatedAt                        string                 `json:"updated_at"`
	ArchivedAt                       *string                `json:"archived_at"`
	ArchivedBy                       *string                `json:"archived_by"`

	SystemKey string `json:"system_key"`
}

const runtimeConfigGatewayTokenMask = "***"

func (h *Handler) agentToResponse(a db.Agent) AgentResponse {
	var rc any
	if a.RuntimeConfig != nil {
		json.Unmarshal(a.RuntimeConfig, &rc)
	}
	if rc == nil {
		rc = map[string]any{}
	}
	maskGatewayToken(rc)

	mcpConfigEncrypted := isSealedMcpConfig(a.McpConfig)
	mcpConfigUnreadable := false
	if opened, err := h.openMcpConfig(a.McpConfig); err != nil {
		slog.Error("failed to open agent mcp_config", "agent_id", uuidToString(a.ID), "error", err)
		a.McpConfig = nil
		mcpConfigUnreadable = true
	} else {
		a.McpConfig = opened
	}

	envKeyCount := 0
	if a.CustomEnv != nil {
		var customEnv map[string]string
		if err := json.Unmarshal(a.CustomEnv, &customEnv); err != nil {
			slog.Warn("failed to unmarshal agent custom_env", "agent_id", uuidToString(a.ID), "error", err)
		}
		envKeyCount = len(customEnv)
	}

	var customArgs []string
	if a.CustomArgs != nil {
		if err := json.Unmarshal(a.CustomArgs, &customArgs); err != nil {
			slog.Warn("failed to unmarshal agent custom_args", "agent_id", uuidToString(a.ID), "error", err)
		}
	}
	if customArgs == nil {
		customArgs = []string{}
	}

	var mcpConfig json.RawMessage
	if a.McpConfig != nil {
		mcpConfig = json.RawMessage(a.McpConfig)
	}

	composioAllowlist := a.ComposioToolkitAllowlist

	resp := AgentResponse{
		ID:                       uuidToString(a.ID),
		WorkspaceID:              uuidToString(a.WorkspaceID),
		RuntimeID:                uuidToString(a.RuntimeID),
		Name:                     a.Name,
		Description:              a.Description,
		Instructions:             a.Instructions,
		AvatarURL:                textToPtr(a.AvatarUrl),
		RuntimeMode:              a.RuntimeMode,
		RuntimeConfig:            rc,
		CustomArgs:               customArgs,
		McpConfig:                mcpConfig,
		HasCustomEnv:             envKeyCount > 0,
		CustomEnvKeyCount:        envKeyCount,
		Visibility:               a.Visibility,
		PermissionMode:           a.PermissionMode,
		InvocationTargets:        []AgentInvocationTargetDTO{},
		Status:                   a.Status,
		MaxConcurrentTasks:       a.MaxConcurrentTasks,
		Model:                    a.Model.String,
		ThinkingLevel:            a.ThinkingLevel.String,
		ServiceTier:              a.ServiceTier.String,
		ComposioToolkitAllowlist: composioAllowlist,
		OwnerID:                  uuidToPtr(a.OwnerID),
		Skills:                   []AgentSkillSummary{},
		DisabledRuntimeSkills:    decodeDisabledRuntimeSkills(a.DisabledRuntimeSkills),
		CreatedAt:                timestampToString(a.CreatedAt),
		UpdatedAt:                timestampToString(a.UpdatedAt),
		ArchivedAt:               timestampToPtr(a.ArchivedAt),
		ArchivedBy:               uuidToPtr(a.ArchivedBy),
		SystemKey:                a.SystemKey.String,
	}
	resp.McpConfigRedacted = mcpConfigUnreadable
	resp.McpConfigEncrypted = mcpConfigEncrypted
	return resp
}

func maskGatewayToken(rc any) {
	root, ok := rc.(map[string]any)
	if !ok {
		return
	}
	gw, ok := root["gateway"].(map[string]any)
	if !ok {
		return
	}
	tok, _ := gw["token"].(string)
	if tok == "" {
		return
	}
	gw["token"] = runtimeConfigGatewayTokenMask
}

func preserveMaskedGatewayToken(incoming any, persistedRuntimeConfig []byte) {
	root, ok := incoming.(map[string]any)
	if !ok {
		return
	}
	gw, ok := root["gateway"].(map[string]any)
	if !ok {
		return
	}
	tok, _ := gw["token"].(string)
	if tok != runtimeConfigGatewayTokenMask {
		return
	}

	var prev struct {
		Gateway struct {
			Token string `json:"token"`
		} `json:"gateway"`
	}
	if len(persistedRuntimeConfig) == 0 {

		delete(gw, "token")
		return
	}
	if err := json.Unmarshal(persistedRuntimeConfig, &prev); err != nil || prev.Gateway.Token == "" {
		delete(gw, "token")
		return
	}
	gw["token"] = prev.Gateway.Token
}

func runtimeConfigInputChangesAgent(stored []byte, incoming any) bool {
	var storedDoc any
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &storedDoc); err != nil {
			return true
		}
	}
	maskGatewayToken(storedDoc)
	storedJSON, err := json.Marshal(storedDoc)
	if err != nil {
		return true
	}
	incomingJSON, err := json.Marshal(incoming)
	if err != nil {
		return true
	}
	return !bytes.Equal(storedJSON, incomingJSON)
}

type RepoData struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Ref         string `json:"ref,omitempty"`
}

type ProjectResourceData struct {
	ID           string          `json:"id"`
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        string          `json:"label,omitempty"`
}

type ConnectedAppData = runtimeapps.ConnectedApp

type AgentTaskResponse struct {
	ID          string `json:"id"`
	AgentID     string `json:"agent_id"`
	RuntimeID   string `json:"runtime_id"`
	IssueID     string `json:"issue_id"`
	WorkspaceID string `json:"workspace_id"`

	WorkspaceContext   string                `json:"workspace_context,omitempty"`
	ThreadName         string                `json:"thread_name,omitempty"`
	Status             string                `json:"status"`
	Priority           int32                 `json:"priority"`
	DispatchedAt       *string               `json:"dispatched_at"`
	StartedAt          *string               `json:"started_at"`
	CompletedAt        *string               `json:"completed_at"`
	Result             any                   `json:"result"`
	Error              *string               `json:"error"`
	FailureReason      string                `json:"failure_reason,omitempty"`
	Attempt            int32                 `json:"attempt"`
	MaxAttempts        int32                 `json:"max_attempts"`
	ParentTaskID       *string               `json:"parent_task_id,omitempty"`
	IsLeaderTask       bool                  `json:"is_leader_task,omitempty"`
	Agent              *TaskAgentData        `json:"agent,omitempty"`
	ConnectedApps      []ConnectedAppData    `json:"connected_apps,omitempty"`
	Repos              []RepoData            `json:"repos,omitempty"`
	ProjectID          string                `json:"project_id,omitempty"`
	ProjectTitle       string                `json:"project_title,omitempty"`
	ProjectDescription string                `json:"project_description,omitempty"`
	ProjectResources   []ProjectResourceData `json:"project_resources,omitempty"`
	CreatedAt          string                `json:"created_at"`
	PriorSessionID     string                `json:"prior_session_id,omitempty"`
	PriorWorkDir       string                `json:"prior_work_dir,omitempty"`

	PriorSessionResumeUnavailable bool   `json:"prior_session_resume_unavailable,omitempty"`
	WorkDir                       string `json:"work_dir,omitempty"`

	RelativeWorkDir          string                 `json:"relative_work_dir,omitempty"`
	TriggerCommentID         *string                `json:"trigger_comment_id,omitempty"`
	CoalescedCommentIDs      []string               `json:"coalesced_comment_ids,omitempty"`
	CoalescedComments        []CoalescedCommentData `json:"coalesced_comments,omitempty"`
	DeliveredCommentIDs      []string               `json:"delivered_comment_ids"`
	TriggerThreadID          string                 `json:"trigger_thread_id,omitempty"`
	TriggerCommentContent    string                 `json:"trigger_comment_content,omitempty"`
	TriggerSummary           *string                `json:"trigger_summary,omitempty"`
	TriggerAuthorType        string                 `json:"trigger_author_type,omitempty"`
	TriggerAuthorName        string                 `json:"trigger_author_name,omitempty"`
	NewCommentCount          int                    `json:"new_comment_count,omitempty"`
	NewCommentsSince         string                 `json:"new_comments_since,omitempty"`
	ChatSessionID            string                 `json:"chat_session_id,omitempty"`
	ChatChannelType          string                 `json:"chat_channel_type,omitempty"`
	ChatInThread             bool                   `json:"chat_in_thread,omitempty"`
	ChatMessage              string                 `json:"chat_message,omitempty"`
	ChatMessageAttachments   []ChatAttachmentMeta   `json:"chat_message_attachments,omitempty"`
	ChatIntro                bool                   `json:"chat_intro,omitempty"`
	AutopilotRunID           string                 `json:"autopilot_run_id,omitempty"`
	AutopilotID              string                 `json:"autopilot_id,omitempty"`
	AutopilotTitle           string                 `json:"autopilot_title,omitempty"`
	AutopilotDescription     string                 `json:"autopilot_description,omitempty"`
	AutopilotSource          string                 `json:"autopilot_source,omitempty"`
	AutopilotTriggerPayload  json.RawMessage        `json:"autopilot_trigger_payload,omitempty"`
	QuickCreatePrompt        string                 `json:"quick_create_prompt,omitempty"`
	QuickCreatePriority      string                 `json:"quick_create_priority,omitempty"`
	QuickCreateDueDate       string                 `json:"quick_create_due_date,omitempty"`
	QuickCreateAttachmentIDs []string               `json:"quick_create_attachment_ids,omitempty"`
	HandoffNote              string                 `json:"handoff_note,omitempty"`

	McpPolicy             *DaemonMcpPolicy `json:"mcp_policy,omitempty"`
	SquadID               string           `json:"squad_id,omitempty"`
	SquadName             string           `json:"squad_name,omitempty"`
	ParentIssueID         string           `json:"parent_issue_id,omitempty"`
	ParentIssueIdentifier string           `json:"parent_issue_identifier,omitempty"`

	RequestingUserName               string `json:"requesting_user_name,omitempty"`
	RequestingUserProfileDescription string `json:"requesting_user_profile_description,omitempty"`

	InitiatorType  string `json:"initiator_type,omitempty"`
	InitiatorID    string `json:"initiator_id,omitempty"`
	InitiatorName  string `json:"initiator_name,omitempty"`
	InitiatorEmail string `json:"initiator_email,omitempty"`
	Kind           string `json:"kind"`

	Attribution *TaskAttribution `json:"attribution,omitempty"`

	AuthToken string `json:"auth_token,omitempty"`
}

type TaskAttribution struct {
	Source string `json:"source"`

	Precise bool `json:"precise"`

	Initiator *AttributionUser `json:"initiator,omitempty"`

	Originator *AttributionUser `json:"originator,omitempty"`

	Evidence            *TaskEvidence `json:"evidence,omitempty"`
	RuleVersionID       string        `json:"rule_version_id,omitempty"`
	DelegatedFromTaskID string        `json:"delegated_from_task_id,omitempty"`
	RetryOfTaskID       string        `json:"retry_of_task_id,omitempty"`
	RerunOfTaskID       string        `json:"rerun_of_task_id,omitempty"`
}

type AttributionUser struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type TaskEvidence struct {
	Kind  string `json:"kind"`
	RefID string `json:"ref_id"`
}

func taskAttributionBase(t db.AgentTaskQueue) *TaskAttribution {
	src := attribution.Source(t.OriginatorSource.String)
	attr := &TaskAttribution{
		Source:              src.String(),
		Precise:             src.Precise(),
		RuleVersionID:       uuidToString(t.RuleVersionID),
		DelegatedFromTaskID: uuidToString(t.DelegatedFromTaskID),
		RetryOfTaskID:       uuidToString(t.RetryOfTaskID),
		RerunOfTaskID:       uuidToString(t.RerunOfTaskID),
	}
	if t.AccountableUserID.Valid {
		attr.Initiator = &AttributionUser{ID: uuidToString(t.AccountableUserID)}
	}
	if t.OriginatorUserID.Valid {
		attr.Originator = &AttributionUser{ID: uuidToString(t.OriginatorUserID)}
	}
	if t.TriggerEvidenceKind.Valid && t.TriggerEvidenceKind.String != "" {
		attr.Evidence = &TaskEvidence{Kind: t.TriggerEvidenceKind.String, RefID: uuidToString(t.TriggerEvidenceRefID)}
	}
	return attr
}

func (h *Handler) hydrateTaskAttributions(ctx context.Context, attrs []*TaskAttribution) {
	seen := make(map[string]struct{})
	var ids []pgtype.UUID
	add := func(ref *AttributionUser) {
		if ref == nil || ref.ID == "" {
			return
		}
		if _, ok := seen[ref.ID]; ok {
			return
		}
		if u, err := util.ParseUUID(ref.ID); err == nil {
			seen[ref.ID] = struct{}{}
			ids = append(ids, u)
		}
	}
	for _, a := range attrs {
		if a == nil {
			continue
		}
		add(a.Initiator)
		add(a.Originator)
	}
	if len(ids) == 0 {
		return
	}
	users, err := h.Queries.GetUsersByIDs(ctx, ids)
	if err != nil {
		return
	}
	byID := make(map[string]db.GetUsersByIDsRow, len(users))
	for _, u := range users {
		byID[uuidToString(u.ID)] = u
	}
	fill := func(ref *AttributionUser) {
		if ref == nil {
			return
		}
		if u, ok := byID[ref.ID]; ok {
			ref.Name = u.Name
			ref.Email = u.Email
			if u.AvatarUrl.Valid {
				ref.AvatarURL = u.AvatarUrl.String
			}
		}
	}
	for _, a := range attrs {
		if a == nil {
			continue
		}
		fill(a.Initiator)
		fill(a.Originator)
	}
}

func attributionsOf(resps []AgentTaskResponse) []*TaskAttribution {
	out := make([]*TaskAttribution, 0, len(resps))
	for i := range resps {
		if resps[i].Attribution != nil {
			out = append(out, resps[i].Attribution)
		}
	}
	return out
}

type ChatAttachmentMeta struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
}

type CoalescedCommentData struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id,omitempty"`
	AuthorType string `json:"author_type,omitempty"`
	AuthorName string `json:"author_name,omitempty"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at,omitempty"`
}

type TaskAgentData struct {
	ID                    string                      `json:"id"`
	Name                  string                      `json:"name"`
	Instructions          string                      `json:"instructions"`
	Skills                []service.AgentSkillData    `json:"skills,omitempty"`
	SkillRefs             []service.AgentSkillRefData `json:"skill_refs,omitempty"`
	CustomEnv             map[string]string           `json:"custom_env,omitempty"`
	CustomArgs            []string                    `json:"custom_args,omitempty"`
	McpConfig             json.RawMessage             `json:"mcp_config,omitempty"`
	Model                 string                      `json:"model,omitempty"`
	ThinkingLevel         string                      `json:"thinking_level,omitempty"`
	ServiceTier           string                      `json:"service_tier,omitempty"`
	DisabledRuntimeSkills []DisabledRuntimeSkill      `json:"disabled_runtime_skills,omitempty"`

	RuntimeConfig json.RawMessage `json:"runtime_config,omitempty"`
}

func taskToResponse(t db.AgentTaskQueue, workspaceID string) AgentTaskResponse {
	var result any
	if t.Result != nil {
		json.Unmarshal(t.Result, &result)
	}
	failureReason := ""
	if t.FailureReason.Valid {
		failureReason = t.FailureReason.String
	}
	workDir := ""
	if t.WorkDir.Valid {
		workDir = t.WorkDir.String
	}
	handoffNote := ""
	if t.HandoffNote.Valid {
		handoffNote = t.HandoffNote.String
	}
	return AgentTaskResponse{
		ID:                  uuidToString(t.ID),
		AgentID:             uuidToString(t.AgentID),
		RuntimeID:           uuidToString(t.RuntimeID),
		IssueID:             uuidToString(t.IssueID),
		WorkspaceID:         workspaceID,
		Status:              t.Status,
		Priority:            t.Priority,
		DispatchedAt:        timestampToPtr(t.DispatchedAt),
		StartedAt:           timestampToPtr(t.StartedAt),
		CompletedAt:         timestampToPtr(t.CompletedAt),
		Result:              result,
		Error:               textToPtr(t.Error),
		FailureReason:       failureReason,
		Attempt:             t.Attempt,
		MaxAttempts:         t.MaxAttempts,
		ParentTaskID:        uuidToPtr(t.ParentTaskID),
		IsLeaderTask:        t.IsLeaderTask,
		CreatedAt:           timestampToString(t.CreatedAt),
		TriggerCommentID:    uuidToPtr(t.TriggerCommentID),
		CoalescedCommentIDs: uuidsToStrings(t.CoalescedCommentIds),
		DeliveredCommentIDs: uuidStringsOrEmpty(t.DeliveredCommentIds),
		TriggerSummary:      textToPtr(t.TriggerSummary),
		HandoffNote:         handoffNote,
		WorkDir:             workDir,
		RelativeWorkDir:     relativeWorkDir(workDir, workspaceID, uuidToString(t.ID)),

		ChatSessionID:  uuidToString(t.ChatSessionID),
		AutopilotRunID: uuidToString(t.AutopilotRunID),
		Kind:           computeTaskKind(t),

		Attribution: taskAttributionBase(t),
	}
}

func relativeWorkDir(workDir, workspaceID, taskID string) string {
	if workDir == "" {
		return ""
	}

	normalized := strings.ReplaceAll(workDir, "\\", "/")

	if workspaceID != "" && taskID != "" {
		envRootSuffix := workspaceID + "/" + taskDirSegment(taskID)
		if idx := strings.Index(normalized, envRootSuffix); idx >= 0 {
			return normalized[idx:]
		}
	}

	if stripped, ok := stripHomePrefix(normalized); ok {
		return stripped
	}

	return basename(normalized)
}

const taskDirSegmentLen = 12

func taskDirSegment(uuid string) string {
	s := strings.ReplaceAll(uuid, "-", "")
	if len(s) > taskDirSegmentLen {
		return s[len(s)-taskDirSegmentLen:]
	}
	return s
}

var homeDirPattern = regexp.MustCompile(`(?i)^(?:[A-Za-z]:)?/(?:Users|home)/[^/]+(?:/(.*))?$`)

func stripHomePrefix(p string) (string, bool) {
	m := homeDirPattern.FindStringSubmatch(p)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func basename(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

func computeTaskKind(t db.AgentTaskQueue) string {
	if uuidToString(t.ChatSessionID) != "" {
		return "chat"
	}
	if uuidToString(t.AutopilotRunID) != "" {
		return "autopilot"
	}
	if uuidToString(t.IssueID) == "" {
		return "quick_create"
	}
	if uuidToString(t.TriggerCommentID) != "" {
		return "comment"
	}
	return "direct"
}

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID := requestUserID(r)

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	includeArchived := r.URL.Query().Get("include_archived") == "true"
	listAgents := func() ([]db.Agent, error) {
		if includeArchived {
			return h.Queries.ListAllAgents(r.Context(), parseUUID(workspaceID))
		}
		return h.Queries.ListAgents(r.Context(), parseUUID(workspaceID))
	}

	agents, err := listAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	if actorType == "member" && actorID != "" && !ownsLiveAgent(agents, actorID) {
		if h.provisionMemberHelper(r.Context(), parseUUID(workspaceID), parseUUID(actorID), r.Header.Get("Accept-Language"), helperAnnounceQuiet) {
			agents, err = listAgents()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to list agents")
				return
			}
		}
	}

	if actorType == "member" && !hasLiveRoleAgent(agents) {
		if h.provisionRoleAgent(r.Context(), parseUUID(workspaceID)) {
			agents, err = listAgents()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to list agents")
				return
			}
		}
	}

	skillRows, err := h.Queries.ListAgentSkillsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	skillMap := map[string][]AgentSkillSummary{}
	for _, row := range skillRows {
		agentID := uuidToString(row.AgentID)
		skillMap[agentID] = append(skillMap[agentID], AgentSkillSummary{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Description: row.Description,
			Enabled:     row.Enabled,
		})
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("GetWorkspace failed for redact check", "workspace_id", workspaceID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	alwaysRedact := workspaceAlwaysRedactSecrets(ws.Settings)

	targetsByAgent, ok := h.loadInvocationTargetsByAgent(r.Context(), agents)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to load agent invocation targets")
		return
	}
	visible := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		targets := targetsByAgent[uuidToString(a.ID)]
		if actorType == "member" {
			if !memberAllowedToViewAgent(a, targets, actorID, member.Role) {
				continue
			}
		}
		resp := h.agentToResponse(a)
		applyInvocationTargetsToResponse(&resp, targets)
		if skills, ok := skillMap[resp.ID]; ok {
			resp.Skills = skills
		}

		applyMcpConfigVisibility(&resp, a, actorType, userID, alwaysRedact,
			h.callerManagesAgent(r.Context(), a, actorType, userID))

		if !h.composioMCPAppsEnabled(r.Context()) {
			suppressComposioToolkitAllowlist(&resp)
		} else if actorType == "agent" || uuidToString(a.OwnerID) != userID {
			redactComposioToolkitAllowlist(&resp)
		}
		visible = append(visible, resp)
	}

	writeJSON(w, http.StatusOK, visible)
}

func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	workspaceID := uuidToString(agent.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}
	resp := h.agentToResponse(agent)
	if !h.enrichAgentResponseWithTargetsHTTP(w, r, &resp, agent.ID) {
		return
	}

	if err := h.attachAgentSkills(r.Context(), &resp, agent.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}

	userID := requestUserID(r)
	ws, err := h.Queries.GetWorkspace(r.Context(), agent.WorkspaceID)
	if err != nil {
		slog.Warn("GetWorkspace failed for redact check", "workspace_id", uuidToString(agent.WorkspaceID), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	alwaysRedact := workspaceAlwaysRedactSecrets(ws.Settings)

	applyMcpConfigVisibility(&resp, agent, actorType, userID, alwaysRedact,
		h.callerManagesAgent(r.Context(), agent, actorType, userID))

	if !h.composioMCPAppsEnabled(r.Context()) {
		suppressComposioToolkitAllowlist(&resp)
	} else if actorType == "agent" || uuidToString(agent.OwnerID) != userID {
		redactComposioToolkitAllowlist(&resp)
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateAgentRequest struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Instructions  string            `json:"instructions"`
	AvatarURL     *string           `json:"avatar_url"`
	RuntimeID     string            `json:"runtime_id"`
	RuntimeConfig any               `json:"runtime_config"`
	CustomEnv     map[string]string `json:"custom_env"`
	CustomArgs    []string          `json:"custom_args"`
	McpConfig     json.RawMessage   `json:"mcp_config"`
	Visibility    string            `json:"visibility"`

	PermissionMode     *string                    `json:"permission_mode"`
	InvocationTargets  []AgentInvocationTargetDTO `json:"invocation_targets"`
	MaxConcurrentTasks int32                      `json:"max_concurrent_tasks"`
	Model              string                     `json:"model"`
	ThinkingLevel      string                     `json:"thinking_level"`
	ServiceTier        string                     `json:"service_tier"`

	ComposioToolkitAllowlist []string `json:"composio_toolkit_allowlist"`

	Template string `json:"template"`

	SkillIDs []string `json:"skill_ids"`
}

func decodeJSONBodyWithRawFields(body io.Reader, dst any) (map[string]json.RawMessage, error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(payload, dst); err != nil {
		return nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}

	return raw, nil
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	var req CreateAgentRequest
	rawFields, err := decodeJSONBodyWithRawFields(r.Body, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if utf8.RuneCountInString(req.Description) > maxAgentDescriptionLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxAgentDescriptionLength))
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if req.MaxConcurrentTasks == 0 {
		req.MaxConcurrentTasks = 6
	}

	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	_, hasTargets := rawFields["invocation_targets"]
	legacyVis := req.Visibility
	perm, _, permErr := parsePermissionInput(wsUUID, req.PermissionMode, req.InvocationTargets, req.PermissionMode != nil, hasTargets, &legacyVis)
	if permErr != nil {
		writeError(w, http.StatusBadRequest, permErr.Error())
		return
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can create agents on it")
		return
	}

	if !h.cfg.AllowedProviders.Allows(runtime.Provider) {
		writeError(w, http.StatusForbidden, providerPolicyMessage(runtime.Provider))
		return
	}

	if !agent.IsKnownThinkingValue(runtime.Provider, req.ThinkingLevel) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("thinking_level %q is not a recognised value for runtime %q", req.ThinkingLevel, runtime.Provider))
		return
	}
	if !agent.IsKnownServiceTier(runtime.Provider, req.ServiceTier) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("service_tier %q is not a recognised value for runtime %q", req.ServiceTier, runtime.Provider))
		return
	}

	isFirstAgent := false
	if existing, listErr := h.Queries.ListAgents(r.Context(), wsUUID); listErr == nil {
		isFirstAgent = len(existing) == 0
	}

	preserveMaskedGatewayToken(req.RuntimeConfig, nil)
	rc, _ := json.Marshal(req.RuntimeConfig)
	if req.RuntimeConfig == nil {
		rc = []byte("{}")
	}

	ce, _ := json.Marshal(req.CustomEnv)
	if req.CustomEnv == nil {
		ce = []byte("{}")
	}

	ca, _ := json.Marshal(req.CustomArgs)
	if req.CustomArgs == nil {
		ca = []byte("[]")
	}

	var mc []byte

	auditMcpWrite := false
	if rawMcpConfig, ok := rawFields["mcp_config"]; ok && !bytes.Equal(bytes.TrimSpace(rawMcpConfig), []byte("null")) {
		mc = append([]byte(nil), rawMcpConfig...)

		if containsMaskedMcpMarker(mc) {
			writeError(w, http.StatusBadRequest, "mcp_config carries masked placeholders, which only mean \"keep the stored value\" on update; supply the server configuration itself when creating an agent")
			return
		}

		if err := h.cfg.MCPPolicy.ValidateConfig(mc); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		var err error
		if mc, err = h.prepareMcpConfigForStore(mc); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		auditMcpWrite = true
	}

	allowlist := normaliseComposioToolkitAllowlist(req.ComposioToolkitAllowlist)
	if !h.composioMCPAppsEnabled(r.Context()) {
		allowlist = nil
	}

	skillUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SkillIDs, "skill_ids")
	if !ok {
		return
	}
	for _, skillID := range skillUUIDs {
		if _, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID:          skillID,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "skill does not belong to this workspace")
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start agent create transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	created, err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:              wsUUID,
		Name:                     req.Name,
		Description:              req.Description,
		Instructions:             req.Instructions,
		AvatarUrl:                newAgentAvatar(req.AvatarURL),
		RuntimeMode:              runtime.RuntimeMode,
		RuntimeConfig:            rc,
		RuntimeID:                runtime.ID,
		Visibility:               perm.legacyVisibility(),
		PermissionMode:           perm.mode,
		MaxConcurrentTasks:       req.MaxConcurrentTasks,
		OwnerID:                  parseUUID(ownerID),
		CustomEnv:                ce,
		CustomArgs:               ca,
		McpConfig:                mc,
		Model:                    pgtype.Text{String: req.Model, Valid: req.Model != ""},
		ThinkingLevel:            pgtype.Text{String: req.ThinkingLevel, Valid: req.ThinkingLevel != ""},
		ServiceTier:              pgtype.Text{String: req.ServiceTier, Valid: req.ServiceTier != ""},
		ComposioToolkitAllowlist: allowlist,
	})
	if err != nil {

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "agent_workspace_name_unique" {
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in this workspace", req.Name))
			return
		}
		slog.Warn("create agent failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create agent: "+err.Error())
		return
	}
	if err := replaceInvocationTargetsWithQueries(r.Context(), qtx, created.ID, parseUUID(ownerID), perm.targets); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent access")
		return
	}
	for _, skillID := range skillUUIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: created.ID,
			SkillID: skillID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to attach agent skill")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit agent create")
		return
	}
	if auditMcpWrite {
		h.auditSecretAccess(r, audit.ActionSecretWrite, requestUserID(r), "agent_mcp_config",
			uuidToString(created.ID), workspaceID, audit.OutcomeSuccess, "")
	}
	slog.Info("agent created", append(logger.RequestAttrs(r), "agent_id", uuidToString(created.ID), "name", created.Name, "workspace_id", workspaceID)...)
	if mc != nil {
		h.warnPlaintextMcpConfigWrite(uuidToString(created.ID))
	}

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), created.ID)
		created, _ = h.Queries.GetAgent(r.Context(), created.ID)
	}

	resp := h.agentToResponse(created)
	if err := h.attachAgentSkills(r.Context(), &resp, created.ID); err != nil {
		slog.Warn("create agent: load skills for response failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(created.ID))...)
	}
	if err := h.enrichAgentResponseWithTargets(r.Context(), &resp, created.ID); err != nil {
		slog.Warn("create agent: load invocation targets for response failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(created.ID))...)
	}
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})

	h.sendAgentWelcomeChat(r.Context(), created, ownerID, workspaceID)

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
		ownerID,
		workspaceID,
		uuidToString(created.ID),
		runtime.Provider,
		runtime.RuntimeMode,
		req.Template,
		isFirstAgent,
	))

	redactAgentResponseForCaller(&resp, actorType, created, ownerID,
		h.workspaceKioskRedactsMcpConfig(r.Context(), created.WorkspaceID),
		h.composioMCPAppsEnabled(r.Context()))
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) sendAgentWelcomeChat(ctx context.Context, agent db.Agent, creatorID, workspaceID string) {
	if !agent.RuntimeID.Valid {
		return
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("agent welcome: begin tx failed", "agent_id", uuidToString(agent.ID), "error", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, parseUUID(workspaceID)); err != nil {
		slog.Warn("agent welcome: lock workspace failed", "agent_id", uuidToString(agent.ID), "error", err)
		return
	}
	session, err := qtx.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID:  parseUUID(workspaceID),
		AgentID:      agent.ID,
		CreatorID:    parseUUID(creatorID),
		Title:        "👋 " + agent.Name,
		IsAgentIntro: true,
	})
	if err != nil {
		slog.Warn("agent welcome: create session failed", "agent_id", uuidToString(agent.ID), "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("agent welcome: commit session failed", "agent_id", uuidToString(agent.ID), "error", err)
		return
	}

	if _, err := h.TaskService.EnqueueChatTask(ctx, session, parseUUID(creatorID), false); err != nil {
		slog.Warn("agent welcome: enqueue task failed", "chat_session_id", uuidToString(session.ID), "error", err)
	}
}

type UpdateAgentRequest struct {
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	Instructions  *string `json:"instructions"`
	AvatarURL     *string `json:"avatar_url"`
	RuntimeID     *string `json:"runtime_id"`
	RuntimeConfig any     `json:"runtime_config"`

	CustomArgs *[]string        `json:"custom_args"`
	McpConfig  *json.RawMessage `json:"mcp_config"`
	Visibility *string          `json:"visibility"`

	PermissionMode     *string                     `json:"permission_mode"`
	InvocationTargets  *[]AgentInvocationTargetDTO `json:"invocation_targets"`
	Status             *string                     `json:"status"`
	MaxConcurrentTasks *int32                      `json:"max_concurrent_tasks"`
	Model              *string                     `json:"model"`

	ThinkingLevel *string `json:"thinking_level"`

	ServiceTier *string `json:"service_tier"`

	ComposioToolkitAllowlist *[]string `json:"composio_toolkit_allowlist"`
}

func workspaceAlwaysRedactSecrets(settings []byte) bool {
	if len(settings) == 0 {
		return false
	}
	var s struct {
		AlwaysRedactEnv bool `json:"always_redact_env"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		return false
	}
	return s.AlwaysRedactEnv
}

func canViewAgentSecrets(agent db.Agent, userID string) bool {
	return isAgentOwner(agent, userID)
}

func isAgentOwner(agent db.Agent, userID string) bool {
	owner := uuidToString(agent.OwnerID)
	if owner == "" || userID == "" {
		return false
	}
	return owner == userID
}

func broadcastAgentResponse(resp AgentResponse) AgentResponse {
	out := resp
	redactMcpConfig(&out)
	redactComposioToolkitAllowlist(&out)

	maskGatewayToken(out.RuntimeConfig)
	return out
}

func redactMcpConfig(resp *AgentResponse) {
	if resp.McpConfig != nil {
		resp.McpConfig = nil
		resp.McpConfigRedacted = true
	}
}

func maskMcpConfig(resp *AgentResponse) {
	if resp.McpConfig == nil {
		return
	}
	masked, ok := maskMcpConfigDocument(resp.McpConfig)
	if !ok {
		redactMcpConfig(resp)
		return
	}
	resp.McpConfig = masked
	resp.McpConfigRedacted = true
}

func applyMcpConfigVisibility(resp *AgentResponse, agent db.Agent, actorType, userID string, alwaysRedact, canManage bool) {
	if actorType == "agent" || alwaysRedact {
		redactMcpConfig(resp)
		return
	}
	if canViewAgentSecrets(agent, userID) {
		return
	}
	if !canManage {
		redactMcpConfig(resp)
		return
	}
	maskMcpConfig(resp)
}

func (h *Handler) callerManagesAgent(ctx context.Context, agent db.Agent, actorType, userID string) bool {
	if actorType != "member" || userID == "" {
		return false
	}
	if isAgentOwner(agent, userID) {
		return true
	}
	member, err := h.getWorkspaceMember(ctx, userID, uuidToString(agent.WorkspaceID))
	if err != nil {
		return false
	}
	return roleAllowed(member.Role, "owner", "admin")
}

func redactComposioToolkitAllowlist(resp *AgentResponse) {
	if resp.ComposioToolkitAllowlist != nil {
		resp.ComposioToolkitAllowlist = nil
		resp.ComposioToolkitAllowlistRedacted = true
	}
}

func suppressComposioToolkitAllowlist(resp *AgentResponse) {
	resp.ComposioToolkitAllowlist = nil
	resp.ComposioToolkitAllowlistRedacted = false
}

func normaliseComposioToolkitAllowlist(in []string) []string {
	if in == nil {
		return nil
	}
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func redactAgentResponseForCaller(resp *AgentResponse, actorType string, agent db.Agent, userID string, alwaysRedact, composioEnabled bool) {
	if actorType == "agent" {
		redactMcpConfig(resp)
		redactComposioToolkitAllowlist(resp)
		return
	}

	applyMcpConfigVisibility(resp, agent, actorType, userID, alwaysRedact, true)
	if !composioEnabled {
		suppressComposioToolkitAllowlist(resp)
		return
	}
	if !isAgentOwner(agent, userID) {
		redactComposioToolkitAllowlist(resp)
	}
}

func (h *Handler) workspaceKioskRedactsMcpConfig(ctx context.Context, workspaceID pgtype.UUID) bool {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		slog.Warn("load workspace settings for mcp_config redaction failed; redacting",
			"workspace_id", uuidToString(workspaceID), "error", err)
		return true
	}
	return workspaceAlwaysRedactSecrets(ws.Settings)
}

func runtimeDestinationAllowedForCaller(agent db.Agent, runtime db.AgentRuntime, userID string) bool {
	if uuidToString(agent.RuntimeID) == uuidToString(runtime.ID) {
		return true
	}

	return canViewAgentSecrets(agent, userID)
}

func mcpConfigDeclaresServer(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	containers, _, ok := splitMcpDocument(raw)
	if !ok {
		return true
	}
	for _, entries := range containers {
		if len(entries) > 0 {
			return true
		}
	}
	return false
}

func customArgsInputChangesAgent(stored []byte, incoming []string) bool {
	existing := []string{}
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &existing); err != nil {

			return len(incoming) > 0
		}
	}
	if len(existing) != len(incoming) {
		return true
	}
	for i := range existing {
		if existing[i] != incoming[i] {
			return true
		}
	}
	return false
}

func (h *Handler) canManageAgent(w http.ResponseWriter, r *http.Request, agent db.Agent) bool {

	switch r.Header.Get("X-Actor-Source") {
	case "task_token", "cloud_pat":
		writeError(w, http.StatusForbidden, "agents may not manage agent configuration")
		return false
	}
	wsID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "agent not found", "owner", "admin", "member")
	if !ok {
		return false
	}
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	ownsAgent := isAgentOwner(agent, requestUserID(r))
	if !isAdmin && !ownsAgent {
		writeError(w, http.StatusForbidden, "only the agent owner can manage this agent")
		return false
	}

	if h.denyRoleAgentToNonDeploymentAdmin(w, r, agent) {
		return false
	}
	return true
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, existing) {
		return
	}

	var req UpdateAgentRequest
	rawFields, err := decodeJSONBodyWithRawFields(r.Body, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if _, ok := rawFields["custom_env"]; ok {
		writeError(w, http.StatusBadRequest, "custom_env is no longer accepted on this endpoint; use PUT /api/agents/{id}/env (or `goosar agent env set`)")
		return
	}

	params := db.UpdateAgentParams{
		ID: existing.ID,
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		if utf8.RuneCountInString(*req.Description) > maxAgentDescriptionLength {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxAgentDescriptionLength))
			return
		}
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}

	if !isAgentOwner(existing, requestUserID(r)) {
		if req.Instructions != nil {
			if *req.Instructions != existing.Instructions {
				writeError(w, http.StatusForbidden, "only the agent owner can change instructions: they are executed by the agent on the owner's runtime")
				return
			}
			req.Instructions = nil
		}
		if req.CustomArgs != nil {
			if customArgsInputChangesAgent(existing.CustomArgs, *req.CustomArgs) {
				writeError(w, http.StatusForbidden, "only the agent owner can change custom_args: they alter the command line the owner's runtime executes")
				return
			}
			req.CustomArgs = nil
		}
	}
	if req.Instructions != nil {
		params.Instructions = pgtype.Text{String: *req.Instructions, Valid: true}
	}
	if req.AvatarURL != nil {
		params.AvatarUrl = pgtype.Text{String: *req.AvatarURL, Valid: true}
	}

	if _, hasRuntimeConfig := rawFields["runtime_config"]; hasRuntimeConfig && !isAgentOwner(existing, requestUserID(r)) {
		if runtimeConfigInputChangesAgent(existing.RuntimeConfig, req.RuntimeConfig) {
			writeError(w, http.StatusForbidden, "only the agent owner can change runtime_config: it carries the gateway credential this agent connects with")
			return
		}
		slog.Debug("update agent: non-owner runtime_config matched current state; ignored",
			append(logger.RequestAttrs(r), "agent_id", id)...)
		req.RuntimeConfig = nil
	}
	if req.RuntimeConfig != nil {

		preserveMaskedGatewayToken(req.RuntimeConfig, existing.RuntimeConfig)
		rc, _ := json.Marshal(req.RuntimeConfig)
		params.RuntimeConfig = rc
	}
	if req.CustomArgs != nil {
		ca, _ := json.Marshal(*req.CustomArgs)
		params.CustomArgs = ca
	}
	rawMcpConfig, hasMcpConfig := rawFields["mcp_config"]
	shouldClearMcpConfig := hasMcpConfig && bytes.Equal(bytes.TrimSpace(rawMcpConfig), []byte("null"))
	if hasMcpConfig && !shouldClearMcpConfig {
		incomingMcpConfig := append([]byte(nil), rawMcpConfig...)

		fromMaskedView := !canViewAgentSecrets(existing, requestUserID(r))
		if fromMaskedView || containsMaskedMcpMarker(incomingMcpConfig) {
			stored, err := h.openMcpConfig(existing.McpConfig)
			if err != nil {
				h.auditSecretAccess(r, audit.ActionSecretRead, requestUserID(r), "agent_mcp_config",
					uuidToString(existing.ID), uuidToString(existing.WorkspaceID), audit.OutcomeFailure, audit.ReasonKeyUnavailable)
				slog.Error("update agent: open stored mcp_config for masked merge failed",
					append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
				writeError(w, http.StatusInternalServerError, "failed to read the stored mcp_config")
				return
			}
			merged, err := mergeMaskedMcpConfig(stored, incomingMcpConfig, fromMaskedView)
			if err != nil {

				status := http.StatusBadRequest
				if errors.Is(err, errMcpMaskedWriteNotAllowed) {
					status = http.StatusForbidden
				}
				writeError(w, status, err.Error())
				return
			}
			incomingMcpConfig = merged
		}

		if err := h.cfg.MCPPolicy.ValidateConfig(json.RawMessage(incomingMcpConfig)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		sealed, err := h.prepareMcpConfigForStore(incomingMcpConfig)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.McpConfig = sealed
		h.warnPlaintextMcpConfigWrite(uuidToString(existing.ID))
		h.auditSecretAccess(r, audit.ActionSecretWrite, requestUserID(r), "agent_mcp_config",
			uuidToString(existing.ID), uuidToString(existing.WorkspaceID), audit.OutcomeSuccess, "")
	}

	targetRuntimeID := existing.RuntimeID
	targetProvider := ""
	if req.RuntimeID != nil {
		runtimeUUID, ok := parseUUIDOrBadRequest(w, *req.RuntimeID, "runtime_id")
		if !ok {
			return
		}
		runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
			ID:          runtimeUUID,
			WorkspaceID: existing.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid runtime_id")
			return
		}

		member, ok := h.workspaceMember(w, r, uuidToString(existing.WorkspaceID))
		if !ok {
			return
		}
		if !canUseRuntimeForAgent(member, runtime) {
			writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can move agents onto it")
			return
		}

		if !runtimeDestinationAllowedForCaller(existing, runtime, requestUserID(r)) {
			writeError(w, http.StatusForbidden, "only the agent owner can move this agent to another runtime: the machine running an agent receives its environment variables and MCP configuration in plaintext")
			return
		}

		if !h.cfg.AllowedProviders.Allows(runtime.Provider) {
			writeError(w, http.StatusForbidden, providerPolicyMessage(runtime.Provider))
			return
		}
		params.RuntimeID = runtime.ID
		params.RuntimeMode = pgtype.Text{String: runtime.RuntimeMode, Valid: true}
		targetRuntimeID = runtime.ID
		targetProvider = runtime.Provider
	}

	_, hasPermissionMode := rawFields["permission_mode"]
	_, hasTargets := rawFields["invocation_targets"]
	permissionTouched := hasPermissionMode || hasTargets || req.Visibility != nil
	replacePermissionTargets := false
	var resolvedPerm resolvedPermission
	if permissionTouched {
		ownsAgent := isAgentOwner(existing, requestUserID(r))
		if !ownsAgent {
			changed, permErr := h.permissionInputChangesAgent(r.Context(), existing, req, hasPermissionMode, hasTargets)
			if permErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to evaluate invocation permission change")
				return
			}
			if changed {
				writeError(w, http.StatusForbidden, "only the agent owner can change access (permission_mode / invocation_targets)")
				return
			}
			slog.Debug("update agent: non-owner permission fields matched current state; ignored",
				append(logger.RequestAttrs(r), "agent_id", id)...)
		} else {
			var targetsDTO []AgentInvocationTargetDTO
			if req.InvocationTargets != nil {
				targetsDTO = *req.InvocationTargets
			}
			perm, _, permErr := parsePermissionInput(existing.WorkspaceID, req.PermissionMode, targetsDTO, hasPermissionMode, hasTargets, req.Visibility)
			if permErr != nil {
				writeError(w, http.StatusBadRequest, permErr.Error())
				return
			}
			resolvedPerm = perm
			replacePermissionTargets = true
			params.PermissionMode = pgtype.Text{String: perm.mode, Valid: true}
			params.Visibility = pgtype.Text{String: perm.legacyVisibility(), Valid: true}
		}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.MaxConcurrentTasks != nil {
		params.MaxConcurrentTasks = pgtype.Int4{Int32: *req.MaxConcurrentTasks, Valid: true}
	}
	if req.Model != nil {
		params.Model = pgtype.Text{String: *req.Model, Valid: true}
	} else if req.RuntimeID != nil && existing.Model.Valid && agent.ModelKnownIncompatibleWithProvider(targetProvider, existing.Model.String) {

		params.Model = pgtype.Text{String: "", Valid: true}
	}

	shouldClearThinkingLevel := false
	if req.ThinkingLevel != nil {
		value := *req.ThinkingLevel
		if value == "" {
			shouldClearThinkingLevel = true
		} else {

			provider := targetProvider
			if provider == "" {
				var ok bool
				provider, ok = h.resolveAgentProvider(r, existing.WorkspaceID, targetRuntimeID)
				if !ok {
					writeError(w, http.StatusInternalServerError, "failed to resolve runtime for thinking_level validation")
					return
				}
			}
			if !agent.IsKnownThinkingValue(provider, value) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("thinking_level %q is not a recognised value for runtime %q", value, provider))
				return
			}
			params.ThinkingLevel = pgtype.Text{String: value, Valid: true}
		}
	} else if req.RuntimeID != nil && existing.ThinkingLevel.Valid && existing.ThinkingLevel.String != "" {

		provider := targetProvider
		if provider == "" {
			var ok bool
			provider, ok = h.resolveAgentProvider(r, existing.WorkspaceID, targetRuntimeID)
			if !ok {
				writeError(w, http.StatusInternalServerError, "failed to resolve runtime for thinking_level validation")
				return
			}
		}
		if !agent.IsKnownThinkingValue(provider, existing.ThinkingLevel.String) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"existing thinking_level %q is not valid for runtime %q; pass thinking_level=\"\" to clear or set a value valid for the new runtime",
				existing.ThinkingLevel.String, provider,
			))
			return
		}
	}

	shouldClearServiceTier := false
	if req.ServiceTier != nil {
		value := *req.ServiceTier
		if value == "" {
			shouldClearServiceTier = true
		} else {
			provider := targetProvider
			if provider == "" {
				var ok bool
				provider, ok = h.resolveAgentProvider(r, existing.WorkspaceID, targetRuntimeID)
				if !ok {
					writeError(w, http.StatusInternalServerError, "failed to resolve runtime for service_tier validation")
					return
				}
			}
			if !agent.IsKnownServiceTier(provider, value) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("service_tier %q is not a recognised value for runtime %q", value, provider))
				return
			}
			params.ServiceTier = pgtype.Text{String: value, Valid: true}
		}
	} else if req.RuntimeID != nil && existing.ServiceTier.Valid && existing.ServiceTier.String != "" {
		provider := targetProvider
		if provider == "" {
			var ok bool
			provider, ok = h.resolveAgentProvider(r, existing.WorkspaceID, targetRuntimeID)
			if !ok {
				writeError(w, http.StatusInternalServerError, "failed to resolve runtime for service_tier validation")
				return
			}
		}
		if !agent.IsKnownServiceTier(provider, existing.ServiceTier.String) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"existing service_tier %q is not valid for runtime %q; pass service_tier=\"\" to clear or set a value valid for the new runtime",
				existing.ServiceTier.String, provider,
			))
			return
		}
	}

	shouldClearComposioAllowlist := false
	if _, hasAllowlist := rawFields["composio_toolkit_allowlist"]; hasAllowlist {
		ownsAgent := isAgentOwner(existing, requestUserID(r))
		if !h.composioMCPAppsEnabled(r.Context()) {
			slog.Debug("update agent: composio_toolkit_allowlist write dropped because feature flag is disabled",
				append(logger.RequestAttrs(r), "agent_id", id)...)
		} else if !ownsAgent {
			slog.Debug("update agent: composio_toolkit_allowlist write by non-owner silently dropped",
				append(logger.RequestAttrs(r), "agent_id", id)...)
		} else if req.ComposioToolkitAllowlist == nil {

			shouldClearComposioAllowlist = true
		} else {

			params.ComposioToolkitAllowlist = normaliseComposioToolkitAllowlist(*req.ComposioToolkitAllowlist)
		}
	}

	updated, err := h.Queries.UpdateAgent(r.Context(), params)
	if err != nil {

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "agent_workspace_name_unique" {
			name := ""
			if req.Name != nil {
				name = *req.Name
			}
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in this workspace", name))
			return
		}
		slog.Warn("update agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	if shouldClearMcpConfig {
		updated, err = h.Queries.ClearAgentMcpConfig(r.Context(), updated.ID)
		if err != nil {
			slog.Warn("clear agent mcp_config failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear mcp_config: "+err.Error())
			return
		}
	}
	if shouldClearThinkingLevel {
		updated, err = h.Queries.ClearAgentThinkingLevel(r.Context(), updated.ID)
		if err != nil {
			slog.Warn("clear agent thinking_level failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear thinking_level: "+err.Error())
			return
		}
	}
	if shouldClearServiceTier {
		updated, err = h.Queries.ClearAgentServiceTier(r.Context(), updated.ID)
		if err != nil {
			slog.Warn("clear agent service_tier failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear service_tier: "+err.Error())
			return
		}
	}
	if shouldClearComposioAllowlist {
		updated, err = h.Queries.ClearAgentComposioToolkitAllowlist(r.Context(), updated.ID)
		if err != nil {
			slog.Warn("clear agent composio_toolkit_allowlist failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear composio_toolkit_allowlist: "+err.Error())
			return
		}
	}

	if replacePermissionTargets {
		if err := h.replaceInvocationTargets(r.Context(), updated.ID, parseUUID(requestUserID(r)), resolvedPerm.targets); err != nil {
			slog.Warn("update agent: persist invocation targets failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to update invocation targets: "+err.Error())
			return
		}
	}

	resp := h.agentToResponse(updated)
	if err := h.enrichAgentResponseWithTargets(r.Context(), &resp, updated.ID); err != nil {
		slog.Warn("update agent: load invocation targets for response failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent invocation targets")
		return
	}

	if err := h.attachAgentSkills(r.Context(), &resp, updated.ID); err != nil {
		slog.Warn("load agent skills after update failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	slog.Info("agent updated", append(logger.RequestAttrs(r), "agent_id", id, "workspace_id", uuidToString(updated.WorkspaceID))...)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(updated.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(updated.WorkspaceID), actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})

	redactAgentResponseForCaller(&resp, actorType, updated, userID,
		h.workspaceKioskRedactsMcpConfig(r.Context(), updated.WorkspaceID),
		h.composioMCPAppsEnabled(r.Context()))
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) attachAgentSkills(ctx context.Context, resp *AgentResponse, agentID pgtype.UUID) error {
	skills, err := h.Queries.ListAgentSkillSummaries(ctx, agentID)
	if err != nil {
		return err
	}
	if len(skills) == 0 {
		return nil
	}
	out := make([]AgentSkillSummary, len(skills))
	for i, s := range skills {
		out[i] = AgentSkillSummary{
			ID:          uuidToString(s.ID),
			Name:        s.Name,
			Description: s.Description,
			Enabled:     s.Enabled,
		}
	}
	resp.Skills = out
	return nil
}

func (h *Handler) resolveAgentProvider(r *http.Request, workspaceID pgtype.UUID, runtimeID pgtype.UUID) (string, bool) {
	rt, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return "", false
	}
	return rt.Provider, true
}

func (h *Handler) ArchiveAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "agent is already archived")
		return
	}

	userID := requestUserID(r)
	archived, err := h.Queries.ArchiveAgent(r.Context(), db.ArchiveAgentParams{
		ID:         agent.ID,
		ArchivedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("archive agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to archive agent")
		return
	}

	if cancelled, err := h.Queries.CancelAgentTasksByAgent(r.Context(), agent.ID); err != nil {
		slog.Warn("cancel agent tasks on archive failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
	} else {
		h.TaskService.CaptureCancelledTasks(r.Context(), cancelled)
	}

	wsID := uuidToString(archived.WorkspaceID)
	slog.Info("agent archived", append(logger.RequestAttrs(r), "agent_id", id, "workspace_id", wsID)...)
	resp := h.agentToResponse(archived)
	if err := h.attachAgentSkills(r.Context(), &resp, archived.ID); err != nil {
		slog.Warn("load agent skills after archive failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventAgentArchived, wsID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	redactAgentResponseForCaller(&resp, actorType, archived, userID,
		h.workspaceKioskRedactsMcpConfig(r.Context(), archived.WorkspaceID),
		h.composioMCPAppsEnabled(r.Context()))
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RestoreAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	if !agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "agent is not archived")
		return
	}

	restored, err := h.Queries.RestoreAgent(r.Context(), agent.ID)
	if err != nil {
		slog.Warn("restore agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to restore agent")
		return
	}

	wsID := uuidToString(restored.WorkspaceID)
	slog.Info("agent restored", append(logger.RequestAttrs(r), "agent_id", id, "workspace_id", wsID)...)
	resp := h.agentToResponse(restored)
	if err := h.attachAgentSkills(r.Context(), &resp, restored.ID); err != nil {
		slog.Warn("load agent skills after restore failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventAgentRestored, wsID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	redactAgentResponseForCaller(&resp, actorType, restored, userID,
		h.workspaceKioskRedactsMcpConfig(r.Context(), restored.WorkspaceID),
		h.composioMCPAppsEnabled(r.Context()))
	writeJSON(w, http.StatusOK, resp)
}

type cancelAgentTasksResponse struct {
	Cancelled int `json:"cancelled"`
}

func (h *Handler) CancelAgentTasks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	cancelled, err := h.TaskService.CancelTasksForAgent(r.Context(), parseUUID(id))
	if err != nil {
		slog.Warn("cancel agent tasks failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to cancel tasks")
		return
	}

	slog.Info("agent tasks cancelled",
		append(logger.RequestAttrs(r), "agent_id", id, "count", len(cancelled))...)
	writeJSON(w, http.StatusOK, cancelAgentTasksResponse{Cancelled: len(cancelled)})
}

func (h *Handler) ListAgentTasks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	workspaceID := uuidToString(agent.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}

	tasks, err := h.Queries.ListAgentTasks(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent tasks")
		return
	}

	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t, workspaceID)
	}
	h.hydrateTaskAttributions(r.Context(), attributionsOf(resp))

	writeJSON(w, http.StatusOK, resp)
}

type AgentActivityBucket struct {
	AgentID     string `json:"agent_id"`
	BucketAt    string `json:"bucket_at"`
	TaskCount   int32  `json:"task_count"`
	FailedCount int32  `json:"failed_count"`
}

type AgentRunCount struct {
	AgentID  string `json:"agent_id"`
	RunCount int32  `json:"run_count"`
}

type WorkspaceWorkingAgent struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	AvatarURL        *string  `json:"avatar_url"`
	RunningTaskCount int32    `json:"running_task_count"`
	IssueIDs         []string `json:"issue_ids"`
}

func (h *Handler) ListWorkspaceWorkingAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	workType := strings.TrimSpace(r.URL.Query().Get("type"))
	switch workType {
	case "", "issue", "autopilot", "chat":
	default:
		writeError(w, http.StatusBadRequest, "invalid type: must be issue, autopilot, or chat")
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	mineRelation := strings.TrimSpace(r.URL.Query().Get("relation"))
	var memberID pgtype.UUID
	switch scope {
	case "":
		if mineRelation != "" {
			writeError(w, http.StatusBadRequest, "relation requires scope=mine")
			return
		}
	case "mine":
		if workType != "issue" {
			writeError(w, http.StatusBadRequest, "scope=mine requires type=issue")
			return
		}
		if mineRelation == "" {
			mineRelation = "any"
		}
		switch mineRelation {
		case "assigned", "created", "involved", "any":
		default:
			writeError(w, http.StatusBadRequest, "invalid relation: must be assigned, created, involved, or any")
			return
		}
		userID, ok := requireUserID(w, r)
		if !ok {
			return
		}
		var err error
		memberID, err = util.ParseUUID(userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not authenticated")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid scope: must be mine")
		return
	}

	var parentIssueID pgtype.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("parent")); raw != "" {
		if workType != "issue" {
			writeError(w, http.StatusBadRequest, "parent requires type=issue")
			return
		}
		if scope != "" {
			writeError(w, http.StatusBadRequest, "parent cannot be combined with scope")
			return
		}
		var ok bool
		parentIssueID, ok = parseUUIDOrBadRequest(w, raw, "parent")
		if !ok {
			return
		}
	}

	rows, err := h.Queries.ListWorkspaceWorkingAgents(
		r.Context(),
		db.ListWorkspaceWorkingAgentsParams{
			WorkspaceID:   parseUUID(workspaceID),
			WorkType:      workType,
			MineRelation:  mineRelation,
			MemberID:      memberID,
			ParentIssueID: parentIssueID,
		},
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace working agents")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	resp := make([]WorkspaceWorkingAgent, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.ID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		resp = append(resp, WorkspaceWorkingAgent{
			ID:               agentID,
			Name:             row.Name,
			AvatarURL:        textToPtr(row.AvatarUrl),
			RunningTaskCount: row.RunningTaskCount,
			IssueIDs:         uuidStringsOrEmpty(row.IssueIds),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkspaceAgentRunCounts(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	rows, err := h.Queries.GetWorkspaceAgentRunCounts(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent run counts")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	resp := make([]AgentRunCount, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		resp = append(resp, AgentRunCount{
			AgentID:  agentID,
			RunCount: row.RunCount,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkspaceAgentActivity30d(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	rows, err := h.Queries.GetWorkspaceAgentActivity30d(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent activity")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	resp := make([]AgentActivityBucket, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		resp = append(resp, AgentActivityBucket{
			AgentID:     agentID,
			BucketAt:    timestampToString(row.Bucket),
			TaskCount:   row.TaskCount,
			FailedCount: row.FailedCount,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListWorkspaceAgentTaskSnapshot(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	tasks, err := h.Queries.ListWorkspaceAgentTaskSnapshot(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent task snapshot")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	resp := make([]AgentTaskResponse, 0, len(tasks))
	for _, t := range tasks {
		if _, ok := allowed[uuidToString(t.AgentID)]; !ok {
			continue
		}
		resp = append(resp, taskToResponse(t, workspaceID))
	}
	h.hydrateTaskAttributions(r.Context(), attributionsOf(resp))

	writeJSON(w, http.StatusOK, resp)
}

type DaemonMcpPolicy struct {
	AllowedHosts       []string `json:"allowed_hosts,omitempty"`
	HostsRestricted    bool     `json:"hosts_restricted,omitempty"`
	AllowedCommands    []string `json:"allowed_commands,omitempty"`
	CommandsRestricted bool     `json:"commands_restricted,omitempty"`
}

func daemonMcpPolicyFor(p *perimeterpolicy.MCPPolicy) *DaemonMcpPolicy {
	hosts, hostsRestricted, commands, commandsRestricted := p.Lists()
	if !hostsRestricted && !commandsRestricted {
		return nil
	}
	return &DaemonMcpPolicy{
		AllowedHosts:       hosts,
		HostsRestricted:    hostsRestricted,
		AllowedCommands:    commands,
		CommandsRestricted: commandsRestricted,
	}
}

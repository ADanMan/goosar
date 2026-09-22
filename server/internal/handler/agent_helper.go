package handler

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const (
	helperAgentSystemKey = "goosar_helper"

	helperAgentName = "Goosar Helper"

	helperAgentTemplate = "goosar_helper"

	helperAgentMaxConcurrentTasks = 6

	helperAgentAvatarURL = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1024 1024'%3E%3Cdefs%3E%3ClinearGradient id='t' x1='0' y1='0' x2='0' y2='1'%3E%3Cstop offset='0%25' stop-color='%2323242C'/%3E%3Cstop offset='100%25' stop-color='%2313141A'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='1024' height='1024' rx='224' fill='url(%23t)'/%3E%3Ccircle cx='512' cy='512' r='300' fill='%23FFFFFF'/%3E%3Ccircle cx='512' cy='512' r='174' fill='%23F0BE32'/%3E%3C/svg%3E"

	helperHermesProvider = "runtime-j"

	helperOwnerLabelMaxRunes = 40

	helperNameNumberedAttempts = 32
)

type helperAnnounceMode int

const (
	helperAnnounceFull helperAnnounceMode = iota

	helperAnnounceQuiet
)

func helperLockKey(workspaceID pgtype.UUID) string {
	return "workspace-helper:" + uuidToString(workspaceID)
}

func ownsLiveAgent(agents []db.Agent, userID string) bool {
	for _, a := range agents {
		if a.ArchivedAt.Valid {
			continue
		}
		if a.OwnerID.Valid && uuidToString(a.OwnerID) == userID {
			return true
		}
	}
	return false
}

func (h *Handler) helperProvisionable(ctx context.Context, q *db.Queries, workspaceID, memberUserID pgtype.UUID) bool {
	allowed := h.cfg.AllowedProviders.AllowedList()
	state, err := q.MemberHelperProvisionable(ctx, db.MemberHelperProvisionableParams{
		WorkspaceID:       workspaceID,
		MemberUserID:      memberUserID,
		HelperSystemKey:   pgtype.Text{String: helperAgentSystemKey, Valid: true},
		RestrictProviders: allowed != nil,
		AllowedProviders:  allowed,
	})
	if err != nil {
		slog.Warn("member helper: provisionable probe failed",
			"workspace_id", uuidToString(workspaceID),
			"user_id", uuidToString(memberUserID),
			"error", err)
		return false
	}
	return state.IsMember && state.SlotFree && state.HasUsableRuntime
}

type helperProvisionResult struct {
	Agent        db.Agent
	Created      bool
	IsFirstAgent bool
}

func (h *Handler) ensureMemberHelper(ctx context.Context, q *db.Queries, workspaceID, memberUserID pgtype.UUID, acceptLanguage string) (helperProvisionResult, error) {
	var none helperProvisionResult
	if err := q.LockWorkspaceHelperKey(ctx, helperLockKey(workspaceID)); err != nil {
		return none, err
	}

	if !h.helperProvisionable(ctx, q, workspaceID, memberUserID) {
		return none, nil
	}

	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      memberUserID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {

			return none, nil
		}
		return none, err
	}

	runtime, ok, err := h.pickHelperRuntime(ctx, q, workspaceID, member)
	if err != nil {
		return none, err
	}
	if !ok {
		return none, nil
	}

	lang := helperDefaultContentLang
	owner, err := q.GetUser(ctx, memberUserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return none, err
	}
	if err == nil {
		lang = helperContentLang(owner.Language.String, acceptLanguage)
	}

	baseName := helperAgentName
	instructions := helperInstructionsByLang[lang]
	if defaults, err := q.GetWorkspaceHelperDefault(ctx, workspaceID); err == nil {
		if v := workspaceTemplateText(parseWorkspaceTemplateLangMap(defaults.HelperName), lang); v != "" {
			baseName = v
		}
		if extra := workspaceTemplateText(parseWorkspaceTemplateLangMap(defaults.HelperExtraInstructions), lang); extra != "" {
			instructions = instructions + "\n\n" + extra
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return none, err
	}

	takenNames, err := q.ListWorkspaceAgentNames(ctx, workspaceID)
	if err != nil {
		return none, err
	}
	name, ok := helperAgentNameFor(owner, takenNames, baseName)
	if !ok {
		slog.Warn("member helper: no free name available",
			"workspace_id", uuidToString(workspaceID), "user_id", uuidToString(memberUserID))
		return none, nil
	}

	visibility := "private"
	permissionMode := permissionModePrivate
	if runtime.Visibility == "public" {
		visibility = "workspace"
		permissionMode = permissionModePublicTo
	}

	created, err := q.CreateHelperAgent(ctx, db.CreateHelperAgentParams{
		WorkspaceID:        workspaceID,
		Name:               name,
		Description:        helperDescriptionByLang[lang],
		AvatarUrl:          pgtype.Text{String: helperAgentAvatarURL, Valid: true},
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeID:          runtime.ID,
		Visibility:         visibility,
		PermissionMode:     permissionMode,
		MaxConcurrentTasks: helperAgentMaxConcurrentTasks,
		OwnerID:            memberUserID,
		Instructions:       instructions,
	})
	if err != nil {
		return none, err
	}

	if permissionMode == permissionModePublicTo {
		if err := replaceInvocationTargetsWithQueries(ctx, q, created.ID, memberUserID, []targetSpec{
			{targetType: invocationTargetWorkspace, targetID: workspaceID},
		}); err != nil {
			return none, err
		}
	}

	return helperProvisionResult{Agent: created, Created: true, IsFirstAgent: len(takenNames) == 0}, nil
}

func (h *Handler) provisionMemberHelper(ctx context.Context, workspaceID, memberUserID pgtype.UUID, acceptLanguage string, announce helperAnnounceMode) bool {
	wsID := uuidToString(workspaceID)

	if !h.helperProvisionable(ctx, h.Queries, workspaceID, memberUserID) {
		return false
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("member helper: begin failed", "workspace_id", wsID, "error", err)
		return false
	}
	defer tx.Rollback(ctx)

	result, err := h.ensureMemberHelper(ctx, h.Queries.WithTx(tx), workspaceID, memberUserID, acceptLanguage)
	if err != nil {
		slog.Warn("member helper: provisioning failed", "workspace_id", wsID, "error", err)
		return false
	}
	if !result.Created {
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("member helper: commit failed", "workspace_id", wsID, "error", err)
		return false
	}

	slog.Info("member helper provisioned",
		"workspace_id", wsID,
		"user_id", uuidToString(memberUserID),
		"agent_id", uuidToString(result.Agent.ID))
	h.announceHelperCreated(ctx, wsID, result.Agent, result.IsFirstAgent, announce)
	return true
}

func (h *Handler) announceHelperCreated(ctx context.Context, workspaceID string, helper db.Agent, isFirstAgent bool, announce helperAnnounceMode) {

	actorID := uuidToString(helper.OwnerID)
	actorType := "member"
	if actorID == "" {
		actorType = "system"
	}

	provider := ""
	runtimeOnline := false
	if runtime, err := h.Queries.GetAgentRuntime(ctx, helper.RuntimeID); err == nil {
		provider = runtime.Provider
		runtimeOnline = runtime.Status == "online"
	}

	if runtimeOnline {
		h.TaskService.ReconcileAgentStatus(ctx, helper.ID)
		if refreshed, err := h.Queries.GetAgent(ctx, helper.ID); err == nil {
			helper = refreshed
		}
	}

	resp := broadcastAgentResponse(h.agentToResponse(helper))
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": resp})

	if announce == helperAnnounceFull {
		h.sendAgentWelcomeChat(ctx, helper, actorID, workspaceID)
	}

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
		actorID, workspaceID, uuidToString(helper.ID),
		provider, helper.RuntimeMode, helperAgentTemplate, isFirstAgent,
	))
}

func (h *Handler) pickHelperRuntime(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, member db.Member) (db.AgentRuntime, bool, error) {
	runtimes, err := q.ListAgentRuntimes(ctx, workspaceID)
	if err != nil {
		return db.AgentRuntime{}, false, err
	}

	unprivileged := member
	unprivileged.Role = "member"

	usable := func(rt db.AgentRuntime) bool {
		return rt.Status == "online" &&
			h.cfg.AllowedProviders.Allows(rt.Provider) &&
			canUseRuntimeForAgent(unprivileged, rt)
	}
	isOwn := func(rt db.AgentRuntime) bool {
		return rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID)
	}

	ranks := []func(db.AgentRuntime) bool{
		func(rt db.AgentRuntime) bool { return isOwn(rt) && rt.Provider == helperHermesProvider },
		func(rt db.AgentRuntime) bool { return isOwn(rt) },
	}
	for _, matches := range ranks {
		for _, rt := range runtimes {
			if matches(rt) && usable(rt) {
				return rt, true, nil
			}
		}
	}
	return db.AgentRuntime{}, false, nil
}

func helperOwnerLabel(owner db.User) string {
	label := strings.Join(strings.Fields(owner.Name), " ")
	if label == "" {
		email := strings.TrimSpace(owner.Email)
		if at := strings.IndexByte(email, '@'); at > 0 {
			email = email[:at]
		}
		label = strings.Join(strings.Fields(email), " ")
	}
	if runes := []rune(label); len(runes) > helperOwnerLabelMaxRunes {
		label = strings.TrimSpace(string(runes[:helperOwnerLabelMaxRunes]))
	}
	return label
}

func helperNameCandidates(baseName, label, ownerID string) []string {
	short := strings.ReplaceAll(ownerID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}

	candidates := []string{baseName}
	if label != "" {
		candidates = append(candidates,
			baseName+" ("+label+")",
			baseName+" ("+label+" "+short+")",
		)
	}
	candidates = append(candidates, baseName+" ("+short+")")
	for n := 2; n <= helperNameNumberedAttempts; n++ {
		candidates = append(candidates, baseName+" ("+short+" "+strconv.Itoa(n)+")")
	}
	return candidates
}

func helperAgentNameFor(owner db.User, takenNames []string, baseName string) (string, bool) {
	taken := make(map[string]struct{}, len(takenNames))
	for _, name := range takenNames {
		taken[name] = struct{}{}
	}
	for _, candidate := range helperNameCandidates(baseName, helperOwnerLabel(owner), uuidToString(owner.ID)) {
		if _, clash := taken[candidate]; !clash {
			return candidate, true
		}
	}
	return "", false
}

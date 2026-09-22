package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	"github.com/adanman/goosar/server/pkg/agent"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type AgentRuntimeResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	DaemonID    *string `json:"daemon_id"`
	Name        string  `json:"name"`

	CustomName   *string `json:"custom_name"`
	RuntimeMode  string  `json:"runtime_mode"`
	Provider     string  `json:"provider"`
	LaunchHeader string  `json:"launch_header"`
	Status       string  `json:"status"`
	DeviceInfo   string  `json:"device_info"`
	Metadata     any     `json:"metadata"`
	OwnerID      *string `json:"owner_id"`

	Visibility string `json:"visibility"`

	ProfileID  *string `json:"profile_id"`
	LastSeenAt *string `json:"last_seen_at"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func runtimeToResponse(rt db.AgentRuntime) AgentRuntimeResponse {
	var metadata any
	if rt.Metadata != nil {
		json.Unmarshal(rt.Metadata, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}

	return AgentRuntimeResponse{
		ID:           uuidToString(rt.ID),
		WorkspaceID:  uuidToString(rt.WorkspaceID),
		DaemonID:     textToPtr(rt.DaemonID),
		Name:         rt.Name,
		CustomName:   textToPtr(rt.CustomName),
		RuntimeMode:  rt.RuntimeMode,
		Provider:     rt.Provider,
		LaunchHeader: agent.LaunchHeader(rt.Provider),
		Status:       rt.Status,
		DeviceInfo:   rt.DeviceInfo,
		Metadata:     metadata,
		OwnerID:      uuidToPtr(rt.OwnerID),
		Visibility:   rt.Visibility,
		ProfileID:    uuidToPtr(rt.ProfileID),
		LastSeenAt:   timestampToPtr(rt.LastSeenAt),
		CreatedAt:    timestampToString(rt.CreatedAt),
		UpdatedAt:    timestampToString(rt.UpdatedAt),
	}
}

type RuntimeUsageResponse struct {
	RuntimeID        string `json:"runtime_id"`
	Date             string `json:"date"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`

	CostUSDTicks             int64 `json:"cost_usd_ticks"`
	UncostedInputTokens      int64 `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64 `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64 `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64 `json:"uncosted_cache_write_tokens"`
}

func (h *Handler) GetRuntimeUsage(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	viewTZ := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 90, viewTZ)

	resp, err := h.listRuntimeUsage(r.Context(), rt.ID, viewTZ, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listRuntimeUsage(ctx context.Context, runtimeID pgtype.UUID, tz string, since pgtype.Timestamptz) ([]RuntimeUsageResponse, error) {
	resolvedRuntimeID := uuidToString(runtimeID)
	rows, err := h.Queries.ListRuntimeUsage(ctx, db.ListRuntimeUsageParams{
		RuntimeID: runtimeID,
		Since:     since,
		Tz:        tz,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]RuntimeUsageResponse, len(rows))
	for i, row := range rows {
		resp[i] = RuntimeUsageResponse{
			RuntimeID:                resolvedRuntimeID,
			Date:                     row.Date.Time.Format("2006-01-02"),
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
		}
	}
	return resp, nil
}

func (h *Handler) GetRuntimeTaskActivity(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	viewTZ := h.resolveViewingTZ(r)
	rows, err := h.Queries.GetRuntimeTaskHourlyActivity(r.Context(), db.GetRuntimeTaskHourlyActivityParams{
		RuntimeID: rt.ID,
		Tz:        viewTZ,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get task activity")
		return
	}

	type HourlyActivity struct {
		Hour  int `json:"hour"`
		Count int `json:"count"`
	}

	resp := make([]HourlyActivity, len(rows))
	for i, row := range rows {
		resp[i] = HourlyActivity{Hour: int(row.Hour), Count: int(row.Count)}
	}

	writeJSON(w, http.StatusOK, resp)
}

type RuntimeUsageByAgentResponse struct {
	AgentID          string `json:"agent_id"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`

	CostUSDTicks             int64 `json:"cost_usd_ticks"`
	UncostedInputTokens      int64 `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64 `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64 `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64 `json:"uncosted_cache_write_tokens"`
	TaskCount                int32 `json:"task_count"`
}

func (h *Handler) GetRuntimeUsageByAgent(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	viewTZ := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, viewTZ)

	rows, err := h.Queries.ListRuntimeUsageByAgent(r.Context(), db.ListRuntimeUsageByAgentParams{
		RuntimeID: rt.ID,
		Since:     since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by agent")
		return
	}

	resp := make([]RuntimeUsageByAgentResponse, len(rows))
	for i, row := range rows {
		resp[i] = RuntimeUsageByAgentResponse{
			AgentID:                  uuidToString(row.AgentID),
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
			TaskCount:                row.TaskCount,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type RuntimeUsageByHourResponse struct {
	Hour             int    `json:"hour"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`

	CostUSDTicks             int64 `json:"cost_usd_ticks"`
	UncostedInputTokens      int64 `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64 `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64 `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64 `json:"uncosted_cache_write_tokens"`
	TaskCount                int32 `json:"task_count"`
}

func (h *Handler) GetRuntimeUsageByHour(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	viewTZ := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, viewTZ)

	rows, err := h.Queries.GetRuntimeUsageByHour(r.Context(), db.GetRuntimeUsageByHourParams{
		RuntimeID: rt.ID,
		Since:     since,
		Tz:        viewTZ,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get usage by hour")
		return
	}

	resp := make([]RuntimeUsageByHourResponse, len(rows))
	for i, row := range rows {
		resp[i] = RuntimeUsageByHourResponse{
			Hour:                     int(row.Hour),
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
			TaskCount:                row.TaskCount,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func sinceFromDays(now time.Time, days int, loc *time.Location) time.Time {
	local := now.In(loc)
	startOfToday := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return startOfToday.AddDate(0, 0, -days)
}

func parseSinceParamInTZ(r *http.Request, defaultDays int, tzName string) pgtype.Timestamptz {
	return parseDaysCutoff(r, defaultDays, tzName, 0)
}

func parseExactSinceParamInTZ(r *http.Request, defaultDays int, tzName string) pgtype.Timestamptz {
	return parseDaysCutoff(r, defaultDays, tzName, 1)
}

func parseDaysCutoff(
	r *http.Request,
	defaultDays int,
	tzName string,
	trimDays int,
) pgtype.Timestamptz {
	days := defaultDays
	if d := r.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil || loc == nil {
		loc = time.UTC
	}

	return pgtype.Timestamptz{
		Time:  sinceFromDays(time.Now(), days-trimDays, loc),
		Valid: true,
	}
}

func (h *Handler) resolveViewingTZ(r *http.Request) string {
	if tz := strings.TrimSpace(r.URL.Query().Get("tz")); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil && loc != nil {
			return tz
		}
	}
	if userID := requestUserID(r); userID != "" {
		uid, err := util.ParseUUID(userID)
		if err != nil {
			slog.Warn("resolveViewingTZ: malformed X-User-ID, falling back to UTC",
				"path", r.URL.Path, "user_id", userID)
		}
		if err == nil {
			slog.Debug("resolveViewingTZ cold path: ?tz= missing, reading user.timezone",
				"path", r.URL.Path, "user_id", userID)
			if user, err := h.Queries.GetUser(r.Context(), uid); err == nil && user.Timezone.Valid {
				stored := strings.TrimSpace(user.Timezone.String)
				if stored != "" {
					if loc, err := time.LoadLocation(stored); err == nil && loc != nil {
						return stored
					}
				}
			}
		}
	}
	return "UTC"
}

type UpdateAgentRuntimeRequest struct {
	Visibility *string `json:"visibility,omitempty"`

	CustomName *string `json:"custom_name,omitempty"`

	ApplyToMachine bool `json:"apply_to_machine,omitempty"`
}

const maxRuntimeCustomNameLen = 100

func (h *Handler) UpdateAgentRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only edit your own runtimes")
		return
	}

	var req UpdateAgentRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var (
		newVisibility  string
		needVisibility bool
	)
	if req.Visibility != nil {
		v := *req.Visibility
		if v != "private" && v != "public" {
			writeError(w, http.StatusBadRequest, "visibility must be 'private' or 'public'")
			return
		}
		if v != rt.Visibility {
			newVisibility = v
			needVisibility = true
		}
	}

	if req.CustomName != nil {
		if len([]rune(strings.TrimSpace(*req.CustomName))) > maxRuntimeCustomNameLen {
			writeError(w, http.StatusBadRequest, "custom name is too long")
			return
		}
	}

	changed := false

	if needVisibility {
		updated, err := h.Queries.UpdateAgentRuntimeVisibility(r.Context(), db.UpdateAgentRuntimeVisibilityParams{
			ID:         runtimeUUID,
			Visibility: newVisibility,
		})
		if err != nil {
			slog.Error("UpdateAgentRuntimeVisibility failed", "error", err, "runtime_id", runtimeID)
			writeError(w, http.StatusInternalServerError, "failed to update runtime")
			return
		}
		rt = updated
		changed = true
	}

	if req.CustomName != nil {

		trimmed := strings.TrimSpace(*req.CustomName)
		customName := pgtype.Text{String: trimmed, Valid: trimmed != ""}

		if req.ApplyToMachine && rt.DaemonID.Valid {

			var ownerFilter pgtype.UUID
			if !roleAllowed(member.Role, "owner", "admin") {
				ownerFilter = member.UserID
			}
			rows, err := h.Queries.UpdateAgentRuntimeCustomNameByDaemon(r.Context(), db.UpdateAgentRuntimeCustomNameByDaemonParams{
				CustomName:  customName,
				WorkspaceID: rt.WorkspaceID,
				DaemonID:    rt.DaemonID,
				OwnerID:     ownerFilter,
			})
			if err != nil {
				slog.Error("UpdateAgentRuntimeCustomNameByDaemon failed", "error", err, "runtime_id", runtimeID)
				writeError(w, http.StatusInternalServerError, "failed to update runtime")
				return
			}

			for _, row := range rows {
				if uuidToString(row.ID) == uuidToString(runtimeUUID) {
					rt = row
					break
				}
			}
			changed = true
		} else {
			updated, err := h.Queries.UpdateAgentRuntimeCustomName(r.Context(), db.UpdateAgentRuntimeCustomNameParams{
				CustomName: customName,
				ID:         runtimeUUID,
			})
			if err != nil {
				slog.Error("UpdateAgentRuntimeCustomName failed", "error", err, "runtime_id", runtimeID)
				writeError(w, http.StatusInternalServerError, "failed to update runtime")
				return
			}
			rt = updated
			changed = true
		}
	}

	if changed {

		h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{
			"action": "update",
		})
	}

	writeJSON(w, http.StatusOK, runtimeToResponse(rt))
}

func canEditRuntime(member db.Member, rt db.AgentRuntime) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	return rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID)
}

func (h *Handler) runtimeHasLiveProfile(ctx context.Context, rt db.AgentRuntime) (bool, error) {
	if !rt.ProfileID.Valid {
		return false, nil
	}
	if _, err := h.Queries.GetRuntimeProfileForWorkspace(ctx, db.GetRuntimeProfileForWorkspaceParams{
		ID:          rt.ProfileID,
		WorkspaceID: rt.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func canUseRuntimeForAgent(member db.Member, rt db.AgentRuntime) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	if rt.Visibility == "public" {
		return true
	}
	return rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID)
}

func (h *Handler) ListAgentRuntimes(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	var runtimes []db.AgentRuntime
	var err error

	if ownerFilter := r.URL.Query().Get("owner"); ownerFilter == "me" {
		userID, ok := requireUserID(w, r)
		if !ok {
			return
		}
		runtimes, err = h.Queries.ListAgentRuntimesByOwner(r.Context(), db.ListAgentRuntimesByOwnerParams{
			WorkspaceID: parseUUID(workspaceID),
			OwnerID:     parseUUID(userID),
		})
	} else {
		runtimes, err = h.Queries.ListAgentRuntimes(r.Context(), parseUUID(workspaceID))
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtimes")
		return
	}

	resp := make([]AgentRuntimeResponse, len(runtimes))
	for i, rt := range runtimes {
		resp[i] = runtimeToResponse(rt)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteAgentRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return
	}

	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only delete your own runtimes")
		return
	}
	userID := uuidToString(member.UserID)

	hasLiveProfile, err := h.runtimeHasLiveProfile(r.Context(), rt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime profile")
		return
	}
	if hasLiveProfile {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "cannot delete a custom runtime instance directly; delete its runtime profile instead.",
			"code":  "runtime_profile_instance_delete_unsupported",
		})
		return
	}
	if rt.ProfileID.Valid {
		slog.Warn("deleting orphaned profile-backed runtime instance",
			"runtime_id", uuidToString(rt.ID),
			"profile_id", uuidToString(rt.ProfileID),
			"workspace_id", wsID,
			"deleted_by", userID)
	}

	activeAgents, err := h.Queries.ListActiveAgentsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime dependencies")
		return
	}
	if len(activeAgents) > 0 {
		writeJSON(w, http.StatusConflict, h.runtimeHasActiveAgentsResponse(r, rt.WorkspaceID, member, activeAgents))
		return
	}

	activeSquadCount, err := h.Queries.CountActiveSquadsWithArchivedLeadersByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime squad dependencies")
		return
	}
	if activeSquadCount > 0 {
		writeError(w, http.StatusConflict, "cannot delete runtime: it has active squads led by archived agents. Archive those squads or assign them a new leader first.")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	archivedAgentIDs, err := qtx.ListArchivedAgentIDsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate archived agents")
		return
	}
	if len(archivedAgentIDs) > 0 {
		if err := qtx.PauseAutopilotsByAgentAssignees(r.Context(), archivedAgentIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to pause autopilots")
			return
		}
	}

	if err := qtx.DeleteSquadsByArchivedAgentsOnRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up squads referencing archived agents")
		return
	}

	if err := qtx.DeleteAgentInvocationTargetsByArchivedRuntimeAgents(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up agent invocation targets")
		return
	}

	if err := qtx.DeleteChannelInstallationsByArchivedRuntimeAgents(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up channel installations")
		return
	}
	if err := qtx.DeleteChatPinnedAgentsByArchivedRuntimeAgents(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up chat pins")
		return
	}

	if err := qtx.DeleteAgentLabelAssignmentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up agent label assignments")
		return
	}

	if err := pruneRuntimeAgentChatDraftRestores(r.Context(), qtx, rt.ID, true); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up chat draft restores")
		return
	}

	if err := qtx.DeleteAgentMcpServersByArchivedRuntimeAgents(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up agent MCP assignments")
		return
	}
	if err := qtx.DeleteArchivedAgentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up archived agents")
		return
	}
	if err := qtx.DeleteSystemAgentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up system agents")
		return
	}

	if err := qtx.DeleteAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}

	slog.Info("runtime deleted", "runtime_id", uuidToString(rt.ID), "deleted_by", userID)

	h.publish(protocol.EventDaemonRegister, wsID, "member", userID, map[string]any{
		"action": "delete",
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) runtimeHasActiveAgentsResponse(r *http.Request, wsUUID pgtype.UUID, member db.Member, agents []db.Agent) map[string]any {
	workspaceID := uuidToString(wsUUID)
	userID := uuidToString(member.UserID)
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	alwaysRedact := true
	if ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID); err == nil {
		alwaysRedact = workspaceAlwaysRedactSecrets(ws.Settings)
	} else {
		slog.Warn("runtime 409 body: GetWorkspace failed; redacting agent secrets", "workspace_id", workspaceID, "error", err)
	}
	composioEnabled := h.composioMCPAppsEnabled(r.Context())
	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		ar := h.agentToResponse(a)
		applyMcpConfigVisibility(&ar, a, actorType, userID, alwaysRedact,
			h.callerManagesAgent(r.Context(), a, actorType, userID))
		if !composioEnabled {
			suppressComposioToolkitAllowlist(&ar)
		} else if actorType == "agent" || !isAgentOwner(a, userID) {
			redactComposioToolkitAllowlist(&ar)
		}
		resp[i] = ar
	}
	return map[string]any{

		"error":         "cannot delete runtime: it has active agents bound to it. Archive them, or have each agent's owner reassign their agent — only the owner can move an agent, since the destination machine receives its secrets in plaintext.",
		"code":          "runtime_has_active_agents",
		"active_agents": resp,
	}
}

type archiveAgentsAndDeleteRuntimeRequest struct {
	ExpectedActiveAgentIDs []string `json:"expected_active_agent_ids"`
}

func (h *Handler) ArchiveAgentsAndDeleteRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	var req archiveAgentsAndDeleteRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	expected, ok := parseExpectedActiveAgentIDs(req.ExpectedActiveAgentIDs)
	if !ok {
		writeError(w, http.StatusBadRequest, "expected_active_agent_ids must be a list of valid UUIDs")
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only delete your own runtimes")
		return
	}
	userID := uuidToString(member.UserID)

	hasLiveProfile, err := h.runtimeHasLiveProfile(r.Context(), rt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime profile")
		return
	}
	if hasLiveProfile {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "cannot delete a custom runtime instance directly; delete its runtime profile instead.",
			"code":  "runtime_profile_instance_delete_unsupported",
		})
		return
	}
	if rt.ProfileID.Valid {
		slog.Warn("deleting orphaned profile-backed runtime instance via cascade",
			"runtime_id", uuidToString(rt.ID),
			"profile_id", uuidToString(rt.ProfileID),
			"workspace_id", wsID,
			"deleted_by", userID)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime")
		return
	}

	currentActive, err := qtx.ListActiveAgentsByRuntimeForUpdate(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate active agents")
		return
	}
	if !activeAgentSetMatches(currentActive, expected) {

		body := h.runtimeHasActiveAgentsResponse(r, rt.WorkspaceID, member, currentActive)
		body["code"] = "runtime_delete_plan_changed"
		body["error"] = "the active agent set changed; please review and confirm again."
		writeJSON(w, http.StatusConflict, body)
		return
	}

	currentActiveIDs := make([]pgtype.UUID, len(currentActive))
	for i, a := range currentActive {
		currentActiveIDs[i] = a.ID
	}

	archivedAgents, err := qtx.ArchiveAgentsByIDs(r.Context(), db.ArchiveAgentsByIDsParams{
		ArchivedBy: member.UserID,
		AgentIds:   currentActiveIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive agents")
		return
	}

	archivedIDs := make([]pgtype.UUID, len(archivedAgents))
	for i, a := range archivedAgents {
		archivedIDs[i] = a.ID
	}
	cancelledTasks, err := qtx.CancelAgentTasksByRuntimeOrAgent(r.Context(), db.CancelAgentTasksByRuntimeOrAgentParams{
		RuntimeIds: []pgtype.UUID{rt.ID},
		AgentIds:   archivedIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel tasks")
		return
	}

	allArchivedIDs, err := qtx.ListArchivedAgentIDsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate archived agents")
		return
	}
	if len(allArchivedIDs) > 0 {
		if err := qtx.PauseAutopilotsByAgentAssignees(r.Context(), allArchivedIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to pause autopilots")
			return
		}
	}

	if err := qtx.DeleteAgentInvocationTargetsByArchivedRuntimeAgents(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up agent invocation targets")
		return
	}

	if err := qtx.DeleteChannelInstallationsByArchivedRuntimeAgents(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up channel installations")
		return
	}
	if err := qtx.DeleteChatPinnedAgentsByArchivedRuntimeAgents(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up chat pins")
		return
	}

	if err := qtx.DeleteAgentLabelAssignmentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up agent label assignments")
		return
	}

	if err := pruneRuntimeAgentChatDraftRestores(r.Context(), qtx, rt.ID, true); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up chat draft restores")
		return
	}

	if err := qtx.DeleteAgentMcpServersByArchivedRuntimeAgents(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up agent MCP assignments")
		return
	}
	if err := qtx.DeleteArchivedAgentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up archived agents")
		return
	}
	if err := qtx.DeleteSystemAgentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up system agents")
		return
	}

	if err := qtx.DeleteAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	if h.TaskService != nil && len(cancelledTasks) > 0 {
		h.TaskService.BroadcastCancelledTasks(r.Context(), cancelledTasks)
	}
	for _, a := range archivedAgents {

		h.publish(protocol.EventAgentArchived, wsID, "member", userID, map[string]any{
			"agent": broadcastAgentResponse(h.agentToResponse(a)),
		})
	}
	h.publish(protocol.EventDaemonRegister, wsID, "member", userID, map[string]any{
		"action": "delete",
	})

	slog.Info("runtime deleted via cascade",
		"runtime_id", uuidToString(rt.ID),
		"deleted_by", userID,
		"agents_archived", len(archivedAgents),
		"tasks_cancelled", len(cancelledTasks),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"agents_archived": len(archivedAgents),
		"tasks_cancelled": len(cancelledTasks),
	})
}

func parseExpectedActiveAgentIDs(raw []string) (map[string]struct{}, bool) {
	out := make(map[string]struct{}, len(raw))
	for _, s := range raw {
		u, err := util.ParseUUID(s)
		if err != nil || !u.Valid {
			return nil, false
		}
		out[uuidToString(u)] = struct{}{}
	}
	return out, true
}

func activeAgentSetMatches(current []db.Agent, expected map[string]struct{}) bool {
	if len(current) != len(expected) {
		return false
	}
	for _, a := range current {
		if _, ok := expected[uuidToString(a.ID)]; !ok {
			return false
		}
	}
	return true
}

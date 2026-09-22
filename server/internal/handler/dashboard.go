package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func parseProjectIDParam(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := r.URL.Query().Get("project_id")
	if raw == "" {
		return pgtype.UUID{}, true
	}
	u, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project_id")
		return pgtype.UUID{}, false
	}
	return u, true
}

type DashboardUsageDailyResponse struct {
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
	TaskCount                int32 `json:"task_count"`
}

func (h *Handler) GetDashboardUsageDaily(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	tz := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, tz)

	resp, err := h.listDashboardUsageDaily(r.Context(), parseUUID(workspaceID), tz, since, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listDashboardUsageDaily(
	ctx context.Context,
	workspaceID pgtype.UUID,
	tz string,
	since pgtype.Timestamptz,
	projectID pgtype.UUID,
) ([]DashboardUsageDailyResponse, error) {
	rows, err := h.Queries.ListDashboardUsageDaily(ctx, db.ListDashboardUsageDailyParams{
		WorkspaceID: workspaceID,
		Tz:          tz,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]DashboardUsageDailyResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardUsageDailyResponse{
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
			TaskCount:                row.TaskCount,
		}
	}
	return resp, nil
}

type DashboardUsageByAgentResponse struct {
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

func (h *Handler) GetDashboardUsageByAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}

	tz := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, tz)

	resp, err := h.listDashboardUsageByAgent(r.Context(), parseUUID(workspaceID), since, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by agent")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listDashboardUsageByAgent(
	ctx context.Context,
	workspaceID pgtype.UUID,
	since pgtype.Timestamptz,
	projectID pgtype.UUID,
) ([]DashboardUsageByAgentResponse, error) {
	rows, err := h.Queries.ListDashboardUsageByAgent(ctx, db.ListDashboardUsageByAgentParams{
		WorkspaceID: workspaceID,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]DashboardUsageByAgentResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardUsageByAgentResponse{
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
	return resp, nil
}

type DashboardAgentRunTimeResponse struct {
	AgentID      string `json:"agent_id"`
	TotalSeconds int64  `json:"total_seconds"`
	TaskCount    int32  `json:"task_count"`
	FailedCount  int32  `json:"failed_count"`
}

func (h *Handler) GetDashboardAgentRunTime(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}

	tz := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, tz)

	rows, err := h.Queries.ListDashboardAgentRunTime(r.Context(), db.ListDashboardAgentRunTimeParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent runtime")
		return
	}

	resp := make([]DashboardAgentRunTimeResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardAgentRunTimeResponse{
			AgentID:      uuidToString(row.AgentID),
			TotalSeconds: row.TotalSeconds,
			TaskCount:    row.TaskCount,
			FailedCount:  row.FailedCount,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type DashboardRunTimeDailyResponse struct {
	Date         string `json:"date"`
	TotalSeconds int64  `json:"total_seconds"`
	TaskCount    int32  `json:"task_count"`
	FailedCount  int32  `json:"failed_count"`
}

func (h *Handler) GetDashboardRunTimeDaily(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}

	tz := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, tz)

	rows, err := h.Queries.ListDashboardRunTimeDaily(r.Context(), db.ListDashboardRunTimeDailyParams{
		WorkspaceID: parseUUID(workspaceID),
		Tz:          tz,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list daily runtime")
		return
	}

	resp := make([]DashboardRunTimeDailyResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardRunTimeDailyResponse{
			Date:         row.Date.Time.Format("2006-01-02"),
			TotalSeconds: row.TotalSeconds,
			TaskCount:    row.TaskCount,
			FailedCount:  row.FailedCount,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type DashboardFailureDailyResponse struct {
	Date          string `json:"date"`
	FailureReason string `json:"failure_reason"`
	TaskCount     int32  `json:"task_count"`
}

func (h *Handler) GetDashboardFailuresDaily(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}

	tz := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, tz)

	rows, err := h.Queries.ListDashboardFailuresDaily(r.Context(), db.ListDashboardFailuresDailyParams{
		WorkspaceID: parseUUID(workspaceID),
		Tz:          tz,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list daily failures")
		return
	}

	resp := make([]DashboardFailureDailyResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardFailureDailyResponse{
			Date:          row.Date.Time.Format("2006-01-02"),
			FailureReason: row.FailureReason,
			TaskCount:     row.TaskCount,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type DashboardFailureByAgentResponse struct {
	AgentID       string `json:"agent_id"`
	FailureReason string `json:"failure_reason"`
	TaskCount     int32  `json:"task_count"`
}

func (h *Handler) GetDashboardFailuresByAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}

	tz := h.resolveViewingTZ(r)
	since := parseExactSinceParamInTZ(r, 30, tz)

	rows, err := h.Queries.ListDashboardFailuresByAgent(r.Context(), db.ListDashboardFailuresByAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failures by agent")
		return
	}

	resp := make([]DashboardFailureByAgentResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardFailureByAgentResponse{
			AgentID:       uuidToString(row.AgentID),
			FailureReason: row.FailureReason,
			TaskCount:     row.TaskCount,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

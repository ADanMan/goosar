package runtime

import (
	"encoding/json"
	"time"
)

// runtimeDTO — Runtime/AgentRuntime контракта (одна схема на обе; см.
// docs/51-data-model.md §"Соответствие", "Runtime, AgentRuntime (одна сущность)").
type runtimeDTO struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	DaemonID     *string         `json:"daemon_id"`
	Name         string          `json:"name"`
	CustomName   *string         `json:"custom_name"`
	RuntimeMode  string          `json:"runtime_mode"`
	Provider     string          `json:"provider"`
	LaunchHeader string          `json:"launch_header,omitempty"`
	Status       string          `json:"status"`
	DeviceInfo   string          `json:"device_info"`
	Metadata     json.RawMessage `json:"metadata"`
	OwnerID      *string         `json:"owner_id"`
	Visibility   string          `json:"visibility"`
	ProfileID    *string         `json:"profile_id"`
	LastSeenAt   *time.Time      `json:"last_seen_at"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

func toRuntimeDTO(e Executor) runtimeDTO {
	dto := runtimeDTO{
		ID: e.ID, WorkspaceID: e.WorkspaceID, DaemonID: e.DaemonID, Name: e.Title, CustomName: e.CustomTitle,
		RuntimeMode: e.Mode, Provider: e.Provider, Status: e.Status(), DeviceInfo: e.DeviceInfo,
		Metadata: e.Metadata, OwnerID: e.OwnerID, Visibility: e.Visibility, ProfileID: e.ProfileID,
		LastSeenAt: e.LastSeenAt, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
	if e.LaunchHeader != nil {
		dto.LaunchHeader = *e.LaunchHeader
	}
	return dto
}

func toRuntimeDTOs(list []Executor) []runtimeDTO {
	out := make([]runtimeDTO, len(list))
	for i, e := range list {
		out[i] = toRuntimeDTO(e)
	}
	return out
}

// usageDTO — форme UsageTokenFields + расширение (агент/дата/час/task_count),
// заполняется частично в зависимости от эндпоинта.
type usageDTO struct {
	RuntimeID        string  `json:"runtime_id,omitempty"`
	AgentID          string  `json:"agent_id,omitempty"`
	Date             string  `json:"date,omitempty"`
	Hour             *int    `json:"hour,omitempty"`
	TaskCount        *int64  `json:"task_count,omitempty"`
	Provider         *string `json:"provider,omitempty"`
	Model            string  `json:"model"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CostUSDTicks     int64   `json:"cost_usd_ticks"`
}

func fromUsageRow(u UsageRow) usageDTO {
	return usageDTO{
		Provider: u.Provider, Model: u.Model, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
		CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens, CostUSDTicks: u.CostUSDTicks,
	}
}

// probeDTO — RuntimeUpdateRequest/RuntimeModelListRequest/
// RuntimeLocalSkillListRequest/RuntimeLocalSkillImportRequest: все четыре
// схемы контракта разделяют id/runtime_id/status/created_at/updated_at/error
// и различаются только kind-специфичными полями (target_version, models[],
// skills[], skill_key, ...) — которые лежат в probe_request/probe_outcome как
// есть и переносятся в ответ через json.RawMessage-слияние (mergeProbe), а не
// отдельную Go-структуру на каждую из четырёх схем.
func probeToJSON(p Probe) map[string]any {
	out := map[string]any{
		"id":         p.ID,
		"runtime_id": p.ExecutorID,
		"status":     p.Status,
		"created_at": p.CreatedAt,
		"updated_at": p.UpdatedAt,
	}
	if p.Error != nil {
		out["error"] = *p.Error
	}
	mergeRawObject(out, p.Request)
	mergeRawObject(out, p.Outcome)
	return out
}

func mergeRawObject(dst map[string]any, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var extra map[string]any
	if err := json.Unmarshal(raw, &extra); err != nil {
		return
	}
	for k, v := range extra {
		dst[k] = v
	}
}

package autopilot

import (
	"encoding/json"
	"time"
)

// Autopilot — components/schemas/Autopilot (sentinels, 006_sentinels.up.sql).
// MarshalJSON заполняет пустые срезы/объекты нулевыми значениями контракта
// ("[]"/"{}"), а не JSON null, тем же приёмом, что и task.Issue/chat.Session.
type Autopilot struct {
	ID                  string
	WorkspaceID         string
	Title               string
	Description         *string
	ProjectID           *string
	AssigneeType        string
	AssigneeID          string
	Status              string
	ExecutionMode       string
	IssueTitleTemplate  *string
	CreatedByType       string
	CreatedByID         string
	LastRunAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	TriggerKinds        []string
	NextRunAt           *time.Time
	LastRunStatus       *string
	IsTemplate          bool
	Subscribers         []Subscriber
	CanWrite            *bool `json:"-"`
	CanManageAccess     *bool `json:"-"`
	includeAccessFields bool
}

// WithAccess прикладывает can_write/can_manage_access — оба поля контракт
// требует только "в персонализированном ответе" (списки/getAutopilot), не в
// сырых промежуточных значениях (например перед публикацией realtime-события).
func (a Autopilot) WithAccess(canWrite, canManageAccess bool) Autopilot {
	a.CanWrite = &canWrite
	a.CanManageAccess = &canManageAccess
	a.includeAccessFields = true
	return a
}

func (a Autopilot) MarshalJSON() ([]byte, error) {
	type alias struct {
		ID                 string       `json:"id"`
		WorkspaceID        string       `json:"workspace_id"`
		Title              string       `json:"title"`
		Description        *string      `json:"description"`
		ProjectID          *string      `json:"project_id"`
		AssigneeType       string       `json:"assignee_type"`
		AssigneeID         string       `json:"assignee_id"`
		Status             string       `json:"status"`
		ExecutionMode      string       `json:"execution_mode"`
		IssueTitleTemplate *string      `json:"issue_title_template"`
		CreatedByType      string       `json:"created_by_type"`
		CreatedByID        string       `json:"created_by_id"`
		LastRunAt          *time.Time   `json:"last_run_at"`
		CreatedAt          time.Time    `json:"created_at"`
		UpdatedAt          time.Time    `json:"updated_at"`
		TriggerKinds       []string     `json:"trigger_kinds"`
		NextRunAt          *time.Time   `json:"next_run_at"`
		LastRunStatus      *string      `json:"last_run_status"`
		IsTemplate         bool         `json:"is_template"`
		Subscribers        []Subscriber `json:"subscribers"`
		CanWrite           *bool        `json:"can_write,omitempty"`
		CanManageAccess    *bool        `json:"can_manage_access,omitempty"`
	}
	out := alias{
		ID: a.ID, WorkspaceID: a.WorkspaceID, Title: a.Title, Description: a.Description,
		ProjectID: a.ProjectID, AssigneeType: a.AssigneeType, AssigneeID: a.AssigneeID,
		Status: a.Status, ExecutionMode: a.ExecutionMode, IssueTitleTemplate: a.IssueTitleTemplate,
		CreatedByType: a.CreatedByType, CreatedByID: a.CreatedByID, LastRunAt: a.LastRunAt,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, TriggerKinds: a.TriggerKinds,
		NextRunAt: a.NextRunAt, LastRunStatus: a.LastRunStatus, IsTemplate: a.IsTemplate,
		Subscribers: a.Subscribers,
	}
	if out.TriggerKinds == nil {
		out.TriggerKinds = []string{}
	}
	if out.Subscribers == nil {
		out.Subscribers = []Subscriber{}
	}
	if a.includeAccessFields {
		out.CanWrite, out.CanManageAccess = a.CanWrite, a.CanManageAccess
	}
	return json.Marshal(out)
}

// Subscriber — components/schemas/AutopilotSubscriber (sentinel_subscribers).
type Subscriber struct {
	UserType  string    `json:"user_type"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Trigger — components/schemas/AutopilotTrigger (sentinel_triggers).
// PlainToken/WebhookPath несут секрет вебхука в открытом виде — заполняются
// только сразу после создания/ротации, в Go-коде CreateTrigger/
// RotateWebhookToken, не из БД (T-029 доводка, миграция 400, см.
// server2/docs/decisions.md, раздел «T-029 доводка»): БД хранит только
// strig_webhook_token_digest (sha256 токена, маршрутизация входящего вебхука
// по нему), открытый токен/путь не сохраняется нигде — так что на любом
// последующем чтении (GetTrigger/ListTriggers) оба поля всегда nil/"" и в
// JSON превращаются в null.
type Trigger struct {
	ID                string
	AutopilotID       string
	Kind              string
	Enabled           bool
	CronExpression    *string
	Timezone          *string
	NextRunAt         *time.Time
	WebhookPath       *string
	Provider          *string
	HasSigningSecret  bool
	SigningSecretHint *string
	Label             *string
	LastFiredAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	EventFilters      json.RawMessage

	PlainToken string // непусто только в ответе create/rotate
}

// sensitive управляет тем, показываются ли webhook_token/webhook_path/
// webhook_url в этой сериализации — контракт: видны только владельцу/
// коллаборатору с правом записи, всегда скрыты в широковещательных событиях
// (см. Sanitized ниже).
type triggerJSON struct {
	ID                string          `json:"id"`
	AutopilotID       string          `json:"autopilot_id"`
	Kind              string          `json:"kind"`
	Enabled           bool            `json:"enabled"`
	CronExpression    *string         `json:"cron_expression"`
	Timezone          *string         `json:"timezone"`
	NextRunAt         *time.Time      `json:"next_run_at"`
	WebhookToken      *string         `json:"webhook_token"`
	WebhookPath       *string         `json:"webhook_path"`
	WebhookURL        *string         `json:"webhook_url"`
	Provider          *string         `json:"provider"`
	HasSigningSecret  bool            `json:"has_signing_secret"`
	SigningSecretHint *string         `json:"signing_secret_hint"`
	Label             *string         `json:"label"`
	LastFiredAt       *time.Time      `json:"last_fired_at"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	EventFilters      json.RawMessage `json:"event_filters"`
}

// ToJSON сериализует t для ответа write-access-у: webhook_token/webhook_path/
// webhook_url — только если t.WebhookPath/t.PlainToken заполнены, то есть
// только в самом ответе create/rotate (T-029 доводка, миграция 400: открытый
// токен нигде в БД не хранится, поэтому на последующих чтениях сервер
// физически не может ни то, ни другое восстановить — см.
// server2/docs/decisions.md, раздел «T-029 доводка»; baseURL пуст, если
// сервер не знает публичного адреса — см. internal/config.Config.PublicURL).
func (t Trigger) ToJSON(baseURL string) triggerJSON {
	out := t.toBaseJSON()
	if t.WebhookPath != nil {
		out.WebhookPath = t.WebhookPath
		full := baseURL + "/api/webhooks/autopilots/" + *t.WebhookPath
		out.WebhookURL = &full
	}
	if t.PlainToken != "" {
		tok := t.PlainToken
		out.WebhookToken = &tok
	}
	return out
}

// Sanitized — форма для широковещательных realtime-событий (contract:
// "trigger (без webhook_token/webhook_path/webhook_url)") и для читателей без
// права записи на автопилот.
func (t Trigger) Sanitized() triggerJSON { return t.toBaseJSON() }

func (t Trigger) toBaseJSON() triggerJSON {
	filters := t.EventFilters
	if len(filters) == 0 {
		filters = json.RawMessage(`[]`)
	}
	return triggerJSON{
		ID: t.ID, AutopilotID: t.AutopilotID, Kind: t.Kind, Enabled: t.Enabled,
		CronExpression: t.CronExpression, Timezone: t.Timezone, NextRunAt: t.NextRunAt,
		Provider: t.Provider, HasSigningSecret: t.HasSigningSecret, SigningSecretHint: t.SigningSecretHint,
		Label: t.Label, LastFiredAt: t.LastFiredAt, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		EventFilters: filters,
	}
}

// Collaborator — components/schemas/AutopilotCollaborator (sentinel_collaborators).
type Collaborator struct {
	UserType  string    `json:"user_type"`
	UserID    string    `json:"user_id"`
	GrantedBy string    `json:"granted_by"`
	CreatedAt time.Time `json:"created_at"`
}

// Run — components/schemas/AutopilotRun (sentinel_runs).
type Run struct {
	ID             string
	AutopilotID    string
	TriggerID      *string
	Source         string
	Status         string
	IssueID        *string
	TaskID         *string
	TriggeredAt    time.Time
	CompletedAt    *time.Time
	FailureReason  *string
	ReasonCode     *string
	TriggerPayload json.RawMessage
	Result         json.RawMessage
	CreatedAt      time.Time

	full bool // true — включает trigger_payload/result (getAutopilotRun); false — списковая форма
}

// Full помечает Run для полной сериализации (getAutopilotRun): контракт
// прячет trigger_payload/result из списков.
func (r Run) Full() Run { r.full = true; return r }

type runJSON struct {
	ID             string          `json:"id"`
	AutopilotID    string          `json:"autopilot_id"`
	TriggerID      *string         `json:"trigger_id"`
	Source         string          `json:"source"`
	Status         string          `json:"status"`
	IssueID        *string         `json:"issue_id"`
	TaskID         *string         `json:"task_id"`
	TriggeredAt    time.Time       `json:"triggered_at"`
	CompletedAt    *time.Time      `json:"completed_at"`
	FailureReason  *string         `json:"failure_reason"`
	ReasonCode     *string         `json:"reason_code"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

func (r Run) MarshalJSON() ([]byte, error) {
	out := runJSON{
		ID: r.ID, AutopilotID: r.AutopilotID, TriggerID: r.TriggerID, Source: r.Source, Status: r.Status,
		IssueID: r.IssueID, TaskID: r.TaskID, TriggeredAt: r.TriggeredAt, CompletedAt: r.CompletedAt,
		FailureReason: r.FailureReason, ReasonCode: r.ReasonCode, CreatedAt: r.CreatedAt,
	}
	if r.full {
		out.TriggerPayload, out.Result = r.TriggerPayload, r.Result
	}
	return json.Marshal(out)
}

// Delivery — components/schemas/WebhookDelivery (webhook_events).
type Delivery struct {
	ID                     string
	WorkspaceID            string
	AutopilotID            string
	TriggerID              string
	Provider               string
	Event                  string
	DedupeKey              *string
	DedupeSource           *string
	SignatureStatus        string
	Status                 string
	AttemptCount           int
	DispatchAttempts       int
	AvailableAt            time.Time
	ContentType            *string
	ResponseStatus         *int
	AutopilotRunID         *string
	ReplayedFromDeliveryID *string
	Error                  *string
	ReceivedAt             time.Time
	LastAttemptAt          *time.Time
	CreatedAt              time.Time

	SelectedHeaders map[string]string
	RawBody         *string
	ResponseBody    *string

	full bool
}

// Full — детальная форма (getAutopilotDelivery): включает selected_headers/
// raw_body/response_body, скрытые в listAutopilotDeliveries.
func (d Delivery) Full() Delivery { d.full = true; return d }

type deliveryJSON struct {
	ID                     string            `json:"id"`
	WorkspaceID            string            `json:"workspace_id"`
	AutopilotID            string            `json:"autopilot_id"`
	TriggerID              string            `json:"trigger_id"`
	Provider               string            `json:"provider"`
	Event                  string            `json:"event"`
	DedupeKey              *string           `json:"dedupe_key"`
	DedupeSource           *string           `json:"dedupe_source"`
	SignatureStatus        string            `json:"signature_status"`
	Status                 string            `json:"status"`
	AttemptCount           int               `json:"attempt_count"`
	DispatchAttempts       int               `json:"dispatch_attempts"`
	AvailableAt            time.Time         `json:"available_at"`
	ContentType            *string           `json:"content_type"`
	ResponseStatus         *int              `json:"response_status"`
	AutopilotRunID         *string           `json:"autopilot_run_id"`
	ReplayedFromDeliveryID *string           `json:"replayed_from_delivery_id"`
	Error                  *string           `json:"error"`
	ReceivedAt             time.Time         `json:"received_at"`
	LastAttemptAt          *time.Time        `json:"last_attempt_at"`
	CreatedAt              time.Time         `json:"created_at"`
	SelectedHeaders        map[string]string `json:"selected_headers,omitempty"`
	RawBody                *string           `json:"raw_body,omitempty"`
	ResponseBody           *string           `json:"response_body,omitempty"`
}

func (d Delivery) MarshalJSON() ([]byte, error) {
	out := deliveryJSON{
		ID: d.ID, WorkspaceID: d.WorkspaceID, AutopilotID: d.AutopilotID, TriggerID: d.TriggerID,
		Provider: d.Provider, Event: d.Event, DedupeKey: d.DedupeKey, DedupeSource: d.DedupeSource,
		SignatureStatus: d.SignatureStatus, Status: d.Status, AttemptCount: d.AttemptCount,
		DispatchAttempts: d.DispatchAttempts, AvailableAt: d.AvailableAt, ContentType: d.ContentType,
		ResponseStatus: d.ResponseStatus, AutopilotRunID: d.AutopilotRunID,
		ReplayedFromDeliveryID: d.ReplayedFromDeliveryID, Error: d.Error, ReceivedAt: d.ReceivedAt,
		LastAttemptAt: d.LastAttemptAt, CreatedAt: d.CreatedAt,
	}
	if d.full {
		out.SelectedHeaders, out.RawBody, out.ResponseBody = d.SelectedHeaders, d.RawBody, d.ResponseBody
	}
	return json.Marshal(out)
}

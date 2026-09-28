package feed

import (
	"context"
	"encoding/json"
	"fmt"
)

// kindToGroup — отображение al_kind → группа notification_prefs. Контракт
// (components/schemas/InboxItem.type) перечисляет известные значения, но не
// говорит, к какой из шести групп NotificationPreferencesInput каждое из них
// относится — пробел спецификации, решение зафиксировано здесь и в
// server2/docs/decisions.md. Значение, отсутствующее в карте (новый/будущий
// тип уведомления), по умолчанию попадает в группу "updates" и не
// заглушается, пока предпочтения явно не заведут для него ключ.
var kindToGroup = map[string]string{
	"issue_assigned":           "assignments",
	"unassigned":               "assignments",
	"assignee_changed":         "assignments",
	"status_changed":           "status_changes",
	"priority_changed":         "status_changes",
	"start_date_changed":       "status_changes",
	"due_date_changed":         "status_changes",
	"new_comment":              "comments",
	"mentioned":                "comments",
	"reaction_added":           "updates",
	"task_failed":              "agent_activity",
	"quick_create_done":        "agent_activity",
	"quick_create_failed":      "agent_activity",
	"quick_create_unconfirmed": "agent_activity",
	"issue_subscribed":         "updates",
	"autopilot_paused":         "system_notifications",
}

// defaultSeverity — severity по умолчанию для известных al_kind, когда
// вызывающий домен явно её не задал. Тоже пробел спецификации: контракт
// показывает severity как поле ответа, не как функцию от type.
var defaultSeverity = map[string]string{
	"issue_assigned":           "action_required",
	"unassigned":               "info",
	"assignee_changed":         "info",
	"status_changed":           "info",
	"priority_changed":         "info",
	"start_date_changed":       "info",
	"due_date_changed":         "info",
	"new_comment":              "info",
	"mentioned":                "action_required",
	"reaction_added":           "info",
	"task_failed":              "attention",
	"quick_create_done":        "info",
	"quick_create_failed":      "attention",
	"quick_create_unconfirmed": "action_required",
	"issue_subscribed":         "info",
	"autopilot_paused":         "attention",
}

// GroupFor — группа notification_prefs, отвечающая за al_kind kind (см.
// kindToGroup). Экспортируется, чтобы вызывающий домен мог заранее показать
// пользователю, к какой группе относится его действие, не дублируя карту.
func GroupFor(kind string) string {
	if g, ok := kindToGroup[kind]; ok {
		return g
	}
	return "updates"
}

// NotifyParams — вход Notify.
type NotifyParams struct {
	WorkspaceID   string
	RecipientType string // member|agent
	RecipientID   string
	Kind          string // al_kind — issue_assigned, new_comment, mentioned, ...
	Severity      string // необязательно; по умолчанию — defaultSeverity[Kind] либо "info"
	TicketID      *string
	Title         string
	Body          *string
	ActorType     *string // member|agent|system
	ActorID       *string
	Details       json.RawMessage // произвольные структурированные детали (al_details)
}

// Notify — внутренний API пакета feed для остальных доменов (issues/comments/
// chat/autopilots/...): создаёт одно уведомление в инбоксе получателя.
//
// q принимает и *pgxpool.Pool (Store.Q()), и pgx.Tx — вызывающий домен обычно
// передаёт свою уже открытую транзакцию, чтобы уведомление фиксировалось
// атомарно вместе с основным действием (например: сменили статус тикета и в
// той же транзакции уведомили подписчиков).
//
// Получателям recipient_type=member предпочтения (/api/notification-preferences)
// уважаются: если группа, отвечающая за Kind (см. GroupFor), для получателя
// выставлена в "muted", строка не создаётся вовсе и Notify возвращает
// created=false, err=nil — это не ошибка вызывающего домена, только сигнал
// "уведомление сознательно не показано". Получателям recipient_type=agent
// предпочтения не проверяются (агенты не настраивают
// /api/notification-preferences по контракту — эта настройка принадлежит
// участнику-человеку).
func Notify(ctx context.Context, q Queryer, p NotifyParams) (alert Alert, created bool, err error) {
	if p.WorkspaceID == "" || p.RecipientType == "" || p.RecipientID == "" || p.Kind == "" || p.Title == "" {
		return Alert{}, false, fmt.Errorf("feed.Notify: workspace_id, recipient_type, recipient_id, kind и title обязательны")
	}
	if p.RecipientType == "member" {
		prefs, err := prefsForRecipient(ctx, q, p.WorkspaceID, p.RecipientID)
		if err != nil {
			return Alert{}, false, err
		}
		if prefs[GroupFor(p.Kind)] == "muted" {
			return Alert{}, false, nil
		}
	}
	severity := p.Severity
	if severity == "" {
		severity = defaultSeverity[p.Kind]
	}
	if severity == "" {
		severity = "info"
	}
	details := p.Details
	if len(details) == 0 {
		details = []byte("{}")
	}
	row := q.QueryRow(ctx, `
		INSERT INTO alerts (workspace_id, al_recipient_type, al_recipient_id, al_kind, al_severity, ticket_id,
			al_headline, al_body, al_actor_type, al_actor_id, al_details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+alertColumns,
		p.WorkspaceID, p.RecipientType, p.RecipientID, p.Kind, severity, p.TicketID,
		p.Title, p.Body, p.ActorType, p.ActorID, string(details))
	a, err := scanAlert(row)
	if err != nil {
		return Alert{}, false, fmt.Errorf("feed.Notify: вставка уведомления: %w", err)
	}
	return a, true, nil
}

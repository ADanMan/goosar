package task

import (
	"context"
	"fmt"

	"github.com/adanman/goosar/server2/internal/feed"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// notify.go — T-027 доводка: подключение внутреннего API feed.Notify для
// уведомлений, которые требует контракт при действиях над задачами
// (назначение, смена статуса). Комментарии/упоминания уведомляются из
// пакета note (internal/note/notify.go) — там же, где создаётся комментарий.
//
// Получателям с recipient_type=agent уведомления не создаются: у агентов нет
// /api/notification-preferences и инбокса в смысле контракта — они реагируют
// на постановку в очередь (internal/dispatch), а не на InboxItem; это решение
// зафиксировано в server2/docs/decisions.md (раздел «T-027 доводка»).

// notifyAssigned — issue_assigned: назначенному участнику-человеку, если
// назначение действительно поменялось на этого участника и назначающий — не
// он сам (самоназначение не создаёт уведомление себе же).
func (d *Deps) notifyAssigned(ctx context.Context, ws workspace.Workspace, issue Issue, actorType, actorID string) {
	if issue.AssigneeType == nil || issue.AssigneeID == nil || *issue.AssigneeType != "member" {
		return
	}
	if *issue.AssigneeID == actorID {
		return
	}
	title := fmt.Sprintf("Вам назначена задача «%s»", issue.Title)
	at, aid := actorType, actorID
	if _, _, err := feed.Notify(ctx, d.Store.pool(), feed.NotifyParams{
		WorkspaceID: ws.ID, RecipientType: "member", RecipientID: *issue.AssigneeID,
		Kind: "issue_assigned", TicketID: &issue.ID, Title: title,
		ActorType: &at, ActorID: &aid,
	}); err != nil && d.Logger != nil {
		d.Logger.Warn("task: уведомление о назначении", "err", err, "issue_id", issue.ID)
	}
}

// notifyStatusChanged — status_changed: всем подписчикам-людям задачи, кроме
// того, кто сам инициировал смену статуса.
func (d *Deps) notifyStatusChanged(ctx context.Context, ws workspace.Workspace, before, after Issue, actorType, actorID string) {
	if before.Status == after.Status {
		return
	}
	subs, err := d.Store.ListSubscribers(ctx, ws.ID, after.ID)
	if err != nil {
		if d.Logger != nil {
			d.Logger.Warn("task: список подписчиков для уведомления о статусе", "err", err, "issue_id", after.ID)
		}
		return
	}
	title := fmt.Sprintf("Статус задачи «%s» изменён: %s → %s", after.Title, before.Status, after.Status)
	at, aid := actorType, actorID
	for _, sub := range subs {
		if sub.UserType != "member" || sub.UserID == actorID {
			continue
		}
		if _, _, err := feed.Notify(ctx, d.Store.pool(), feed.NotifyParams{
			WorkspaceID: ws.ID, RecipientType: "member", RecipientID: sub.UserID,
			Kind: "status_changed", TicketID: &after.ID, Title: title,
			ActorType: &at, ActorID: &aid,
		}); err != nil && d.Logger != nil {
			d.Logger.Warn("task: уведомление о смене статуса", "err", err, "issue_id", after.ID, "recipient", sub.UserID)
		}
	}
}

// actorKind — "member"/"agent" для feed.NotifyParams.ActorType/al_actor_type,
// по тому же признаку, что и creatorType в handlers.go.
func actorKind(isHuman bool) string {
	if isHuman {
		return "member"
	}
	return "agent"
}

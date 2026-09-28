package note

import (
	"context"
	"fmt"

	"github.com/adanman/goosar/server2/internal/feed"
)

// notify.go — T-027 доводка: подключение внутреннего API feed.Notify для
// комментариев (контракт требует уведомлять подписчиков задачи о новом
// комментарии и явно упомянутого участника — назначение/смена статуса
// уведомляются из internal/task, см. server2/docs/decisions.md).

// notifySubscribersOfComment — new_comment: всем подписчикам-людям задачи,
// кроме автора комментария.
func (d *Deps) notifySubscribersOfComment(ctx context.Context, ticket TicketInfo, c Comment) {
	rows, err := d.db.Pool.Query(ctx, `
		SELECT tsub_watcher_id FROM ticket_subscribers
		WHERE ticket_id = $1 AND tsub_watcher_type = 'member'`, ticket.ID)
	if err != nil {
		if d.Logger != nil {
			d.Logger.Warn("note: список подписчиков для уведомления о комментарии", "err", err, "issue_id", ticket.ID)
		}
		return
	}
	defer rows.Close()
	var recipients []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if id == c.AuthorID && c.AuthorType == "member" {
			continue
		}
		recipients = append(recipients, id)
	}
	if err := rows.Err(); err != nil {
		return
	}
	title := fmt.Sprintf("Новый комментарий к задаче «%s»", ticket.Headline)
	at, aid := c.AuthorType, c.AuthorID
	for _, recipientID := range recipients {
		if _, _, err := feed.Notify(ctx, d.db.Pool, feed.NotifyParams{
			WorkspaceID: ticket.WorkspaceID, RecipientType: "member", RecipientID: recipientID,
			Kind: "new_comment", TicketID: &ticket.ID, Title: title,
			ActorType: &at, ActorID: &aid,
		}); err != nil && d.Logger != nil {
			d.Logger.Warn("note: уведомление о новом комментарии", "err", err, "comment_id", c.ID, "recipient", recipientID)
		}
	}
}

// notifyMentionedMembers — mentioned: участнику-человеку, явно упомянутому в
// тексте токеном "@member:<uuid>" (тот же однозначный синтаксис, что и
// @agent:/@squad: для запуска агентов, см. internal/note/triggers.go и
// server2/docs/decisions.md — здесь тот же приём распространён на людей,
// раз контракт тоже не задаёт синтаксис человеческого упоминания).
func (d *Deps) notifyMentionedMembers(ctx context.Context, ticket TicketInfo, c Comment) {
	mentions := parseMentions(c.Content)
	if len(mentions) == 0 {
		return
	}
	title := fmt.Sprintf("Вас упомянули в задаче «%s»", ticket.Headline)
	at, aid := c.AuthorType, c.AuthorID
	for _, m := range mentions {
		if m.kind != "member" || m.id == c.AuthorID {
			continue
		}
		if _, _, err := feed.Notify(ctx, d.db.Pool, feed.NotifyParams{
			WorkspaceID: ticket.WorkspaceID, RecipientType: "member", RecipientID: m.id,
			Kind: "mentioned", TicketID: &ticket.ID, Title: title,
			ActorType: &at, ActorID: &aid,
		}); err != nil && d.Logger != nil {
			d.Logger.Warn("note: уведомление об упоминании", "err", err, "comment_id", c.ID, "recipient", m.id)
		}
	}
}

// notifyComment — общая точка вызова из handleCreateComment: /note-комментарии
// (isNoteComment) тоже уведомляют — правило "/note не запускает агентов"
// (contract §1.10) касается только автозапуска, не подписки на задачу.
func (d *Deps) notifyComment(ctx context.Context, ticket TicketInfo, c Comment) {
	d.notifySubscribersOfComment(ctx, ticket, c)
	d.notifyMentionedMembers(ctx, ticket, c)
}

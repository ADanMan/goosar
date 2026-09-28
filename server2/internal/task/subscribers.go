package task

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var validSubscribeReasons = map[string]bool{
	"creator": true, "assignee": true, "commenter": true, "mentioned": true, "manual": true, "autopilot": true,
}

// ListSubscribers — listIssueSubscribers.
func (s *Store) ListSubscribers(ctx context.Context, workspaceID, issueID string) ([]Subscriber, error) {
	// найдём задачу — тот же паттерн 404, что и у остальных issue-scoped ручек.
	if _, err := s.GetIssue(ctx, workspaceID, issueID); err != nil {
		return nil, err
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT ticket_id, tsub_watcher_type, tsub_watcher_id, tsub_reason, created_at
		FROM ticket_subscribers WHERE ticket_id = $1 ORDER BY created_at`, issueID)
	if err != nil {
		return nil, fmt.Errorf("task: список подписчиков: %w", err)
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		var sub Subscriber
		if err := rows.Scan(&sub.IssueID, &sub.UserType, &sub.UserID, &sub.Reason, &sub.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	if out == nil {
		out = []Subscriber{}
	}
	return out, rows.Err()
}

// Subscribe — subscribeToIssue (reason всегда "manual" через API, contract §1.13).
func (s *Store) Subscribe(ctx context.Context, workspaceID, issueID, userType, userID string) error {
	if _, err := s.GetIssue(ctx, workspaceID, issueID); err != nil {
		return err
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO ticket_subscribers (ticket_id, tsub_watcher_type, tsub_watcher_id, tsub_reason)
		VALUES ($1,$2,$3,'manual')
		ON CONFLICT (ticket_id, tsub_watcher_type, tsub_watcher_id) DO NOTHING`, issueID, userType, userID)
	if err != nil {
		return fmt.Errorf("task: подписка на задачу: %w", err)
	}
	return nil
}

// Unsubscribe — unsubscribeFromIssue.
func (s *Store) Unsubscribe(ctx context.Context, workspaceID, issueID, userType, userID string) error {
	if _, err := s.GetIssue(ctx, workspaceID, issueID); err != nil {
		return err
	}
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM ticket_subscribers WHERE ticket_id=$1 AND tsub_watcher_type=$2 AND tsub_watcher_id=$3`,
		issueID, userType, userID)
	if err != nil {
		return fmt.Errorf("task: отписка от задачи: %w", err)
	}
	return nil
}

// EnsureSubscriber — используется при createIssue/updateIssue/комментариях
// для автоматических причин подписки (creator/assignee/...), которые
// контракт устанавливает сам, не через явный вызов клиента.
func ensureSubscriber(ctx context.Context, tx pgx.Tx, issueID, userType, userID, reason string) error {
	if userType == "" || userID == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO ticket_subscribers (ticket_id, tsub_watcher_type, tsub_watcher_id, tsub_reason)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (ticket_id, tsub_watcher_type, tsub_watcher_id) DO NOTHING`, issueID, userType, userID, reason)
	if err != nil {
		return fmt.Errorf("task: автоподписка: %w", err)
	}
	return nil
}

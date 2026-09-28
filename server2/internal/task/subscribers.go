package task

import (
	"context"
	"fmt"
)

// ListSubscribers — listIssueSubscribers.
func (s *Store) ListSubscribers(ctx context.Context, workspaceID, issueID string) ([]Subscriber, error) {
	if _, err := s.GetIssue(ctx, workspaceID, issueID); err != nil {
		return nil, err
	}
	rows, queryErr := s.db.Pool.Query(ctx, `
		SELECT ticket_id, tsub_watcher_type, tsub_watcher_id, tsub_reason, created_at
		FROM ticket_subscribers WHERE ticket_id = $1 ORDER BY created_at`, issueID)
	if queryErr != nil {
		return nil, fmt.Errorf("task: список подписчиков: %w", queryErr)
	}
	defer rows.Close()

	watchers := []Subscriber{}
	for rows.Next() {
		var w Subscriber
		if scanErr := rows.Scan(&w.IssueID, &w.UserType, &w.UserID, &w.Reason, &w.CreatedAt); scanErr != nil {
			return nil, scanErr
		}
		watchers = append(watchers, w)
	}
	return watchers, rows.Err()
}

// setWatcher — общая реализация Subscribe/Unsubscribe: контракт трактует их
// как зеркальные операции над одной и той же строкой ticket_subscribers, с
// той разницей, что подписка идемпотентна по ключу (ON CONFLICT DO NOTHING),
// а отписка удаляет по тому же ключу, чей reason при удалении не важен.
func (s *Store) setWatcher(ctx context.Context, workspaceID, issueID, userType, userID string, subscribe bool) error {
	if _, err := s.GetIssue(ctx, workspaceID, issueID); err != nil {
		return err
	}
	var execErr error
	if subscribe {
		_, execErr = s.db.Pool.Exec(ctx, `
			INSERT INTO ticket_subscribers (ticket_id, tsub_watcher_type, tsub_watcher_id, tsub_reason)
			VALUES ($1, $2, $3, 'manual')
			ON CONFLICT (ticket_id, tsub_watcher_type, tsub_watcher_id) DO NOTHING`, issueID, userType, userID)
	} else {
		_, execErr = s.db.Pool.Exec(ctx, `
			DELETE FROM ticket_subscribers
			WHERE ticket_id = $1 AND tsub_watcher_type = $2 AND tsub_watcher_id = $3`, issueID, userType, userID)
	}
	if execErr != nil {
		return fmt.Errorf("task: изменение подписки на задачу: %w", execErr)
	}
	return nil
}

// Subscribe — subscribeToIssue (reason всегда "manual" через API, contract §1.13).
func (s *Store) Subscribe(ctx context.Context, workspaceID, issueID, userType, userID string) error {
	return s.setWatcher(ctx, workspaceID, issueID, userType, userID, true)
}

// Unsubscribe — unsubscribeFromIssue.
func (s *Store) Unsubscribe(ctx context.Context, workspaceID, issueID, userType, userID string) error {
	return s.setWatcher(ctx, workspaceID, issueID, userType, userID, false)
}

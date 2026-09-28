package task

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Timeline — listIssueTimeline (contract §1.14): объединённая лента
// комментариев (ticket_notes — таблица пакета note, здесь только читается,
// не пишется), системных записей активности (ticket_activity, пишет этот
// пакет) в единую хронологию {type,id,actor_type,actor_id,action,details,created_at}.
func (s *Store) Timeline(ctx context.Context, workspaceID, issueID string) ([]TimelineEntry, error) {
	if _, err := s.GetIssue(ctx, workspaceID, issueID); err != nil {
		return nil, err
	}

	var out []TimelineEntry

	actRows, err := s.db.Pool.Query(ctx, `
		SELECT id, ta_actor_type, ta_actor_id, ta_action, ta_details, created_at
		FROM ticket_activity WHERE ticket_id = $1`, issueID)
	if err != nil {
		return nil, fmt.Errorf("task: лента активности: %w", err)
	}
	for actRows.Next() {
		var e TimelineEntry
		var details []byte
		if err := actRows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &details, &e.CreatedAt); err != nil {
			actRows.Close()
			return nil, err
		}
		e.Type = "activity"
		if len(details) > 0 {
			e.Details = details
		} else {
			e.Details = json.RawMessage(`{}`)
		}
		out = append(out, e)
	}
	actRows.Close()
	if err := actRows.Err(); err != nil {
		return nil, err
	}

	// ticket_notes принадлежит пакету note (комментарии) — читается здесь
	// только для слияния в единую хронологию, не изменяется этим доменом.
	noteRows, err := s.db.Pool.Query(ctx, `
		SELECT id, tn_author_type, tn_author_id, tn_kind, tn_body, created_at
		FROM ticket_notes WHERE ticket_id = $1`, issueID)
	if err != nil {
		// ticket_notes может ещё не существовать содержательно, если пакет
		// note ещё не написал в неё ни строки — это не ошибка сервера,
		// таймлайн просто состоит из одной активности.
		if isUndefinedTable(err) {
			return sortedTimeline(out), nil
		}
		return nil, fmt.Errorf("task: комментарии для ленты активности: %w", err)
	}
	defer noteRows.Close()
	for noteRows.Next() {
		var e TimelineEntry
		var kind, body string
		if err := noteRows.Scan(&e.ID, &e.ActorType, &e.ActorID, &kind, &body, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Type = "comment"
		e.Action = kind
		details, _ := json.Marshal(map[string]any{"body": body})
		e.Details = details
		out = append(out, e)
	}
	if err := noteRows.Err(); err != nil {
		return nil, err
	}

	return sortedTimeline(out), nil
}

func sortedTimeline(entries []TimelineEntry) []TimelineEntry {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].CreatedAt.Before(entries[j].CreatedAt) })
	if entries == nil {
		entries = []TimelineEntry{}
	}
	return entries
}

// isUndefinedTable — ticket_notes принадлежит соседнему пакету note; если по
// какой-то причине миграция, создающая её, ещё не применена в конкретной БД,
// таймлайн деградирует до одной активности вместо 500.
func isUndefinedTable(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "42P01") || strings.Contains(err.Error(), "does not exist"))
}

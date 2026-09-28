package squad

import (
	"context"
	"fmt"
	"time"
)

// MemberStatus — components/schemas/SquadMemberStatus.
type MemberStatus struct {
	MemberType   string             `json:"member_type"`
	MemberID     string             `json:"member_id"`
	Status       *string            `json:"status"`
	ActiveIssues []ActiveIssueBrief `json:"active_issues"`
	LastActiveAt *time.Time         `json:"last_active_at"`
}

// ActiveIssueBrief — components/schemas/SquadActiveIssueBrief.
type ActiveIssueBrief struct {
	IssueID     string `json:"issue_id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	IssueStatus string `json:"issue_status"`
}

// activeDispatchStatuses — те же значения, что dispatch.Status активных
// запусков (queued/dispatched/waiting_local_directory/running/deferred);
// дублирование строкового списка — тот же приём, что internal/task/table.go
// применяет к squad-назначениям, чтобы не заводить обратную зависимость
// squad -> dispatch только ради одного списка констант.
var activeDispatchStatuses = []string{"queued", "dispatched", "waiting_local_directory", "running", "deferred"}

// MemberStatuses — listSquadMemberStatus (contract §6): для агентов —
// working (активный запуск) / idle (runtime online) / unstable (не online,
// но на связи <5 минут назад) / offline; для людей — status=nil.
func (s *Store) MemberStatuses(ctx context.Context, squadID string) ([]MemberStatus, error) {
	members, err := s.ListMembers(ctx, squadID)
	if err != nil {
		return nil, err
	}
	out := make([]MemberStatus, 0, len(members))
	for _, m := range members {
		ms := MemberStatus{MemberType: m.MemberType, MemberID: m.MemberID, ActiveIssues: []ActiveIssueBrief{}}
		if m.MemberType != "agent" {
			out = append(out, ms)
			continue
		}
		if err := s.fillAgentStatus(ctx, &ms); err != nil {
			return nil, err
		}
		out = append(out, ms)
	}
	return out, nil
}

func (s *Store) fillAgentStatus(ctx context.Context, ms *MemberStatus) error {
	var archivedAt *time.Time
	var exStatus string
	var lastSeen *time.Time
	err := s.db.Pool.QueryRow(ctx, `
		SELECT o.op_archived_at, e.ex_status, e.ex_last_seen_at
		FROM operatives o JOIN executors e ON e.id = o.executor_id
		WHERE o.id = $1`, ms.MemberID).Scan(&archivedAt, &exStatus, &lastSeen)
	if err != nil {
		return fmt.Errorf("squad: статус агента-участника: %w", err)
	}
	ms.LastActiveAt = lastSeen
	status := "offline"
	switch {
	case archivedAt != nil:
		status = "archived"
	default:
		hasActive, err := s.db.RowExists(ctx, `SELECT EXISTS(
			SELECT 1 FROM dispatch_jobs WHERE operative_id = $1 AND dj_status = ANY($2))`, ms.MemberID, activeDispatchStatuses)
		if err != nil {
			return fmt.Errorf("squad: активные запуски участника: %w", err)
		}
		switch {
		case hasActive:
			status = "working"
			issues, err := s.activeIssuesFor(ctx, ms.MemberID)
			if err != nil {
				return err
			}
			ms.ActiveIssues = issues
		case exStatus == "online":
			status = "idle"
		case lastSeen != nil && time.Since(*lastSeen) < 5*time.Minute:
			status = "unstable"
		default:
			status = "offline"
		}
	}
	ms.Status = &status
	return nil
}

func (s *Store) activeIssuesFor(ctx context.Context, operativeID string) ([]ActiveIssueBrief, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT t.id, t.tk_display_key, t.tk_headline, t.tk_status
		FROM dispatch_jobs dj JOIN tickets t ON t.id = dj.ticket_id
		WHERE dj.operative_id = $1 AND dj.dj_status = ANY($2) AND dj.ticket_id IS NOT NULL`,
		operativeID, activeDispatchStatuses)
	if err != nil {
		return nil, fmt.Errorf("squad: активные задачи участника: %w", err)
	}
	defer rows.Close()
	out := []ActiveIssueBrief{}
	for rows.Next() {
		var b ActiveIssueBrief
		if err := rows.Scan(&b.IssueID, &b.Identifier, &b.Title, &b.IssueStatus); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

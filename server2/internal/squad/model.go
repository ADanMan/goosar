// Package squad реализует тег Squads контракта (`/api/squads/**`): CRUD
// отряда, участники (люди и агенты), роли, живой статус участников, и
// POST /api/issues/{id}/squad-evaluated (только сам агент-лидер, в рамках
// своего запуска — записывает решение лидера по задаче как запись
// активности задачи). Таблицы — crews/crew_members (004_crews.up.sql).
//
// Права вызова агента-лидера переиспользуют internal/agent.Store.CanInvoke
// (squad импортирует agent, не наоборот) — тот же канонический алгоритм,
// что использует остальной контракт (§1.8/§10.1).
package squad

import "time"

// Squad — строка crews.
type Squad struct {
	ID           string
	WorkspaceID  string
	Title        string
	Summary      string
	Instructions string
	AvatarURI    *string
	LeaderType   string
	LeaderID     string
	CreatorID    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ArchivedAt   *time.Time
	ArchivedBy   *string
}

// MemberPreview — components/schemas/SquadMemberPreview.
type MemberPreview struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Role       string `json:"role"`
}

// Member — components/schemas/SquadMember.
type Member struct {
	ID         string    `json:"id"`
	SquadID    string    `json:"squad_id"`
	MemberType string    `json:"member_type"`
	MemberID   string    `json:"member_id"`
	Role       string    `json:"role"`
	CreatedAt  time.Time `json:"created_at"`
}

// View — components/schemas/Squad (без превью/счётчика — заполняются
// отдельно, см. store.go ListWithPreview).
func (s Squad) View(memberCount int, preview []MemberPreview) map[string]any {
	if preview == nil {
		preview = []MemberPreview{}
	}
	return map[string]any{
		"id":             s.ID,
		"workspace_id":   s.WorkspaceID,
		"name":           s.Title,
		"description":    s.Summary,
		"instructions":   s.Instructions,
		"avatar_url":     s.AvatarURI,
		"leader_id":      s.LeaderID,
		"creator_id":     s.CreatorID,
		"created_at":     s.CreatedAt,
		"updated_at":     s.UpdatedAt,
		"archived_at":    s.ArchivedAt,
		"archived_by":    s.ArchivedBy,
		"member_count":   memberCount,
		"member_preview": preview,
	}
}

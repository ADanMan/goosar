// Package skill реализует тег Skills контракта (`/api/skills/**`): CRUD
// навыков и их файлов, поиск во внешнем каталоге, импорт (ссылка/архив),
// метки. Таблицы — capabilities/capability_files (003_agents.up.sql) плюс
// общие tags/capability_tag_links (005_tasks.up.sql).
//
// ResolveBundles (bundle.go) — экспортированная функция для домена daemon
// (T-028, сосед): собирает содержимое навыков (SKILL.md + файлы) с хэшем и
// суммарным размером в форме, готовой передать демону/CLI. См.
// server2/docs/decisions.md, раздел T-028.
package skill

import (
	"encoding/json"
	"time"
)

// Skill — строка capabilities (без файлов).
type Skill struct {
	ID          string
	WorkspaceID string
	Name        string
	Description string
	Config      json.RawMessage
	Content     string
	CreatedBy   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// File — строка capability_files.
type File struct {
	ID        string    `json:"id"`
	SkillID   string    `json:"skill_id"`
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReservedFilePath — путь, зарезервированный под основное содержимое навыка;
// не может быть создан/заменён через /api/skills/{id}/files.
const ReservedFilePath = "SKILL.md"

// Summary — components/schemas/SkillSummary (без content; enabled
// заполняется только в контексте конкретного агента — здесь всегда false,
// вызывающий код агента подставляет значение из operative_capabilities).
func (s Skill) Summary() map[string]any {
	return map[string]any{
		"id":           s.ID,
		"workspace_id": s.WorkspaceID,
		"name":         s.Name,
		"description":  s.Description,
		"config":       configOrEmpty(s.Config),
		"created_by":   s.CreatedBy,
		"created_at":   s.CreatedAt,
		"updated_at":   s.UpdatedAt,
		"enabled":      false,
	}
}

// Full — components/schemas/Skill (Summary + content).
func (s Skill) Full() map[string]any {
	out := s.Summary()
	out["content"] = s.Content
	return out
}

// WithFiles — components/schemas/SkillWithFiles (Full + files).
func (s Skill) WithFiles(files []File) map[string]any {
	out := s.Full()
	if files == nil {
		files = []File{}
	}
	out["files"] = files
	return out
}

func configOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

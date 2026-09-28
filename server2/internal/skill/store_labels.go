package skill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrLabelNotFound — метка типа skill не существует в воркспейсе (404).
var ErrLabelNotFound = errors.New("skill: метка не найдена")

// Label — components/schemas/SkillLabel (resource_type=skill), тот же приём,
// что internal/agent.Label — собственная копия формы поверх общей таблицы
// tags/capability_tag_links, без CRUD-владения определениями меток.
type Label struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	ResourceType string    `json:"resource_type"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color"`
	UsageCount   int       `json:"usage_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ResourceLabelsEnabled — тот же флаг, что internal/tagging/internal/agent
// (contract §3): spaces.ws_settings->>'resource_labels_enabled'.
func (s *Store) ResourceLabelsEnabled(ctx context.Context, workspaceID string) (bool, error) {
	var enabled bool
	err := s.db.Pool.QueryRow(ctx, `SELECT COALESCE((ws_settings->>'resource_labels_enabled')::boolean, false)
		FROM spaces WHERE id = $1`, workspaceID).Scan(&enabled)
	if err != nil {
		return false, fmt.Errorf("skill: проверка флага ресурсных меток: %w", err)
	}
	return enabled, nil
}

const skillLabelColumns = `id, workspace_id, tag_resource_type, tag_label, tag_summary, tag_color, tag_usage_count, created_at, updated_at`

// ListLabels — метки, привязанные к навыку.
func (s *Store) ListLabels(ctx context.Context, skillID string) ([]Label, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+skillLabelColumns+` FROM tags t JOIN capability_tag_links l ON l.tag_id = t.id
		WHERE l.capability_id = $1 ORDER BY t.tag_label ASC`, skillID)
	if err != nil {
		return nil, fmt.Errorf("skill: метки навыка: %w", err)
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		var l Label
		var desc *string
		if err := rows.Scan(&l.ID, &l.WorkspaceID, &l.ResourceType, &l.Name, &desc, &l.Color, &l.UsageCount,
			&l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		if desc != nil {
			l.Description = *desc
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AttachLabel — прикрепить метку типа skill к навыку.
func (s *Store) AttachLabel(ctx context.Context, workspaceID, skillID, labelID string) ([]Label, error) {
	exists, err := s.db.RowExists(ctx, `SELECT EXISTS(
		SELECT 1 FROM tags WHERE id = $1 AND workspace_id = $2 AND tag_resource_type = 'skill')`, labelID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("skill: проверка метки: %w", err)
	}
	if !exists {
		return nil, ErrLabelNotFound
	}
	if _, err := s.db.Pool.Exec(ctx, `INSERT INTO capability_tag_links (capability_id, tag_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING`, skillID, labelID); err != nil {
		return nil, fmt.Errorf("skill: привязка метки: %w", err)
	}
	return s.ListLabels(ctx, skillID)
}

// DetachLabel — открепить.
func (s *Store) DetachLabel(ctx context.Context, skillID, labelID string) ([]Label, error) {
	if _, err := s.db.Pool.Exec(ctx, `DELETE FROM capability_tag_links WHERE capability_id = $1 AND tag_id = $2`,
		skillID, labelID); err != nil {
		return nil, fmt.Errorf("skill: отвязка метки: %w", err)
	}
	return s.ListLabels(ctx, skillID)
}

// --- поиск во внешнем каталоге ------------------------------------------------

// SearchCandidate — components/schemas/SkillSearchCandidate.
type SearchCandidate struct {
	Name         string  `json:"name"`
	URL          string  `json:"url"`
	Source       string  `json:"source"`
	Repo         *string `json:"repo"`
	InstallCount *int    `json:"install_count"`
	GithubStars  *int    `json:"github_stars"`
	Description  string  `json:"description"`
}

// SearchDisabled — "Источник поиска можно отключить на уровне деплоя"
// (contract §3). Ни contract, ни data-model не заводят структурированную
// колонку под это конкретное переключение — решение этой сессии: необяза-
// тельная строка platform_policy (011_governance.up.sql, id=1),
// pp_body->>'skills_search_disabled' (см. server2/docs/decisions.md).
func (s *Store) SearchDisabled(ctx context.Context) (bool, error) {
	var disabled bool
	err := s.db.Pool.QueryRow(ctx, `SELECT COALESCE((pp_body->>'skills_search_disabled')::boolean, false)
		FROM platform_policy WHERE id = 1`).Scan(&disabled)
	if err != nil {
		// Ни таблицы (деплой без governance-миграций), ни строки-документа
		// (011_governance.up.sql создаёт таблицу, но не сажает строку id=1 —
		// это делает домен deployment/T-029) — оба случая означают "флаг не
		// установлен", не ошибку сервера.
		if isUndefinedTable(err) || errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("skill: проверка флага поиска: %w", err)
	}
	return disabled, nil
}

// Search — GET /api/skills/search: проксирует запрос во внешний публичный
// каталог (домен clawhub.ai). Контракт не документирует форму этого
// внешнего API (только форму собственного ответа, SkillSearchCandidate) —
// эта версия не реализует реальный сетевой поиск (решение этой сессии,
// server2/docs/decisions.md, раздел T-028): SearchDisabled==false всегда
// заканчивается ErrUpstreamUnavailable (502, документированный контрактом
// ответ для "внешний каталог недоступен"), а не угадыванием чужого API.
func Search(ctx context.Context, query string) ([]SearchCandidate, error) {
	return nil, ErrUpstreamUnavailable
}

func isUndefinedTable(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "42P01") || strings.Contains(err.Error(), "does not exist"))
}

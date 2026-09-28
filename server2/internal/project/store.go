package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

var ErrNotFound = errors.New("project: не найдено")
var ErrResourceConflict = errors.New("project: ресурс уже привязан или занят демон")

// Store — доступ к initiatives/initiative_resources (005_tasks.up.sql).
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// Project — форма components/schemas/Project.
type Project struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspace_id"`
	Title         string     `json:"title"`
	Description   *string    `json:"description"`
	Icon          *string    `json:"icon"`
	Status        string     `json:"status"`
	Priority      string     `json:"priority"`
	LeadType      *string    `json:"lead_type"`
	LeadID        *string    `json:"lead_id"`
	StartDate     *time.Time `json:"start_date"`
	DueDate       *time.Time `json:"due_date"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	IssueCount    int        `json:"issue_count"`
	DoneCount     int        `json:"done_count"`
	ResourceCount int        `json:"resource_count"`
}

const projectColumns = `p.id, p.workspace_id, p.init_title, p.init_summary, p.init_icon, p.init_status,
	p.init_priority, p.init_lead_type, p.init_lead_id, p.init_start_date, p.init_due_date, p.created_at, p.updated_at,
	(SELECT count(*) FROM tickets t WHERE t.initiative_id = p.id) AS issue_count,
	(SELECT count(*) FROM tickets t WHERE t.initiative_id = p.id AND t.tk_status = 'done') AS done_count,
	(SELECT count(*) FROM initiative_resources ir WHERE ir.initiative_id = p.id) AS resource_count`

const projectFrom = `FROM initiatives p`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	if err := row.Scan(&p.ID, &p.WorkspaceID, &p.Title, &p.Description, &p.Icon, &p.Status, &p.Priority,
		&p.LeadType, &p.LeadID, &p.StartDate, &p.DueDate, &p.CreatedAt, &p.UpdatedAt,
		&p.IssueCount, &p.DoneCount, &p.ResourceCount); err != nil {
		return Project{}, err
	}
	return p, nil
}

// CreateParams — вход CreateProject.
type CreateParams struct {
	WorkspaceID string
	Title       string
	Description *string
	Icon        *string
	Status      string
	Priority    string
	LeadType    *string
	LeadID      *string
	StartDate   *time.Time
	DueDate     *time.Time
	Resources   []CreateResourceParams
}

// CreateProject создаёt проект и (опционально) сразу привязанные ресурсы,
// одной транзакцией.
func (s *Store) CreateProject(ctx context.Context, p CreateParams) (Project, []Resource, error) {
	var out Project
	var resources []Resource
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO initiatives (workspace_id, init_title, init_summary, init_icon, init_status,
				init_priority, init_lead_type, init_lead_id, init_start_date, init_due_date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id, workspace_id, init_title, init_summary, init_icon, init_status, init_priority,
				init_lead_type, init_lead_id, init_start_date, init_due_date, created_at, updated_at`,
			p.WorkspaceID, p.Title, p.Description, p.Icon, p.Status, p.Priority,
			p.LeadType, p.LeadID, p.StartDate, p.DueDate)
		var id string
		if err := row.Scan(&id, &out.WorkspaceID, &out.Title, &out.Description, &out.Icon, &out.Status,
			&out.Priority, &out.LeadType, &out.LeadID, &out.StartDate, &out.DueDate, &out.CreatedAt, &out.UpdatedAt); err != nil {
			return err
		}
		out.ID = id
		for i, rp := range p.Resources {
			rp.Position = i
			r, err := s.insertResourceTx(ctx, tx, p.WorkspaceID, id, rp)
			if err != nil {
				return err
			}
			resources = append(resources, r)
		}
		return nil
	})
	if err != nil {
		return Project{}, nil, err
	}
	return s.GetProject(ctx, p.WorkspaceID, out.ID)
}

// GetProject — по id, в границах воркспейса.
func (s *Store) GetProject(ctx context.Context, workspaceID, id string) (Project, []Resource, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+projectColumns+` `+projectFrom+` WHERE p.workspace_id = $1 AND p.id = $2`, workspaceID, id)
	proj, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, nil, ErrNotFound
	}
	if err != nil {
		return Project{}, nil, fmt.Errorf("project: получение проекта: %w", err)
	}
	res, err := s.ListResources(ctx, workspaceID, id)
	if err != nil {
		return Project{}, nil, err
	}
	return proj, res, nil
}

// ListFilter — параметры listProjects.
type ListFilter struct {
	Status   string
	Priority string
}

// ListProjects — проекты воркспейса, отфильтрованные по статусу/приоритету.
func (s *Store) ListProjects(ctx context.Context, workspaceID string, f ListFilter) ([]Project, error) {
	q := `SELECT ` + projectColumns + ` ` + projectFrom + ` WHERE p.workspace_id = $1`
	args := []any{workspaceID}
	if f.Status != "" {
		args = append(args, f.Status)
		q += fmt.Sprintf(" AND p.init_status = $%d", len(args))
	}
	if f.Priority != "" {
		args = append(args, f.Priority)
		q += fmt.Sprintf(" AND p.init_priority = $%d", len(args))
	}
	q += " ORDER BY p.created_at DESC"
	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("project: список проектов: %w", err)
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SearchResult — один элемент SearchProjectsResponse.projects.
type SearchResult struct {
	Project
	MatchSource    string  `json:"match_source"`
	MatchedSnippet *string `json:"matched_snippet"`
}

// SearchProjects — полнотекстовый (ILIKE) поиск по названию/описанию.
func (s *Store) SearchProjects(ctx context.Context, workspaceID, q string, limit, offset int, includeClosed bool) ([]SearchResult, int, error) {
	like := "%" + escapeLike(q) + "%"
	closedClause := ""
	if !includeClosed {
		closedClause = " AND p.init_status NOT IN ('completed', 'cancelled')"
	}
	countRow := s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM initiatives p
		WHERE p.workspace_id = $1 AND (p.init_title ILIKE $2 OR p.init_summary ILIKE $2)`+closedClause,
		workspaceID, like)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("project: подсчёт результатов поиска: %w", err)
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+projectColumns+`, (p.init_title ILIKE $2) AS title_match
		`+projectFrom+`
		WHERE p.workspace_id = $1 AND (p.init_title ILIKE $2 OR p.init_summary ILIKE $2)`+closedClause+`
		ORDER BY title_match DESC, p.created_at DESC
		LIMIT $3 OFFSET $4`,
		workspaceID, like, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("project: поиск проектов: %w", err)
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var sr SearchResult
		var titleMatch bool
		if err := rows.Scan(&sr.ID, &sr.WorkspaceID, &sr.Title, &sr.Description, &sr.Icon, &sr.Status, &sr.Priority,
			&sr.LeadType, &sr.LeadID, &sr.StartDate, &sr.DueDate, &sr.CreatedAt, &sr.UpdatedAt,
			&sr.IssueCount, &sr.DoneCount, &sr.ResourceCount, &titleMatch); err != nil {
			return nil, 0, err
		}
		if titleMatch {
			sr.MatchSource = "title"
		} else {
			sr.MatchSource = "description"
			sr.MatchedSnippet = sr.Description
		}
		out = append(out, sr)
	}
	return out, total, rows.Err()
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// UpdatePatch — частичный патч UpdateProjectRequest.
type UpdatePatch struct {
	Title       *string
	Description *string
	HasDesc     bool
	Icon        *string
	HasIcon     bool
	Status      *string
	Priority    *string
	LeadType    *string
	HasLead     bool
	LeadID      *string
	StartDate   *time.Time
	HasStart    bool
	DueDate     *time.Time
	HasDue      bool
}

func (s *Store) UpdateProject(ctx context.Context, workspaceID, id string, p UpdatePatch) (Project, []Resource, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE initiatives SET
			init_title = COALESCE($3, init_title),
			init_summary = CASE WHEN $4 THEN $5 ELSE init_summary END,
			init_icon = CASE WHEN $6 THEN $7 ELSE init_icon END,
			init_status = COALESCE($8, init_status),
			init_priority = COALESCE($9, init_priority),
			init_lead_type = CASE WHEN $10 THEN $11 ELSE init_lead_type END,
			init_lead_id = CASE WHEN $10 THEN $12 ELSE init_lead_id END,
			init_start_date = CASE WHEN $13 THEN $14 ELSE init_start_date END,
			init_due_date = CASE WHEN $15 THEN $16 ELSE init_due_date END,
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING id`,
		workspaceID, id, p.Title, p.HasDesc, p.Description, p.HasIcon, p.Icon, p.Status, p.Priority,
		p.HasLead, p.LeadType, p.LeadID, p.HasStart, p.StartDate, p.HasDue, p.DueDate)
	var gotID string
	if err := row.Scan(&gotID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, nil, ErrNotFound
		}
		return Project{}, nil, fmt.Errorf("project: обновление проекта: %w", err)
	}
	return s.GetProject(ctx, workspaceID, id)
}

// DeleteProject удаляет проект. Отвязка чат-сессий/тикетов от проекта
// происходит автоматически через ON DELETE SET NULL (convos.initiative_id,
// tickets.initiative_id — см. 007_chat.up.sql, 005_tasks.up.sql), поэтому
// хендлер не должен делать это отдельным запросом.
func (s *Store) DeleteProject(ctx context.Context, workspaceID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM initiatives WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("project: удаление проекта: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- resources -----------------------------------------------------------------

// Resource — форма components/schemas/ProjectResource.
type Resource struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"project_id"`
	WorkspaceID string          `json:"workspace_id"`
	Type        string          `json:"resource_type"`
	Ref         json.RawMessage `json:"resource_ref"`
	Label       *string         `json:"label"`
	Position    int             `json:"position"`
	CreatedAt   time.Time       `json:"created_at"`
	CreatedBy   *string         `json:"created_by"`
}

const resourceColumns = `id, initiative_id, workspace_id, ir_resource_type, ir_resource_ref, ir_label, ir_position, created_at, ir_created_by`

func scanResource(row pgx.Row) (Resource, error) {
	var r Resource
	var ref []byte
	if err := row.Scan(&r.ID, &r.ProjectID, &r.WorkspaceID, &r.Type, &ref, &r.Label, &r.Position, &r.CreatedAt, &r.CreatedBy); err != nil {
		return Resource{}, err
	}
	if len(ref) == 0 {
		ref = []byte("{}")
	}
	r.Ref = ref
	return r, nil
}

func (s *Store) ListResources(ctx context.Context, workspaceID, projectID string) ([]Resource, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+resourceColumns+` FROM initiative_resources
		WHERE workspace_id = $1 AND initiative_id = $2
		ORDER BY ir_position, created_at`, workspaceID, projectID)
	if err != nil {
		return nil, fmt.Errorf("project: список ресурсов: %w", err)
	}
	defer rows.Close()
	var out []Resource
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateResourceParams — вход CreateProjectResource / элемент CreateProjectRequest.Resources.
type CreateResourceParams struct {
	Type      string
	Ref       json.RawMessage
	Label     *string
	Position  int
	CreatedBy *string
}

// refDaemonID/refURL — вытаскивают поля из resource_ref для проверки дублей.
func refDaemonID(ref json.RawMessage) string {
	var v struct {
		DaemonID string `json:"daemon_id"`
	}
	_ = json.Unmarshal(ref, &v)
	return v.DaemonID
}

func normalizeGithubRef(ref json.RawMessage) (json.RawMessage, string, error) {
	var v map[string]any
	if err := json.Unmarshal(ref, &v); err != nil {
		return nil, "", fmt.Errorf("project: некорректный resource_ref: %w", err)
	}
	url, _ := v["url"].(string)
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	v["url"] = url
	out, err := json.Marshal(v)
	if err != nil {
		return nil, "", err
	}
	return out, strings.ToLower(url), nil
}

// CreateResource привязывает один ресурс к проекту, с проверкой дублей
// (github_repo — по нормализованному url в рамках проекта; local_directory —
// не более одного на daemon_id в рамках проекта, см. описание createProject
// в контракте).
func (s *Store) CreateResource(ctx context.Context, workspaceID, projectID string, p CreateResourceParams) (Resource, error) {
	var r Resource
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		r, err = s.insertResourceTx(ctx, tx, workspaceID, projectID, p)
		return err
	})
	return r, err
}

func (s *Store) insertResourceTx(ctx context.Context, tx pgx.Tx, workspaceID, projectID string, p CreateResourceParams) (Resource, error) {
	ref := p.Ref
	if len(ref) == 0 {
		ref = []byte("{}")
	}
	switch p.Type {
	case "github_repo":
		normalized, key, err := normalizeGithubRef(ref)
		if err != nil {
			return Resource{}, err
		}
		ref = normalized
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM initiative_resources
				WHERE initiative_id = $1 AND ir_resource_type = 'github_repo' AND lower(ir_resource_ref->>'url') = $2)`,
			projectID, key).Scan(&exists); err != nil {
			return Resource{}, fmt.Errorf("project: проверка дубля репозитория: %w", err)
		}
		if exists {
			return Resource{}, ErrResourceConflict
		}
	case "local_directory":
		daemonID := refDaemonID(ref)
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM initiative_resources
				WHERE initiative_id = $1 AND ir_resource_type = 'local_directory' AND ir_resource_ref->>'daemon_id' = $2)`,
			projectID, daemonID).Scan(&exists); err != nil {
			return Resource{}, fmt.Errorf("project: проверка дубля local_directory: %w", err)
		}
		if exists {
			return Resource{}, ErrResourceConflict
		}
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO initiative_resources (initiative_id, workspace_id, ir_resource_type, ir_resource_ref, ir_label, ir_position, ir_created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+resourceColumns,
		projectID, workspaceID, p.Type, string(ref), p.Label, p.Position, p.CreatedBy)
	return scanResource(row)
}

// UpdateResourcePatch — частичный патч UpdateProjectResourceRequest.
type UpdateResourcePatch struct {
	Ref      json.RawMessage
	Label    *string
	HasLabel bool
	Position *int
}

func (s *Store) UpdateResource(ctx context.Context, workspaceID, projectID, id string, p UpdateResourcePatch) (Resource, error) {
	var out Resource
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		existing, err := s.getResourceTx(ctx, tx, workspaceID, projectID, id)
		if err != nil {
			return err
		}
		ref := p.Ref
		if len(ref) == 0 {
			ref = existing.Ref
		}
		switch existing.Type {
		case "github_repo":
			normalized, key, err := normalizeGithubRef(ref)
			if err != nil {
				return err
			}
			ref = normalized
			var exists bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM initiative_resources
					WHERE initiative_id = $1 AND id <> $2 AND ir_resource_type = 'github_repo' AND lower(ir_resource_ref->>'url') = $3)`,
				projectID, id, key).Scan(&exists); err != nil {
				return fmt.Errorf("project: проверка дубля репозитория: %w", err)
			}
			if exists {
				return ErrResourceConflict
			}
		case "local_directory":
			daemonID := refDaemonID(ref)
			var exists bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM initiative_resources
					WHERE initiative_id = $1 AND id <> $2 AND ir_resource_type = 'local_directory' AND ir_resource_ref->>'daemon_id' = $3)`,
				projectID, id, daemonID).Scan(&exists); err != nil {
				return fmt.Errorf("project: проверка дубля local_directory: %w", err)
			}
			if exists {
				return ErrResourceConflict
			}
		}
		row := tx.QueryRow(ctx, `
			UPDATE initiative_resources SET
				ir_resource_ref = COALESCE($4, ir_resource_ref),
				ir_label = CASE WHEN $5 THEN $6 ELSE ir_label END,
				ir_position = COALESCE($7, ir_position)
			WHERE workspace_id = $1 AND initiative_id = $2 AND id = $3
			RETURNING `+resourceColumns,
			workspaceID, projectID, id, nullableJSON(ref), p.HasLabel, p.Label, p.Position)
		out, err = scanResource(row)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Resource{}, ErrNotFound
	}
	return out, err
}

func (s *Store) getResourceTx(ctx context.Context, tx pgx.Tx, workspaceID, projectID, id string) (Resource, error) {
	row := tx.QueryRow(ctx, `SELECT `+resourceColumns+` FROM initiative_resources WHERE workspace_id = $1 AND initiative_id = $2 AND id = $3`,
		workspaceID, projectID, id)
	r, err := scanResource(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Resource{}, ErrNotFound
	}
	return r, err
}

func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func (s *Store) DeleteResource(ctx context.Context, workspaceID, projectID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM initiative_resources WHERE workspace_id = $1 AND initiative_id = $2 AND id = $3`,
		workspaceID, projectID, id)
	if err != nil {
		return fmt.Errorf("project: удаление ресурса: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

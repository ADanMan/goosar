package tagging

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

// randomID генерирует UUID v4 без внешней зависимости (contract:
// PropertyOption.id — "генерируется автоматически, если не передан").
func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

var (
	ErrNotFound     = errors.New("tagging: не найдено")
	ErrNameTaken    = errors.New("tagging: имя уже занято")
	ErrOptionsInUse = errors.New("tagging: вариант свойства ещё используется в задачах")
)

// resourceLabelsSettingsKey — пробел спецификации: контракт описывает флаг
// «включены ли ресурсные метки» (resource_type=agent/skill) на уровне
// воркспейса, но 51-data-model.md не заводит для него отдельную колонку —
// решение записано в server2/docs/decisions.md: читается из свободного
// ws_settings (jsonb) спейса, ключ ниже; контракт не даёт отдельного
// маршрута, которым его можно было бы включить, так что сейчас это
// фактически "всегда выключено", пока кто-то не проставит его в БД вручную.
const resourceLabelsSettingsKey = "resource_labels_enabled"

// Label — форма components/schemas/Label.
type Label struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	ResourceType string    `json:"resource_type"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Color        string    `json:"color"`
	UsageCount   int       `json:"usage_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const labelColumns = `id, workspace_id, tag_resource_type, tag_label, tag_summary, tag_color,
	tag_usage_count, created_at, updated_at`

func scanLabel(row pgx.Row) (Label, error) {
	var l Label
	var summary *string
	if err := row.Scan(&l.ID, &l.WorkspaceID, &l.ResourceType, &l.Name, &summary, &l.Color,
		&l.UsageCount, &l.CreatedAt, &l.UpdatedAt); err != nil {
		return Label{}, err
	}
	if summary != nil {
		l.Description = *summary
	}
	return l, nil
}

type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// ResourceLabelsEnabled — см. resourceLabelsSettingsKey.
func (s *Store) ResourceLabelsEnabled(ctx context.Context, workspaceID string) (bool, error) {
	var enabled bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE((ws_settings->>$2)::boolean, false) FROM spaces WHERE id = $1`,
		workspaceID, resourceLabelsSettingsKey).Scan(&enabled)
	if err != nil {
		return false, fmt.Errorf("tagging: чтение флага ресурсных меток: %w", err)
	}
	return enabled, nil
}

func normalizeColor(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	if c != "" && !strings.HasPrefix(c, "#") {
		c = "#" + c
	}
	return c
}

// hexColorRe — контракт: "6-значный HEX, с # или без".
var hexColorRe = regexp.MustCompile(`^#?[0-9a-f]{6}$`)

// --- labels ------------------------------------------------------------------

type CreateLabelParams struct {
	WorkspaceID  string
	ResourceType string
	Name         string
	Description  string
	Color        string
}

func (s *Store) CreateLabel(ctx context.Context, p CreateLabelParams) (Label, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO tags (workspace_id, tag_resource_type, tag_label, tag_summary, tag_color)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		RETURNING `+labelColumns,
		p.WorkspaceID, p.ResourceType, p.Name, p.Description, p.Color)
	l, err := scanLabel(row)
	if store.IsUniqueViolation(err) {
		return Label{}, ErrNameTaken
	}
	if err != nil {
		return Label{}, fmt.Errorf("tagging: создание метки: %w", err)
	}
	return l, nil
}

func (s *Store) ListLabels(ctx context.Context, workspaceID, resourceType string) ([]Label, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+labelColumns+` FROM tags WHERE workspace_id = $1 AND tag_resource_type = $2
		ORDER BY tag_label`, workspaceID, resourceType)
	if err != nil {
		return nil, fmt.Errorf("tagging: список меток: %w", err)
	}
	defer rows.Close()
	var out []Label
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) GetLabel(ctx context.Context, workspaceID, id string) (Label, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+labelColumns+` FROM tags WHERE workspace_id = $1 AND id = $2`,
		workspaceID, id)
	l, err := scanLabel(row)
	if store.IsNoRows(err) {
		return Label{}, ErrNotFound
	}
	if err != nil {
		return Label{}, fmt.Errorf("tagging: получение метки: %w", err)
	}
	return l, nil
}

type UpdateLabelParams struct {
	Name        *string
	Description *string
	HasDesc     bool
	Color       *string
}

func (s *Store) UpdateLabel(ctx context.Context, workspaceID, id string, p UpdateLabelParams) (Label, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE tags SET
			tag_label = COALESCE($3, tag_label),
			tag_summary = CASE WHEN $4 THEN NULLIF($5, '') ELSE tag_summary END,
			tag_color = COALESCE($6, tag_color),
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING `+labelColumns,
		workspaceID, id, p.Name, p.HasDesc, p.Description, p.Color)
	l, err := scanLabel(row)
	if store.IsUniqueViolation(err) {
		return Label{}, ErrNameTaken
	}
	if store.IsNoRows(err) {
		return Label{}, ErrNotFound
	}
	if err != nil {
		return Label{}, fmt.Errorf("tagging: обновление метки: %w", err)
	}
	return l, nil
}

func (s *Store) DeleteLabel(ctx context.Context, workspaceID, id string) error {
	// ticket_tag_links/operative_tag_links/capability_tag_links все ссылаются
	// на tags(id) ON DELETE CASCADE — удаление строки tags одной командой уже
	// атомарно снимает метку со всех задач/агентов/навыков (контракт §3).
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM tags WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("tagging: удаление метки: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- issue <-> label -----------------------------------------------------------

// ListIssueLabels — метки, прикреплённые к задаче.
func (s *Store) ListIssueLabels(ctx context.Context, workspaceID, issueID string) ([]Label, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+qualify(labelColumns, "t")+`
		FROM ticket_tag_links l JOIN tags t ON t.id = l.tag_id
		WHERE t.workspace_id = $1 AND l.ticket_id = $2
		ORDER BY t.tag_label`, workspaceID, issueID)
	if err != nil {
		return nil, fmt.Errorf("tagging: метки задачи: %w", err)
	}
	defer rows.Close()
	var out []Label
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AttachIssueLabel прикрепляет метку типа issue к задаче и увеличивает
// tag_usage_count — идемпотентно (повторное прикрепление не даёт ошибку и не
// удваивает счётчик).
func (s *Store) AttachIssueLabel(ctx context.Context, workspaceID, issueID, labelID string) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tags
			WHERE id = $1 AND workspace_id = $2 AND tag_resource_type = 'issue')`, labelID, workspaceID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		tag, err := tx.Exec(ctx, `INSERT INTO ticket_tag_links (ticket_id, tag_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, issueID, labelID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			_, err = tx.Exec(ctx, `UPDATE tags SET tag_usage_count = tag_usage_count + 1 WHERE id = $1`, labelID)
		}
		return err
	})
}

func (s *Store) DetachIssueLabel(ctx context.Context, workspaceID, issueID, labelID string) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			DELETE FROM ticket_tag_links USING tags
			WHERE ticket_tag_links.tag_id = tags.id
			  AND tags.workspace_id = $1 AND ticket_tag_links.ticket_id = $2 AND ticket_tag_links.tag_id = $3`,
			workspaceID, issueID, labelID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			_, err = tx.Exec(ctx, `UPDATE tags SET tag_usage_count = GREATEST(tag_usage_count - 1, 0) WHERE id = $1`, labelID)
		}
		return err
	})
}

func qualify(columns, alias string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// --- properties ----------------------------------------------------------------

// reservedPropertyNames — контракт §4: имена, зарезервированные встроенными
// полями задачи.
var reservedPropertyNames = map[string]bool{
	"status": true, "priority": true, "assignee": true, "project": true, "parent": true,
	"stage": true, "label": true, "labels": true, "start_date": true, "due_date": true,
	"title": true, "description": true, "creator": true, "created_at": true, "updated_at": true,
	"metadata": true, "properties": true,
}

// maxActiveProperties — контракт §4: до 20 неархивированных определений
// одновременно.
const maxActiveProperties = 20

// propertyAdvisoryLockClass — произвольный, но стабильный класс advisory-лока
// (см. contract: "защищено advisory-локом БД, чтобы конкурентные запросы не
// пробили лимит"); значение — не секрет, только различает домены, если
// кто-то ещё когда-нибудь возьмёт advisory-локи в этой БД.
const propertyAdvisoryLockClass = 270_027

type PropertyOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type PropertyConfig struct {
	Options []PropertyOption `json:"options,omitempty"`
}

type Property struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	Icon        string         `json:"icon,omitempty"`
	Config      PropertyConfig `json:"config"`
	Position    float64        `json:"position"`
	Archived    bool           `json:"archived"`
	ArchivedAt  *time.Time     `json:"archived_at"`
	UsageCount  int            `json:"usage_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

const propertyColumns = `id, workspace_id, fd_title, fd_type, fd_summary, fd_icon, fd_config,
	fd_position, fd_archived_at, fd_usage_count, created_at, updated_at`

func scanProperty(row pgx.Row) (Property, error) {
	var p Property
	var summary, icon *string
	var cfg []byte
	if err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Type, &summary, &icon, &cfg,
		&p.Position, &p.ArchivedAt, &p.UsageCount, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Property{}, err
	}
	if summary != nil {
		p.Description = *summary
	}
	if icon != nil {
		p.Icon = *icon
	}
	if len(cfg) > 0 {
		_ = json.Unmarshal(cfg, &p.Config)
	}
	p.Archived = p.ArchivedAt != nil
	return p, nil
}

type CreatePropertyParams struct {
	WorkspaceID string
	Name        string
	Type        string
	Description string
	Icon        string
	Config      PropertyConfig
}

// CountActiveProperties — сколько неархивированных определений сейчас в
// воркспейсе (для проверки лимита в 20 до и после advisory-лока).
func (s *Store) CountActiveProperties(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM field_defs WHERE workspace_id = $1 AND fd_archived_at IS NULL`, workspaceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("tagging: подсчёт активных свойств: %w", err)
	}
	return n, nil
}

func (s *Store) NameTaken(ctx context.Context, workspaceID, name, excludeID string) (bool, error) {
	return s.db.RowExists(ctx, `
		SELECT EXISTS(SELECT 1 FROM field_defs WHERE workspace_id = $1 AND fd_title = $2 AND id <> $3)`,
		workspaceID, name, excludeID)
}

// CreateProperty вставляет определение под advisory-локом воркспейса, чтобы
// конкурентные создания не пробили лимит maxActiveProperties (контракт §4).
func (s *Store) CreateProperty(ctx context.Context, p CreatePropertyParams) (Property, error) {
	var out Property
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
			propertyAdvisoryLockClass, p.WorkspaceID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM field_defs
			WHERE workspace_id = $1 AND fd_archived_at IS NULL`, p.WorkspaceID).Scan(&n); err != nil {
			return err
		}
		if n >= maxActiveProperties {
			return fmt.Errorf("%w: workspace already has %d active properties", errLimitExceeded, maxActiveProperties)
		}
		cfg, err := json.Marshal(p.Config)
		if err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO field_defs (workspace_id, fd_title, fd_type, fd_summary, fd_icon, fd_config)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)
			RETURNING `+propertyColumns,
			p.WorkspaceID, p.Name, p.Type, p.Description, p.Icon, cfg)
		out, err = scanProperty(row)
		return err
	})
	if store.IsUniqueViolation(err) {
		return Property{}, ErrNameTaken
	}
	if err != nil {
		return Property{}, err
	}
	return out, nil
}

var errLimitExceeded = errors.New("tagging: превышен лимит активных свойств")

// ErrLimitExceeded — экспортируемый маркер, чтобы обработчик отличал его от
// прочих ошибок через errors.Is.
func ErrLimitExceeded() error { return errLimitExceeded }

func (s *Store) ListProperties(ctx context.Context, workspaceID string, includeArchived bool) ([]Property, error) {
	q := `SELECT ` + propertyColumns + ` FROM field_defs WHERE workspace_id = $1`
	if !includeArchived {
		q += ` AND fd_archived_at IS NULL`
	}
	q += ` ORDER BY fd_position, created_at`
	rows, err := s.db.Pool.Query(ctx, q, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("tagging: список свойств: %w", err)
	}
	defer rows.Close()
	var out []Property
	for rows.Next() {
		p, err := scanProperty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProperty(ctx context.Context, workspaceID, id string) (Property, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+propertyColumns+` FROM field_defs WHERE workspace_id = $1 AND id = $2`,
		workspaceID, id)
	p, err := scanProperty(row)
	if store.IsNoRows(err) {
		return Property{}, ErrNotFound
	}
	if err != nil {
		return Property{}, fmt.Errorf("tagging: получение свойства: %w", err)
	}
	return p, nil
}

type UpdatePropertyParams struct {
	Name        *string
	Description *string
	HasDesc     bool
	Icon        *string
	HasIcon     bool
	Config      *PropertyConfig
	Archived    *bool
}

// UpdateProperty применяет патч под тем же advisory-локом (нужен только при
// разархивации — она подчиняется тому же лимиту, что создание).
func (s *Store) UpdateProperty(ctx context.Context, workspaceID, id string, p UpdatePropertyParams) (Property, error) {
	var out Property
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
			propertyAdvisoryLockClass, workspaceID); err != nil {
			return err
		}
		current, err := s.getForUpdate(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}
		unarchiving := p.Archived != nil && !*p.Archived && current.Archived
		if unarchiving {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM field_defs
				WHERE workspace_id = $1 AND fd_archived_at IS NULL AND id <> $2`, workspaceID, id).Scan(&n); err != nil {
				return err
			}
			if n >= maxActiveProperties {
				return fmt.Errorf("%w: workspace already has %d active properties", errLimitExceeded, maxActiveProperties)
			}
		}
		var cfgArg any
		if p.Config != nil {
			b, err := json.Marshal(*p.Config)
			if err != nil {
				return err
			}
			cfgArg = string(b)
		}
		var archivedAtSet, archivedAtClear bool
		if p.Archived != nil {
			archivedAtSet = *p.Archived
			archivedAtClear = !*p.Archived
		}
		row := tx.QueryRow(ctx, `
			UPDATE field_defs SET
				fd_title = COALESCE($3, fd_title),
				fd_summary = CASE WHEN $4 THEN NULLIF($5, '') ELSE fd_summary END,
				fd_icon = CASE WHEN $6 THEN NULLIF($7, '') ELSE fd_icon END,
				fd_config = COALESCE($8, fd_config),
				fd_archived_at = CASE WHEN $9 THEN now() WHEN $10 THEN NULL ELSE fd_archived_at END,
				updated_at = now()
			WHERE workspace_id = $1 AND id = $2
			RETURNING `+propertyColumns,
			workspaceID, id, p.Name, p.HasDesc, p.Description, p.HasIcon, p.Icon, cfgArg,
			archivedAtSet, archivedAtClear)
		out, err = scanProperty(row)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return Property{}, ErrNotFound
	}
	if store.IsUniqueViolation(err) {
		return Property{}, ErrNameTaken
	}
	if err != nil {
		return Property{}, err
	}
	return out, nil
}

func (s *Store) getForUpdate(ctx context.Context, tx pgx.Tx, workspaceID, id string) (Property, error) {
	row := tx.QueryRow(ctx, `SELECT `+propertyColumns+` FROM field_defs WHERE workspace_id = $1 AND id = $2 FOR UPDATE`,
		workspaceID, id)
	p, err := scanProperty(row)
	if store.IsNoRows(err) {
		return Property{}, ErrNotFound
	}
	return p, err
}

// OptionsInUse — какие из optionIDs всё ещё встречаются в
// tickets.tk_custom_field_values для этого определения (для 409 при попытке
// убрать вариант, который ещё используется).
func (s *Store) OptionsInUse(ctx context.Context, workspaceID, propertyID string, optionIDs []string) ([]string, error) {
	if len(optionIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT v.opt
		FROM tickets t, jsonb_array_elements_text(
			CASE jsonb_typeof(t.tk_custom_field_values -> $2)
				WHEN 'array' THEN t.tk_custom_field_values -> $2
				WHEN 'string' THEN jsonb_build_array(t.tk_custom_field_values -> $2)
				ELSE '[]'::jsonb
			END) AS v(opt)
		WHERE t.workspace_id = $1 AND t.tk_custom_field_values ? $2 AND v.opt = ANY($3)`,
		workspaceID, propertyID, optionIDs)
	if err != nil {
		return nil, fmt.Errorf("tagging: проверка использования вариантов: %w", err)
	}
	defer rows.Close()
	var used []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		used = append(used, v)
	}
	return used, rows.Err()
}

// --- issue <-> property value ----------------------------------------------------

// GetIssuePropertyValues возвращает весь блок tk_custom_field_values задачи.
func (s *Store) GetIssuePropertyValues(ctx context.Context, workspaceID, issueID string) (json.RawMessage, error) {
	var raw []byte
	err := s.db.Pool.QueryRow(ctx, `
		SELECT tk_custom_field_values FROM tickets WHERE workspace_id = $1 AND id = $2`,
		workspaceID, issueID).Scan(&raw)
	if store.IsNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("tagging: чтение свойств задачи: %w", err)
	}
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	return raw, nil
}

// maxPropertyBlockBytes — контракт §1.16: "итоговый блок properties задачи
// ограничен 16 КБ".
const maxPropertyBlockBytes = 16 * 1024

// ErrPropertyBlockTooLarge — новое значение раздуло tk_custom_field_values
// сверх maxPropertyBlockBytes.
var ErrPropertyBlockTooLarge = errors.New("tagging: блок properties задачи превышает 16 КБ")

// SetIssuePropertyValue устанавливает одно значение внутри
// tk_custom_field_values (jsonb-мердж по одному ключу) и увеличивает
// fd_usage_count, если ключ раньше отсутствовал.
func (s *Store) SetIssuePropertyValue(ctx context.Context, workspaceID, issueID, propertyID string, value json.RawMessage) (json.RawMessage, error) {
	var out json.RawMessage
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var existed bool
		if err := tx.QueryRow(ctx, `SELECT tk_custom_field_values ? $2
			FROM tickets WHERE workspace_id = $1 AND id = $3 FOR UPDATE`, workspaceID, propertyID, issueID).Scan(&existed); err != nil {
			if store.IsNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE tickets SET
				tk_custom_field_values = jsonb_set(tk_custom_field_values, ARRAY[$3], $4::jsonb, true),
				updated_at = now()
			WHERE workspace_id = $1 AND id = $2
			RETURNING tk_custom_field_values`, workspaceID, issueID, propertyID, string(value))
		if err := row.Scan(&out); err != nil {
			return err
		}
		if len(out) > maxPropertyBlockBytes {
			return ErrPropertyBlockTooLarge
		}
		if !existed {
			_, err := tx.Exec(ctx, `UPDATE field_defs SET fd_usage_count = fd_usage_count + 1 WHERE id = $1`, propertyID)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteIssuePropertyValue снимает значение (ключ целиком) и уменьшает
// fd_usage_count, если ключ реально был.
func (s *Store) DeleteIssuePropertyValue(ctx context.Context, workspaceID, issueID, propertyID string) (json.RawMessage, error) {
	var out json.RawMessage
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var existed bool
		if err := tx.QueryRow(ctx, `SELECT tk_custom_field_values ? $2
			FROM tickets WHERE workspace_id = $1 AND id = $3 FOR UPDATE`, workspaceID, propertyID, issueID).Scan(&existed); err != nil {
			if store.IsNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE tickets SET
				tk_custom_field_values = tk_custom_field_values - $3,
				updated_at = now()
			WHERE workspace_id = $1 AND id = $2
			RETURNING tk_custom_field_values`, workspaceID, issueID, propertyID)
		if err := row.Scan(&out); err != nil {
			return err
		}
		if existed {
			_, err := tx.Exec(ctx, `UPDATE field_defs SET fd_usage_count = GREATEST(fd_usage_count - 1, 0) WHERE id = $1`, propertyID)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

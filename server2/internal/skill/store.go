package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// ErrNotFound — навык не существует в этом воркспейсе.
var ErrNotFound = errors.New("skill: не найден")

// ErrNameTaken — имя навыка уже занято в воркспейсе (409).
var ErrNameTaken = errors.New("skill: имя уже используется в воркспейсе")

// Store — доступ к capabilities/capability_files (003_agents.up.sql).
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

const skillColumns = `id, workspace_id, cap_title, cap_summary, cap_config, cap_body_md, cap_created_by, created_at, updated_at`

func scanSkill(row pgx.Row) (Skill, error) {
	var s Skill
	var summary *string
	var cfg []byte
	if err := row.Scan(&s.ID, &s.WorkspaceID, &s.Name, &summary, &cfg, &s.Content, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return Skill{}, err
	}
	if summary != nil {
		s.Description = *summary
	}
	s.Config = cfg
	return s, nil
}

// Get — один навык по id, в пределах воркспейса.
func (s *Store) Get(ctx context.Context, workspaceID, id string) (Skill, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+skillColumns+` FROM capabilities WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	sk, err := scanSkill(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, ErrNotFound
	}
	if err != nil {
		return Skill{}, fmt.Errorf("skill: получение навыка: %w", err)
	}
	return sk, nil
}

// List — сводки всех навыков воркспейса.
func (s *Store) List(ctx context.Context, workspaceID string) ([]Skill, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+skillColumns+` FROM capabilities WHERE workspace_id = $1 ORDER BY cap_title ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("skill: список навыков: %w", err)
	}
	defer rows.Close()
	var out []Skill
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// ByName — найти навык по точному имени (используется импортом/шаблонами
// для "переиспользовать уже существующий в воркспейсе навык с тем же именем").
func (s *Store) ByName(ctx context.Context, workspaceID, name string) (Skill, bool, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+skillColumns+` FROM capabilities WHERE workspace_id = $1 AND cap_title = $2`, workspaceID, name)
	sk, err := scanSkill(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, false, nil
	}
	if err != nil {
		return Skill{}, false, fmt.Errorf("skill: поиск навыка по имени: %w", err)
	}
	return sk, true, nil
}

// NameTaken — есть ли уже навык с этим именем, кроме excludeID.
func (s *Store) NameTaken(ctx context.Context, workspaceID, name, excludeID string) (bool, error) {
	// Тот же приём, что internal/agent.Store.NameTaken: excludeID пуст при
	// create, а id != '' отклоняется Postgres на этапе типизации параметра.
	return s.db.RowExists(ctx, `SELECT EXISTS(
		SELECT 1 FROM capabilities WHERE workspace_id = $1 AND cap_title = $2
			AND id != COALESCE(NULLIF($3,'')::uuid, '00000000-0000-0000-0000-000000000000'::uuid))`,
		workspaceID, name, excludeID)
}

// CreateParams — вход Create.
type CreateParams struct {
	WorkspaceID string
	Name        string
	Summary     string
	Content     string
	Config      json.RawMessage
	CreatedBy   string
	Files       []FileInput
}

// FileInput — components/schemas/SkillFileInput.
type FileInput struct {
	Path    string
	Content string
}

// Create вставляет навык и (опционально) его вспомогательные файлы, одной
// транзакцией. ErrNameTaken — имя уже занято (409).
func (s *Store) Create(ctx context.Context, p CreateParams) (Skill, []File, error) {
	if p.Config == nil {
		p.Config = json.RawMessage(`{}`)
	}
	var id string
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		exists, err := s.db.RowExists(ctx, `SELECT EXISTS(SELECT 1 FROM capabilities WHERE workspace_id = $1 AND cap_title = $2)`,
			p.WorkspaceID, p.Name)
		if err != nil {
			return err
		}
		if exists {
			return ErrNameTaken
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO capabilities (workspace_id, cap_title, cap_summary, cap_config, cap_body_md, cap_created_by)
			VALUES ($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,'')::uuid) RETURNING id`,
			p.WorkspaceID, p.Name, p.Summary, p.Config, p.Content, p.CreatedBy).Scan(&id); err != nil {
			return err
		}
		for _, f := range p.Files {
			if _, err := tx.Exec(ctx, `INSERT INTO capability_files (capability_id, capf_path, capf_body)
				VALUES ($1,$2,$3)`, id, f.Path, f.Content); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, ErrNameTaken) {
		return Skill{}, nil, ErrNameTaken
	}
	if err != nil {
		return Skill{}, nil, fmt.Errorf("skill: создание навыка: %w", err)
	}
	sk, err := s.Get(ctx, p.WorkspaceID, id)
	if err != nil {
		return Skill{}, nil, err
	}
	files, err := s.ListFiles(ctx, id)
	if err != nil {
		return Skill{}, nil, err
	}
	return sk, files, nil
}

// UpdateParams — частичное обновление (updateSkill); Present-флаги решают,
// какие поля вообще менять (contract §11: "не передавать — не трогать").
type UpdateParams struct {
	Name         *string
	Summary      *string
	Content      *string
	Config       json.RawMessage
	ReplaceFiles bool // поле files присутствовало в теле (даже пустым списком)
	Files        []FileInput
}

// Update применяет частичные изменения. ErrNameTaken — новое имя занято.
func (s *Store) Update(ctx context.Context, workspaceID, id string, p UpdateParams) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if p.Name != nil {
			taken, err := s.db.RowExists(ctx, `SELECT EXISTS(
				SELECT 1 FROM capabilities WHERE workspace_id = $1 AND cap_title = $2 AND id != $3)`,
				workspaceID, *p.Name, id)
			if err != nil {
				return err
			}
			if taken {
				return ErrNameTaken
			}
			if _, err := tx.Exec(ctx, `UPDATE capabilities SET cap_title = $3, updated_at = now() WHERE workspace_id = $1 AND id = $2`,
				workspaceID, id, *p.Name); err != nil {
				return err
			}
		}
		if p.Summary != nil {
			if _, err := tx.Exec(ctx, `UPDATE capabilities SET cap_summary = NULLIF($3,''), updated_at = now() WHERE workspace_id = $1 AND id = $2`,
				workspaceID, id, *p.Summary); err != nil {
				return err
			}
		}
		if p.Content != nil {
			if _, err := tx.Exec(ctx, `UPDATE capabilities SET cap_body_md = $3, updated_at = now() WHERE workspace_id = $1 AND id = $2`,
				workspaceID, id, *p.Content); err != nil {
				return err
			}
		}
		if p.Config != nil {
			if _, err := tx.Exec(ctx, `UPDATE capabilities SET cap_config = $3, updated_at = now() WHERE workspace_id = $1 AND id = $2`,
				workspaceID, id, json.RawMessage(p.Config)); err != nil {
				return err
			}
		}
		if p.ReplaceFiles {
			if _, err := tx.Exec(ctx, `DELETE FROM capability_files WHERE capability_id = $1`, id); err != nil {
				return err
			}
			for _, f := range p.Files {
				if f.Path == ReservedFilePath {
					continue // зарезервированный путь молча игнорируется этим полем
				}
				if _, err := tx.Exec(ctx, `INSERT INTO capability_files (capability_id, capf_path, capf_body) VALUES ($1,$2,$3)`,
					id, f.Path, f.Content); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Delete удаляет навык (капабилити) и его назначения меток/агентов (ON
// DELETE CASCADE у capability_tag_links/operative_capabilities/capability_files).
func (s *Store) Delete(ctx context.Context, workspaceID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM capabilities WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("skill: удаление навыка: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Deps: HTTP-слой домена --------------------------------------------------

// Deps — зависимости домена skill.
type Deps struct {
	Store     *Store
	DB        *store.Store
	Resolver  *wsctx.Resolver
	Publisher realtime.Publisher
	Logger    *slog.Logger
}

func New(db *store.Store, pub realtime.Publisher, logger *slog.Logger) *Deps {
	return &Deps{Store: NewStore(db), DB: db, Resolver: wsctx.New(db), Publisher: pub, Logger: logger}
}

func (d *Deps) notify(workspaceID, eventType string, payload any) {
	if d.Publisher == nil || workspaceID == "" {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}

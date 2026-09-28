package squad

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/agent"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// ErrNotFound — отряд не существует в этом воркспейсе.
var ErrNotFound = errors.New("squad: не найден")

// ErrLeaderInvalid — leader_id не является существующим неархивированным
// агентом воркспейса (400).
var ErrLeaderInvalid = errors.New("squad: leader_id не является агентом воркспейса")

// ErrMemberExists — участник уже в отряде (409).
var ErrMemberExists = errors.New("squad: участник уже в отряде")

// ErrMemberNotFound — участник не найден в отряде (404).
var ErrMemberNotFound = errors.New("squad: участник не найден в отряде")

// ErrCannotRemoveLeader — попытка удалить текущего лидера напрямую (400).
var ErrCannotRemoveLeader = errors.New("squad: нельзя удалить текущего лидера — сначала назначьте нового")

// ErrForbiddenLeader — у вызывающего нет права использовать этого агента
// как лидера отряда (403).
var ErrForbiddenLeader = errors.New("squad: у вызывающего нет права использовать этого агента как лидера")

// Store — доступ к crews/crew_members (004_crews.up.sql).
type Store struct {
	db    *store.Store
	agent *agent.Store
}

func NewStore(db *store.Store, agentStore *agent.Store) *Store {
	return &Store{db: db, agent: agentStore}
}

const squadColumns = `id, workspace_id, crew_title, crew_summary, crew_instructions, crew_avatar_uri,
	crew_leader_type, crew_leader_id, crew_creator_account_id, created_at, updated_at, crew_archived_at, crew_archived_by`

func scanSquad(row pgx.Row) (Squad, error) {
	var s Squad
	var summary, instructions *string
	if err := row.Scan(&s.ID, &s.WorkspaceID, &s.Title, &summary, &instructions, &s.AvatarURI,
		&s.LeaderType, &s.LeaderID, &s.CreatorID, &s.CreatedAt, &s.UpdatedAt, &s.ArchivedAt, &s.ArchivedBy); err != nil {
		return Squad{}, err
	}
	if summary != nil {
		s.Summary = *summary
	}
	if instructions != nil {
		s.Instructions = *instructions
	}
	return s, nil
}

// Get — один отряд по id.
func (s *Store) Get(ctx context.Context, workspaceID, id string) (Squad, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+squadColumns+` FROM crews WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	sq, err := scanSquad(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Squad{}, ErrNotFound
	}
	if err != nil {
		return Squad{}, fmt.Errorf("squad: получение отряда: %w", err)
	}
	return sq, nil
}

// List — все неархивированные отряды воркспейса (listSquads: contract не
// упоминает include_archived — по умолчанию видимы только активные).
func (s *Store) List(ctx context.Context, workspaceID string) ([]Squad, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+squadColumns+` FROM crews
		WHERE workspace_id = $1 AND crew_archived_at IS NULL ORDER BY created_at ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("squad: список отрядов: %w", err)
	}
	defer rows.Close()
	var out []Squad
	for rows.Next() {
		sq, err := scanSquad(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sq)
	}
	return out, rows.Err()
}

// MemberCountAndPreview — число участников и первые до 3 для превью
// (listSquads/getSquad).
func (s *Store) MemberCountAndPreview(ctx context.Context, squadID string) (int, []MemberPreview, error) {
	var count int
	if err := s.db.Pool.QueryRow(ctx, `SELECT count(*) FROM crew_members WHERE crew_id = $1`, squadID).Scan(&count); err != nil {
		return 0, nil, fmt.Errorf("squad: число участников: %w", err)
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT cm_member_type, cm_member_id, cm_role FROM crew_members
		WHERE crew_id = $1 ORDER BY created_at ASC LIMIT 3`, squadID)
	if err != nil {
		return 0, nil, fmt.Errorf("squad: превью участников: %w", err)
	}
	defer rows.Close()
	preview := []MemberPreview{}
	for rows.Next() {
		var p MemberPreview
		if err := rows.Scan(&p.MemberType, &p.MemberID, &p.Role); err != nil {
			return 0, nil, err
		}
		preview = append(preview, p)
	}
	return count, preview, rows.Err()
}

// ValidLeader — leader_id существует, не архивирован, и вызывающий вправе
// его вызывать (canManage owner/admin всегда проходит через agent.CanInvoke).
func (s *Store) ValidLeader(ctx context.Context, workspaceID, leaderID, viewerID string, isHuman bool, role httpapi.Role) (agent.Agent, error) {
	a, err := s.agent.Get(ctx, workspaceID, leaderID)
	if errors.Is(err, agent.ErrNotFound) {
		return agent.Agent{}, ErrLeaderInvalid
	}
	if err != nil {
		return agent.Agent{}, fmt.Errorf("squad: получение агента-лидера: %w", err)
	}
	if a.ArchivedAt != nil {
		return agent.Agent{}, ErrLeaderInvalid
	}
	ok, err := s.agent.CanInvoke(ctx, a, viewerID, isHuman, role)
	if err != nil {
		return agent.Agent{}, err
	}
	if !ok {
		return agent.Agent{}, ErrForbiddenLeader
	}
	return a, nil
}

// CreateParams — вход Create.
type CreateParams struct {
	WorkspaceID  string
	Name         string
	Summary      string
	Instructions string
	AvatarURI    *string
	LeaderID     string
	CreatorID    string
}

// Create — createSquad: вставляет crew и лидера первым участником
// (cm_role='leader'), одной транзакцией.
func (s *Store) Create(ctx context.Context, p CreateParams) (Squad, error) {
	var id string
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO crews (workspace_id, crew_title, crew_summary, crew_instructions, crew_avatar_uri,
				crew_leader_type, crew_leader_id, crew_creator_account_id)
			VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),$5,'agent',$6,$7) RETURNING id`,
			p.WorkspaceID, p.Name, p.Summary, p.Instructions, p.AvatarURI, p.LeaderID, p.CreatorID).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO crew_members (crew_id, cm_member_type, cm_member_id, cm_role)
			VALUES ($1,'agent',$2,'leader')`, id, p.LeaderID)
		return err
	})
	if err != nil {
		return Squad{}, fmt.Errorf("squad: создание отряда: %w", err)
	}
	return s.Get(ctx, p.WorkspaceID, id)
}

// UpdateParams — updateSquad (частичное; nil — не менять).
type UpdateParams struct {
	Name         *string
	Summary      *string
	Instructions *string
	AvatarURI    *string
	LeaderID     *string
}

// Update применяет изменения; если leader_id меняется, новый лидер
// добавляется участником с ролью leader (если ещё не входит), старый
// сохраняет свою роль участника (contract: "смена лидера" не удаляет
// прежнего лидера из участников).
func (s *Store) Update(ctx context.Context, workspaceID, id string, p UpdateParams) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if p.Name != nil {
			if _, err := tx.Exec(ctx, `UPDATE crews SET crew_title = $3, updated_at = now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, *p.Name); err != nil {
				return err
			}
		}
		if p.Summary != nil {
			if _, err := tx.Exec(ctx, `UPDATE crews SET crew_summary = NULLIF($3,''), updated_at = now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, *p.Summary); err != nil {
				return err
			}
		}
		if p.Instructions != nil {
			if _, err := tx.Exec(ctx, `UPDATE crews SET crew_instructions = NULLIF($3,''), updated_at = now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, *p.Instructions); err != nil {
				return err
			}
		}
		if p.AvatarURI != nil {
			if _, err := tx.Exec(ctx, `UPDATE crews SET crew_avatar_uri = $3, updated_at = now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, *p.AvatarURI); err != nil {
				return err
			}
		}
		if p.LeaderID != nil {
			if _, err := tx.Exec(ctx, `UPDATE crews SET crew_leader_type='agent', crew_leader_id = $3, updated_at = now() WHERE workspace_id=$1 AND id=$2`,
				workspaceID, id, *p.LeaderID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO crew_members (crew_id, cm_member_type, cm_member_id, cm_role)
				VALUES ($1,'agent',$2,'leader') ON CONFLICT (crew_id, cm_member_type, cm_member_id) DO UPDATE SET cm_role = 'leader'`,
				id, *p.LeaderID); err != nil {
				return err
			}
		}
		return nil
	})
}

// Archive — deleteSquad (мягкое удаление): переносит задачи/автопилоты
// отряда на текущего лидера, затем ставит crew_archived_at. found=false —
// уже был архивирован (400 у вызывающего).
func (s *Store) Archive(ctx context.Context, workspaceID, id, byAccountID string) (bool, error) {
	var archived bool
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var leaderID string
		var archivedAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT crew_leader_id, crew_archived_at FROM crews WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id).Scan(&leaderID, &archivedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if archivedAt != nil {
			return nil // already archived — вызывающий отвечает 400
		}
		if _, err := tx.Exec(ctx, `UPDATE tickets SET tk_assignee_type='agent', tk_assignee_id=$3, updated_at=now()
			WHERE workspace_id=$1 AND tk_assignee_type='squad' AND tk_assignee_id=$2`, workspaceID, id, leaderID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sentinels SET sen_assignee_type='agent', sen_assignee_id=$3, updated_at=now()
			WHERE workspace_id=$1 AND sen_assignee_type='squad' AND sen_assignee_id=$2`, workspaceID, id, leaderID); err != nil {
			if !isUndefinedTable(err) {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE crews SET crew_archived_at = now(), crew_archived_by = $3, updated_at = now()
			WHERE workspace_id=$1 AND id=$2`, workspaceID, id, byAccountID); err != nil {
			return err
		}
		archived = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("squad: архивация: %w", err)
	}
	return archived, nil
}

// --- участники --------------------------------------------------------------

// ListMembers — участники отряда.
func (s *Store) ListMembers(ctx context.Context, squadID string) ([]Member, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT id, crew_id, cm_member_type, cm_member_id, cm_role, created_at
		FROM crew_members WHERE crew_id = $1 ORDER BY created_at ASC`, squadID)
	if err != nil {
		return nil, fmt.Errorf("squad: участники отряда: %w", err)
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.SquadID, &m.MemberType, &m.MemberID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMember — addSquadMember. ErrMemberExists — уже участник (409).
func (s *Store) AddMember(ctx context.Context, squadID, memberType, memberID, role string) (Member, error) {
	if role == "" {
		role = "member"
	}
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO crew_members (crew_id, cm_member_type, cm_member_id, cm_role)
		VALUES ($1,$2,$3,$4) RETURNING id, crew_id, cm_member_type, cm_member_id, cm_role, created_at`,
		squadID, memberType, memberID, role)
	var m Member
	err := row.Scan(&m.ID, &m.SquadID, &m.MemberType, &m.MemberID, &m.Role, &m.CreatedAt)
	if store.IsUniqueViolation(err) {
		return Member{}, ErrMemberExists
	}
	if err != nil {
		return Member{}, fmt.Errorf("squad: добавление участника: %w", err)
	}
	return m, nil
}

// RemoveMember — removeSquadMember. ErrCannotRemoveLeader — это текущий
// лидер (400). ErrMemberNotFound — такого участника нет (404).
func (s *Store) RemoveMember(ctx context.Context, squadID, memberType, memberID string) error {
	var leaderID string
	if err := s.db.Pool.QueryRow(ctx, `SELECT crew_leader_id FROM crews WHERE id = $1`, squadID).Scan(&leaderID); err != nil {
		return fmt.Errorf("squad: проверка лидера: %w", err)
	}
	if memberType == "agent" && memberID == leaderID {
		return ErrCannotRemoveLeader
	}
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM crew_members WHERE crew_id=$1 AND cm_member_type=$2 AND cm_member_id=$3`,
		squadID, memberType, memberID)
	if err != nil {
		return fmt.Errorf("squad: удаление участника: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	return nil
}

// UpdateMemberRole — updateSquadMemberRole.
func (s *Store) UpdateMemberRole(ctx context.Context, squadID, memberType, memberID, role string) (Member, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE crew_members SET cm_role = $4 WHERE crew_id=$1 AND cm_member_type=$2 AND cm_member_id=$3
		RETURNING id, crew_id, cm_member_type, cm_member_id, cm_role, created_at`,
		squadID, memberType, memberID, role)
	var m Member
	err := row.Scan(&m.ID, &m.SquadID, &m.MemberType, &m.MemberID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrMemberNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("squad: смена роли участника: %w", err)
	}
	return m, nil
}

func isUndefinedTable(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "42P01") || strings.Contains(err.Error(), "does not exist"))
}

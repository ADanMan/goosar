package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

var ErrNotFound = errors.New("workspace: не найдено")

// Store — доступ к таблицам 002_workspace.up.sql.
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// Workspace — форма components/schemas/Workspace / WorkspaceItem.
type Workspace struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description *string         `json:"description"`
	Context     *string         `json:"context"`
	Settings    json.RawMessage `json:"settings"`
	Repos       json.RawMessage `json:"repos"`
	IssuePrefix string          `json:"issue_prefix"`
	AvatarURL   *string         `json:"avatar_url"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

const workspaceColumns = `id, ws_title, ws_slug, ws_summary, ws_operating_context, ws_settings,
	ws_repo_refs, ws_ticket_prefix, ws_avatar_uri, created_at, updated_at`

// workspaceColumnsQualified — тот же список, квалифицированный алиасом s.,
// для запросов с JOIN (иначе "id" неоднозначен между spaces и space_members).
const workspaceColumnsQualified = `s.id, s.ws_title, s.ws_slug, s.ws_summary, s.ws_operating_context, s.ws_settings,
	s.ws_repo_refs, s.ws_ticket_prefix, s.ws_avatar_uri, s.created_at, s.updated_at`

func scanWorkspace(row pgx.Row) (Workspace, error) {
	var w Workspace
	var settings, repos []byte
	if err := row.Scan(&w.ID, &w.Name, &w.Slug, &w.Description, &w.Context, &settings,
		&repos, &w.IssuePrefix, &w.AvatarURL, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return Workspace{}, err
	}
	w.Settings = orEmptyObject(settings)
	w.Repos = orEmptyArray(repos)
	return w, nil
}

func orEmptyObject(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return b
}

func orEmptyArray(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("[]")
	}
	return b
}

// CreateWorkspaceParams — вход CreateWorkspace.
type CreateWorkspaceParams struct {
	Name        string
	Slug        string
	Description *string
	Context     *string
	IssuePrefix string
	OwnerID     string
}

// CreateWorkspace создаёт пространство и делает OwnerID его owner'ом одной транзакцией.
func (s *Store) CreateWorkspace(ctx context.Context, p CreateWorkspaceParams) (Workspace, error) {
	var w Workspace
	err := s.db.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO spaces (ws_title, ws_slug, ws_summary, ws_operating_context, ws_ticket_prefix)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING `+workspaceColumns,
			p.Name, p.Slug, p.Description, p.Context, p.IssuePrefix)
		var err error
		w, err = scanWorkspace(row)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO space_members (workspace_id, account_id, sm_role, sm_perimeter_access)
			VALUES ($1, $2, 'owner', true)`, w.ID, p.OwnerID)
		return err
	})
	if err != nil {
		return Workspace{}, err
	}
	return w, nil
}

// GetWorkspace — по id.
func (s *Store) GetWorkspace(ctx context.Context, id string) (Workspace, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM spaces WHERE id = $1`, id)
	w, err := scanWorkspace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("workspace: получение пространства: %w", err)
	}
	return w, nil
}

// GetWorkspaceBySlug — по slug.
func (s *Store) GetWorkspaceBySlug(ctx context.Context, slug string) (Workspace, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM spaces WHERE ws_slug = $1`, slug)
	w, err := scanWorkspace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("workspace: получение пространства по slug: %w", err)
	}
	return w, nil
}

// SlugTaken — существует ли уже пространство с этим slug.
func (s *Store) SlugTaken(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM spaces WHERE ws_slug = $1)`, slug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("workspace: проверка slug: %w", err)
	}
	return exists, nil
}

// ListForUser — пространства, где accountID состоит участником.
func (s *Store) ListForUser(ctx context.Context, accountID string) ([]Workspace, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+workspaceColumnsQualified+` FROM spaces s
		JOIN space_members m ON m.workspace_id = s.id
		WHERE m.account_id = $1
		ORDER BY s.created_at`, accountID)
	if err != nil {
		return nil, fmt.Errorf("workspace: список пространств: %w", err)
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UpdatePatch — частичный патч UpdateWorkspaceRequest.
type UpdatePatch struct {
	Name        *string
	Description *string
	HasDesc     bool // description может быть явно передан как null
	Context     *string
	HasContext  bool
	Settings    json.RawMessage
	Repos       json.RawMessage
	IssuePrefix *string
	AvatarURL   *string
}

// UpdateWorkspace применяет частичный патч.
func (s *Store) UpdateWorkspace(ctx context.Context, id string, p UpdatePatch) (Workspace, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE spaces SET
			ws_title = COALESCE($2, ws_title),
			ws_summary = CASE WHEN $3 THEN $4 ELSE ws_summary END,
			ws_operating_context = CASE WHEN $5 THEN $6 ELSE ws_operating_context END,
			ws_settings = COALESCE($7, ws_settings),
			ws_repo_refs = COALESCE($8, ws_repo_refs),
			ws_ticket_prefix = COALESCE($9, ws_ticket_prefix),
			ws_avatar_uri = COALESCE($10, ws_avatar_uri),
			updated_at = now()
		WHERE id = $1
		RETURNING `+workspaceColumns,
		id, p.Name, p.HasDesc, p.Description, p.HasContext, p.Context,
		nullableJSON(p.Settings), nullableJSON(p.Repos), p.IssuePrefix, p.AvatarURL)
	w, err := scanWorkspace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("workspace: обновление пространства: %w", err)
	}
	return w, nil
}

func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// DeleteWorkspace удаляет пространство целиком (каскад через FK ON DELETE CASCADE).
func (s *Store) DeleteWorkspace(ctx context.Context, id string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM spaces WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("workspace: удаление пространства: %w", err)
	}
	return nil
}

// --- members -----------------------------------------------------------------

type MemberWithUser struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	UserID          string    `json:"user_id"`
	Role            string    `json:"role"`
	CreatedAt       time.Time `json:"created_at"`
	Name            string    `json:"name"`
	Email           string    `json:"email"`
	AvatarURL       *string   `json:"avatar_url"`
	PerimeterAccess bool      `json:"perimeter_access"`
}

const memberColumns = `m.id, m.workspace_id, m.account_id, m.sm_role, m.created_at,
	a.acct_full_name, a.acct_email, a.acct_avatar_uri, m.sm_perimeter_access`

func scanMember(row pgx.Row) (MemberWithUser, error) {
	var m MemberWithUser
	if err := row.Scan(&m.ID, &m.WorkspaceID, &m.UserID, &m.Role, &m.CreatedAt,
		&m.Name, &m.Email, &m.AvatarURL, &m.PerimeterAccess); err != nil {
		return MemberWithUser{}, err
	}
	return m, nil
}

// ListMembers — участники пространства с данными пользователя.
func (s *Store) ListMembers(ctx context.Context, workspaceID string) ([]MemberWithUser, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+memberColumns+`
		FROM space_members m JOIN accounts a ON a.id = m.account_id
		WHERE m.workspace_id = $1
		ORDER BY m.created_at`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace: список участников: %w", err)
	}
	defer rows.Close()
	var out []MemberWithUser
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMemberByUser — членство accountID в workspaceID, если есть.
func (s *Store) GetMemberByUser(ctx context.Context, workspaceID, accountID string) (MemberWithUser, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+memberColumns+`
		FROM space_members m JOIN accounts a ON a.id = m.account_id
		WHERE m.workspace_id = $1 AND m.account_id = $2`, workspaceID, accountID)
	m, err := scanMember(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberWithUser{}, ErrNotFound
	}
	if err != nil {
		return MemberWithUser{}, fmt.Errorf("workspace: чтение членства: %w", err)
	}
	return m, nil
}

// GetMemberByUserEmail — членство по email пользователя (для проверки
// "приглашаемый уже участник" до создания приглашения).
func (s *Store) GetMemberByUserEmail(ctx context.Context, workspaceID, email string) (MemberWithUser, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+memberColumns+`
		FROM space_members m JOIN accounts a ON a.id = m.account_id
		WHERE m.workspace_id = $1 AND lower(a.acct_email) = lower($2)`, workspaceID, email)
	m, err := scanMember(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberWithUser{}, ErrNotFound
	}
	if err != nil {
		return MemberWithUser{}, fmt.Errorf("workspace: чтение членства по email: %w", err)
	}
	return m, nil
}

// GetMemberByID — членство по id строки space_members.
func (s *Store) GetMemberByID(ctx context.Context, workspaceID, memberID string) (MemberWithUser, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+memberColumns+`
		FROM space_members m JOIN accounts a ON a.id = m.account_id
		WHERE m.workspace_id = $1 AND m.id = $2`, workspaceID, memberID)
	m, err := scanMember(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberWithUser{}, ErrNotFound
	}
	if err != nil {
		return MemberWithUser{}, fmt.Errorf("workspace: чтение членства по id: %w", err)
	}
	return m, nil
}

// CountOwners — сколько owner'ов сейчас в пространстве (для правила "минимум один owner").
func (s *Store) CountOwners(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM space_members WHERE workspace_id = $1 AND sm_role = 'owner'`, workspaceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("workspace: подсчёт owner: %w", err)
	}
	return n, nil
}

// AddMember создаёт членство напрямую (используется acceptInvitation).
func (s *Store) AddMember(ctx context.Context, workspaceID, accountID, role string) (MemberWithUser, error) {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO space_members (workspace_id, account_id, sm_role)
		VALUES ($1, $2, $3)
		ON CONFLICT (workspace_id, account_id) DO NOTHING`, workspaceID, accountID, role)
	if err != nil {
		return MemberWithUser{}, fmt.Errorf("workspace: добавление участника: %w", err)
	}
	return s.GetMemberByUser(ctx, workspaceID, accountID)
}

// UpdateMember меняет роль и/или perimeter_access.
func (s *Store) UpdateMember(ctx context.Context, workspaceID, memberID string, role *string, perimeter *bool) (MemberWithUser, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE space_members SET
			sm_role = COALESCE($3, sm_role),
			sm_perimeter_access = COALESCE($4, sm_perimeter_access)
		WHERE workspace_id = $1 AND id = $2
		RETURNING id`, workspaceID, memberID, role, perimeter)
	var id string
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MemberWithUser{}, ErrNotFound
		}
		return MemberWithUser{}, fmt.Errorf("workspace: обновление участника: %w", err)
	}
	return s.GetMemberByID(ctx, workspaceID, memberID)
}

// RemoveMember удаляет строку членства.
func (s *Store) RemoveMember(ctx context.Context, workspaceID, memberID string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM space_members WHERE workspace_id = $1 AND id = $2`, workspaceID, memberID)
	if err != nil {
		return 0, fmt.Errorf("workspace: удаление участника: %w", err)
	}
	return tag.RowsAffected(), nil
}

// --- invitations ---------------------------------------------------------------

type Invitation struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	InviterID     string    `json:"inviter_id"`
	InviteeEmail  string    `json:"invitee_email"`
	InviteeUserID *string   `json:"invitee_user_id"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	InviterName   string    `json:"inviter_name"`
	InviterEmail  string    `json:"inviter_email"`
	WorkspaceName string    `json:"workspace_name"`
}

const invitationColumns = `i.id, i.workspace_id, i.inv_inviter_account_id, i.inv_invitee_email,
	i.inv_invitee_account_id, i.inv_role, i.inv_status, i.created_at, i.updated_at, i.inv_valid_until,
	inviter.acct_full_name, inviter.acct_email, s.ws_title`

func scanInvitation(row pgx.Row) (Invitation, error) {
	var inv Invitation
	if err := row.Scan(&inv.ID, &inv.WorkspaceID, &inv.InviterID, &inv.InviteeEmail,
		&inv.InviteeUserID, &inv.Role, &inv.Status, &inv.CreatedAt, &inv.UpdatedAt, &inv.ExpiresAt,
		&inv.InviterName, &inv.InviterEmail, &inv.WorkspaceName); err != nil {
		return Invitation{}, err
	}
	return inv, nil
}

const invitationFrom = `
	FROM space_invitations i
	JOIN accounts inviter ON inviter.id = i.inv_inviter_account_id
	JOIN spaces s ON s.id = i.workspace_id`

// PendingInvitationExists — есть ли уже неистёкшее pending-приглашение для email в этом пространстве.
func (s *Store) PendingInvitationExists(ctx context.Context, workspaceID, email string) (bool, error) {
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM space_invitations
			WHERE workspace_id = $1 AND lower(inv_invitee_email) = lower($2)
			  AND inv_status = 'pending' AND inv_valid_until > now())`, workspaceID, email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("workspace: проверка приглашения: %w", err)
	}
	return exists, nil
}

// CreateInvitation создаёт pending-приглашение на 14 дней.
func (s *Store) CreateInvitation(ctx context.Context, workspaceID, inviterID, email, role string) (Invitation, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO space_invitations (workspace_id, inv_inviter_account_id, inv_invitee_email,
			inv_invitee_account_id, inv_role, inv_valid_until)
		VALUES ($1, $2, $3, (SELECT id FROM accounts WHERE lower(acct_email) = lower($3)), $4, now() + interval '14 days')
		RETURNING id`, workspaceID, inviterID, email, role)
	var id string
	if err := row.Scan(&id); err != nil {
		return Invitation{}, fmt.Errorf("workspace: создание приглашения: %w", err)
	}
	return s.GetInvitation(ctx, id)
}

func (s *Store) GetInvitation(ctx context.Context, id string) (Invitation, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+invitationColumns+invitationFrom+` WHERE i.id = $1`, id)
	inv, err := scanInvitation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("workspace: получение приглашения: %w", err)
	}
	return inv, nil
}

// ListPendingForWorkspace — pending-приглашения пространства.
func (s *Store) ListPendingForWorkspace(ctx context.Context, workspaceID string) ([]Invitation, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+invitationColumns+invitationFrom+`
		WHERE i.workspace_id = $1 AND i.inv_status = 'pending'
		ORDER BY i.created_at DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace: список приглашений пространства: %w", err)
	}
	defer rows.Close()
	return scanInvitations(rows)
}

// ListForRecipient — приглашения, адресованные accountID (по user_id или email).
func (s *Store) ListForRecipient(ctx context.Context, accountID, email string) ([]Invitation, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+invitationColumns+invitationFrom+`
		WHERE i.inv_status = 'pending' AND (i.inv_invitee_account_id = $1 OR lower(i.inv_invitee_email) = lower($2))
		ORDER BY i.created_at DESC`, accountID, email)
	if err != nil {
		return nil, fmt.Errorf("workspace: список моих приглашений: %w", err)
	}
	defer rows.Close()
	return scanInvitations(rows)
}

func scanInvitations(rows pgx.Rows) ([]Invitation, error) {
	var out []Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RevokeInvitation переводит приглашение в revoked, если оно pending и принадлежит workspaceID.
func (s *Store) RevokeInvitation(ctx context.Context, workspaceID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE space_invitations SET inv_status = 'revoked', updated_at = now()
		WHERE id = $1 AND workspace_id = $2 AND inv_status = 'pending'`, id, workspaceID)
	if err != nil {
		return fmt.Errorf("workspace: отзыв приглашения: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetInvitationStatus — общий переход статуса (accepted/declined), с проверкой pending+не истёк.
func (s *Store) SetInvitationStatus(ctx context.Context, id, status string, requireNotExpired bool) error {
	q := `UPDATE space_invitations SET inv_status = $2, updated_at = now()
		WHERE id = $1 AND inv_status = 'pending'`
	if requireNotExpired {
		q += ` AND inv_valid_until > now()`
	}
	tag, err := s.db.Pool.Exec(ctx, q, id, status)
	if err != nil {
		return fmt.Errorf("workspace: смена статуса приглашения: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- runtime profiles ------------------------------------------------------------

type RuntimeProfile struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"workspace_id"`
	DisplayName    string          `json:"display_name"`
	ProtocolFamily string          `json:"protocol_family"`
	CommandName    string          `json:"command_name"`
	Description    *string         `json:"description"`
	FixedArgs      json.RawMessage `json:"fixed_args"`
	Visibility     string          `json:"visibility"`
	CreatedBy      *string         `json:"created_by"`
	Enabled        bool            `json:"enabled"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

const profileColumns = `id, workspace_id, proto_display_title, proto_family, proto_command,
	proto_summary, proto_fixed_args, proto_visibility, proto_created_by, proto_enabled, created_at, updated_at`

func scanProfile(row pgx.Row) (RuntimeProfile, error) {
	var p RuntimeProfile
	var args []byte
	if err := row.Scan(&p.ID, &p.WorkspaceID, &p.DisplayName, &p.ProtocolFamily, &p.CommandName,
		&p.Description, &args, &p.Visibility, &p.CreatedBy, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return RuntimeProfile{}, err
	}
	p.FixedArgs = orEmptyArray(args)
	return p, nil
}

type CreateProfileParams struct {
	WorkspaceID    string
	DisplayName    string
	ProtocolFamily string
	CommandName    string
	Description    *string
	FixedArgs      json.RawMessage
	Enabled        bool
	CreatedBy      string
}

func (s *Store) CreateRuntimeProfile(ctx context.Context, p CreateProfileParams) (RuntimeProfile, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO agent_protocols (workspace_id, proto_display_title, proto_family, proto_command,
			proto_summary, proto_fixed_args, proto_enabled, proto_created_by)
		VALUES ($1, $2, $3, $4, $5, COALESCE($6, '[]'::jsonb), $7, $8)
		RETURNING `+profileColumns,
		p.WorkspaceID, p.DisplayName, p.ProtocolFamily, p.CommandName, p.Description,
		nullableJSON(p.FixedArgs), p.Enabled, p.CreatedBy)
	prof, err := scanProfile(row)
	if err != nil {
		return RuntimeProfile{}, err
	}
	return prof, nil
}

func (s *Store) DisplayNameTaken(ctx context.Context, workspaceID, name, excludeID string) (bool, error) {
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM agent_protocols
			WHERE workspace_id = $1 AND proto_display_title = $2 AND id <> $3)`,
		workspaceID, name, excludeID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("workspace: проверка display_name: %w", err)
	}
	return exists, nil
}

func (s *Store) ListRuntimeProfiles(ctx context.Context, workspaceID string) ([]RuntimeProfile, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+profileColumns+` FROM agent_protocols WHERE workspace_id = $1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace: список профилей рантайма: %w", err)
	}
	defer rows.Close()
	var out []RuntimeProfile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetRuntimeProfile(ctx context.Context, workspaceID, id string) (RuntimeProfile, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+profileColumns+` FROM agent_protocols WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	p, err := scanProfile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeProfile{}, ErrNotFound
	}
	if err != nil {
		return RuntimeProfile{}, fmt.Errorf("workspace: получение профиля рантайма: %w", err)
	}
	return p, nil
}

type UpdateProfileParams struct {
	DisplayName *string
	CommandName *string
	Description *string
	HasDesc     bool
	FixedArgs   json.RawMessage
	Enabled     *bool
}

func (s *Store) UpdateRuntimeProfile(ctx context.Context, workspaceID, id string, p UpdateProfileParams) (RuntimeProfile, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE agent_protocols SET
			proto_display_title = COALESCE($3, proto_display_title),
			proto_command = COALESCE($4, proto_command),
			proto_summary = CASE WHEN $5 THEN $6 ELSE proto_summary END,
			proto_fixed_args = COALESCE($7, proto_fixed_args),
			proto_enabled = COALESCE($8, proto_enabled),
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING `+profileColumns,
		workspaceID, id, p.DisplayName, p.CommandName, p.HasDesc, p.Description,
		nullableJSON(p.FixedArgs), p.Enabled)
	prof, err := scanProfile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeProfile{}, ErrNotFound
	}
	if err != nil {
		return RuntimeProfile{}, fmt.Errorf("workspace: обновление профиля рантайма: %w", err)
	}
	return prof, nil
}

// HasActiveAgentsOnProfile — есть ли хоть один активный агент, использующий профиль
// (упрощение относительно описания в контракте: T-026 ещё не реализует домен
// agents/executors — 003_agents.up.sql, поэтому проверка отложена до T-027 и
// зафиксирована в decisions.md; здесь всегда возвращает false).
func (s *Store) HasActiveAgentsOnProfile(ctx context.Context, profileID string) (bool, error) {
	return false, nil
}

func (s *Store) DeleteRuntimeProfile(ctx context.Context, workspaceID, id string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM agent_protocols WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return 0, fmt.Errorf("workspace: удаление профиля рантайма: %w", err)
	}
	return tag.RowsAffected(), nil
}

// TicketNumbering — пара (следующий порядковый номер, текущий префикс
// воркспейса), которую IncrementTicketSeq выдаёт домену task для присвоения
// number/identifier новой задаче (docs/51-data-model.md, «Нумерация задач»).
type TicketNumbering struct {
	Seq    int64
	Prefix string
}

// ticketSeqQuerier — минимум, нужный IncrementTicketSeq: и *pgxpool.Pool, и
// pgx.Tx ему удовлетворяют.
type ticketSeqQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// IncrementTicketSeq инкрементирует ws_next_ticket_seq пространства (блокируя
// его строку в spaces до конца транзакции q) и возвращает новый номер вместе
// с текущим ws_ticket_prefix. Домен task вызывает это внутри собственной
// транзакции создания задачи, чтобы инкремент и вставка строки tickets были
// атомарны (q — тот же pgx.Tx, в котором создаётся tickets).
func (s *Store) IncrementTicketSeq(ctx context.Context, q ticketSeqQuerier, workspaceID string) (TicketNumbering, error) {
	var n TicketNumbering
	err := q.QueryRow(ctx, `UPDATE spaces SET ws_next_ticket_seq = ws_next_ticket_seq + 1 WHERE id = $1
		RETURNING ws_next_ticket_seq, ws_ticket_prefix`, workspaceID).Scan(&n.Seq, &n.Prefix)
	if errors.Is(err, pgx.ErrNoRows) {
		return TicketNumbering{}, ErrNotFound
	}
	if err != nil {
		return TicketNumbering{}, fmt.Errorf("workspace: инкремент номера задачи: %w", err)
	}
	return n, nil
}

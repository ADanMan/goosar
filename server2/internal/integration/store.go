package integration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/store"
)

// Store — доступ к таблицам этого домена (010_integrations.up.sql +
// 320_slack_binding.up.sql).
type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// --- GitHub -----------------------------------------------------------------

// GitHubInstallation — schemas.GitHubInstallation.
type GitHubInstallation struct {
	ID             string
	WorkspaceID    string
	InstallationID *int64
	AccountLogin   string
	AccountType    string
	AccountAvatar  *string
	CreatedAt      time.Time
}

func (s *Store) ListGitHubInstallations(ctx context.Context, workspaceID string) ([]GitHubInstallation, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, workspace_id, gh_installation_id, gh_account_login, gh_account_type,
		       gh_account_avatar_uri, created_at
		FROM github_installations WHERE workspace_id = $1 ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("integration: список установок github: %w", err)
	}
	defer rows.Close()
	var out []GitHubInstallation
	for rows.Next() {
		var g GitHubInstallation
		if err := rows.Scan(&g.ID, &g.WorkspaceID, &g.InstallationID, &g.AccountLogin, &g.AccountType,
			&g.AccountAvatar, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UpsertGitHubInstallation — апсерт по (gh_installation_id): вебхук
// `installation.created` и коллбэк /api/github/setup используют один и тот
// же путь. ghInstallationID может быть nil (ещё не пришёл вебхук).
func (s *Store) UpsertGitHubInstallation(ctx context.Context, workspaceID string, ghInstallationID *int64, accountLogin, accountType string, accountAvatar *string) (GitHubInstallation, error) {
	var g GitHubInstallation
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO github_installations (workspace_id, gh_installation_id, gh_account_login, gh_account_type, gh_account_avatar_uri)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO NOTHING
		RETURNING id, workspace_id, gh_installation_id, gh_account_login, gh_account_type, gh_account_avatar_uri, created_at`,
		workspaceID, ghInstallationID, accountLogin, accountType, accountAvatar,
	).Scan(&g.ID, &g.WorkspaceID, &g.InstallationID, &g.AccountLogin, &g.AccountType, &g.AccountAvatar, &g.CreatedAt)
	if err != nil {
		return GitHubInstallation{}, fmt.Errorf("integration: сохранение установки github: %w", err)
	}
	return g, nil
}

// UpsertGitHubInstallationByGHID — то же самое, но апсерт по gh_installation_id
// (вебхук installation.created/deleted не знает наш workspace_id внутренней
// строки — он приходит только с числовым installation.id GitHub, найденным
// заранее через github_installations.gh_installation_id).
func (s *Store) UpsertGitHubInstallationByGHID(ctx context.Context, ghInstallationID int64, accountLogin, accountType string, accountAvatar *string) (GitHubInstallation, bool, error) {
	var g GitHubInstallation
	err := s.db.Pool.QueryRow(ctx, `
		UPDATE github_installations SET gh_account_login = $2, gh_account_type = $3, gh_account_avatar_uri = $4
		WHERE gh_installation_id = $1
		RETURNING id, workspace_id, gh_installation_id, gh_account_login, gh_account_type, gh_account_avatar_uri, created_at`,
		ghInstallationID, accountLogin, accountType, accountAvatar,
	).Scan(&g.ID, &g.WorkspaceID, &g.InstallationID, &g.AccountLogin, &g.AccountType, &g.AccountAvatar, &g.CreatedAt)
	if store.IsNoRows(err) {
		return GitHubInstallation{}, false, nil
	}
	if err != nil {
		return GitHubInstallation{}, false, fmt.Errorf("integration: обновление установки github: %w", err)
	}
	return g, true, nil
}

func (s *Store) DeleteGitHubInstallationsByGHID(ctx context.Context, ghInstallationID int64) ([]string, error) {
	rows, err := s.db.Pool.Query(ctx, `DELETE FROM github_installations WHERE gh_installation_id = $1 RETURNING workspace_id`, ghInstallationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) DeleteGitHubInstallation(ctx context.Context, workspaceID, id string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM github_installations WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) GitHubInstallationByID(ctx context.Context, workspaceID, id string) (GitHubInstallation, bool, error) {
	var g GitHubInstallation
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, workspace_id, gh_installation_id, gh_account_login, gh_account_type, gh_account_avatar_uri, created_at
		FROM github_installations WHERE id = $1 AND workspace_id = $2`, id, workspaceID,
	).Scan(&g.ID, &g.WorkspaceID, &g.InstallationID, &g.AccountLogin, &g.AccountType, &g.AccountAvatar, &g.CreatedAt)
	if store.IsNoRows(err) {
		return GitHubInstallation{}, false, nil
	}
	if err != nil {
		return GitHubInstallation{}, false, err
	}
	return g, true, nil
}

// --- VCS ---------------------------------------------------------------------

// VCSConnection — schemas.VcsConnection (+ webhook_secret en clair, seulement
// à la création/rotation — см. VCSConnectionSecrets).
type VCSConnection struct {
	ID           string
	WorkspaceID  string
	Provider     string
	InstanceURL  string
	AccountLogin *string
	WebhookURL   *string
	WebhookPath  *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (s *Store) ListVCSConnections(ctx context.Context, workspaceID string) ([]VCSConnection, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, workspace_id, vcs_provider, vcs_instance_uri, vcs_account_login, vcs_webhook_uri, vcs_webhook_path, created_at, updated_at
		FROM vcs_connections WHERE workspace_id = $1 ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VCSConnection
	for rows.Next() {
		var c VCSConnection
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Provider, &c.InstanceURL, &c.AccountLogin, &c.WebhookURL, &c.WebhookPath, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpsertVCSConnection — upsert по (workspace_id, provider, instance_url),
// contract: "Подключено (или обновлено — upsert по паре provider+instance_url)".
func (s *Store) UpsertVCSConnection(ctx context.Context, workspaceID, provider, instanceURL string, accountLogin *string, tokenSealed, webhookSecretSealed []byte, webhookURL, webhookPath string) (VCSConnection, error) {
	var c VCSConnection
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO vcs_connections (workspace_id, vcs_provider, vcs_instance_uri, vcs_account_login,
			vcs_access_token_sealed, vcs_webhook_secret_sealed, vcs_webhook_uri, vcs_webhook_path)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (workspace_id, vcs_provider, vcs_instance_uri) DO UPDATE SET
			vcs_account_login = EXCLUDED.vcs_account_login,
			vcs_access_token_sealed = EXCLUDED.vcs_access_token_sealed,
			vcs_webhook_secret_sealed = EXCLUDED.vcs_webhook_secret_sealed,
			vcs_webhook_uri = EXCLUDED.vcs_webhook_uri,
			vcs_webhook_path = EXCLUDED.vcs_webhook_path,
			updated_at = now()
		RETURNING id, workspace_id, vcs_provider, vcs_instance_uri, vcs_account_login, vcs_webhook_uri, vcs_webhook_path, created_at, updated_at`,
		workspaceID, provider, instanceURL, accountLogin, tokenSealed, webhookSecretSealed, webhookURL, webhookPath,
	).Scan(&c.ID, &c.WorkspaceID, &c.Provider, &c.InstanceURL, &c.AccountLogin, &c.WebhookURL, &c.WebhookPath, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return VCSConnection{}, fmt.Errorf("integration: сохранение vcs-подключения: %w", err)
	}
	return c, nil
}

// SetVCSWebhookURL записывает вычисленный на основе id адрес вебхука
// (contract: GET-ответ содержит webhook_url/webhook_path) — отдельным UPDATE
// после вставки, потому что сам id известен только после INSERT.
func (s *Store) SetVCSWebhookURL(ctx context.Context, id, webhookURL, webhookPath string) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE vcs_connections SET vcs_webhook_uri = $2, vcs_webhook_path = $3 WHERE id = $1`,
		id, webhookURL, webhookPath)
	return err
}

func (s *Store) DeleteVCSConnection(ctx context.Context, workspaceID, id string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM vcs_connections WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) VCSConnectionByID(ctx context.Context, workspaceID, id string) (VCSConnection, bool, error) {
	var c VCSConnection
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, workspace_id, vcs_provider, vcs_instance_uri, vcs_account_login, vcs_webhook_uri, vcs_webhook_path, created_at, updated_at
		FROM vcs_connections WHERE id = $1 AND workspace_id = $2`, id, workspaceID,
	).Scan(&c.ID, &c.WorkspaceID, &c.Provider, &c.InstanceURL, &c.AccountLogin, &c.WebhookURL, &c.WebhookPath, &c.CreatedAt, &c.UpdatedAt)
	if store.IsNoRows(err) {
		return VCSConnection{}, false, nil
	}
	if err != nil {
		return VCSConnection{}, false, err
	}
	return c, true, nil
}

// VCSConnectionSecret — id + workspace_id + оба запечатанных секрета, для
// вебхук-обработчика (проверка подписи) и rotate-webhook.
type VCSConnectionSecret struct {
	ID                  string
	WorkspaceID         string
	Provider            string
	WebhookSecretSealed []byte
}

func (s *Store) VCSConnectionSecretByID(ctx context.Context, id string) (VCSConnectionSecret, bool, error) {
	var c VCSConnectionSecret
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, workspace_id, vcs_provider, vcs_webhook_secret_sealed FROM vcs_connections WHERE id = $1`, id,
	).Scan(&c.ID, &c.WorkspaceID, &c.Provider, &c.WebhookSecretSealed)
	if store.IsNoRows(err) {
		return VCSConnectionSecret{}, false, nil
	}
	if err != nil {
		return VCSConnectionSecret{}, false, err
	}
	return c, true, nil
}

func (s *Store) RotateVCSWebhookSecret(ctx context.Context, workspaceID, id string, webhookSecretSealed []byte) (VCSConnection, bool, error) {
	var c VCSConnection
	err := s.db.Pool.QueryRow(ctx, `
		UPDATE vcs_connections SET vcs_webhook_secret_sealed = $3, updated_at = now()
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, vcs_provider, vcs_instance_uri, vcs_account_login, vcs_webhook_uri, vcs_webhook_path, created_at, updated_at`,
		id, workspaceID, webhookSecretSealed,
	).Scan(&c.ID, &c.WorkspaceID, &c.Provider, &c.InstanceURL, &c.AccountLogin, &c.WebhookURL, &c.WebhookPath, &c.CreatedAt, &c.UpdatedAt)
	if store.IsNoRows(err) {
		return VCSConnection{}, false, nil
	}
	if err != nil {
		return VCSConnection{}, false, err
	}
	return c, true, nil
}

// --- Slack -------------------------------------------------------------------

// SlackInstallation — schemas.SlackInstallation.
type SlackInstallation struct {
	ID              string
	WorkspaceID     string
	AgentID         string
	TeamID          string
	BotUserID       *string
	InstallerUserID string
	Status          string
	InstalledAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (s *Store) ListSlackInstallations(ctx context.Context, workspaceID string) ([]SlackInstallation, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, workspace_id, operative_id, sl_team_id, sl_bot_user_id, sl_installer_account_id,
		       sl_status, sl_installed_at, created_at, updated_at
		FROM slack_installations WHERE workspace_id = $1 ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SlackInstallation
	for rows.Next() {
		var si SlackInstallation
		if err := rows.Scan(&si.ID, &si.WorkspaceID, &si.AgentID, &si.TeamID, &si.BotUserID,
			&si.InstallerUserID, &si.Status, &si.InstalledAt, &si.CreatedAt, &si.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// TeamBoundElsewhere — теам уже привязан в этом пространстве к другому
// (не-архивному) агенту, к архивному агенту, или в другом пространстве;
// contract: три разных 409-текста.
type TeamConflict int

const (
	TeamConflictNone TeamConflict = iota
	TeamConflictOtherAgent
	TeamConflictArchivedAgent
	TeamConflictOtherWorkspace
)

func (s *Store) SlackTeamConflict(ctx context.Context, workspaceID, agentID, teamID string) (TeamConflict, error) {
	var (
		existingWorkspace string
		existingAgent     string
		agentArchived     bool
	)
	err := s.db.Pool.QueryRow(ctx, `
		SELECT si.workspace_id, si.operative_id, (o.op_archived_at IS NOT NULL)
		FROM slack_installations si
		JOIN operatives o ON o.id = si.operative_id
		WHERE si.sl_team_id = $1 AND si.sl_status = 'active'
		LIMIT 1`, teamID).Scan(&existingWorkspace, &existingAgent, &agentArchived)
	if store.IsNoRows(err) {
		return TeamConflictNone, nil
	}
	if err != nil {
		return TeamConflictNone, err
	}
	if existingWorkspace != workspaceID {
		return TeamConflictOtherWorkspace, nil
	}
	if existingAgent != agentID {
		if agentArchived {
			return TeamConflictArchivedAgent, nil
		}
		return TeamConflictOtherAgent, nil
	}
	return TeamConflictNone, nil
}

func (s *Store) UpsertSlackInstallation(ctx context.Context, workspaceID, agentID, teamID string, botUserID *string, botTokenSealed, appTokenSealed []byte, installerAccountID string) (SlackInstallation, error) {
	var si SlackInstallation
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO slack_installations (workspace_id, operative_id, sl_team_id, sl_bot_user_id,
			sl_bot_token_sealed, sl_app_token_sealed, sl_installer_account_id, sl_status, sl_installed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', now())
		RETURNING id, workspace_id, operative_id, sl_team_id, sl_bot_user_id, sl_installer_account_id,
			sl_status, sl_installed_at, created_at, updated_at`,
		workspaceID, agentID, teamID, botUserID, botTokenSealed, appTokenSealed, installerAccountID,
	).Scan(&si.ID, &si.WorkspaceID, &si.AgentID, &si.TeamID, &si.BotUserID, &si.InstallerUserID,
		&si.Status, &si.InstalledAt, &si.CreatedAt, &si.UpdatedAt)
	if err != nil {
		return SlackInstallation{}, fmt.Errorf("integration: сохранение slack-установки: %w", err)
	}
	return si, nil
}

func (s *Store) DeleteSlackInstallation(ctx context.Context, workspaceID, id string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM slack_installations WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- Slack binding tokens ------------------------------------------------------

// SlackBindingToken — одноразовый токен привязки Slack-аккаунта к
// пользователю Goosar (320_slack_binding.up.sql). Выдаётся Slack-ботом вне
// этого контракта (см. server2/docs/decisions.md, раздел T-029, "Пробелы
// спецификации" — контракт документирует только redeem, не выпуск).
type SlackBindingToken struct {
	ID             string
	InstallationID string
	WorkspaceID    string
	SlackUserID    string
	Status         string
	ExpiresAt      time.Time
}

func (s *Store) RedeemSlackBindingToken(ctx context.Context, tokenHash string) (SlackBindingToken, bool, error) {
	var t SlackBindingToken
	err := s.db.Pool.QueryRow(ctx, `
		UPDATE slack_binding_tokens SET sbt_status = 'redeemed'
		WHERE sbt_token_hash = $1 AND sbt_status = 'pending' AND sbt_expires_at > now()
		RETURNING id, sbt_installation_id, sbt_workspace_id, sbt_slack_user_id, sbt_status, sbt_expires_at`,
		tokenHash,
	).Scan(&t.ID, &t.InstallationID, &t.WorkspaceID, &t.SlackUserID, &t.Status, &t.ExpiresAt)
	if store.IsNoRows(err) {
		return SlackBindingToken{}, false, nil
	}
	if err != nil {
		return SlackBindingToken{}, false, err
	}
	return t, true, nil
}

// SlackAccountAlreadyBound — сообщает, привязан ли slack_user_id уже к
// другому account_id в этом же installation.
func (s *Store) SlackAccountBoundTo(ctx context.Context, installationID, slackUserID string) (string, bool, error) {
	var accountID string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT account_id FROM slack_account_bindings WHERE sab_installation_id = $1 AND sab_slack_user_id = $2`,
		installationID, slackUserID).Scan(&accountID)
	if store.IsNoRows(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return accountID, true, nil
}

func (s *Store) BindSlackAccount(ctx context.Context, installationID, slackUserID, accountID string) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO slack_account_bindings (sab_installation_id, sab_slack_user_id, account_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (sab_installation_id, sab_slack_user_id) DO UPDATE SET account_id = EXCLUDED.account_id`,
		installationID, slackUserID, accountID)
	return err
}

// --- Composio ------------------------------------------------------------------

// ComposioConnection — schemas.ComposioConnection.
type ComposioConnection struct {
	ID          string
	WorkspaceID string
	AccountID   string
	ToolkitSlug string
	ExternalID  string
	Status      string
	ConnectedAt *time.Time
	LastUsedAt  *time.Time
}

func (s *Store) ListComposioConnections(ctx context.Context, accountID string) ([]ComposioConnection, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, workspace_id, account_id, cx_toolkit_slug, cx_external_connection_id, cx_status, cx_connected_at, cx_last_used_at
		FROM composio_connections WHERE account_id = $1 ORDER BY created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComposioConnection
	for rows.Next() {
		var c ComposioConnection
		var external *string
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.AccountID, &c.ToolkitSlug, &external, &c.Status, &c.ConnectedAt, &c.LastUsedAt); err != nil {
			return nil, err
		}
		if external != nil {
			c.ExternalID = *external
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) UpsertPendingComposioConnection(ctx context.Context, workspaceID, accountID, toolkitSlug string) (ComposioConnection, error) {
	var c ComposioConnection
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO composio_connections (workspace_id, account_id, cx_toolkit_slug, cx_status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (workspace_id, account_id, cx_toolkit_slug) DO UPDATE SET cx_status = 'pending'
		RETURNING id, workspace_id, account_id, cx_toolkit_slug, cx_status, cx_connected_at, cx_last_used_at`,
		workspaceID, accountID, toolkitSlug,
	).Scan(&c.ID, &c.WorkspaceID, &c.AccountID, &c.ToolkitSlug, &c.Status, &c.ConnectedAt, &c.LastUsedAt)
	if err != nil {
		return ComposioConnection{}, err
	}
	return c, nil
}

func (s *Store) DeleteComposioConnection(ctx context.Context, accountID, id string) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM composio_connections WHERE id = $1 AND account_id = $2`, id, accountID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- Task/issue linking (PR webhooks) ------------------------------------------

// TicketRef — минимум, нужный вебхук-обработчику, чтобы связать PR с задачей.
type TicketRef struct {
	ID          string
	WorkspaceID string
}

// FindTicketsByDisplayKeys ищет тикеты воркспейса по набору отображаемых
// ключей (T-123 и т.п.), извлечённых из заголовка/тела/имени ветки PR —
// contract: "пытается связать с issue по идентификаторам в заголовке/теле/ветке".
func (s *Store) FindTicketsByDisplayKeys(ctx context.Context, workspaceID string, keys []string) ([]TicketRef, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, workspace_id FROM tickets WHERE workspace_id = $1 AND upper(tk_display_key) = ANY($2)`,
		workspaceID, upperAll(keys))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketRef
	for rows.Next() {
		var t TicketRef
		if err := rows.Scan(&t.ID, &t.WorkspaceID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func upperAll(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strings.ToUpper(v)
	}
	return out
}

// PRLinkFields — поля вебхук-карточки PR, известные на момент приёма
// pull_request/merge_request события (остальные поля IssuePullRequestLink —
// checks_*/snapshot_*/additions-deletions-changed_files/mergeable* —
// заполняются отдельным пересчётом снапшота, который вне области этой
// сессии — см. internal/integration/webhooks.go про check_suite/check_run).
type PRLinkFields struct {
	RepoOwner       string
	RepoName        string
	Branch          string
	AuthorLogin     string
	AuthorAvatarURL string
}

// UpsertPRLink апсертит ticket_pr_links (005_tasks.up.sql, домен task) по
// (ticket_id, tpr_url) и публикует pull_request:updated самим вызывающим
// кодом (не здесь — этот метод только пишет строку).
func (s *Store) UpsertPRLink(ctx context.Context, ticketID, provider, url string, number int, title, prState *string, extra PRLinkFields) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO ticket_pr_links (ticket_id, tpr_provider, tpr_url, tpr_number, tpr_title, tpr_state,
		                              tpr_repo_owner, tpr_repo_name, tpr_branch, tpr_author_login, tpr_author_avatar_url,
		                              tpr_pr_updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), now())
		ON CONFLICT (ticket_id, tpr_url) DO UPDATE SET
		    tpr_title = EXCLUDED.tpr_title, tpr_state = EXCLUDED.tpr_state,
		    tpr_repo_owner = EXCLUDED.tpr_repo_owner, tpr_repo_name = EXCLUDED.tpr_repo_name,
		    tpr_branch = EXCLUDED.tpr_branch, tpr_author_login = EXCLUDED.tpr_author_login,
		    tpr_author_avatar_url = EXCLUDED.tpr_author_avatar_url, tpr_pr_updated_at = now()`,
		ticketID, provider, url, number, title, prState,
		extra.RepoOwner, extra.RepoName, extra.Branch, extra.AuthorLogin, extra.AuthorAvatarURL)
	return err
}

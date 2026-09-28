package misc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/adanman/goosar/server2/internal/store"
)

type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

// --- feedback ------------------------------------------------------------------

func (s *Store) CreateFeedback(ctx context.Context, accountID, workspaceID, message, url, kind string, clientMeta map[string]any) (id string, createdAt time.Time, err error) {
	var wsID any
	if workspaceID != "" {
		wsID = workspaceID
	}
	err = s.db.Pool.QueryRow(ctx, `
		INSERT INTO member_feedback (account_id, workspace_id, fb_message, fb_url, fb_kind, fb_client_meta)
		VALUES ($1, $2, $3, NULLIF($4,''), $5, $6)
		RETURNING id, created_at`,
		accountID, wsID, message, url, kind, clientMetaJSON(clientMeta),
	).Scan(&id, &createdAt)
	return id, createdAt, err
}

func clientMetaJSON(m map[string]any) []byte {
	if m == nil {
		return []byte("{}")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func (s *Store) FeedbackCountLastHour(ctx context.Context, accountID string) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM member_feedback WHERE account_id = $1 AND created_at > now() - interval '1 hour'`,
		accountID).Scan(&n)
	return n, err
}

// --- contact-sales ---------------------------------------------------------------

type ContactSalesRequest struct {
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	BusinessEmail   string `json:"business_email"`
	CompanyName     string `json:"company_name"`
	CompanySize     string `json:"company_size"`
	CountryRegion   string `json:"country_region"`
	UseCase         string `json:"use_case"`
	Goals           string `json:"goals"`
	Source          string `json:"source"`
	ConsentOutreach bool   `json:"consent_outreach"`
	ConsentUpdates  bool   `json:"consent_updates"`
}

func (s *Store) CreateContactLead(ctx context.Context, req ContactSalesRequest) (id string, createdAt time.Time, err error) {
	err = s.db.Pool.QueryRow(ctx, `
		INSERT INTO contact_leads (lead_first_name, lead_last_name, lead_business_email, lead_company_name,
			lead_company_size, lead_country_region, lead_use_case, lead_goals, lead_source,
			lead_consent_outreach, lead_consent_updates)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11)
		RETURNING id, created_at`,
		req.FirstName, req.LastName, req.BusinessEmail, req.CompanyName, req.CompanySize,
		req.CountryRegion, req.UseCase, req.Goals, req.Source, req.ConsentOutreach, req.ConsentUpdates,
	).Scan(&id, &createdAt)
	return id, createdAt, err
}

func (s *Store) ContactLeadCountLastHourByEmail(ctx context.Context, email string) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM contact_leads WHERE lower(lead_business_email) = lower($1) AND created_at > now() - interval '1 hour'`,
		email).Scan(&n)
	return n, err
}

// --- client-usage ------------------------------------------------------------------

type ClientUsageInput struct {
	AccountID     string
	InstallID     string
	Platform      string
	ClientVersion string
	ClientOS      string
	RuntimeProbe  map[string]any // nil, если платформа не desktop
}

func (s *Store) UpsertClientUsage(ctx context.Context, in ClientUsageInput) error {
	var probe any
	if in.RuntimeProbe != nil {
		probe = clientMetaJSON(in.RuntimeProbe)
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO install_usage_pings (account_id, cud_install_id, cud_platform, cud_client_version, cud_client_os, cud_runtime_probe)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6)
		ON CONFLICT (account_id, cud_install_id, cud_date) DO UPDATE SET
			cud_platform = EXCLUDED.cud_platform, cud_client_version = EXCLUDED.cud_client_version,
			cud_client_os = EXCLUDED.cud_client_os, cud_runtime_probe = EXCLUDED.cud_runtime_probe, updated_at = now()`,
		in.AccountID, in.InstallID, in.Platform, in.ClientVersion, in.ClientOS, probe)
	return err
}

// --- status ------------------------------------------------------------------------

type WorkspaceHeader struct {
	ID, Name, Slug string
}

func (s *Store) WorkspaceHeader(ctx context.Context, workspaceID string) (WorkspaceHeader, error) {
	var h WorkspaceHeader
	err := s.db.Pool.QueryRow(ctx, `SELECT id, ws_title, ws_slug FROM spaces WHERE id = $1`, workspaceID).
		Scan(&h.ID, &h.Name, &h.Slug)
	return h, err
}

type RuntimeSummary struct {
	Total, Online int
	Items         []RuntimeItem
}

type RuntimeItem struct {
	ID, Name, Provider, Status string
	LastSeenAt                 *time.Time
}

func (s *Store) RuntimeSummary(ctx context.Context, workspaceID string) (RuntimeSummary, error) {
	var sum RuntimeSummary
	err := s.db.Pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE ex_status = 'online') FROM executors WHERE workspace_id = $1`,
		workspaceID).Scan(&sum.Total, &sum.Online)
	if err != nil {
		return RuntimeSummary{}, err
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, ex_title, ex_provider, ex_status, ex_last_seen_at
		FROM executors WHERE workspace_id = $1 ORDER BY ex_last_seen_at DESC NULLS LAST LIMIT 20`, workspaceID)
	if err != nil {
		return RuntimeSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var it RuntimeItem
		if err := rows.Scan(&it.ID, &it.Name, &it.Provider, &it.Status, &it.LastSeenAt); err != nil {
			return RuntimeSummary{}, err
		}
		sum.Items = append(sum.Items, it)
	}
	return sum, rows.Err()
}

func (s *Store) ProvisioningPinCount(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `SELECT count(*) FROM provisioning_pins WHERE workspace_id = $1`, workspaceID).Scan(&n)
	return n, err
}

type McpServerSummary struct {
	Name      string
	Transport string
	Enabled   bool
}

func (s *Store) WorkspaceMcpServers(ctx context.Context, workspaceID string) ([]McpServerSummary, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT wmcp_name, wmcp_transport, wmcp_enabled FROM space_mcp_servers WHERE workspace_id = $1 ORDER BY created_at`,
		workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []McpServerSummary
	for rows.Next() {
		var m McpServerSummary
		if err := rows.Scan(&m.Name, &m.Transport, &m.Enabled); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type LLMConfig struct {
	BaseURL *string
	Model   *string
	HasKey  bool
	Origin  string // "workspace" | "none"
}

func (s *Store) WorkspaceLLMConfig(ctx context.Context, workspaceID string) (LLMConfig, error) {
	var cfg LLMConfig
	var keySealed []byte
	err := s.db.Pool.QueryRow(ctx, `
		SELECT cfg_llm_base_url, cfg_llm_model, cfg_llm_api_key_sealed FROM space_config WHERE workspace_id = $1`,
		workspaceID).Scan(&cfg.BaseURL, &cfg.Model, &keySealed)
	if store.IsNoRows(err) {
		cfg.Origin = "none"
		return cfg, nil
	}
	if err != nil {
		return LLMConfig{}, err
	}
	cfg.HasKey = len(keySealed) > 0
	if cfg.BaseURL != nil || cfg.Model != nil || cfg.HasKey {
		cfg.Origin = "workspace"
	} else {
		cfg.Origin = "none"
	}
	return cfg, nil
}

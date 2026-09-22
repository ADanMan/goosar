// Пакет composio — связка между Goosar и SDK Composio (server/pkg/composio):
// подписанное состояние connect-рукопожатия, локальное зеркало
// user_composio_connection, идемпотентное отключение и MCP-сессии пользователя.
package composio

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	sdk "github.com/adanman/goosar/server/pkg/composio"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

var (
	ErrToolkitNotSupported = errors.New("composio: toolkit not supported")

	ErrConnectNotSuccessful = errors.New("composio: connection was not successful")

	ErrConnectionNotFound = errors.New("composio: connection not found")

	ErrAccountVerification = errors.New("composio: connected account verification failed")
)

const defaultStateTTL = 5 * time.Minute

const defaultAuthCacheTTL = 5 * time.Minute

const (
	maxAuthConfigPages  = 20
	maxToolkitPages     = 20
	listPageLimit       = 1000
	composioLogoBaseURL = "https://logos.composio.dev/api"
)

type SDK interface {
	CreateLink(ctx context.Context, req sdk.CreateLinkRequest) (*sdk.CreateLinkResponse, error)
	ListConnectedAccounts(ctx context.Context, req sdk.ListConnectedAccountsRequest) (*sdk.ListConnectedAccountsResponse, error)
	ListAuthConfigs(ctx context.Context, req sdk.ListAuthConfigsRequest) (*sdk.ListAuthConfigsResponse, error)
	ListToolkits(ctx context.Context, req sdk.ListToolkitsRequest) (*sdk.ListToolkitsResponse, error)
	RevokeConnection(ctx context.Context, connectedAccountID string) error
	DeleteConnectedAccount(ctx context.Context, connectedAccountID string) error
	CreateSession(ctx context.Context, req sdk.CreateSessionRequest) (*sdk.CreateSessionResponse, error)
	MCPAuthHeaders() map[string]string
}

type Store interface {
	UpsertUserComposioConnection(ctx context.Context, arg db.UpsertUserComposioConnectionParams) (db.UserComposioConnection, error)
	ListActiveUserComposioConnections(ctx context.Context, userID pgtype.UUID) ([]db.UserComposioConnection, error)
	GetUserComposioConnection(ctx context.Context, arg db.GetUserComposioConnectionParams) (db.UserComposioConnection, error)
	MarkUserComposioConnectionRevoked(ctx context.Context, arg db.MarkUserComposioConnectionRevokedParams) error
}

type Config struct {
	StateSecret []byte

	CallbackBaseURL string

	FrontendBaseURL string

	StateTTL time.Duration

	AuthConfigTTL time.Duration

	Now func() time.Time
}

const callbackPath = "/api/integrations/composio/callback"

type Service struct {
	sdk         SDK
	store       Store
	secret      []byte
	callbackURL string
	frontendURL string
	stateTTL    time.Duration
	now         func() time.Time

	authCacheMu  sync.Mutex
	authCache    map[string]string
	authCacheExp time.Time
	authCacheTTL time.Duration
}

func NewService(client SDK, store Store, cfg Config) (*Service, error) {
	if client == nil {
		return nil, errors.New("composio: SDK client is required")
	}
	if store == nil {
		return nil, errors.New("composio: store is required")
	}
	if len(cfg.StateSecret) == 0 {
		return nil, errors.New("composio: StateSecret is required")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.CallbackBaseURL), "/")
	if base == "" {
		return nil, errors.New("composio: CallbackBaseURL is required")
	}

	ttl := cfg.StateTTL
	if ttl <= 0 {
		ttl = defaultStateTTL
	}
	authTTL := cfg.AuthConfigTTL
	if authTTL <= 0 {
		authTTL = defaultAuthCacheTTL
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Service{
		sdk:          client,
		store:        store,
		secret:       cfg.StateSecret,
		callbackURL:  base + callbackPath,
		frontendURL:  strings.TrimRight(strings.TrimSpace(cfg.FrontendBaseURL), "/"),
		stateTTL:     ttl,
		now:          now,
		authCacheTTL: authTTL,
	}, nil
}

type Connection struct {
	ID          string  `json:"id"`
	ToolkitSlug string  `json:"toolkit_slug"`
	Status      string  `json:"status"`
	ConnectedAt string  `json:"connected_at"`
	LastUsedAt  *string `json:"last_used_at"`
}

type MCPSession struct {
	URL     string
	Headers map[string]string
}

type ToolkitView struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	LogoURL     string `json:"logo,omitempty"`
	Category    string `json:"category,omitempty"`
	Connectable bool   `json:"connectable"`
}

func toolkitLogoURL(slug, upstreamLogoURL string) string {
	if upstreamLogoURL != "" {
		return upstreamLogoURL
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return ""
	}
	return composioLogoBaseURL + "/" + url.PathEscape(slug)
}

func (s *Service) BeginConnect(ctx context.Context, userID pgtype.UUID, toolkitSlug string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(toolkitSlug))
	authConfigID, err := s.authConfigForToolkit(ctx, slug)
	if err != nil {
		return "", err
	}
	if authConfigID == "" {
		return "", ErrToolkitNotSupported
	}
	if !userID.Valid {
		return "", errors.New("composio: invalid user id")
	}
	composioUserID := util.UUIDToString(userID)

	state, err := signState(s.secret, stateClaims{
		UserID:       composioUserID,
		ToolkitSlug:  slug,
		AuthConfigID: authConfigID,
		Exp:          s.now().Add(s.stateTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("composio: sign state: %w", err)
	}

	callbackURL := s.callbackURL + "?state=" + url.QueryEscape(state)

	resp, err := s.sdk.CreateLink(ctx, sdk.CreateLinkRequest{
		AuthConfigID: authConfigID,
		UserID:       composioUserID,
		CallbackURL:  callbackURL,
	})
	if err != nil {
		return "", fmt.Errorf("composio: create link: %w", err)
	}
	return resp.RedirectURL, nil
}

func (s *Service) CompleteCallback(ctx context.Context, state, status, connectedAccountID string) (string, error) {
	claims, err := verifyState(s.secret, state, s.now())
	if err != nil {
		return "", err
	}

	if !strings.EqualFold(strings.TrimSpace(status), "success") {

		return claims.ToolkitSlug, ErrConnectNotSuccessful
	}
	if strings.TrimSpace(connectedAccountID) == "" {
		return claims.ToolkitSlug, errors.New("composio: callback missing connected_account_id")
	}

	userID, err := util.ParseUUID(claims.UserID)
	if err != nil {
		return claims.ToolkitSlug, fmt.Errorf("composio: state has invalid user id: %w", err)
	}

	authConfigID := claims.AuthConfigID

	if err := s.verifyAccountOwnership(ctx, connectedAccountID, claims.UserID, authConfigID); err != nil {
		return claims.ToolkitSlug, err
	}

	if _, err := s.store.UpsertUserComposioConnection(ctx, db.UpsertUserComposioConnectionParams{
		UserID:             userID,
		ToolkitSlug:        claims.ToolkitSlug,
		AuthConfigID:       authConfigID,
		ConnectedAccountID: connectedAccountID,

		ComposioUserID: claims.UserID,
	}); err != nil {
		return claims.ToolkitSlug, fmt.Errorf("composio: upsert connection: %w", err)
	}
	return claims.ToolkitSlug, nil
}

func (s *Service) ListConnections(ctx context.Context, userID pgtype.UUID) ([]Connection, error) {
	rows, err := s.store.ListActiveUserComposioConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowToConnection(row))
	}
	return out, nil
}

func (s *Service) Disconnect(ctx context.Context, userID, connectionID pgtype.UUID) error {
	row, err := s.store.GetUserComposioConnection(ctx, db.GetUserComposioConnectionParams{
		ID:     connectionID,
		UserID: userID,
	})
	if err != nil {

		return ErrConnectionNotFound
	}

	if !strings.EqualFold(row.Status, "active") {
		return nil
	}

	if err := s.sdk.RevokeConnection(ctx, row.ConnectedAccountID); err != nil && !isNotFound(err) {
		return fmt.Errorf("composio: revoke connection: %w", err)
	}

	if err := s.sdk.DeleteConnectedAccount(ctx, row.ConnectedAccountID); err != nil && !isNotFound(err) {
		return fmt.Errorf("composio: delete connected account: %w", err)
	}

	if err := s.store.MarkUserComposioConnectionRevoked(ctx, db.MarkUserComposioConnectionRevokedParams{
		ID:     connectionID,
		UserID: userID,
	}); err != nil {
		return fmt.Errorf("composio: mark revoked: %w", err)
	}
	return nil
}

func (s *Service) CreateMCPSession(ctx context.Context, userID pgtype.UUID) (*MCPSession, error) {
	rows, err := s.store.ListActiveUserComposioConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	connectedAccounts := make(map[string]any, len(rows))
	for _, row := range rows {

		if _, exists := connectedAccounts[row.ToolkitSlug]; exists {
			continue
		}
		connectedAccounts[row.ToolkitSlug] = []string{row.ConnectedAccountID}
	}

	resp, err := s.sdk.CreateSession(ctx, sdk.CreateSessionRequest{
		UserID:            util.UUIDToString(userID),
		ConnectedAccounts: connectedAccounts,
	})
	if err != nil {
		return nil, fmt.Errorf("composio: create session: %w", err)
	}
	return &MCPSession{
		URL:     resp.MCP.URL,
		Headers: s.sdk.MCPAuthHeaders(),
	}, nil
}

func (s *Service) CallbackRedirect(slug string, success bool) string {
	var path string
	if success {
		path = "/settings?tab=integrations&connected=" + url.QueryEscape(slug)
	} else {
		path = "/settings?tab=integrations&error=composio_connect_failed"
	}
	return s.frontendURL + path
}

func rowToConnection(row db.UserComposioConnection) Connection {
	c := Connection{
		ID:          util.UUIDToString(row.ID),
		ToolkitSlug: row.ToolkitSlug,
		Status:      row.Status,
	}
	if row.ConnectedAt.Valid {
		c.ConnectedAt = row.ConnectedAt.Time.UTC().Format(time.RFC3339)
	}
	c.LastUsedAt = util.TimestampToPtr(row.LastUsedAt)
	return c
}

func (s *Service) ListToolkits(ctx context.Context) ([]ToolkitView, error) {

	connectable, err := s.authConfigMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("composio: resolve connectable toolkits: %w", err)
	}

	out := []ToolkitView{}
	seen := make(map[string]struct{})
	cursor := ""
	for page := 0; page < maxToolkitPages; page++ {
		resp, err := s.sdk.ListToolkits(ctx, sdk.ListToolkitsRequest{
			Limit:  listPageLimit,
			Cursor: cursor,
			SortBy: "usage",
		})
		if err != nil {
			return nil, fmt.Errorf("composio: list toolkits: %w", err)
		}
		for _, tk := range resp.Items {
			slug := strings.ToLower(strings.TrimSpace(tk.Slug))
			if slug == "" {
				continue
			}
			if _, dup := seen[slug]; dup {
				continue
			}
			seen[slug] = struct{}{}

			if _, canConnect := connectable[slug]; !canConnect {
				continue
			}
			category := ""
			if len(tk.Categories) > 0 {
				category = tk.Categories[0]
			}
			out = append(out, ToolkitView{
				Slug:     tk.Slug,
				Name:     tk.Name,
				LogoURL:  toolkitLogoURL(slug, tk.LogoURL),
				Category: category,

				Connectable: true,
			})
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	return out, nil
}

func (s *Service) authConfigForToolkit(ctx context.Context, slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return "", nil
	}
	m, err := s.authConfigMap(ctx)
	if err != nil {
		return "", err
	}
	return m[slug], nil
}

func (s *Service) authConfigMap(ctx context.Context) (map[string]string, error) {
	s.authCacheMu.Lock()
	defer s.authCacheMu.Unlock()
	if s.authCache != nil && s.now().Before(s.authCacheExp) {
		return s.authCache, nil
	}
	m, err := s.fetchAuthConfigMap(ctx)
	if err != nil {

		if s.authCache != nil {
			return s.authCache, nil
		}
		return nil, err
	}
	s.authCache = m
	s.authCacheExp = s.now().Add(s.authCacheTTL)
	return m, nil
}

type authCandidate struct {
	id      string
	managed bool
	updated string
}

func (s *Service) fetchAuthConfigMap(ctx context.Context) (map[string]string, error) {
	best := make(map[string]authCandidate)
	cursor := ""
	for page := 0; page < maxAuthConfigPages; page++ {
		resp, err := s.sdk.ListAuthConfigs(ctx, sdk.ListAuthConfigsRequest{
			ShowDisabled: false,
			Limit:        listPageLimit,
			Cursor:       cursor,
		})
		if err != nil {
			return nil, fmt.Errorf("composio: list auth configs: %w", err)
		}
		for _, ac := range resp.Items {
			if ac.ID == "" || strings.EqualFold(ac.Status, "DISABLED") {
				continue
			}
			slug := strings.ToLower(strings.TrimSpace(ac.Toolkit.Slug))
			if slug == "" {
				continue
			}
			cand := authCandidate{id: ac.ID, managed: ac.IsComposioManaged, updated: ac.LastUpdatedAt}
			if cur, ok := best[slug]; !ok || betterAuthConfig(cand, cur) {
				best[slug] = cand
			}
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	out := make(map[string]string, len(best))
	for slug, c := range best {
		out[slug] = c.id
	}
	return out, nil
}

func betterAuthConfig(a, b authCandidate) bool {
	if a.managed != b.managed {
		return !a.managed
	}
	return a.updated > b.updated
}

func (s *Service) verifyAccountOwnership(ctx context.Context, connectedAccountID, expectedUserID, expectedAuthConfigID string) error {
	resp, err := s.sdk.ListConnectedAccounts(ctx, sdk.ListConnectedAccountsRequest{
		ConnectedAccountIDs: []string{connectedAccountID},
	})
	if err != nil {
		return fmt.Errorf("composio: verify connected account: %w", err)
	}
	var acct *sdk.ConnectedAccount
	for i := range resp.Items {
		if resp.Items[i].ID == connectedAccountID {
			acct = &resp.Items[i]
			break
		}
	}
	if acct == nil {
		return ErrAccountVerification
	}
	if acct.UserID != expectedUserID {
		return ErrAccountVerification
	}

	accountAuthConfigID := acct.AuthConfigID
	if accountAuthConfigID == "" {
		accountAuthConfigID = acct.AuthConfig.ID
	}
	if expectedAuthConfigID == "" || accountAuthConfigID != expectedAuthConfigID {
		return ErrAccountVerification
	}
	return nil
}

func isNotFound(err error) bool {
	var apiErr *sdk.APIError
	return errors.As(err, &apiErr) && apiErr.IsNotFound()
}

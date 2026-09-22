package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	sdk "github.com/adanman/goosar/server/pkg/composio"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type fakeSDK struct {
	createLinkResp  *sdk.CreateLinkResponse
	createLinkErr   error
	lastCreateLink  sdk.CreateLinkRequest
	revoked         []string
	revokeErr       error
	deleted         []string
	deleteErr       error
	createSessResp  *sdk.CreateSessionResponse
	createSessErr   error
	lastSessReq     sdk.CreateSessionRequest
	createSessCalls int

	acctUserID             string
	acctAuthConfigID       string
	acctNestedAuthConfigID string
	acctMissing            bool
	listAccountsErr        error
	lastListAccounts       sdk.ListConnectedAccountsRequest

	authConfigs    []sdk.AuthConfig
	authConfigsSet bool
	listAuthErr    error

	toolkits        []sdk.Toolkit
	listToolkitsErr error
}

func (f *fakeSDK) CreateLink(_ context.Context, req sdk.CreateLinkRequest) (*sdk.CreateLinkResponse, error) {
	f.lastCreateLink = req
	if f.createLinkErr != nil {
		return nil, f.createLinkErr
	}
	if f.createLinkResp != nil {
		return f.createLinkResp, nil
	}
	return &sdk.CreateLinkResponse{RedirectURL: "https://composio.example/redirect", ConnectedAccountID: "ca_pending"}, nil
}

func (f *fakeSDK) ListConnectedAccounts(_ context.Context, req sdk.ListConnectedAccountsRequest) (*sdk.ListConnectedAccountsResponse, error) {
	f.lastListAccounts = req
	if f.listAccountsErr != nil {
		return nil, f.listAccountsErr
	}
	if f.acctMissing {
		return &sdk.ListConnectedAccountsResponse{}, nil
	}
	id := ""
	if len(req.ConnectedAccountIDs) > 0 {
		id = req.ConnectedAccountIDs[0]
	}
	return &sdk.ListConnectedAccountsResponse{Items: []sdk.ConnectedAccount{{
		ID:           id,
		UserID:       f.acctUserID,
		AuthConfigID: f.acctAuthConfigID,
		AuthConfig:   sdk.AuthConfigRef{ID: f.acctNestedAuthConfigID},
	}}}, nil
}

func (f *fakeSDK) ListAuthConfigs(_ context.Context, _ sdk.ListAuthConfigsRequest) (*sdk.ListAuthConfigsResponse, error) {
	if f.listAuthErr != nil {
		return nil, f.listAuthErr
	}
	items := f.authConfigs
	if !f.authConfigsSet && items == nil {
		items = []sdk.AuthConfig{{
			ID:                "ac_notion",
			Toolkit:           sdk.Toolkit{Slug: "notion"},
			Status:            "ENABLED",
			IsComposioManaged: true,
		}}
	}
	return &sdk.ListAuthConfigsResponse{Items: items}, nil
}

func (f *fakeSDK) ListToolkits(_ context.Context, _ sdk.ListToolkitsRequest) (*sdk.ListToolkitsResponse, error) {
	if f.listToolkitsErr != nil {
		return nil, f.listToolkitsErr
	}
	return &sdk.ListToolkitsResponse{Items: f.toolkits}, nil
}

func (f *fakeSDK) RevokeConnection(_ context.Context, id string) error {
	f.revoked = append(f.revoked, id)
	return f.revokeErr
}

func (f *fakeSDK) DeleteConnectedAccount(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}

func (f *fakeSDK) CreateSession(_ context.Context, req sdk.CreateSessionRequest) (*sdk.CreateSessionResponse, error) {
	f.createSessCalls++
	f.lastSessReq = req
	if f.createSessErr != nil {
		return nil, f.createSessErr
	}
	if f.createSessResp != nil {
		return f.createSessResp, nil
	}
	return &sdk.CreateSessionResponse{MCP: sdk.MCPDescriptor{URL: "https://mcp.example/session"}}, nil
}

func (f *fakeSDK) MCPAuthHeaders() map[string]string {
	return map[string]string{"x-api-key": "secret"}
}

type fakeStore struct {
	rows   []db.UserComposioConnection
	nextID byte
}

func newFakeStore() *fakeStore { return &fakeStore{nextID: 1} }

func (s *fakeStore) UpsertUserComposioConnection(_ context.Context, arg db.UpsertUserComposioConnectionParams) (db.UserComposioConnection, error) {
	for i := range s.rows {
		if uuidEqual(s.rows[i].UserID, arg.UserID) && s.rows[i].ConnectedAccountID == arg.ConnectedAccountID {
			s.rows[i].ToolkitSlug = arg.ToolkitSlug
			s.rows[i].AuthConfigID = arg.AuthConfigID
			s.rows[i].ComposioUserID = arg.ComposioUserID
			s.rows[i].Status = "active"
			s.rows[i].UpdatedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			return s.rows[i], nil
		}
	}
	row := db.UserComposioConnection{
		ID:                 mintUUID(s.nextID),
		UserID:             arg.UserID,
		ToolkitSlug:        arg.ToolkitSlug,
		AuthConfigID:       arg.AuthConfigID,
		ConnectedAccountID: arg.ConnectedAccountID,
		ComposioUserID:     arg.ComposioUserID,
		Status:             "active",
		ConnectedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	s.nextID++
	s.rows = append(s.rows, row)
	return row, nil
}

func (s *fakeStore) ListActiveUserComposioConnections(_ context.Context, userID pgtype.UUID) ([]db.UserComposioConnection, error) {
	out := []db.UserComposioConnection{}
	for _, r := range s.rows {
		if uuidEqual(r.UserID, userID) && r.Status == "active" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *fakeStore) GetUserComposioConnection(_ context.Context, arg db.GetUserComposioConnectionParams) (db.UserComposioConnection, error) {
	for _, r := range s.rows {
		if uuidEqual(r.ID, arg.ID) && uuidEqual(r.UserID, arg.UserID) {
			return r, nil
		}
	}
	return db.UserComposioConnection{}, pgx.ErrNoRows
}

func (s *fakeStore) MarkUserComposioConnectionRevoked(_ context.Context, arg db.MarkUserComposioConnectionRevokedParams) error {
	for i := range s.rows {
		if uuidEqual(s.rows[i].ID, arg.ID) && uuidEqual(s.rows[i].UserID, arg.UserID) {
			s.rows[i].Status = "revoked"
		}
	}
	return nil
}

func uuidEqual(a, b pgtype.UUID) bool { return a.Valid && b.Valid && a.Bytes == b.Bytes }

func mintUUID(n byte) pgtype.UUID {
	var b [16]byte
	b[15] = n
	return pgtype.UUID{Bytes: b, Valid: true}
}

func newTestService(t *testing.T, client SDK, store Store) *Service {
	t.Helper()
	svc, err := NewService(client, store, Config{
		StateSecret:     testSecret,
		CallbackBaseURL: "https://goosar.ru",
		FrontendBaseURL: "https://goosar.ru",
		Now:             func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestNewService_Validation(t *testing.T) {
	t.Parallel()
	if _, err := NewService(nil, newFakeStore(), Config{StateSecret: testSecret, CallbackBaseURL: "x"}); err == nil {
		t.Error("expected error for nil client")
	}
	if _, err := NewService(&fakeSDK{}, nil, Config{StateSecret: testSecret, CallbackBaseURL: "x"}); err == nil {
		t.Error("expected error for nil store")
	}
	if _, err := NewService(&fakeSDK{}, newFakeStore(), Config{CallbackBaseURL: "x"}); err == nil {
		t.Error("expected error for empty secret")
	}
	if _, err := NewService(&fakeSDK{}, newFakeStore(), Config{StateSecret: testSecret}); err == nil {
		t.Error("expected error for empty callback base")
	}
}

func TestBeginConnect_MappingAndState(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, newFakeStore())
	userID := mintUUID(7)

	redirect, err := svc.BeginConnect(context.Background(), userID, "Notion")
	if err != nil {
		t.Fatalf("BeginConnect: %v", err)
	}
	if redirect != "https://composio.example/redirect" {
		t.Errorf("redirect = %q", redirect)
	}

	if sdkFake.lastCreateLink.AuthConfigID != "ac_notion" {
		t.Errorf("auth config = %q", sdkFake.lastCreateLink.AuthConfigID)
	}

	if sdkFake.lastCreateLink.UserID != util.UUIDToString(userID) {
		t.Errorf("composio user id = %q, want %q", sdkFake.lastCreateLink.UserID, util.UUIDToString(userID))
	}

	cb := sdkFake.lastCreateLink.CallbackURL
	if !strings.HasPrefix(cb, "https://goosar.ru"+callbackPath+"?state=") {
		t.Fatalf("callback url = %q", cb)
	}
	u, _ := url.Parse(cb)
	state := u.Query().Get("state")
	claims, err := verifyState(testSecret, state, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("state did not verify: %v", err)
	}
	if claims.ToolkitSlug != "notion" || claims.UserID != util.UUIDToString(userID) {
		t.Errorf("claims = %+v", claims)
	}

	if claims.AuthConfigID != "ac_notion" {
		t.Errorf("state auth config = %q, want ac_notion", claims.AuthConfigID)
	}
}

func TestBeginConnect_UnsupportedToolkit(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeSDK{}, newFakeStore())
	if _, err := svc.BeginConnect(context.Background(), mintUUID(1), "github"); !errors.Is(err, ErrToolkitNotSupported) {
		t.Fatalf("expected ErrToolkitNotSupported, got %v", err)
	}
}

func TestBeginConnect_UnsupportedWhenNoAuthConfig(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{authConfigsSet: true, authConfigs: []sdk.AuthConfig{}}
	svc := newTestService(t, sdkFake, newFakeStore())
	if _, err := svc.BeginConnect(context.Background(), mintUUID(1), "notion"); !errors.Is(err, ErrToolkitNotSupported) {
		t.Fatalf("expected ErrToolkitNotSupported with no auth configs, got %v", err)
	}
}

func TestBeginConnect_PrefersCustomAuthConfig(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{authConfigsSet: true, authConfigs: []sdk.AuthConfig{
		{ID: "ac_managed", Toolkit: sdk.Toolkit{Slug: "notion"}, Status: "ENABLED", IsComposioManaged: true},
		{ID: "ac_custom", Toolkit: sdk.Toolkit{Slug: "notion"}, Status: "ENABLED", IsComposioManaged: false},
	}}
	svc := newTestService(t, sdkFake, newFakeStore())
	if _, err := svc.BeginConnect(context.Background(), mintUUID(1), "notion"); err != nil {
		t.Fatalf("BeginConnect: %v", err)
	}
	if sdkFake.lastCreateLink.AuthConfigID != "ac_custom" {
		t.Errorf("auth config = %q, want ac_custom (custom preferred over managed)", sdkFake.lastCreateLink.AuthConfigID)
	}
}

func TestListToolkits_FiltersToConnectable(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		authConfigsSet: true,
		authConfigs: []sdk.AuthConfig{
			{ID: "ac_notion", Toolkit: sdk.Toolkit{Slug: "notion"}, Status: "ENABLED"},
			{ID: "ac_slack", Toolkit: sdk.Toolkit{Slug: "slack"}, Status: "ENABLED"},
		},
		toolkits: []sdk.Toolkit{
			{Slug: "github", Name: "GitHub", LogoURL: "https://logo/gh", Categories: []string{"dev"}},
			{Slug: "notion", Name: "Notion", LogoURL: "https://logo/notion", Categories: []string{"productivity"}},
			{Slug: "slack", Name: "Slack"},
		},
	}
	svc := newTestService(t, sdkFake, newFakeStore())
	tks, err := svc.ListToolkits(context.Background())
	if err != nil {
		t.Fatalf("ListToolkits: %v", err)
	}

	if len(tks) != 2 {
		t.Fatalf("expected 2 connectable toolkits, got %d: %+v", len(tks), tks)
	}
	bySlug := make(map[string]ToolkitView, len(tks))
	for _, tk := range tks {
		if !tk.Connectable {
			t.Errorf("surfaced toolkit %q must be connectable", tk.Slug)
		}
		if tk.Slug == "github" {
			t.Errorf("non-connectable toolkit %q should have been filtered out", tk.Slug)
		}
		bySlug[tk.Slug] = tk
	}
	notion, ok := bySlug["notion"]
	if !ok {
		t.Fatalf("expected notion in results: %+v", tks)
	}
	if notion.Name != "Notion" || notion.LogoURL != "https://logo/notion" || notion.Category != "productivity" {
		t.Errorf("notion fields not mapped: %+v", notion)
	}

	if slack, ok := bySlug["slack"]; !ok || slack.LogoURL != "https://logos.composio.dev/api/slack" {
		t.Errorf("slack default logo = %+v", bySlug["slack"])
	}
}

func TestListToolkits_ResolverErrorReturnsError(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		listAuthErr: errors.New("upstream blip"),
		toolkits:    []sdk.Toolkit{{Slug: "notion", Name: "Notion"}},
	}
	svc := newTestService(t, sdkFake, newFakeStore())
	tks, err := svc.ListToolkits(context.Background())
	if err == nil {
		t.Fatalf("ListToolkits should fail on auth-config resolver error, got %+v", tks)
	}
	if tks != nil {
		t.Errorf("expected nil toolkits on error, got %+v", tks)
	}
}

func TestCompleteCallback_SuccessAndIdempotent(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(3)

	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(userID), acctAuthConfigID: "ac_notion"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:       util.UUIDToString(userID),
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})

	slug, err := svc.CompleteCallback(context.Background(), state, "success", "ca_123")
	if err != nil {
		t.Fatalf("CompleteCallback: %v", err)
	}
	if slug != "notion" {
		t.Errorf("slug = %q", slug)
	}

	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_123"); err != nil {
		t.Fatalf("second CompleteCallback: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected 1 row after duplicate callback, got %d", len(store.rows))
	}
	row := store.rows[0]
	if row.ComposioUserID != util.UUIDToString(userID) {
		t.Errorf("composio_user_id invariant broken: %q", row.ComposioUserID)
	}
	if row.AuthConfigID != "ac_notion" || row.ToolkitSlug != "notion" || row.Status != "active" {
		t.Errorf("row = %+v", row)
	}
}

func TestCompleteCallback_AcceptsNestedAuthConfig(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(30)

	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(userID), acctNestedAuthConfigID: "ac_notion"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:       util.UUIDToString(userID),
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})

	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_nested"); err != nil {
		t.Fatalf("CompleteCallback: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(store.rows))
	}
}

func TestCompleteCallback_NonSuccessNoRow(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := newTestService(t, &fakeSDK{}, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:      util.UUIDToString(mintUUID(4)),
		ToolkitSlug: "notion",
		Exp:         time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	slug, err := svc.CompleteCallback(context.Background(), state, "failed", "ca_x")
	if !errors.Is(err, ErrConnectNotSuccessful) {
		t.Fatalf("expected ErrConnectNotSuccessful, got %v", err)
	}
	if slug != "notion" {
		t.Errorf("slug = %q (should still be returned for redirect)", slug)
	}
	if len(store.rows) != 0 {
		t.Fatalf("expected no row written on non-success, got %d", len(store.rows))
	}
}

func TestCompleteCallback_BadState(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeSDK{}, newFakeStore())
	if _, err := svc.CompleteCallback(context.Background(), "garbage", "success", "ca_1"); err == nil {
		t.Fatal("expected error for malformed state")
	}
}

func TestCompleteCallback_TamperedAccountRejected(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(20)

	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(mintUUID(99)), acctAuthConfigID: "ac_notion"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:       util.UUIDToString(userID),
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_evil"); !errors.Is(err, ErrAccountVerification) {
		t.Fatalf("expected ErrAccountVerification for foreign account, got %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("no row should be written when ownership fails, got %d", len(store.rows))
	}
}

func TestCompleteCallback_WrongAuthConfigRejected(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(21)

	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(userID), acctAuthConfigID: "ac_other"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:       util.UUIDToString(userID),
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_x"); !errors.Is(err, ErrAccountVerification) {
		t.Fatalf("expected ErrAccountVerification for wrong auth config, got %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("no row should be written, got %d", len(store.rows))
	}
}

func TestCompleteCallback_MissingAuthConfigFailsClosed(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(25)

	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(userID), acctAuthConfigID: "ac_notion"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:      util.UUIDToString(userID),
		ToolkitSlug: "notion",

		Exp: time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_owned"); !errors.Is(err, ErrAccountVerification) {
		t.Fatalf("expected ErrAccountVerification when state carries no auth config, got %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("no row should be written, got %d", len(store.rows))
	}
}

func TestCompleteCallback_UnknownAccountRejected(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(22)
	sdkFake := &fakeSDK{acctMissing: true}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:      util.UUIDToString(userID),
		ToolkitSlug: "notion",
		Exp:         time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_ghost"); !errors.Is(err, ErrAccountVerification) {
		t.Fatalf("expected ErrAccountVerification for unknown account, got %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("no row should be written, got %d", len(store.rows))
	}
}

func TestListConnections(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := newTestService(t, &fakeSDK{}, store)
	userID := mintUUID(5)
	seedActive(store, userID, "notion", "ca_a")

	conns, err := svc.ListConnections(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != 1 || conns[0].ToolkitSlug != "notion" || conns[0].Status != "active" {
		t.Fatalf("conns = %+v", conns)
	}
}

func TestDisconnect_OwnerRevokeIdempotentAndFilter(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, store)
	userID := mintUUID(6)
	row := seedActive(store, userID, "notion", "ca_z")

	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if len(sdkFake.revoked) != 1 || sdkFake.revoked[0] != "ca_z" {
		t.Errorf("revoked = %v", sdkFake.revoked)
	}

	conns, _ := svc.ListConnections(context.Background(), userID)
	if len(conns) != 0 {
		t.Errorf("expected 0 active after disconnect, got %d", len(conns))
	}

	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("idempotent Disconnect: %v", err)
	}
}

func TestDisconnect_RevokedRowNoOp(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, store)
	userID := mintUUID(30)
	row := seedActive(store, userID, "notion", "ca_noop")

	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("first Disconnect: %v", err)
	}
	if len(sdkFake.revoked) != 1 {
		t.Fatalf("expected 1 upstream revoke, got %d", len(sdkFake.revoked))
	}

	sdkFake.revokeErr = &sdk.APIError{HTTPStatus: http.StatusInternalServerError}
	sdkFake.deleteErr = &sdk.APIError{HTTPStatus: http.StatusInternalServerError}
	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("second Disconnect on already-revoked row should be a no-op, got %v", err)
	}
	if len(sdkFake.revoked) != 1 {
		t.Errorf("second disconnect must not call upstream revoke again, revoked=%v", sdkFake.revoked)
	}
}

func TestDisconnect_UpstreamNotFoundIsIdempotent(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sdkFake := &fakeSDK{revokeErr: &sdk.APIError{HTTPStatus: http.StatusNotFound}}
	svc := newTestService(t, sdkFake, store)
	userID := mintUUID(8)
	row := seedActive(store, userID, "notion", "ca_404")

	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("Disconnect should treat upstream 404 as success, got %v", err)
	}
}

func TestDisconnect_NotOwner(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := newTestService(t, &fakeSDK{}, store)
	owner := mintUUID(9)
	row := seedActive(store, owner, "notion", "ca_o")
	attacker := mintUUID(10)
	if err := svc.Disconnect(context.Background(), attacker, row.ID); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("expected ErrConnectionNotFound for non-owner, got %v", err)
	}
}

func TestCreateMCPSession_NoOpWhenEmpty(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, newFakeStore())
	sess, err := svc.CreateMCPSession(context.Background(), mintUUID(11))
	if err != nil {
		t.Fatalf("CreateMCPSession: %v", err)
	}
	if sess != nil {
		t.Fatalf("expected nil session when no connections, got %+v", sess)
	}
	if sdkFake.createSessCalls != 0 {
		t.Errorf("CreateSession should not be called when there are no connections")
	}
}

func TestCreateMCPSession_PinsConnectedAccounts(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, store)
	userID := mintUUID(12)
	seedActive(store, userID, "notion", "ca_pin")

	sess, err := svc.CreateMCPSession(context.Background(), userID)
	if err != nil {
		t.Fatalf("CreateMCPSession: %v", err)
	}
	if sess == nil || sess.URL != "https://mcp.example/session" {
		t.Fatalf("session = %+v", sess)
	}
	if sess.Headers["x-api-key"] != "secret" {
		t.Errorf("headers = %+v", sess.Headers)
	}
	if sdkFake.lastSessReq.UserID != util.UUIDToString(userID) {
		t.Errorf("session user id = %q", sdkFake.lastSessReq.UserID)
	}
	assertPinnedAccount(t, sdkFake.lastSessReq, "notion", "ca_pin")
}

func TestCallbackRedirect(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeSDK{}, newFakeStore())
	if got := svc.CallbackRedirect("notion", true); got != "https://goosar.ru/settings?tab=integrations&connected=notion" {
		t.Errorf("success redirect = %q", got)
	}
	if got := svc.CallbackRedirect("notion", false); got != "https://goosar.ru/settings?tab=integrations&error=composio_connect_failed" {
		t.Errorf("failure redirect = %q", got)
	}
}

func seedActive(store *fakeStore, userID pgtype.UUID, slug, caID string) db.UserComposioConnection {
	row, _ := store.UpsertUserComposioConnection(context.Background(), db.UpsertUserComposioConnectionParams{
		UserID:             userID,
		ToolkitSlug:        slug,
		AuthConfigID:       "ac_notion",
		ConnectedAccountID: caID,
		ComposioUserID:     util.UUIDToString(userID),
	})
	return row
}

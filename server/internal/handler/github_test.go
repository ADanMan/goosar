package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func TestExtractIdentifiers(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "branch_name",
			in:   []string{"", "", "mul-1510/fix-login"},
			want: []string{"MUL-1510"},
		},
		{
			name: "title_and_body",
			in:   []string{"Fix MUL-82", "Closes MUL-1510 and ABC-7", ""},
			want: []string{"MUL-82", "MUL-1510", "ABC-7"},
		},
		{
			name: "dedupe_across_fields",
			in:   []string{"MUL-1", "MUL-1 again", "mul-1/branch"},
			want: []string{"MUL-1"},
		},
		{
			name: "ignore_email_and_versions",
			in:   []string{"reply@user-1 v1.2-3 here", "", ""},

			want: []string{"USER-1"},
		},
		{
			name: "no_match",
			in:   []string{"plain text", "no idents", ""},
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractIdentifiers(tc.in...)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractIdentifiers() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractClosingIdentifiers(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "single_closes",
			in:   []string{"", "Closes MUL-1"},
			want: []string{"MUL-1"},
		},
		{
			name: "all_keyword_inflections",
			in: []string{
				"",
				"close MUL-1\nclosed MUL-2\ncloses MUL-3\nfix MUL-4\nfixes MUL-5\nfixed MUL-6\nresolve MUL-7\nresolves MUL-8\nresolved MUL-9",
			},
			want: []string{"MUL-1", "MUL-2", "MUL-3", "MUL-4", "MUL-5", "MUL-6", "MUL-7", "MUL-8", "MUL-9"},
		},
		{
			name: "case_insensitive_and_colon",
			in:   []string{"CLOSES: MUL-1", "Fixes:MUL-2 resolves   MUL-3"},
			want: []string{"MUL-1", "MUL-2", "MUL-3"},
		},
		{
			name: "bare_reference_does_not_close",

			in:   []string{"ABC-1: Lorem Ipsum", "Closes ABC-1. Follow up work planned in ABC-2. Unblocks ABC-3."},
			want: []string{"ABC-1"},
		},
		{
			name: "keyword_not_adjacent_does_not_close",

			in:   []string{"Fix login MUL-1", ""},
			want: []string{},
		},
		{
			name: "dedupe_across_fields",
			in:   []string{"Closes MUL-1", "fixes mul-1"},
			want: []string{"MUL-1"},
		},
		{
			name: "no_match_on_disclosed_or_foreclose",

			in:   []string{"Disclosed MUL-1 in foreclose MUL-2", ""},
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractClosingIdentifiers(tc.in...)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractClosingIdentifiers() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDerivePRState(t *testing.T) {
	cases := []struct {
		state  string
		draft  bool
		merged bool
		want   string
	}{
		{"open", false, false, "open"},
		{"open", true, false, "draft"},
		{"closed", false, false, "closed"},
		{"closed", false, true, "merged"},
		{"closed", true, true, "merged"},
	}
	for _, tc := range cases {
		got := derivePRState(tc.state, tc.draft, tc.merged)
		if got != tc.want {
			t.Errorf("derivePRState(%q, draft=%v, merged=%v) = %q, want %q",
				tc.state, tc.draft, tc.merged, got, tc.want)
		}
	}
}

func TestIssuePullRequestResponseHidesUnavailableSnapshot(t *testing.T) {
	fetchedAt := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	row := db.ListPullRequestsByIssueRow{
		State:               "open",
		HeadSha:             "B",
		SnapshotHeadSha:     "A",
		SnapshotFetchedAt:   fetchedAt,
		ApiMergeable:        pgtype.Text{String: "CONFLICTING", Valid: true},
		ApiMergeStateStatus: pgtype.Text{String: "DIRTY", Valid: true},
		ChecksRollupState:   pgtype.Text{String: "FAILURE", Valid: true},
		ChecksTotal:         1,
		ChecksFailed:        1,
		FailedCheckNames:    []string{"backend"},
	}

	resp := issuePullRequestRowToResponse(row, true)
	if resp.SnapshotAvailable == nil || *resp.SnapshotAvailable {
		t.Fatal("mismatched-head snapshot must be marked unavailable")
	}
	if resp.Mergeable != nil || resp.ChecksRollup != nil || resp.ChecksFailed != 0 {
		t.Fatalf("mismatched-head snapshot leaked into response: %+v", resp)
	}

	row.SnapshotHeadSha = "B"
	resp = issuePullRequestRowToResponse(row, false)
	if resp.SnapshotAvailable == nil || *resp.SnapshotAvailable {
		t.Fatal("disabled snapshot feature must be marked unavailable")
	}
	if resp.Mergeable != nil || resp.ChecksRollup != nil || resp.ChecksFailed != 0 {
		t.Fatalf("disabled feature exposed last-known snapshot: %+v", resp)
	}

	resp = issuePullRequestRowToResponse(row, true)
	if resp.SnapshotAvailable == nil || !*resp.SnapshotAvailable {
		t.Fatal("enabled current-head snapshot must be available")
	}
	if resp.Mergeable == nil || *resp.Mergeable != "conflicting" || resp.ChecksFailed != 1 {
		t.Fatalf("current snapshot was not exposed: %+v", resp)
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "shared-secret"
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !verifyWebhookSignature(secret, good, body) {
		t.Error("expected valid signature to verify")
	}
	if verifyWebhookSignature(secret, "sha256=deadbeef", body) {
		t.Error("expected bad hex to fail")
	}
	if verifyWebhookSignature(secret, "", body) {
		t.Error("expected empty header to fail")
	}
	if verifyWebhookSignature(secret, "sha1=whatever", body) {
		t.Error("expected non-sha256 prefix to fail")
	}
	if verifyWebhookSignature("other-secret", good, body) {
		t.Error("expected wrong secret to fail")
	}
}

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	wsID := "11111111-2222-3333-4444-555555555555"

	tok, err := signState(wsID)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	if parts := strings.Split(tok, "."); len(parts) != 3 {
		t.Fatalf("default return state has %d parts, want legacy 3-part format", len(parts))
	}
	got, ok := verifyState(tok)
	if !ok {
		t.Fatal("verifyState rejected a freshly-signed token")
	}
	if got != wsID {
		t.Errorf("verifyState() = %q, want %q", got, wsID)
	}

	tampered := "01111111" + tok[8:]
	if _, ok := verifyState(tampered); ok {
		t.Error("tampered state token should fail to verify")
	}

	t.Setenv("GITHUB_WEBHOOK_SECRET", "different")
	if _, ok := verifyState(tok); ok {
		t.Error("token signed with old secret should fail under a new one")
	}
}

func TestStateRoundTripWithRepositoryReturnTarget(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	wsID := "11111111-2222-3333-4444-555555555555"

	tok, err := signStateForReturn(wsID, githubReturnToRepositories)
	if err != nil {
		t.Fatalf("signStateForReturn: %v", err)
	}
	if parts := strings.Split(tok, "."); len(parts) != 4 {
		t.Fatalf("repository return state has %d parts, want 4", len(parts))
	}
	gotWorkspaceID, gotReturnTo, ok := verifyStateWithReturn(tok)
	if !ok {
		t.Fatal("verifyStateWithReturn rejected a freshly-signed token")
	}
	if gotWorkspaceID != wsID || gotReturnTo != githubReturnToRepositories {
		t.Errorf(
			"verifyStateWithReturn() = (%q, %q), want (%q, %q)",
			gotWorkspaceID,
			gotReturnTo,
			wsID,
			githubReturnToRepositories,
		)
	}

	tampered := strings.Replace(tok, ".repositories.", ".github.", 1)
	if _, _, ok := verifyStateWithReturn(tampered); ok {
		t.Error("tampered return target should fail verification")
	}
}

func TestGitHubConnectRepositoryReturnTarget(t *testing.T) {
	t.Setenv("GITHUB_APP_SLUG", "goosar-test")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	wsID := "11111111-2222-3333-4444-555555555555"

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/workspaces/"+wsID+"/github/connect?return_to=repositories",
		nil,
	)
	req = withURLParam(req, "id", wsID)
	rec := httptest.NewRecorder()
	(&Handler{}).GitHubConnect(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GitHubConnect: got %d (%s)", rec.Code, rec.Body.String())
	}
	var body GitHubConnectResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode connect response: %v", err)
	}
	installURL, err := url.Parse(body.URL)
	if err != nil {
		t.Fatalf("parse install URL: %v", err)
	}
	_, returnTo, ok := verifyStateWithReturn(installURL.Query().Get("state"))
	if !ok || returnTo != githubReturnToRepositories {
		t.Fatalf("signed return target = %q, valid=%v, want repositories", returnTo, ok)
	}

	badReq := httptest.NewRequest(
		http.MethodGet,
		"/api/workspaces/"+wsID+"/github/connect?return_to=https://evil.example",
		nil,
	)
	badReq = withURLParam(badReq, "id", wsID)
	badRec := httptest.NewRecorder()
	(&Handler{}).GitHubConnect(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid return target: got %d, want 400", badRec.Code)
	}
}

func TestGitHubSetupCallbackRepositoryReturnTarget(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	t.Setenv("FRONTEND_ORIGIN", "https://app.goosar.test/")
	wsID := "11111111-2222-3333-4444-555555555555"
	state, err := signStateForReturn(wsID, githubReturnToRepositories)
	if err != nil {
		t.Fatalf("signStateForReturn: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/github/setup?installation_id=not-a-number&state="+url.QueryEscape(state),
		nil,
	)
	rec := httptest.NewRecorder()
	(&Handler{}).GitHubSetupCallback(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("GitHubSetupCallback: got %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://app.goosar.test/settings?tab=repositories&github_error=bad_installation_id" {
		t.Fatalf("redirect = %q, want repository settings error", got)
	}
}

func TestSignStateRequiresSecret(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "")
	if _, err := signState("ws"); err == nil {
		t.Error("signState should error when secret is unset")
	}
}

func TestWebhook_MergedPR_AdvancesLinkedIssueToDone(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "merge-sync-test-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR auto-merge test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 99887766
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "merge-sync-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	body := map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":     1234,
			"html_url":   "https://github.com/acme/widget/pull/1234",
			"title":      "Fix login " + created.Identifier,
			"body":       "Closes " + created.Identifier,
			"state":      "closed",
			"draft":      false,
			"merged":     true,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": "fix/login"},
			"user":       map[string]any{"login": "octocat", "avatar_url": ""},
		},
		"repository": map[string]any{
			"name":  "widget",
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	w = httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(w, req2)
	if w.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	pr, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RepoOwner:   "acme",
		RepoName:    "widget",
		PrNumber:    1234,
	})
	if err != nil {
		t.Fatalf("GetGitHubPullRequest: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("expected pr state merged, got %q", pr.State)
	}

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked PR, got %d", len(linked))
	}

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "done" {
		t.Errorf("expected issue status 'done', got %q", updated.Status)
	}
}

func TestWebhook_MergedPR_PreservesCancelled(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "cancelled-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Already cancelled",
		"status": "cancelled",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 11223344
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "cancelled-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number": 7, "html_url": "https://x", "title": "Closes " + created.Identifier,
			"state": "closed", "merged": true, "draft": false,
			"merged_at": "2026-04-29T00:00:00Z", "closed_at": "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-29T00:00:00Z",
			"head": map[string]any{"ref": "x"}, "user": map[string]any{"login": "u"},
		},
		"repository":   map[string]any{"name": "r", "owner": map[string]any{"login": "o"}},
		"installation": map[string]any{"id": installationID},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	w = httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(w, req2)

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "cancelled" {
		t.Errorf("expected status to remain 'cancelled', got %q", updated.Status)
	}
}

func TestWebhook_UninstallReturnsWorkspaceForBroadcast(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const installationID int64 = 55443322

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "uninstall-test",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
	})

	deleted, err := testHandler.Queries.DeleteGitHubInstallationByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("DeleteGitHubInstallationByInstallationID: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted binding, got %d", len(deleted))
	}
	if uuidToString(deleted[0].WorkspaceID) != testWorkspaceID {
		t.Errorf("expected returned workspace_id %s, got %s", testWorkspaceID, uuidToString(deleted[0].WorkspaceID))
	}

	if again, err := testHandler.Queries.DeleteGitHubInstallationByInstallationID(ctx, installationID); err != nil {
		t.Errorf("second delete errored: %v", err)
	} else if len(again) != 0 {
		t.Errorf("expected 0 rows on second delete, got %d", len(again))
	}
}

func TestWebhook_MergedPR_WaitsForOpenSibling(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "multi-pr-test-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Multi-PR auto-merge test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 55667788
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "multi-pr-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	fire := func(t *testing.T, repo string, prNumber int32, merged bool) {
		t.Helper()
		state := "open"
		if merged {
			state = "closed"
		}
		payload := map[string]any{
			"action": state,
			"pull_request": map[string]any{
				"number":     prNumber,
				"html_url":   "https://github.com/acme/" + repo + "/pull/1",
				"title":      "Fix " + created.Identifier,
				"body":       "",
				"state":      state,
				"draft":      false,
				"merged":     merged,
				"merged_at":  "2026-04-29T00:00:00Z",
				"closed_at":  "2026-04-29T00:00:00Z",
				"created_at": "2026-04-28T00:00:00Z",
				"updated_at": "2026-04-29T00:00:00Z",
				"head":       map[string]any{"ref": "fix/multi"},
				"user":       map[string]any{"login": "octocat"},
			},
			"repository": map[string]any{
				"name":  repo,
				"owner": map[string]any{"login": "acme"},
			},
			"installation": map[string]any{"id": installationID},
		}
		raw, _ := json.Marshal(payload)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(raw)
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

		rec := httptest.NewRecorder()
		hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
		hookReq.Header.Set("X-GitHub-Event", "pull_request")
		hookReq.Header.Set("X-Hub-Signature-256", sig)
		testHandler.HandleGitHubWebhook(rec, hookReq)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
		}
	}

	fire(t, "repo-a", 1, false)
	fire(t, "repo-b", 2, false)

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 2 {
		t.Fatalf("expected 2 linked PRs, got %d", len(linked))
	}

	fire(t, "repo-a", 1, true)
	issueAfterA, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issueAfterA.Status != "in_progress" {
		t.Errorf("issue should stay in_progress while sibling PR is open, got %q", issueAfterA.Status)
	}

	fire(t, "repo-b", 2, true)
	issueAfterB, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issueAfterB.Status != "done" {
		t.Errorf("expected issue 'done' after every linked PR merged, got %q", issueAfterB.Status)
	}
}

func firePullRequestWebhook(t *testing.T, secret, identifier string, installationID int64, repo string, prNumber int32, prState string) {
	t.Helper()
	state := "open"
	merged := false
	switch prState {
	case "merged":
		state = "closed"
		merged = true
	case "closed":
		state = "closed"
	}
	payload := map[string]any{
		"action": state,
		"pull_request": map[string]any{
			"number":     prNumber,
			"html_url":   "https://github.com/acme/" + repo + "/pull/1",
			"title":      "Fix " + identifier,
			"body":       "",
			"state":      state,
			"draft":      false,
			"merged":     merged,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": "fix/multi"},
			"user":       map[string]any{"login": "octocat"},
		},
		"repository": map[string]any{
			"name":  repo,
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "pull_request")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook %s pr=%d state=%s: expected 202, got %d (%s)",
			repo, prNumber, prState, rec.Code, rec.Body.String())
	}
}

func TestWebhook_ClosedSiblingAfterMerge(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "closed-sibling-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Closed sibling after merge",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 66778899
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "closed-sibling-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-a", 1, "open")
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "open")

	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-a", 1, "merged")
	intermediate, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if intermediate.Status != "in_progress" {
		t.Fatalf("issue should stay in_progress while sibling PR open, got %q", intermediate.Status)
	}

	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "closed")
	final, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if final.Status != "done" {
		t.Errorf("expected issue 'done' after sibling closed-without-merge follows a prior merge, got %q", final.Status)
	}
}

func TestWebhook_AllClosedWithoutMerge(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "all-closed-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "All closed no merge",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 77889900
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "all-closed-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-a", 1, "open")
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "open")

	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-a", 1, "closed")
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "closed")

	final, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if final.Status != "in_progress" {
		t.Errorf("issue must stay in_progress when no linked PR ever merged, got %q", final.Status)
	}
}

func fireBareWebhook(t *testing.T, secret string, installationID int64, prNumber int32, title, body, branch string) {
	t.Helper()
	payload := map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":     prNumber,
			"html_url":   fmt.Sprintf("https://github.com/acme/widget/pull/%d", prNumber),
			"title":      title,
			"body":       body,
			"state":      "closed",
			"draft":      false,
			"merged":     true,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": branch},
			"user":       map[string]any{"login": "octocat"},
		},
		"repository":   map[string]any{"name": "widget", "owner": map[string]any{"login": "acme"}},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook pr=%d: expected 202, got %d (%s)", prNumber, rec.Code, rec.Body.String())
	}
}

func TestWebhook_MergedPR_OnlyClosesIdentifiersWithClosingKeyword(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "closing-keyword-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	createIssue := func(title string) IssueResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":  title,
			"status": "in_progress",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue %q: %d %s", title, w.Code, w.Body.String())
		}
		var out IssueResponse
		json.NewDecoder(w.Body).Decode(&out)
		return out
	}
	closes := createIssue("primary work")
	followUp := createIssue("follow up work")
	unblocks := createIssue("unblocked work")

	t.Cleanup(func() {
		for _, id := range []string{closes.ID, followUp.ID, unblocks.ID} {
			testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, id)
			testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, id)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
		}
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
	})

	const installationID int64 = 30264001
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "closing-keyword-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	title := closes.Identifier + ": Lorem Ipsum dolor sit amet"
	body := fmt.Sprintf(
		"Closes %s. Follow up work planned in %s. Unblocks %s.",
		closes.Identifier, followUp.Identifier, unblocks.Identifier,
	)
	fireBareWebhook(t, secret, installationID, 1, title, body, "fix/login")

	listed, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(closes.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue(%s): %v", closes.Identifier, err)
	}
	if len(listed) != 1 {
		t.Errorf("expected %s (closing keyword) to show in the PR list, got %d rows", closes.Identifier, len(listed))
	}
	for _, issue := range []IssueResponse{followUp, unblocks} {
		listed, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(issue.ID))
		if err != nil {
			t.Fatalf("ListPullRequestsByIssue(%s): %v", issue.Identifier, err)
		}
		if len(listed) != 0 {
			t.Errorf("expected %s (bare body mention) to be hidden from the PR list, got %d rows", issue.Identifier, len(listed))
		}

		var refOnly bool
		if err := testPool.QueryRow(ctx,
			`SELECT reference_only FROM issue_pull_request WHERE issue_id = $1`, issue.ID,
		).Scan(&refOnly); err != nil {
			t.Fatalf("query reference_only(%s): %v", issue.Identifier, err)
		}
		if !refOnly {
			t.Errorf("expected %s link to be reference_only, got false", issue.Identifier)
		}
	}

	wantStatus := map[string]string{
		closes.ID:   "done",
		followUp.ID: "in_progress",
		unblocks.ID: "in_progress",
	}
	for _, issue := range []IssueResponse{closes, followUp, unblocks} {
		got, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
		if err != nil {
			t.Fatalf("GetIssue(%s): %v", issue.Identifier, err)
		}
		if got.Status != wantStatus[issue.ID] {
			t.Errorf("issue %s: status = %q, want %q", issue.Identifier, got.Status, wantStatus[issue.ID])
		}
	}
}

func TestWebhook_MergedPR_TitlePrefixDoesNotClose(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "title-prefix-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "title-prefix repro",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264002
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "title-prefix-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	fireBareWebhook(t, secret, installationID, 2, created.Identifier+": fix something", "", "fix/login")

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Errorf("expected 1 linked PR even without a closing keyword, got %d", len(linked))
	}

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("expected issue to stay in_progress (title prefix alone is not closing intent), got %q", got.Status)
	}
}

func TestWebhook_MergedPR_BranchNameDoesNotClose(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "branch-name-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "branch-name repro",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264003
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "branch-name-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	branch := strings.ToLower(created.Identifier) + "/fix-login"
	fireBareWebhook(t, secret, installationID, 3, "Fix login flow", "", branch)

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Errorf("expected branch-name reference to still link the PR, got %d link rows", len(linked))
	}

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("expected issue to stay in_progress (branch-name reference is not closing intent), got %q", got.Status)
	}
}

func firePRWebhook(t *testing.T, secret string, installationID int64, prNumber int32, title, body, branch, lifecycle string) {
	t.Helper()
	var action, state string
	var merged bool
	var mergedAt, closedAt any
	switch lifecycle {
	case "opened":
		action, state, merged = "opened", "open", false
		mergedAt, closedAt = nil, nil
	case "edited":
		action, state, merged = "edited", "open", false
		mergedAt, closedAt = nil, nil
	case "merged":
		action, state, merged = "closed", "closed", true
		mergedAt, closedAt = "2026-04-29T00:00:00Z", "2026-04-29T00:00:00Z"
	case "edited_merged":
		action, state, merged = "edited", "closed", true
		mergedAt, closedAt = "2026-04-29T00:00:00Z", "2026-04-29T00:00:00Z"
	case "closed":
		action, state, merged = "closed", "closed", false
		mergedAt, closedAt = nil, "2026-04-29T00:00:00Z"
	default:
		t.Fatalf("firePRWebhook: unknown lifecycle %q", lifecycle)
	}
	payload := map[string]any{
		"action": action,
		"pull_request": map[string]any{
			"number":     prNumber,
			"html_url":   fmt.Sprintf("https://github.com/acme/widget/pull/%d", prNumber),
			"title":      title,
			"body":       body,
			"state":      state,
			"draft":      false,
			"merged":     merged,
			"merged_at":  mergedAt,
			"closed_at":  closedAt,
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": branch},
			"user":       map[string]any{"login": "octocat"},
		},
		"repository":   map[string]any{"name": "widget", "owner": map[string]any{"login": "acme"}},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook pr=%d (%s): expected 202, got %d (%s)", prNumber, lifecycle, rec.Code, rec.Body.String())
	}
}

func TestWebhook_CloseKeywordRemovedBeforeMergeDoesNotClose(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "close-intent-removal-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "close intent can be removed",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264005
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "close-intent-removal-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	firePRWebhook(t, secret, installationID, 1, "Implement removal path", "Closes "+created.Identifier, "feat/remove-close-intent", "opened")
	firePRWebhook(t, secret, installationID, 1, "Implement removal path", "Related "+created.Identifier, "feat/remove-close-intent", "edited")
	firePRWebhook(t, secret, installationID, 1, "Implement removal path", "Related "+created.Identifier, "feat/remove-close-intent", "merged")

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after merge: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("after closing keyword was removed before merge: status = %q, want in_progress", got.Status)
	}
	counts, err := testHandler.Queries.GetIssuePullRequestCloseAggregate(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssuePullRequestCloseAggregate: %v", err)
	}
	if counts.MergedWithCloseIntentCount != 0 {
		t.Fatalf("merged_with_close_intent_count = %d, want 0", counts.MergedWithCloseIntentCount)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "post merge close keyword is link only",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue second: %d %s", w.Code, w.Body.String())
	}
	var second IssueResponse
	json.NewDecoder(w.Body).Decode(&second)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, second.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, second.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, second.ID)
	})

	firePRWebhook(t, secret, installationID, 1, "Implement removal path", "Closes "+created.Identifier+"\nCloses "+second.Identifier, "feat/remove-close-intent", "edited_merged")
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after post-merge edit: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("after adding closing keyword post-merge: status = %q, want in_progress", got.Status)
	}
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(second.ID))
	if err != nil {
		t.Fatalf("GetIssue second after post-merge edit: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("second issue after post-merge closing keyword: status = %q, want in_progress", got.Status)
	}
}

func TestWebhook_LinkOnlySiblingMergeAfterCloseKeywordPR(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "link-only-sibling-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "needs two prs",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264004
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "link-only-sibling-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	firePRWebhook(t, secret, installationID, 1, "Implement primary path", "Closes "+created.Identifier, "feat/primary", "opened")

	firePRWebhook(t, secret, installationID, 2, created.Identifier+": follow-up cleanup", "", "feat/cleanup", "opened")

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after open: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("after both PRs opened: status = %q, want in_progress", got.Status)
	}

	firePRWebhook(t, secret, installationID, 1, "Implement primary path", "Closes "+created.Identifier, "feat/primary", "merged")
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after A merge: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("after PR A merged with PR B still open: status = %q, want in_progress", got.Status)
	}

	firePRWebhook(t, secret, installationID, 2, created.Identifier+": follow-up cleanup", "", "feat/cleanup", "merged")
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after B merge: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("after both PRs merged (A with close_intent, B link-only): status = %q, want done", got.Status)
	}
}

func TestWebhook_BareBodyMentionHiddenFromPRList(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "bare-mention-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "mentioned in passing",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264006
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "bare-mention-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	listLen := func() int {
		t.Helper()
		rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
		if err != nil {
			t.Fatalf("ListPullRequestsByIssue: %v", err)
		}
		return len(rows)
	}

	firePRWebhook(t, secret, installationID, 1, "Unrelated cleanup", "Context for reviewers: see "+created.Identifier, "feat/cleanup", "opened")
	if n := listLen(); n != 0 {
		t.Errorf("bare body mention should be hidden from PR list, got %d rows", n)
	}

	firePRWebhook(t, secret, installationID, 1, "Unrelated cleanup", "Closes "+created.Identifier, "feat/cleanup", "edited")
	if n := listLen(); n != 1 {
		t.Errorf("after adding a closing keyword the PR should show, got %d rows", n)
	}

	firePRWebhook(t, secret, installationID, 1, "Unrelated cleanup", "Reverting: just referencing "+created.Identifier, "feat/cleanup", "edited")
	if n := listLen(); n != 0 {
		t.Errorf("after removing the closing keyword the PR should be hidden again, got %d rows", n)
	}
}

func TestWebhook_HiddenBodyMentionDoesNotBlockAutoAdvance(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "hidden-mention-gate-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "closing PR plus invisible mention",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264007
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "hidden-mention-gate-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	firePRWebhook(t, secret, installationID, 1, "Unrelated cleanup", "Context: see "+created.Identifier, "feat/cleanup", "opened")

	firePRWebhook(t, secret, installationID, 2, "Primary work", "Closes "+created.Identifier, "feat/primary", "opened")

	listed, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected only the closing PR to show, got %d rows", len(listed))
	}

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after open: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("after both PRs opened: status = %q, want in_progress", got.Status)
	}

	firePRWebhook(t, secret, installationID, 2, "Primary work", "Closes "+created.Identifier, "feat/primary", "merged")
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after merge: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("closing PR merged while only a hidden body-only mention is open: status = %q, want done", got.Status)
	}
}

func TestDerivePRMergeableState(t *testing.T) {
	cases := []struct {
		name           string
		action         string
		payload        string
		baseRefChanged bool
		wantValid      bool
		wantStr        string
		wantClear      bool
	}{
		{"opened_clears", "opened", "clean", false, false, "", true},
		{"synchronize_clears", "synchronize", "clean", false, false, "", true},
		{"reopened_clears", "reopened", "dirty", false, false, "", true},
		{"edited_base_changed_clears", "edited", "clean", true, false, "", true},
		{"edited_title_only_keeps_value", "edited", "clean", false, true, "clean", false},
		{"labeled_keeps_value", "labeled", "clean", false, true, "clean", false},
		{"labeled_empty_payload_preserves", "labeled", "", false, false, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, clear := derivePRMergeableState(tc.action, tc.payload, tc.baseRefChanged)
			if got.Valid != tc.wantValid {
				t.Errorf("Valid=%v want %v", got.Valid, tc.wantValid)
			}
			if got.String != tc.wantStr {
				t.Errorf("String=%q want %q", got.String, tc.wantStr)
			}
			if clear != tc.wantClear {
				t.Errorf("clear=%v want %v", clear, tc.wantClear)
			}
		})
	}
}

func firePullRequestWebhookWithHead(t *testing.T, secret, identifier string, installationID int64, repo string, prNumber int32, action, headSHA, mergeableState string) {
	t.Helper()
	payload := map[string]any{
		"action": action,
		"pull_request": map[string]any{
			"number":          prNumber,
			"html_url":        "https://github.com/acme/" + repo + "/pull/1",
			"title":           "Fix " + identifier,
			"body":            "",
			"state":           "open",
			"draft":           false,
			"merged":          false,
			"merged_at":       nil,
			"closed_at":       nil,
			"created_at":      "2026-04-28T00:00:00Z",
			"updated_at":      "2026-04-29T00:00:00Z",
			"mergeable_state": mergeableState,
			"head":            map[string]any{"ref": "fix/foo", "sha": headSHA},
			"user":            map[string]any{"login": "octocat"},
		},
		"repository": map[string]any{
			"name":  repo,
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "pull_request")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook %s pr=%d action=%s: expected 202, got %d (%s)",
			repo, prNumber, action, rec.Code, rec.Body.String())
	}
}

func setupPRTestIssue(t *testing.T, ctx context.Context, secret string) (IssueResponse, int64) {
	t.Helper()
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR CI test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	installationID := int64(33445566) + int64(time.Now().UnixNano()%1000000)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_pull_request_check_suite WHERE pr_id IN (SELECT id FROM github_pull_request WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_pending_check_suite WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "ci-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	return created, installationID
}

func TestWebhook_PullRequest_SynchronizeClearsMergeable(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-mergeable-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-d", 44, "opened", "head1", "")
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-d", 44, "labeled", "head1", "clean")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if !rows[0].MergeableState.Valid || rows[0].MergeableState.String != "clean" {
		t.Fatalf("setup: expected mergeable_state=clean, got %+v", rows[0].MergeableState)
	}

	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-d", 44, "synchronize", "head2", "clean")

	rows, err = testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if rows[0].MergeableState.Valid {
		t.Errorf("expected mergeable_state cleared on synchronize, got %q", rows[0].MergeableState.String)
	}
	if rows[0].HeadSha != "head2" {
		t.Errorf("expected head_sha updated to head2, got %q", rows[0].HeadSha)
	}
}

func TestWebhook_PullRequest_MetadataPreservesMergeable(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-mergeable-preserve-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-e", 55, "opened", "headA", "")
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-e", 55, "labeled", "headA", "clean")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if !rows[0].MergeableState.Valid || rows[0].MergeableState.String != "clean" {
		t.Fatalf("setup: expected mergeable_state=clean, got %+v", rows[0].MergeableState)
	}

	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-e", 55, "labeled", "headA", "")

	rows, err = testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if !rows[0].MergeableState.Valid || rows[0].MergeableState.String != "clean" {
		t.Errorf("expected mergeable_state preserved as clean after metadata event, got %+v", rows[0].MergeableState)
	}
}

func TestListGitHubInstallations_RoleGating(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()

	const installationID int64 = 42424242
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "role-gating-acct",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
	})

	call := func(t *testing.T, role string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/github/installations", nil)
		req = withURLParam(req, "id", testWorkspaceID)
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: role}))
		w := httptest.NewRecorder()
		testHandler.ListGitHubInstallations(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListGitHubInstallations(%s): %d %s", role, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body (%s): %v", role, err)
		}
		return body
	}

	t.Run("admin sees installation_id + can_manage true", func(t *testing.T) {
		body := call(t, "admin")
		if got, _ := body["can_manage"].(bool); !got {
			t.Errorf("can_manage = %v, want true", body["can_manage"])
		}
		installs, _ := body["installations"].([]any)
		if len(installs) == 0 {
			t.Fatalf("expected at least one installation row, got %v", installs)
		}
		row, _ := installs[0].(map[string]any)
		gotID, ok := row["installation_id"].(float64)
		if !ok {
			t.Fatalf("admin response missing installation_id: %v", row)
		}
		if int64(gotID) != installationID {
			t.Errorf("installation_id = %v, want %d", gotID, installationID)
		}
	})

	t.Run("owner sees installation_id + can_manage true", func(t *testing.T) {
		body := call(t, "owner")
		if got, _ := body["can_manage"].(bool); !got {
			t.Errorf("can_manage = %v, want true", body["can_manage"])
		}
		installs, _ := body["installations"].([]any)
		row, _ := installs[0].(map[string]any)
		if _, ok := row["installation_id"]; !ok {
			t.Errorf("owner response missing installation_id: %v", row)
		}
	})

	t.Run("member sees row without installation_id and can_manage false", func(t *testing.T) {
		body := call(t, "member")
		canManage, _ := body["can_manage"].(bool)
		if canManage {
			t.Errorf("can_manage = true, want false for non-admin member")
		}
		installs, _ := body["installations"].([]any)
		if len(installs) == 0 {
			t.Fatalf("member should still see installation rows, got %v", installs)
		}
		row, _ := installs[0].(map[string]any)
		if _, present := row["installation_id"]; present {
			t.Errorf("installation_id must be omitted for non-admin members, row=%v", row)
		}

		if got, _ := row["account_login"].(string); got != "role-gating-acct" {
			t.Errorf("account_login = %q, want role-gating-acct", got)
		}
	})

	t.Run("guest is treated as read-only and can_manage is false", func(t *testing.T) {
		body := call(t, "guest")
		if canManage, _ := body["can_manage"].(bool); canManage {
			t.Errorf("can_manage = true, want false for guest")
		}
		installs, _ := body["installations"].([]any)
		row, _ := installs[0].(map[string]any)
		if _, present := row["installation_id"]; present {
			t.Errorf("installation_id must be omitted for guest, row=%v", row)
		}
	})
}

func TestGitHubRoutes_RoleGating(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()

	const slug = "github-routes-role-gating"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email LIKE $1`, "github-routes-"+slug+"-%")

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "GitHub Routes Role Gating", slug, "github routes role gating", "GRG").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	mkUser := func(t *testing.T, label string) string {
		t.Helper()
		var id string
		email := fmt.Sprintf("github-routes-%s-%s@goosar.ru", slug, label)
		if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "GHR "+label, email).Scan(&id); err != nil {
			t.Fatalf("create user %s: %v", label, err)
		}
		return id
	}
	adminUserID := mkUser(t, "admin")
	memberUserID := mkUser(t, "member")
	outsiderUserID := mkUser(t, "outsider")

	for _, m := range []struct {
		userID, role string
	}{
		{adminUserID, "admin"},
		{memberUserID, "member"},
	} {
		if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)
`, wsID, m.userID, m.role); err != nil {
			t.Fatalf("insert member (%s): %v", m.role, err)
		}
	}

	const installationID int64 = 90909090
	createdInst, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(wsID),
		InstallationID: installationID,
		AccountLogin:   "routes-acct",
		AccountType:    "User",
	})
	if err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		for _, uid := range []string{adminUserID, memberUserID, outsiderUserID} {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid)
		}
	})

	router := chi.NewRouter()
	router.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMemberFromURL(testHandler.Queries, "id"))
			r.Get("/github/installations", testHandler.ListGitHubInstallations)
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
			r.Get("/github/connect", testHandler.GitHubConnect)
			r.Get("/github/installations/{installationId}/repositories", testHandler.ListGitHubInstallationRepositories)
			r.Delete("/github/installations/{installationId}", testHandler.DeleteGitHubInstallation)
		})
	})

	exercise := func(t *testing.T, method, path, userID string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("GET installations is reachable by members", func(t *testing.T) {
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/installations", memberUserID); code != http.StatusOK {
			t.Errorf("member GET installations: want 200, got %d", code)
		}
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/installations", adminUserID); code != http.StatusOK {
			t.Errorf("admin GET installations: want 200, got %d", code)
		}
	})

	t.Run("GET installations rejects non-members", func(t *testing.T) {

		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/installations", outsiderUserID); code != http.StatusNotFound {
			t.Errorf("outsider GET installations: want 404, got %d", code)
		}
	})

	t.Run("GET connect remains owner/admin only", func(t *testing.T) {
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/connect", adminUserID); code != http.StatusOK {
			t.Errorf("admin GET connect: want 200, got %d", code)
		}
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/connect", memberUserID); code != http.StatusForbidden {
			t.Errorf("member GET connect: want 403, got %d", code)
		}
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/connect", outsiderUserID); code != http.StatusNotFound {
			t.Errorf("outsider GET connect: want 404, got %d", code)
		}
	})

	t.Run("GET repositories remains owner/admin only", func(t *testing.T) {
		path := "/api/workspaces/" + wsID + "/github/installations/" + uuidToString(createdInst.ID) + "/repositories"
		if code := exercise(t, http.MethodGet, path, memberUserID); code != http.StatusForbidden {
			t.Errorf("member GET repositories: want 403, got %d", code)
		}
		if code := exercise(t, http.MethodGet, path, outsiderUserID); code != http.StatusNotFound {
			t.Errorf("outsider GET repositories: want 404, got %d", code)
		}
	})

	t.Run("DELETE installation remains owner/admin only", func(t *testing.T) {

		if code := exercise(t, http.MethodDelete, "/api/workspaces/"+wsID+"/github/installations/"+uuidToString(createdInst.ID), memberUserID); code != http.StatusForbidden {
			t.Errorf("member DELETE installation: want 403, got %d", code)
		}

		if code := exercise(t, http.MethodDelete, "/api/workspaces/"+wsID+"/github/installations/"+uuidToString(createdInst.ID), outsiderUserID); code != http.StatusNotFound {
			t.Errorf("outsider DELETE installation: want 404, got %d", code)
		}

		if code := exercise(t, http.MethodDelete, "/api/workspaces/"+wsID+"/github/installations/"+uuidToString(createdInst.ID), adminUserID); code != http.StatusNoContent {
			t.Errorf("admin DELETE installation: want 204, got %d", code)
		}
		var remaining int
		if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM github_installation WHERE id = $1`, uuidToString(createdInst.ID)).Scan(&remaining); err != nil {
			t.Fatalf("verify deletion: %v", err)
		}
		if remaining != 0 {
			t.Errorf("expected installation row gone after admin DELETE, got %d remaining", remaining)
		}
	})
}

func TestGitHubInstallationBroadcastRedaction(t *testing.T) {
	inst := db.GithubInstallation{
		InstallationID: 123456789,
		AccountLogin:   "broadcast-acct",
		AccountType:    "User",
	}
	got := githubInstallationToBroadcast(inst)
	if got.InstallationID != nil {
		t.Errorf("broadcast payload must omit installation_id, got %v", *got.InstallationID)
	}
	if got.AccountLogin != "broadcast-acct" {
		t.Errorf("expected account_login preserved, got %q", got.AccountLogin)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal broadcast payload: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal broadcast payload: %v", err)
	}
	if _, present := generic["installation_id"]; present {
		t.Errorf("installation_id leaked into broadcast JSON: %s", string(raw))
	}
}

func TestWebhook_MergedPR_ChildWithParent_NotifiesParent(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "merge-parent-notify-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR-merge parent " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue parent: %d %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	json.NewDecoder(w.Body).Decode(&parent)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "PR-merge child " + time.Now().Format(time.RFC3339Nano),
		"status":          "in_progress",
		"parent_issue_id": parent.ID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue child: %d %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	json.NewDecoder(w.Body).Decode(&child)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id IN ($1, $2)`, child.ID, parent.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id IN ($1, $2)`, child.ID, parent.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id IN ($1, $2)`, child.ID, parent.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, child.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	const installationID int64 = 88990011
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "merge-parent-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":     4242,
			"html_url":   "https://github.com/acme/widget/pull/4242",
			"title":      "Fix " + child.Identifier,
			"body":       "",
			"state":      "closed",
			"draft":      false,
			"merged":     true,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": "fix/child"},
			"user":       map[string]any{"login": "octocat", "avatar_url": ""},
		},
		"repository": map[string]any{
			"name":  "widget",
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	w = httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(w, req2)
	if w.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	updatedChild, err := testHandler.Queries.GetIssue(ctx, parseUUID(child.ID))
	if err != nil {
		t.Fatalf("GetIssue child: %v", err)
	}
	if updatedChild.Status != "done" {
		t.Fatalf("expected child status 'done', got %q", updatedChild.Status)
	}

	var sysCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'system'`,
		parent.ID,
	).Scan(&sysCount); err != nil {
		t.Fatalf("count system comments on parent: %v", err)
	}
	if sysCount != 1 {
		t.Fatalf("expected 1 system comment on parent after PR-merge auto-done, got %d", sysCount)
	}

	var content string
	if err := testPool.QueryRow(ctx,
		`SELECT content FROM comment WHERE issue_id = $1 AND author_type = 'system' LIMIT 1`,
		parent.ID,
	).Scan(&content); err != nil {
		t.Fatalf("read system comment: %v", err)
	}
	if !strings.Contains(content, child.Identifier) {
		t.Errorf("system comment should reference child identifier %q, got: %s", child.Identifier, content)
	}

	for _, banned := range []string{"mention://agent/", "mention://member/", "mention://squad/"} {
		if strings.Contains(content, banned) {
			t.Errorf("system comment must not include %q mention (parent unassigned), got: %s", banned, content)
		}
	}
}

func generateTestRSAKeyPEM(t *testing.T) (pemBytes []byte, key *rsa.PrivateKey) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(k)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}), k
}

func TestSignGitHubAppJWT_NotConfigured(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	tok, err := signGitHubAppJWT(time.Now())
	if err != nil {
		t.Fatalf("expected nil error when env not set, got %v", err)
	}
	if tok != "" {
		t.Errorf("expected empty token when env not set, got %q", tok)
	}

	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	tok, err = signGitHubAppJWT(time.Now())
	if err != nil || tok != "" {
		t.Errorf("partial config should return empty token, got tok=%q err=%v", tok, err)
	}
}

func TestSignGitHubAppJWT_InvalidPEM(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "not a real PEM block")
	if _, err := signGitHubAppJWT(time.Now()); err == nil {
		t.Error("expected error for malformed private key, got nil")
	}
}

func TestSignGitHubAppJWT_ClaimsAndSignature(t *testing.T) {
	pemBytes, key := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "424242")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	tok, err := signGitHubAppJWT(now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token when fully configured")
	}

	parsed, err := jwt.Parse(
		tok,
		func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return &key.PublicKey, nil
		},
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil || !parsed.Valid {
		t.Fatalf("verify token: err=%v valid=%v", err, parsed != nil && parsed.Valid)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims type: %T", parsed.Claims)
	}
	if got, _ := claims["iss"].(string); got != "424242" {
		t.Errorf("iss = %q, want 424242", got)
	}
	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))
	if iat != now.Add(-60*time.Second).Unix() {
		t.Errorf("iat = %d, want %d (now - 60s for clock skew)", iat, now.Add(-60*time.Second).Unix())
	}
	if exp != now.Add(9*time.Minute).Unix() {
		t.Errorf("exp = %d, want %d (now + 9m, inside GitHub's 10m cap)", exp, now.Add(9*time.Minute).Unix())
	}
	if exp-iat > int64(10*time.Minute/time.Second) {
		t.Errorf("exp-iat = %d s, exceeds GitHub's 10m max", exp-iat)
	}
}

func TestFetchGitHubInstallationRepositories(t *testing.T) {
	pemBytes, key := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "424242")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))

	const installationID int64 = 314159
	var tokenRevoked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/314159/access_tokens":
			bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if bearer == "" {
				http.Error(w, "missing app jwt", http.StatusUnauthorized)
				return
			}
			if _, err := jwt.Parse(bearer, func(token *jwt.Token) (any, error) {
				return &key.PublicKey, nil
			}); err != nil {
				http.Error(w, "bad app jwt", http.StatusUnauthorized)
				return
			}
			var tokenRequest struct {
				Permissions map[string]string `json:"permissions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&tokenRequest); err != nil {
				http.Error(w, "bad token request", http.StatusBadRequest)
				return
			}
			if !reflect.DeepEqual(tokenRequest.Permissions, map[string]string{"metadata": "read"}) {
				http.Error(w, "overbroad token permissions", http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"token": "installation-secret"})
		case r.Method == http.MethodGet && r.URL.Path == "/installation/repositories":
			if got := r.Header.Get("Authorization"); got != "Bearer installation-secret" {
				http.Error(w, "bad installation token", http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "1" {
				http.Error(w, "bad pagination", http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"total_count": 3,
				"repositories": []map[string]any{{
					"id":             9,
					"full_name":      "acme/private-repo",
					"html_url":       "https://github.com/acme/private-repo",
					"clone_url":      "https://github.com/acme/private-repo.git",
					"description":    "Private repository",
					"private":        true,
					"archived":       false,
					"default_branch": "main",
				}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/installation/token":
			tokenRevoked = r.Header.Get("Authorization") == "Bearer installation-secret"
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	got, err := fetchGitHubInstallationRepositories(
		context.Background(),
		installationID,
		2,
		1,
	)
	if err != nil {
		t.Fatalf("fetchGitHubInstallationRepositories: %v", err)
	}
	if len(got.Repositories) != 1 {
		t.Fatalf("repositories = %d, want 1", len(got.Repositories))
	}
	repository := got.Repositories[0]
	if repository.FullName != "acme/private-repo" || !repository.Private {
		t.Errorf("repository = %+v, want mapped private repository", repository)
	}
	if got.TotalCount != 3 || got.NextPage == nil || *got.NextPage != 3 {
		t.Errorf("pagination = total %d, next %v; want total 3, next 3", got.TotalCount, got.NextPage)
	}
	if !tokenRevoked {
		t.Error("installation token was not revoked after repository listing")
	}
}

func TestListGitHubInstallationRepositoriesRejectsCrossWorkspaceRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const installationID int64 = 818181
	row, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "cross-workspace-acct",
		AccountType:    "Organization",
	})
	if err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})

	otherWorkspaceID := "11111111-2222-3333-4444-555555555555"
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/workspaces/"+otherWorkspaceID+"/github/installations/"+uuidToString(row.ID)+"/repositories",
		nil,
	)
	rec := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Get(
		"/api/workspaces/{id}/github/installations/{installationId}/repositories",
		testHandler.ListGitHubInstallationRepositories,
	)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace row: got %d (%s), want 404", rec.Code, rec.Body.String())
	}
}

func TestFetchInstallationAccount_AuthenticatedPopulatesRow(t *testing.T) {
	pemBytes, key := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "11111")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))

	const wantInstallationID int64 = 7777777
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		expectedPath := fmt.Sprintf("/app/installations/%d", wantInstallationID)
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: got %q want %q", r.URL.Path, expectedPath)
		}

		bearer := strings.TrimPrefix(sawAuth, "Bearer ")
		if bearer == sawAuth {
			http.Error(w, "missing Bearer prefix", http.StatusUnauthorized)
			return
		}
		if _, err := jwt.Parse(bearer, func(token *jwt.Token) (any, error) {
			return &key.PublicKey, nil
		}); err != nil {
			http.Error(w, "bad jwt: "+err.Error(), http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"account": map[string]any{
				"login":      "octocat",
				"type":       "Organization",
				"avatar_url": "https://example.com/o.png",
			},
		})
	}))
	t.Cleanup(srv.Close)

	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	login, accountType, avatar := fetchInstallationAccount(context.Background(), wantInstallationID)
	if login != "octocat" {
		t.Errorf("login = %q, want %q (the bug repro: stayed as 'unknown' before the fix)", login, "octocat")
	}
	if accountType != "Organization" {
		t.Errorf("accountType = %q, want Organization", accountType)
	}
	if avatar == nil || *avatar != "https://example.com/o.png" {
		t.Errorf("avatar = %v, want pointer to https://example.com/o.png", avatar)
	}
	if !strings.HasPrefix(sawAuth, "Bearer ") {
		t.Errorf("expected Bearer auth header, got %q", sawAuth)
	}
}

func TestFetchInstallationAccount_UnauthenticatedFallsBack(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "auth required", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"account": map[string]any{"login": "should-not-see"}})
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	login, _, _ := fetchInstallationAccount(context.Background(), 999)
	if login != "unknown" {
		t.Errorf("login = %q, want unknown placeholder when auth not configured", login)
	}
}

func TestFetchInstallationAccount_EmptyAccountKeepsPlaceholder(t *testing.T) {
	pemBytes, _ := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"account": map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	login, accountType, avatar := fetchInstallationAccount(context.Background(), 12)
	if login != "unknown" {
		t.Errorf("expected 'unknown' placeholder for empty account.login, got %q", login)
	}
	if accountType != "User" {
		t.Errorf("expected default 'User' accountType, got %q", accountType)
	}
	if avatar != nil {
		t.Errorf("expected nil avatar, got %v", *avatar)
	}
}

func TestWebhook_InstallationCreatedRefreshesUnknownLogin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "installation-refresh-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	const installationID int64 = 71717171
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "unknown",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("seed installation row: %v", err)
	}

	gotEvent := make(chan events.Event, 1)
	testHandler.Bus.Subscribe(protocol.EventGitHubInstallationCreated, func(e events.Event) {
		select {
		case gotEvent <- e:
		default:
		}
	})

	body, _ := json.Marshal(map[string]any{
		"action": "created",
		"installation": map[string]any{
			"id": installationID,
			"account": map[string]any{
				"login":      "real-octocat",
				"type":       "Organization",
				"avatar_url": "https://example.com/avatar.png",
			},
		},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 installation row, got %d", len(rows))
	}
	got := rows[0]
	if got.AccountLogin != "real-octocat" {
		t.Errorf("account_login = %q, want %q (refresh did not overwrite the unknown placeholder)",
			got.AccountLogin, "real-octocat")
	}
	if got.AccountType != "Organization" {
		t.Errorf("account_type = %q, want Organization", got.AccountType)
	}

	select {
	case ev := <-gotEvent:
		if ev.WorkspaceID != testWorkspaceID {
			t.Errorf("broadcast WorkspaceID = %q, want %q", ev.WorkspaceID, testWorkspaceID)
		}

		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			t.Fatalf("broadcast payload type: %T", ev.Payload)
		}
		inst, ok := payload["installation"].(GitHubInstallationResponse)
		if !ok {
			t.Fatalf("installation payload type: %T", payload["installation"])
		}
		if inst.AccountLogin != "real-octocat" {
			t.Errorf("broadcast account_login = %q, want real-octocat", inst.AccountLogin)
		}
		if inst.InstallationID != nil {
			t.Errorf("broadcast must redact installation_id, got %v", *inst.InstallationID)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("expected github_installation:created broadcast after webhook refresh, got none in 2s")
	}
}

func TestSetupCallback_ConsumesPendingInstallationCreated(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "pending-installation-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.test")

	const installationID int64 = 81818181
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM github_pending_installation WHERE installation_id = $1`, installationID)
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "auth required", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	body, _ := json.Marshal(map[string]any{
		"action": "created",
		"installation": map[string]any{
			"id": installationID,
			"account": map[string]any{
				"login":      "pending-octocat",
				"type":       "Organization",
				"avatar_url": "https://example.com/pending.png",
			},
		},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	hookReq.Header.Set("X-GitHub-Event", "installation")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	var pendingLogin string
	if err := testPool.QueryRow(ctx,
		`SELECT account_login FROM github_pending_installation WHERE installation_id = $1`,
		installationID,
	).Scan(&pendingLogin); err != nil {
		t.Fatalf("pending installation row not stored: %v", err)
	}
	if pendingLogin != "pending-octocat" {
		t.Fatalf("pending account_login = %q, want pending-octocat", pendingLogin)
	}

	state, err := signState(testWorkspaceID)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	setupReq := httptest.NewRequest("GET",
		fmt.Sprintf("/api/github/setup?installation_id=%d&state=%s", installationID, state),
		nil,
	)
	setupRec := httptest.NewRecorder()
	testHandler.GitHubSetupCallback(setupRec, setupReq)
	if setupRec.Code != http.StatusFound {
		t.Fatalf("setup callback: expected 302, got %d (%s)", setupRec.Code, setupRec.Body.String())
	}
	if loc := setupRec.Header().Get("Location"); !strings.Contains(loc, "github_connected=1") {
		t.Fatalf("setup callback redirect = %q, want github_connected=1", loc)
	}

	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 installation row, got %d", len(rows))
	}
	got := rows[0]
	if got.AccountLogin != "pending-octocat" {
		t.Errorf("account_login = %q, want pending-octocat (callback left the unknown placeholder)", got.AccountLogin)
	}
	if got.AccountType != "Organization" {
		t.Errorf("account_type = %q, want Organization", got.AccountType)
	}
	if got.AccountAvatarUrl.String != "https://example.com/pending.png" || !got.AccountAvatarUrl.Valid {
		t.Errorf("account_avatar_url = %+v, want pending avatar", got.AccountAvatarUrl)
	}

	var pendingCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM github_pending_installation WHERE installation_id = $1`,
		installationID,
	).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending installation: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending installation row should be consumed, got count %d", pendingCount)
	}
}

func TestWebhook_PullRequest_FansOutToBoundWorkspaces(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "fanout-pr-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "fan-out PR test",
		"status": "in_progress",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	const repo = "fanout-repo"
	const prNumber int32 = 4343
	const installationID int64 = 778899101

	testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "fanout-pr-ws-a")
	wsA, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name: "fanout-pr-ws-a", Slug: "fanout-pr-ws-a", IssuePrefix: "FPA",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID: parseUUID(testWorkspaceID), InstallationID: installationID, AccountLogin: "acme", AccountType: "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation B: %v", err)
	}
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID: wsA.ID, InstallationID: installationID, AccountLogin: "acme", AccountType: "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation A: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE repo_owner = 'acme' AND repo_name = $1`, repo)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsA.ID)
	})

	firePullRequestWebhook(t, secret, created.Identifier, installationID, repo, prNumber, "open")

	if _, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), RepoOwner: "acme", RepoName: repo, PrNumber: prNumber,
	}); err != nil {
		t.Fatalf("expected PR mirrored in workspace B: %v", err)
	}
	prA, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: wsA.ID, RepoOwner: "acme", RepoName: repo, PrNumber: prNumber,
	})
	if err != nil {
		t.Fatalf("expected PR fanned out to workspace A: %v", err)
	}

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked PR on workspace B's issue, got %d", len(linked))
	}
	if issues, _ := testHandler.Queries.ListIssueIDsForPullRequest(ctx, prA.ID); len(issues) != 0 {
		t.Fatalf("workspace A has no matching issue, expected 0 links, got %d", len(issues))
	}
}

func TestSecondWorkspaceBindDoesNotUnbindFirst(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const installationID int64 = 909090909

	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "multi-bind-ws-b")
	wsB, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "multi-bind-ws-b",
		Slug:        "multi-bind-ws-b",
		IssuePrefix: "MBB",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsB.ID)
	})

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "shared-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("bind workspace A: %v", err)
	}
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    wsB.ID,
		InstallationID: installationID,
		AccountLogin:   "shared-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("bind workspace B: %v", err)
	}

	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 bindings to coexist (silent unbind regression), got %d", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[uuidToString(r.WorkspaceID)] = true
	}
	if !seen[testWorkspaceID] || !seen[uuidToString(wsB.ID)] {
		t.Errorf("both workspaces must retain a binding; got %v", seen)
	}

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "shared-org-renamed",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("re-bind workspace A: %v", err)
	}
	rows, err = testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations after re-bind: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("re-binding an existing (workspace, installation) must upsert, got %d rows", len(rows))
	}
}

func TestWebhook_UninstallDeletesAllBindings(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "uninstall-all-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 707070707

	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "uninstall-all-ws-b")
	wsB, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "uninstall-all-ws-b",
		Slug:        "uninstall-all-ws-b",
		IssuePrefix: "UAB",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsB.ID)
	})

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "shared-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("bind workspace A: %v", err)
	}
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    wsB.ID,
		InstallationID: installationID,
		AccountLogin:   "shared-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("bind workspace B: %v", err)
	}

	gotWS := make(chan string, 2)
	testHandler.Bus.Subscribe(protocol.EventGitHubInstallationDeleted, func(e events.Event) {
		select {
		case gotWS <- e.WorkspaceID:
		default:
		}
	})

	body, _ := json.Marshal(map[string]any{
		"action":       "deleted",
		"installation": map[string]any{"id": installationID},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected all bindings deleted, got %d", len(rows))
	}

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case ws := <-gotWS:
			seen[ws] = true
		case <-deadline:
			t.Fatalf("expected 2 deleted broadcasts (one per workspace), saw %v", seen)
		}
	}
	if !seen[testWorkspaceID] || !seen[uuidToString(wsB.ID)] {
		t.Errorf("deleted broadcasts must cover both workspaces; saw %v", seen)
	}
}

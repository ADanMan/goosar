package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

var githubAPIBase = "https://api.github.com"

const (
	githubReturnToGitHub       = "github"
	githubReturnToRepositories = "repositories"
	githubAPIResponseLimit     = 4 << 20
)

type GitHubInstallationResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	InstallationID   *int64  `json:"installation_id,omitempty"`
	AccountLogin     string  `json:"account_login"`
	AccountType      string  `json:"account_type"`
	AccountAvatarURL *string `json:"account_avatar_url"`
	CreatedAt        string  `json:"created_at"`
}

type GitHubPullRequestResponse struct {
	ID string `json:"id"`

	Provider        string  `json:"provider"`
	WorkspaceID     string  `json:"workspace_id"`
	RepoOwner       string  `json:"repo_owner"`
	RepoName        string  `json:"repo_name"`
	Number          int32   `json:"number"`
	Title           string  `json:"title"`
	State           string  `json:"state"`
	HtmlURL         string  `json:"html_url"`
	Branch          *string `json:"branch"`
	AuthorLogin     *string `json:"author_login"`
	AuthorAvatarURL *string `json:"author_avatar_url"`
	MergedAt        *string `json:"merged_at"`
	ClosedAt        *string `json:"closed_at"`
	PRCreatedAt     string  `json:"pr_created_at"`
	PRUpdatedAt     string  `json:"pr_updated_at"`

	MergeableState *string `json:"mergeable_state"`

	Mergeable *string `json:"mergeable"`

	MergeStateStatus *string `json:"merge_state_status"`

	SnapshotAvailable *bool `json:"snapshot_available,omitempty"`

	ChecksRollup *string `json:"checks_rollup"`

	ChecksConclusion *string `json:"checks_conclusion"`

	ChecksTotal   int64 `json:"checks_total"`
	ChecksPassed  int64 `json:"checks_passed"`
	ChecksFailed  int64 `json:"checks_failed"`
	ChecksRunning int64 `json:"checks_running"`
	ChecksPending int64 `json:"checks_pending"`

	FailedCheckNames []string `json:"failed_check_names"`

	SnapshotStale bool `json:"snapshot_stale"`

	SnapshotFetchedAt *string `json:"snapshot_fetched_at"`

	Additions    int32 `json:"additions"`
	Deletions    int32 `json:"deletions"`
	ChangedFiles int32 `json:"changed_files"`
}

type GitHubConnectResponse struct {
	URL        string `json:"url"`
	Configured bool   `json:"configured"`
}

type GitHubRepositoryResponse struct {
	ID            int64   `json:"id"`
	FullName      string  `json:"full_name"`
	HTMLURL       string  `json:"html_url"`
	CloneURL      string  `json:"clone_url"`
	Description   *string `json:"description"`
	Private       bool    `json:"private"`
	Archived      bool    `json:"archived"`
	DefaultBranch string  `json:"default_branch"`
}

type GitHubRepositoriesResponse struct {
	Repositories []GitHubRepositoryResponse `json:"repositories"`
	TotalCount   int64                      `json:"total_count"`
	NextPage     *int                       `json:"next_page"`
}

func githubInstallationToResponse(i db.GithubInstallation) GitHubInstallationResponse {
	instID := i.InstallationID
	return GitHubInstallationResponse{
		ID:               uuidToString(i.ID),
		WorkspaceID:      uuidToString(i.WorkspaceID),
		InstallationID:   &instID,
		AccountLogin:     i.AccountLogin,
		AccountType:      i.AccountType,
		AccountAvatarURL: textToPtr(i.AccountAvatarUrl),
		CreatedAt:        timestampToString(i.CreatedAt),
	}
}

func githubInstallationToBroadcast(i db.GithubInstallation) GitHubInstallationResponse {
	resp := githubInstallationToResponse(i)
	resp.InstallationID = nil
	return resp
}

func githubPullRequestToResponse(p db.GithubPullRequest, snapshotEnabled bool) GitHubPullRequestResponse {
	snapshotAvailable := currentGitHubSnapshotAvailable(
		snapshotEnabled, p.HeadSha, p.SnapshotHeadSha, p.SnapshotFetchedAt,
	)
	return GitHubPullRequestResponse{
		ID:                uuidToString(p.ID),
		Provider:          "github",
		WorkspaceID:       uuidToString(p.WorkspaceID),
		RepoOwner:         p.RepoOwner,
		RepoName:          p.RepoName,
		Number:            p.PrNumber,
		Title:             p.Title,
		State:             p.State,
		HtmlURL:           p.HtmlUrl,
		Branch:            textToPtr(p.Branch),
		AuthorLogin:       textToPtr(p.AuthorLogin),
		AuthorAvatarURL:   textToPtr(p.AuthorAvatarUrl),
		MergedAt:          timestampToPtr(p.MergedAt),
		ClosedAt:          timestampToPtr(p.ClosedAt),
		PRCreatedAt:       timestampToString(p.PrCreatedAt),
		PRUpdatedAt:       timestampToString(p.PrUpdatedAt),
		MergeableState:    textToPtr(p.MergeableState),
		SnapshotAvailable: &snapshotAvailable,

		ChecksConclusion: nil,
		FailedCheckNames: []string{},
		Additions:        p.Additions,
		Deletions:        p.Deletions,
		ChangedFiles:     p.ChangedFiles,
	}
}

const prSnapshotStaleThreshold = 30 * time.Minute

func issuePullRequestRowToResponse(p db.ListPullRequestsByIssueRow, snapshotEnabled bool) GitHubPullRequestResponse {
	snapshotAvailable := currentGitHubSnapshotAvailable(
		snapshotEnabled, p.HeadSha, p.SnapshotHeadSha, p.SnapshotFetchedAt,
	)
	stale := false
	if snapshotAvailable && (p.State == "open" || p.State == "draft") {
		stale = time.Since(p.SnapshotFetchedAt.Time) > prSnapshotStaleThreshold
	}
	failedNames := p.FailedCheckNames
	if failedNames == nil {
		failedNames = []string{}
	}
	resp := GitHubPullRequestResponse{
		ID:                uuidToString(p.ID),
		Provider:          "github",
		WorkspaceID:       uuidToString(p.WorkspaceID),
		RepoOwner:         p.RepoOwner,
		RepoName:          p.RepoName,
		Number:            p.PrNumber,
		Title:             p.Title,
		State:             p.State,
		HtmlURL:           p.HtmlUrl,
		Branch:            textToPtr(p.Branch),
		AuthorLogin:       textToPtr(p.AuthorLogin),
		AuthorAvatarURL:   textToPtr(p.AuthorAvatarUrl),
		MergedAt:          timestampToPtr(p.MergedAt),
		ClosedAt:          timestampToPtr(p.ClosedAt),
		PRCreatedAt:       timestampToString(p.PrCreatedAt),
		PRUpdatedAt:       timestampToString(p.PrUpdatedAt),
		MergeableState:    textToPtr(p.MergeableState),
		SnapshotAvailable: &snapshotAvailable,
		FailedCheckNames:  []string{},
		SnapshotStale:     stale,
		Additions:         p.Additions,
		Deletions:         p.Deletions,
		ChangedFiles:      p.ChangedFiles,
	}
	if snapshotAvailable {
		resp.Mergeable = lowerTextPtr(p.ApiMergeable)
		resp.MergeStateStatus = lowerTextPtr(p.ApiMergeStateStatus)
		resp.ChecksRollup = lowerTextPtr(p.ChecksRollupState)
		resp.ChecksConclusion = rollupToConclusion(p.ChecksRollupState, p.ChecksFailed, p.ChecksRunning, p.ChecksPassed)
		resp.ChecksTotal = p.ChecksTotal
		resp.ChecksPassed = p.ChecksPassed
		resp.ChecksFailed = p.ChecksFailed
		resp.ChecksRunning = p.ChecksRunning
		resp.ChecksPending = p.ChecksRunning
		resp.FailedCheckNames = failedNames
		resp.SnapshotFetchedAt = timestampToPtr(p.SnapshotFetchedAt)
	}
	return resp
}

func currentGitHubSnapshotAvailable(
	enabled bool,
	headSHA string,
	snapshotHeadSHA string,
	fetchedAt pgtype.Timestamptz,
) bool {
	return enabled && fetchedAt.Valid && snapshotHeadSHA != "" && snapshotHeadSHA == headSHA
}

func aggregateChecksConclusion(failed, passed, pending, total int64) *string {
	if total == 0 {
		return nil
	}
	var v string
	switch {
	case failed > 0:
		v = "failed"
	case pending > 0:
		v = "pending"
	case passed > 0:
		v = "passed"
	default:
		return nil
	}
	return &v
}

func lowerTextPtr(t pgtype.Text) *string {
	if !t.Valid || t.String == "" {
		return nil
	}
	v := strings.ToLower(t.String)
	return &v
}

func rollupToConclusion(rollup pgtype.Text, failed, running, passed int64) *string {
	if !rollup.Valid || rollup.String == "" {
		return nil
	}
	var v string
	switch strings.ToUpper(rollup.String) {
	case "FAILURE", "ERROR":
		v = "failed"
	case "PENDING", "EXPECTED":
		v = "pending"
	case "SUCCESS":
		v = "passed"
	default:
		switch {
		case failed > 0:
			v = "failed"
		case running > 0:
			v = "pending"
		case passed > 0:
			v = "passed"
		default:
			return nil
		}
	}
	return &v
}

func githubAppSlug() string { return strings.TrimSpace(os.Getenv("GITHUB_APP_SLUG")) }

func githubWebhookSecret() string { return strings.TrimSpace(os.Getenv("GITHUB_WEBHOOK_SECRET")) }

func isGitHubConfigured() bool { return githubAppSlug() != "" && githubWebhookSecret() != "" }

func isGitHubRepositoryBrowseConfigured() bool {
	return strings.TrimSpace(os.Getenv("GITHUB_APP_ID")) != "" &&
		strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY")) != ""
}

func signState(workspaceID string) (string, error) {
	return signStateForReturn(workspaceID, githubReturnToGitHub)
}

func signStateForReturn(workspaceID, returnTo string) (string, error) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", errors.New("github integration is not configured")
	}
	if !isAllowedGitHubReturnTo(returnTo) {
		return "", errors.New("invalid github return target")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)
	payload := workspaceID + "." + nonce
	if returnTo != githubReturnToGitHub {
		payload = workspaceID + "." + returnTo + "." + nonce
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func verifyState(token string) (string, bool) {
	workspaceID, _, ok := verifyStateWithReturn(token)
	return workspaceID, ok
}

func verifyStateWithReturn(token string) (workspaceID, returnTo string, ok bool) {
	secret := githubWebhookSecret()
	if secret == "" {
		return "", "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return "", "", false
	}
	workspaceID = parts[0]
	returnTo = githubReturnToGitHub
	nonceIndex := 1
	if len(parts) == 4 {
		returnTo = parts[1]
		nonceIndex = 2
		if !isAllowedGitHubReturnTo(returnTo) {
			return "", "", false
		}
	}
	sig := parts[nonceIndex+1]
	payload := strings.Join(parts[:nonceIndex+1], ".")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", "", false
	}
	return workspaceID, returnTo, true
}

func isAllowedGitHubReturnTo(returnTo string) bool {
	return returnTo == githubReturnToGitHub || returnTo == githubReturnToRepositories
}

func githubSettingsURL(frontend, returnTo string) string {
	if !isAllowedGitHubReturnTo(returnTo) {
		returnTo = githubReturnToGitHub
	}
	return strings.TrimRight(frontend, "/") + "/settings?tab=" + url.QueryEscape(returnTo)
}

func (h *Handler) GitHubConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if !isGitHubConfigured() {
		writeJSON(w, http.StatusOK, GitHubConnectResponse{Configured: false})
		return
	}
	returnTo := strings.TrimSpace(r.URL.Query().Get("return_to"))
	if returnTo == "" {
		returnTo = githubReturnToGitHub
	}
	if !isAllowedGitHubReturnTo(returnTo) {
		writeError(w, http.StatusBadRequest, "invalid return target")
		return
	}
	slug := githubAppSlug()
	state, err := signStateForReturn(workspaceID, returnTo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	installURL := fmt.Sprintf(
		"https://github.com/apps/%s/installations/new?state=%s",
		url.PathEscape(slug),
		url.QueryEscape(state),
	)
	writeJSON(w, http.StatusOK, GitHubConnectResponse{URL: installURL, Configured: true})
}

func (h *Handler) GitHubSetupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	installationIDStr := q.Get("installation_id")
	state := q.Get("state")
	frontend := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	settingsURL := githubSettingsURL(frontend, githubReturnToGitHub)

	if state == "" {
		http.Redirect(w, r, settingsURL+"&github_error=missing_params", http.StatusFound)
		return
	}
	workspaceID, returnTo, ok := verifyStateWithReturn(state)
	if !ok {
		http.Redirect(w, r, settingsURL+"&github_error=invalid_state", http.StatusFound)
		return
	}
	settingsURL = githubSettingsURL(frontend, returnTo)
	if installationIDStr == "" {
		http.Redirect(w, r, settingsURL+"&github_error=missing_params", http.StatusFound)
		return
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&github_error=bad_installation_id", http.StatusFound)
		return
	}
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&github_error=bad_workspace", http.StatusFound)
		return
	}

	login, accountType, avatar := fetchInstallationAccount(r.Context(), installationID)

	connectedBy := pgtype.UUID{}
	if userID := requestUserID(r); userID != "" {
		if u, err := parseStrictUUID(userID); err == nil {
			connectedBy = u
		}
	}

	inst, err := h.Queries.CreateGitHubInstallation(r.Context(), db.CreateGitHubInstallationParams{
		WorkspaceID:      wsUUID,
		InstallationID:   installationID,
		AccountLogin:     login,
		AccountType:      accountType,
		AccountAvatarUrl: ptrToText(avatar),
		ConnectedByID:    connectedBy,
	})
	if err != nil {
		slog.Error("github: failed to persist installation", "err", err, "installation_id", installationID)
		http.Redirect(w, r, settingsURL+"&github_error=persist_failed", http.StatusFound)
		return
	}
	inst, err = h.consumePendingGitHubInstallation(r.Context(), inst)
	if err != nil {
		slog.Error("github: failed to apply pending installation metadata", "err", err, "installation_id", installationID)
		http.Redirect(w, r, settingsURL+"&github_error=persist_failed", http.StatusFound)
		return
	}
	h.publish(protocol.EventGitHubInstallationCreated, workspaceID, "system", "", map[string]any{
		"installation": githubInstallationToBroadcast(inst),
	})
	http.Redirect(w, r, settingsURL+"&github_connected=1", http.StatusFound)
}

func (h *Handler) consumePendingGitHubInstallation(ctx context.Context, inst db.GithubInstallation) (db.GithubInstallation, error) {
	pending, err := h.Queries.GetPendingGitHubInstallation(ctx, inst.InstallationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return inst, nil
		}
		return inst, err
	}
	refreshed, err := h.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:      inst.WorkspaceID,
		InstallationID:   inst.InstallationID,
		AccountLogin:     pending.AccountLogin,
		AccountType:      coalesce(pending.AccountType, "User"),
		AccountAvatarUrl: pending.AccountAvatarUrl,
		ConnectedByID:    inst.ConnectedByID,
	})
	if err != nil {
		return inst, err
	}
	if err := h.Queries.DeletePendingGitHubInstallation(ctx, inst.InstallationID); err != nil {
		return inst, err
	}
	return refreshed, nil
}

func fetchInstallationAccount(ctx context.Context, installationID int64) (login, accountType string, avatar *string) {
	login = "unknown"
	accountType = "User"
	avatar = nil
	endpoint := fmt.Sprintf("%s/app/installations/%d", strings.TrimRight(githubAPIBase, "/"), installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token, err := signGitHubAppJWT(time.Now()); err != nil {

		slog.Warn("github: sign App JWT failed", "err", err)
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		Account struct {
			Login     string `json:"login"`
			Type      string `json:"type"`
			AvatarURL string `json:"avatar_url"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}
	if body.Account.Login != "" {
		login = body.Account.Login
	}
	if body.Account.Type != "" {
		accountType = body.Account.Type
	}
	if body.Account.AvatarURL != "" {
		v := body.Account.AvatarURL
		avatar = &v
	}
	return
}

func signGitHubAppJWT(now time.Time) (string, error) {
	appID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	pemKey := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if appID == "" || pemKey == "" {
		return "", nil
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pemKey))
	if err != nil {
		return "", fmt.Errorf("parse GITHUB_APP_PRIVATE_KEY: %w", err)
	}

	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign App JWT: %w", err)
	}
	return signed, nil
}

func (h *Handler) ListGitHubInstallations(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	rows, err := h.Queries.ListGitHubInstallationsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list installations")
		return
	}
	out := make([]GitHubInstallationResponse, 0, len(rows))
	for _, row := range rows {
		resp := githubInstallationToResponse(row)
		if !canManage {
			resp.InstallationID = nil
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":                out,
		"configured":                   isGitHubConfigured(),
		"repository_browse_configured": isGitHubRepositoryBrowseConfigured(),
		"can_manage":                   canManage,
	})
}

func (h *Handler) ListGitHubInstallationRepositories(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	installationRowID := chi.URLParam(r, "installationId")
	rowUUID, ok := parseUUIDOrBadRequest(w, installationRowID, "installation id")
	if !ok {
		return
	}
	row, err := h.Queries.GetGitHubInstallationByID(r.Context(), rowUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "github installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load github installation")
		return
	}
	if uuidToString(row.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "github installation not found")
		return
	}
	if !isGitHubRepositoryBrowseConfigured() {
		writeError(w, http.StatusServiceUnavailable, "github repository browsing is not configured")
		return
	}
	page, ok := parseGitHubPageParam(w, r, "page", 1, 1, 100000)
	if !ok {
		return
	}
	perPage, ok := parseGitHubPageParam(w, r, "per_page", 100, 1, 100)
	if !ok {
		return
	}

	repositories, err := fetchGitHubInstallationRepositories(
		r.Context(),
		row.InstallationID,
		page,
		perPage,
	)
	if err != nil {
		slog.Warn("github: list installation repositories failed", "err", err)
		writeError(w, http.StatusBadGateway, "failed to list github repositories")
		return
	}
	writeJSON(w, http.StatusOK, repositories)
}

func parseGitHubPageParam(
	w http.ResponseWriter,
	r *http.Request,
	name string,
	defaultValue, minValue, maxValue int,
) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return value, true
}

func fetchGitHubInstallationRepositories(
	ctx context.Context,
	installationID int64,
	page, perPage int,
) (GitHubRepositoriesResponse, error) {
	appJWT, err := signGitHubAppJWT(time.Now())
	if err != nil {
		return GitHubRepositoriesResponse{}, err
	}
	if appJWT == "" {
		return GitHubRepositoriesResponse{}, errors.New("github App JWT credentials unavailable")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	tokenEndpoint := fmt.Sprintf(
		"%s/app/installations/%d/access_tokens",
		strings.TrimRight(githubAPIBase, "/"),
		installationID,
	)
	tokenReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenEndpoint,
		strings.NewReader(`{"permissions":{"metadata":"read"}}`),
	)
	if err != nil {
		return GitHubRepositoriesResponse{}, err
	}
	setGitHubAPIHeaders(tokenReq, appJWT)
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return GitHubRepositoriesResponse{}, fmt.Errorf("create installation token: %w", err)
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(tokenResp.Body, githubAPIResponseLimit))
		return GitHubRepositoriesResponse{}, fmt.Errorf("create installation token: github status %d", tokenResp.StatusCode)
	}
	var tokenBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(tokenResp.Body, githubAPIResponseLimit)).Decode(&tokenBody); err != nil {
		return GitHubRepositoriesResponse{}, fmt.Errorf("decode installation token: %w", err)
	}
	if tokenBody.Token == "" {
		return GitHubRepositoriesResponse{}, errors.New("github returned an empty installation token")
	}
	defer revokeGitHubInstallationToken(client, tokenBody.Token)

	repositoriesEndpoint := fmt.Sprintf(
		"%s/installation/repositories?page=%d&per_page=%d",
		strings.TrimRight(githubAPIBase, "/"),
		page,
		perPage,
	)
	repositoriesReq, err := http.NewRequestWithContext(ctx, http.MethodGet, repositoriesEndpoint, nil)
	if err != nil {
		return GitHubRepositoriesResponse{}, err
	}
	setGitHubAPIHeaders(repositoriesReq, tokenBody.Token)
	repositoriesResp, err := client.Do(repositoriesReq)
	if err != nil {
		return GitHubRepositoriesResponse{}, fmt.Errorf("list installation repositories: %w", err)
	}
	defer repositoriesResp.Body.Close()
	if repositoriesResp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(repositoriesResp.Body, githubAPIResponseLimit))
		return GitHubRepositoriesResponse{}, fmt.Errorf("list installation repositories: github status %d", repositoriesResp.StatusCode)
	}
	var body struct {
		TotalCount   int64 `json:"total_count"`
		Repositories []struct {
			ID            int64   `json:"id"`
			FullName      string  `json:"full_name"`
			HTMLURL       string  `json:"html_url"`
			CloneURL      string  `json:"clone_url"`
			Description   *string `json:"description"`
			Private       bool    `json:"private"`
			Archived      bool    `json:"archived"`
			DefaultBranch string  `json:"default_branch"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(io.LimitReader(repositoriesResp.Body, githubAPIResponseLimit)).Decode(&body); err != nil {
		return GitHubRepositoriesResponse{}, fmt.Errorf("decode installation repositories: %w", err)
	}
	out := GitHubRepositoriesResponse{
		Repositories: make([]GitHubRepositoryResponse, 0, len(body.Repositories)),
		TotalCount:   body.TotalCount,
	}
	for _, repository := range body.Repositories {
		out.Repositories = append(out.Repositories, GitHubRepositoryResponse(repository))
	}
	if int64(page*perPage) < body.TotalCount {
		nextPage := page + 1
		out.NextPage = &nextPage
	}
	return out, nil
}

func setGitHubAPIHeaders(req *http.Request, token string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

func revokeGitHubInstallationToken(client *http.Client, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(githubAPIBase, "/") + "/installation/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return
	}
	setGitHubAPIHeaders(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, githubAPIResponseLimit))
}

func (h *Handler) DeleteGitHubInstallation(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	id := chi.URLParam(r, "installationId")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "installation id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteGitHubInstallation(r.Context(), db.DeleteGitHubInstallationParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove installation")
		return
	}
	h.publish(protocol.EventGitHubInstallationDeleted, workspaceID, "system", "", map[string]any{
		"id": id,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListPullRequestsForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListPullRequestsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}
	out := make([]GitHubPullRequestResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, issuePullRequestRowToResponse(row, h.PRRefresh.Enabled()))

		h.PRRefresh.MaybeEnqueueOnView(
			row.InstallationID, row.RepoOwner, row.RepoName, row.PrNumber,
			row.SnapshotFetchedAt.Time,
			row.SnapshotFetchedAt.Valid &&
				row.SnapshotHeadSha != "" &&
				row.SnapshotHeadSha == row.HeadSha,
		)
	}

	vcsRows, err := h.Queries.ListVCSPullRequestsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}
	for _, row := range vcsRows {
		out = append(out, vcsPullRequestRowToResponse(row))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].PRCreatedAt > out[j].PRCreatedAt
	})
	writeJSON(w, http.StatusOK, map[string]any{"pull_requests": out})
}

func (h *Handler) broadcastPRSnapshotApplied(ctx context.Context, prID pgtype.UUID) {
	pr, err := h.Queries.GetGitHubPullRequestByID(ctx, prID)
	if err != nil {
		return
	}
	issueIDs, err := h.Queries.ListIssueIDsForPullRequest(ctx, prID)
	if err != nil {
		return
	}
	linked := make([]string, 0, len(issueIDs))
	for _, id := range issueIDs {
		linked = append(linked, uuidToString(id))
	}
	h.publish(protocol.EventPullRequestUpdated, uuidToString(pr.WorkspaceID), "system", "", map[string]any{
		"pull_request":     githubPullRequestToResponse(pr, h.PRRefresh.Enabled()),
		"linked_issue_ids": linked,
	})
}

var identifierRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9]{1,9})-(\d+)\b`)

var closingIdentifierRe = regexp.MustCompile(
	`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[:\s]+([a-z][a-z0-9]{1,9})-(\d+)\b`,
)

func (h *Handler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	secret := githubWebhookSecret()
	if secret == "" {

		writeError(w, http.StatusServiceUnavailable, "github webhooks not configured")
		return
	}
	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if !verifyWebhookSignature(secret, sigHeader, body) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	ctx := r.Context()
	switch event {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]string{"ok": "pong"})
		return
	case "installation":
		h.handleInstallationEvent(ctx, body)
	case "pull_request":
		h.handlePullRequestEvent(ctx, body)
	case "check_suite", "check_run", "status":

		h.triggerPRRefreshFromCIEvent(ctx, body)
	default:

	}
	w.WriteHeader(http.StatusAccepted)
}

func verifyWebhookSignature(secret, header string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

type ghInstallationPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login     string `json:"login"`
			Type      string `json:"type"`
			AvatarURL string `json:"avatar_url"`
		} `json:"account"`
	} `json:"installation"`
}

func githubInstallationAccountFromPayload(p ghInstallationPayload) (login, accountType string, avatar *string, ok bool) {
	login = strings.TrimSpace(p.Installation.Account.Login)
	if login == "" {
		return "", "", nil, false
	}
	accountType = coalesce(p.Installation.Account.Type, "User")
	avatar = strPtrOrNil(p.Installation.Account.AvatarURL)
	return login, accountType, avatar, true
}

func (h *Handler) handleInstallationEvent(ctx context.Context, body []byte) {
	var p ghInstallationPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad installation payload", "err", err)
		return
	}
	switch p.Action {
	case "deleted", "suspend":

		deleted, err := h.Queries.DeleteGitHubInstallationByInstallationID(ctx, p.Installation.ID)
		if err != nil {
			slog.Warn("github: delete installation failed", "err", err, "installation_id", p.Installation.ID)
			return
		}
		if err := h.Queries.DeletePendingGitHubInstallation(ctx, p.Installation.ID); err != nil {
			slog.Warn("github: delete pending installation failed", "err", err, "installation_id", p.Installation.ID)
		}

		for _, row := range deleted {
			h.publish(protocol.EventGitHubInstallationDeleted, uuidToString(row.WorkspaceID), "system", "", map[string]any{
				"id": uuidToString(row.ID),
			})
		}
	case "created", "new_permissions_accepted", "unsuspend":
		login, accountType, avatar, ok := githubInstallationAccountFromPayload(p)
		if !ok {
			slog.Warn("github: installation payload missing account login", "installation_id", p.Installation.ID)
			return
		}

		existing, err := h.Queries.ListGitHubInstallationsByInstallationID(ctx, p.Installation.ID)
		if err != nil {
			slog.Warn("github: lookup installation failed", "err", err, "installation_id", p.Installation.ID)
			return
		}
		if len(existing) == 0 {
			if _, err := h.Queries.UpsertPendingGitHubInstallation(ctx, db.UpsertPendingGitHubInstallationParams{
				InstallationID:   p.Installation.ID,
				AccountLogin:     login,
				AccountType:      accountType,
				AccountAvatarUrl: ptrToText(avatar),
			}); err != nil {
				slog.Warn("github: store pending installation failed", "err", err, "installation_id", p.Installation.ID)
			}
			return
		}

		refreshed, err := h.Queries.UpdateGitHubInstallationAccountByInstallationID(ctx, db.UpdateGitHubInstallationAccountByInstallationIDParams{
			InstallationID:   p.Installation.ID,
			AccountLogin:     login,
			AccountType:      accountType,
			AccountAvatarUrl: ptrToText(avatar),
		})
		if err != nil {
			slog.Warn("github: refresh installation failed", "err", err)
			return
		}
		if err := h.Queries.DeletePendingGitHubInstallation(ctx, p.Installation.ID); err != nil {
			slog.Warn("github: delete pending installation failed", "err", err, "installation_id", p.Installation.ID)
		}

		for _, inst := range refreshed {
			h.publish(protocol.EventGitHubInstallationCreated, uuidToString(inst.WorkspaceID), "system", "", map[string]any{
				"installation": githubInstallationToBroadcast(inst),
			})
		}
	}
}

type ghPullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number         int32  `json:"number"`
		HTMLURL        string `json:"html_url"`
		Title          string `json:"title"`
		Body           string `json:"body"`
		State          string `json:"state"`
		Draft          bool   `json:"draft"`
		Merged         bool   `json:"merged"`
		MergedAt       string `json:"merged_at"`
		ClosedAt       string `json:"closed_at"`
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
		MergeableState string `json:"mergeable_state"`
		Additions      int32  `json:"additions"`
		Deletions      int32  `json:"deletions"`
		ChangedFiles   int32  `json:"changed_files"`
		Head           struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		User struct {
			Login     string `json:"login"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
	} `json:"pull_request"`
	Changes    *ghPRChanges `json:"changes"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (h *Handler) handlePullRequestEvent(ctx context.Context, body []byte) {
	var p ghPullRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github: bad pull_request payload", "err", err)
		return
	}
	if p.Installation.ID == 0 {
		return
	}
	insts, err := h.Queries.ListGitHubInstallationsByInstallationID(ctx, p.Installation.ID)
	if err != nil {
		slog.Warn("github: lookup installation failed", "err", err)
		return
	}
	if len(insts) == 0 {

		return
	}

	for _, inst := range insts {
		h.mirrorPullRequestForWorkspace(ctx, inst.WorkspaceID, inst.InstallationID, &p)
	}

	h.PRRefresh.Enqueue(p.Installation.ID, p.Repository.Owner.Login, p.Repository.Name, p.PullRequest.Number)
}

type ghCIEventPayload struct {
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`

	SHA        string `json:"sha"`
	CheckSuite struct {
		HeadSHA      string `json:"head_sha"`
		PullRequests []struct {
			Number int32 `json:"number"`
		} `json:"pull_requests"`
	} `json:"check_suite"`
	CheckRun struct {
		PullRequests []struct {
			Number int32 `json:"number"`
		} `json:"pull_requests"`
		CheckSuite struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_suite"`
	} `json:"check_run"`
}

func (h *Handler) triggerPRRefreshFromCIEvent(ctx context.Context, body []byte) {
	if !h.PRRefresh.Enabled() {
		return
	}
	var p ghCIEventPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return
	}
	if p.Installation.ID == 0 || p.Repository.Name == "" {
		return
	}
	owner, repo := p.Repository.Owner.Login, p.Repository.Name

	seen := map[int32]struct{}{}
	enqueue := func(number int32) {
		if number == 0 {
			return
		}
		if _, ok := seen[number]; ok {
			return
		}
		seen[number] = struct{}{}
		h.PRRefresh.Enqueue(p.Installation.ID, owner, repo, number)
	}
	for _, pr := range p.CheckSuite.PullRequests {
		enqueue(pr.Number)
	}
	for _, pr := range p.CheckRun.PullRequests {
		enqueue(pr.Number)
	}
	if len(seen) > 0 {
		return
	}

	sha := p.SHA
	if sha == "" {
		sha = coalesce(p.CheckSuite.HeadSHA, p.CheckRun.CheckSuite.HeadSHA)
	}
	if sha == "" {
		return
	}
	numbers, err := h.Queries.ListGitHubPRNumbersByHeadSHA(ctx, db.ListGitHubPRNumbersByHeadSHAParams{
		InstallationID: p.Installation.ID,
		RepoOwner:      owner,
		RepoName:       repo,
		HeadSha:        sha,
	})
	if err != nil {
		return
	}
	for _, number := range numbers {
		enqueue(number)
	}
}

func (h *Handler) mirrorPullRequestForWorkspace(ctx context.Context, wsID pgtype.UUID, installationID int64, p *ghPullRequestPayload) {
	state := derivePRState(p.PullRequest.State, p.PullRequest.Draft, p.PullRequest.Merged)
	mergeable, clearMergeable := derivePRMergeableState(p.Action, p.PullRequest.MergeableState, baseRefChanged(p.Changes))
	pr, err := h.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID:         wsID,
		InstallationID:      installationID,
		RepoOwner:           p.Repository.Owner.Login,
		RepoName:            p.Repository.Name,
		PrNumber:            p.PullRequest.Number,
		Title:               p.PullRequest.Title,
		State:               state,
		HtmlUrl:             p.PullRequest.HTMLURL,
		Branch:              ptrToText(strPtrOrNil(p.PullRequest.Head.Ref)),
		AuthorLogin:         ptrToText(strPtrOrNil(p.PullRequest.User.Login)),
		AuthorAvatarUrl:     ptrToText(strPtrOrNil(p.PullRequest.User.AvatarURL)),
		MergedAt:            parseGHTime(p.PullRequest.MergedAt),
		ClosedAt:            parseGHTime(p.PullRequest.ClosedAt),
		PrCreatedAt:         parseGHTimeRequired(p.PullRequest.CreatedAt),
		PrUpdatedAt:         parseGHTimeRequired(p.PullRequest.UpdatedAt),
		HeadSha:             p.PullRequest.Head.SHA,
		MergeableState:      mergeable,
		ClearMergeableState: pgtype.Bool{Bool: clearMergeable, Valid: true},
		Additions:           p.PullRequest.Additions,
		Deletions:           p.PullRequest.Deletions,
		ChangedFiles:        p.PullRequest.ChangedFiles,
	})
	if err != nil {
		slog.Warn("github: upsert pr failed", "err", err)
		return
	}

	workspaceID := uuidToString(wsID)
	resp := githubPullRequestToResponse(pr, h.PRRefresh.Enabled())

	linkedIssueIDs := make([]string, 0)
	if h.workspaceAutoLinkPRsEnabled(ctx, wsID) {
		idents := extractIdentifiers(p.PullRequest.Title, p.PullRequest.Body, p.PullRequest.Head.Ref)

		closingIdents := map[string]struct{}{}
		for _, c := range extractClosingIdentifiers(p.PullRequest.Title, p.PullRequest.Body) {
			closingIdents[c] = struct{}{}
		}

		qualifyingIdents := map[string]struct{}{}
		for _, id := range extractIdentifiers(p.PullRequest.Title, p.PullRequest.Head.Ref) {
			qualifyingIdents[id] = struct{}{}
		}
		for c := range closingIdents {
			qualifyingIdents[c] = struct{}{}
		}

		preserveCloseIntent := p.Action != "closed" && (state == "merged" || state == "closed")
		prefix := h.getIssuePrefix(ctx, wsID)

		reevalIssues := make([]db.Issue, 0, len(idents))
		for _, id := range idents {
			issue, ok := h.lookupIssueByIdentifier(ctx, wsID, prefix, id)
			if !ok {
				continue
			}
			_, declared := closingIdents[id]
			closeIntent := declared && !preserveCloseIntent
			_, qualifies := qualifyingIdents[id]
			referenceOnly := !qualifies
			if err := h.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
				IssueID:             issue.ID,
				PullRequestID:       pr.ID,
				CloseIntent:         closeIntent,
				ReferenceOnly:       referenceOnly,
				PreserveCloseIntent: preserveCloseIntent,
				LinkedByType:        strToText("system"),
				LinkedByID:          pgtype.UUID{},
			}); err != nil {
				slog.Warn("github: link failed", "err", err)
				continue
			}
			linkedIssueIDs = append(linkedIssueIDs, uuidToString(issue.ID))
			reevalIssues = append(reevalIssues, issue)
		}

		if state == "merged" || state == "closed" {
			for _, issue := range reevalIssues {
				if issue.Status == "done" || issue.Status == "cancelled" {
					continue
				}

				counts, err := h.Queries.GetIssueCombinedPullRequestCloseAggregate(ctx, issue.ID)
				if err != nil {
					slog.Warn("github: count linked pr states failed", "err", err, "issue_id", uuidToString(issue.ID))
					continue
				}
				if counts.OpenCount == 0 && counts.MergedWithCloseIntentCount > 0 {
					h.advanceIssueToDone(ctx, issue, workspaceID)
				}
			}
		}
	}

	h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request":     resp,
		"linked_issue_ids": linkedIssueIDs,
	})
}

func derivePRMergeableState(action, payload string, baseRefChanged bool) (pgtype.Text, bool) {
	if action == "opened" || action == "synchronize" || action == "reopened" {
		return pgtype.Text{}, true
	}
	if action == "edited" && baseRefChanged {
		return pgtype.Text{}, true
	}
	if payload == "" {
		return pgtype.Text{}, false
	}
	return pgtype.Text{String: payload, Valid: true}, false
}

type ghPRChanges struct {
	Base *struct {
		Ref *struct {
			From string `json:"from"`
		} `json:"ref"`
	} `json:"base"`
}

func baseRefChanged(c *ghPRChanges) bool {
	return c != nil && c.Base != nil && c.Base.Ref != nil && c.Base.Ref.From != ""
}

func derivePRState(state string, draft, merged bool) string {
	if merged {
		return "merged"
	}
	if state == "closed" {
		return "closed"
	}
	if draft {
		return "draft"
	}
	return "open"
}

func parseGHTime(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func parseGHTimeRequired(s string) pgtype.Timestamptz {
	t := parseGHTime(s)
	if !t.Valid {
		return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	return t
}

func extractIdentifiers(parts ...string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, src := range parts {
		for _, m := range identifierRe.FindAllStringSubmatch(src, -1) {
			ident := strings.ToUpper(m[1]) + "-" + m[2]
			if _, dup := seen[ident]; dup {
				continue
			}
			seen[ident] = struct{}{}
			out = append(out, ident)
		}
	}
	return out
}

func extractClosingIdentifiers(parts ...string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, src := range parts {
		for _, m := range closingIdentifierRe.FindAllStringSubmatch(src, -1) {
			ident := strings.ToUpper(m[1]) + "-" + m[2]
			if _, dup := seen[ident]; dup {
				continue
			}
			seen[ident] = struct{}{}
			out = append(out, ident)
		}
	}
	return out
}

func (h *Handler) workspaceAutoLinkPRsEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil || len(ws.Settings) == 0 {
		return true
	}
	var s struct {
		GitHubEnabled            *bool `json:"github_enabled"`
		GitHubAutoLinkPRsEnabled *bool `json:"github_auto_link_prs_enabled"`
	}
	if err := json.Unmarshal(ws.Settings, &s); err != nil {
		return true
	}
	if s.GitHubEnabled != nil && !*s.GitHubEnabled {
		return false
	}
	if s.GitHubAutoLinkPRsEnabled == nil {
		return true
	}
	return *s.GitHubAutoLinkPRsEnabled
}

func (h *Handler) lookupIssueByIdentifier(ctx context.Context, workspaceID pgtype.UUID, prefix, identifier string) (db.Issue, bool) {
	idx := strings.LastIndex(identifier, "-")
	if idx < 0 {
		return db.Issue{}, false
	}
	gotPrefix, numStr := identifier[:idx], identifier[idx+1:]
	if !strings.EqualFold(gotPrefix, prefix) {
		return db.Issue{}, false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: workspaceID,
		Number:      int32(n),
	})
	if err != nil {
		return db.Issue{}, false
	}
	return issue, true
}

func (h *Handler) advanceIssueToDone(ctx context.Context, issue db.Issue, workspaceID string) {
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          issue.ID,
		Status:      "done",
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("github: advance issue to done failed", "err", err)
		return
	}

	h.notifyParentOfChildDone(ctx, issue, updated)

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	resp := issueToResponse(updated, prefix)
	h.publish(protocol.EventIssueUpdated, workspaceID, "system", "", map[string]any{
		"issue":          resp,
		"status_changed": true,
		"prev_status":    issue.Status,
		"creator_type":   issue.CreatorType,
		"creator_id":     uuidToString(issue.CreatorID),
		"source":         "github_pr_merged",
	})
}

func parseStrictUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

func coalesce(a, fallback string) string {
	if strings.TrimSpace(a) == "" {
		return fallback
	}
	return a
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

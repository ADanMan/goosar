package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

const maxWebhookBodyBytes = 1 << 20 // 1 MiB, тот же порядок, что contract называет для webhooks/stripe

// verifyHMACSignature — X-Hub-Signature-256: sha256=<hex hmac(body, secret)>,
// используется и GitHub, и (по секрету соединения) VCS-вебхуком.
func verifyHMACSignature(secret string, body []byte, header string) bool {
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

// handleGitHubWebhook — POST /api/webhooks/github (public, HMAC).
func (d *Deps) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if d.Cfg.GitHubWebhookSecret == "" {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "GitHub webhooks not configured", "github_webhooks_not_configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes+1))
	if err != nil || len(body) > maxWebhookBodyBytes {
		httpapi.BadRequest(w, "payload too large or unreadable")
		return
	}
	if !verifyHMACSignature(d.Cfg.GitHubWebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		httpapi.Unauthorized(w, "invalid X-Hub-Signature-256")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	switch event {
	case "ping":
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"ok": "pong"})
		return
	case "installation":
		d.handleGitHubInstallationEvent(r, body)
	case "pull_request":
		d.handleGitHubPullRequestEvent(r, body)
	case "check_suite", "check_run", "status":
		// contract: "запускает пересчёт снапшота PR" — этой сессии
		// достаточно принять событие; полноценный пересчёт снапшота (набор
		// проверок конкретного PR по SHA) выходит за пределы T-029 и
		// зафиксирован как пробел в server2/docs/decisions.md.
	}
	w.WriteHeader(http.StatusAccepted)
}

type githubInstallationPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
	} `json:"installation"`
}

func (d *Deps) handleGitHubInstallationEvent(r *http.Request, body []byte) {
	var p githubInstallationPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return
	}
	switch p.Action {
	case "deleted", "suspend":
		ids, err := d.Store.DeleteGitHubInstallationsByGHID(r.Context(), p.Installation.ID)
		if err == nil {
			for _, wsID := range ids {
				d.publish(wsID, "github_installation:deleted", map[string]any{"gh_installation_id": p.Installation.ID})
			}
		}
	default: // created, unsuspend, new_permissions_accepted...
		installation, found, err := d.Store.UpsertGitHubInstallationByGHID(r.Context(), p.Installation.ID,
			p.Installation.Account.Login, p.Installation.Account.Type, nil)
		if err == nil && found {
			d.publish(installation.WorkspaceID, "github_installation:created", map[string]any{"installation": githubInstallationJSON(installation, true)})
		}
		// found == false: инсталляция пока не связана с воркспейсом (т.е.
		// /api/github/setup ещё не прошёл) — вебхук installation.created
		// обычно приходит раньше редиректа коллбэка; contract рассчитывает
		// на UpsertGitHubInstallationByGHID именно в setup callback, здесь
		// это best-effort обновление уже существующей строки.
	}
}

type githubPullRequestPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	Repository  struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	PullRequest struct {
		HTMLURL string `json:"html_url"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
}

func (d *Deps) handleGitHubPullRequestEvent(r *http.Request, body []byte) {
	var p githubPullRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return
	}
	workspaceID, ok := d.workspaceForGitHubInstallation(r, p.Installation.ID)
	if !ok {
		return
	}
	keys := extractDisplayKeys(p.PullRequest.Title + " " + p.PullRequest.Body + " " + p.PullRequest.Head.Ref)
	tickets, err := d.Store.FindTicketsByDisplayKeys(r.Context(), workspaceID, keys)
	if err != nil {
		return
	}
	prState := p.PullRequest.State
	for _, t := range tickets {
		if err := d.Store.UpsertPRLink(r.Context(), t.ID, "github", p.PullRequest.HTMLURL, p.Number, &p.PullRequest.Title, &prState); err == nil {
			d.publish(t.WorkspaceID, "pull_request:updated", map[string]any{
				"issue_id": t.ID, "provider": "github", "url": p.PullRequest.HTMLURL, "number": p.Number, "state": prState,
			})
		}
	}
}

func (d *Deps) workspaceForGitHubInstallation(r *http.Request, ghInstallationID int64) (string, bool) {
	var workspaceID string
	err := d.Store.db.Pool.QueryRow(r.Context(), `SELECT workspace_id FROM github_installations WHERE gh_installation_id = $1`, ghInstallationID).Scan(&workspaceID)
	return workspaceID, err == nil
}

// displayKeyPattern — issue_prefix (буквы/цифры) + '-' + номер, до 10 букв
// префикса; contract не фиксирует точный формат ключа задачи, но
// tk_display_key (005_tasks.up.sql) устроен ровно так же, как
// issue_prefix+seq_number из /api/workspaces (docs/50-api-contract.md,
// "issue_prefix"), — решение T-029 в server2/docs/decisions.md.
var displayKeyPattern = regexp.MustCompile(`(?i)\b([A-Za-z]{1,10}-\d{1,10})\b`)

func extractDisplayKeys(text string) []string {
	matches := displayKeyPattern.FindAllString(text, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		u := strings.ToUpper(m)
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// handleVCSWebhook — POST /api/webhooks/vcs/{connectionId} (public, HMAC на
// секрет соединения).
func (d *Deps) handleVCSWebhook(w http.ResponseWriter, r *http.Request) {
	if !d.vcsAvailable() {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "VCS webhooks not configured", "vcs_webhooks_not_configured")
		return
	}
	connectionID := r.PathValue("connectionId")
	secret, found, err := d.Store.VCSConnectionSecretByID(r.Context(), connectionID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.WriteError(w, http.StatusNotFound, "unknown connection, or VCS integration disabled", "not_found")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes+1))
	if err != nil || len(body) > maxWebhookBodyBytes {
		httpapi.BadRequest(w, "payload too large or unreadable")
		return
	}
	plainSecret, ok := unseal(d.Cfg.VCSSecretKey, d.Cfg.VCSSecretKeyPrevious, secret.WebhookSecretSealed)
	if !ok || !verifyHMACSignature(string(plainSecret), body, r.Header.Get("X-Hub-Signature-256")) {
		httpapi.Unauthorized(w, "invalid signature")
		return
	}
	d.handleVCSPullRequestPayload(r, secret, body)
	w.WriteHeader(http.StatusAccepted)
}

type vcsMergeRequestPayload struct {
	ObjectAttributes struct {
		URL         string `json:"url"`
		IID         int    `json:"iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       string `json:"state"`
		SourceBranch string `json:"source_branch"`
	} `json:"object_attributes"`
}

func (d *Deps) handleVCSPullRequestPayload(r *http.Request, conn VCSConnectionSecret, body []byte) {
	var p vcsMergeRequestPayload
	if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.URL == "" {
		return
	}
	keys := extractDisplayKeys(p.ObjectAttributes.Title + " " + p.ObjectAttributes.Description + " " + p.ObjectAttributes.SourceBranch)
	tickets, err := d.Store.FindTicketsByDisplayKeys(r.Context(), conn.WorkspaceID, keys)
	if err != nil {
		return
	}
	state := p.ObjectAttributes.State
	for _, t := range tickets {
		if err := d.Store.UpsertPRLink(r.Context(), t.ID, conn.Provider, p.ObjectAttributes.URL, p.ObjectAttributes.IID, &p.ObjectAttributes.Title, &state); err == nil {
			d.publish(t.WorkspaceID, "pull_request:updated", map[string]any{
				"issue_id": t.ID, "provider": conn.Provider, "url": p.ObjectAttributes.URL, "number": p.ObjectAttributes.IID, "state": state,
			})
		}
	}
}

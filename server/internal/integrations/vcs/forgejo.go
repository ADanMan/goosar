package vcs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type forgejoProvider struct{ kind Kind }

func init() {
	register(forgejoProvider{kind: KindForgejo})
	register(forgejoProvider{kind: KindGitea})
}

func (p forgejoProvider) Kind() Kind { return p.kind }

func (p forgejoProvider) EventKind(h http.Header) EventKind {
	event := h.Get("X-Gitea-Event")
	if event == "" {
		event = h.Get("X-GitHub-Event")
	}
	switch event {
	case "pull_request":
		return EventPullRequest
	case "status":
		return EventCIStatus
	default:
		return EventOther
	}
}

func (p forgejoProvider) VerifySignature(secret string, h http.Header, body []byte) bool {

	if secret == "" {
		return false
	}
	sig := strings.TrimSpace(h.Get("X-Gitea-Signature"))
	sig = strings.TrimPrefix(sig, "sha256=")
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

type fjPullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number       int32  `json:"number"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		State        string `json:"state"`
		Merged       bool   `json:"merged"`
		Draft        bool   `json:"draft"`
		HTMLURL      string `json:"html_url"`
		Additions    int32  `json:"additions"`
		Deletions    int32  `json:"deletions"`
		ChangedFiles int32  `json:"changed_files"`
		MergedAt     string `json:"merged_at"`
		ClosedAt     string `json:"closed_at"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
		User         struct {
			Login     string `json:"login"`
			UserName  string `json:"username"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
			Sha string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login    string `json:"login"`
			UserName string `json:"username"`
		} `json:"owner"`
	} `json:"repository"`
}

func (p forgejoProvider) ParsePullRequest(body []byte) (PullRequestEvent, error) {
	var d fjPullRequestPayload
	if err := json.Unmarshal(body, &d); err != nil {
		return PullRequestEvent{}, err
	}
	owner := coalesce(d.Repository.Owner.UserName, d.Repository.Owner.Login)
	if owner == "" {
		if i := strings.Index(d.Repository.FullName, "/"); i > 0 {
			owner = d.Repository.FullName[:i]
		}
	}
	return PullRequestEvent{
		Action:          d.Action,
		RepoOwner:       owner,
		RepoName:        d.Repository.Name,
		Number:          d.PullRequest.Number,
		Title:           d.PullRequest.Title,
		Body:            d.PullRequest.Body,
		State:           derivePRState(d.PullRequest.State, d.PullRequest.Draft, d.PullRequest.Merged),
		HTMLURL:         d.PullRequest.HTMLURL,
		Branch:          d.PullRequest.Head.Ref,
		HeadSHA:         d.PullRequest.Head.Sha,
		AuthorLogin:     coalesce(d.PullRequest.User.UserName, d.PullRequest.User.Login),
		AuthorAvatarURL: d.PullRequest.User.AvatarURL,
		Additions:       d.PullRequest.Additions,
		Deletions:       d.PullRequest.Deletions,
		ChangedFiles:    d.PullRequest.ChangedFiles,
		MergedAt:        d.PullRequest.MergedAt,
		ClosedAt:        d.PullRequest.ClosedAt,
		CreatedAt:       d.PullRequest.CreatedAt,
		UpdatedAt:       d.PullRequest.UpdatedAt,
	}, nil
}

type fjStatusPayload struct {
	SHA         string `json:"sha"`
	Context     string `json:"context"`
	State       string `json:"state"`
	TargetURL   string `json:"target_url"`
	Description string `json:"description"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (p forgejoProvider) ParseCIStatus(body []byte) (CIStatusEvent, error) {
	var d fjStatusPayload
	if err := json.Unmarshal(body, &d); err != nil {
		return CIStatusEvent{}, err
	}

	updatedAt := d.UpdatedAt
	if updatedAt == "" {
		updatedAt = d.CreatedAt
	}
	return CIStatusEvent{
		SHA:         d.SHA,
		Context:     d.Context,
		State:       normalizeForgejoState(d.State),
		TargetURL:   d.TargetURL,
		Description: d.Description,
		UpdatedAt:   updatedAt,
	}, nil
}

func normalizeForgejoState(s string) string {
	switch s {
	case "success", "warning":
		return "passed"
	case "failure", "error":
		return "failed"
	default:
		return "pending"
	}
}

func (p forgejoProvider) ValidateToken(ctx context.Context, instanceURL, token string) (Account, error) {
	endpoint := NormalizeInstanceURL(instanceURL) + "/api/v1/user"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Account{}, fmt.Errorf("forgejo: build request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Account{}, fmt.Errorf("forgejo: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {

		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Warn("forgejo: token validation rejected",
			"endpoint", endpoint,
			"status", resp.StatusCode,
			"body", strings.TrimSpace(string(b)))
		return Account{}, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Account{}, fmt.Errorf("forgejo: GET /user: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var u struct {
		Login    string `json:"login"`
		UserName string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return Account{}, fmt.Errorf("forgejo: decode user: %w", err)
	}
	login := coalesce(u.Login, u.UserName)
	if login == "" {
		return Account{}, errors.New("forgejo: user response missing login")
	}
	return Account{Login: login}, nil
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func NormalizeInstanceURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
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

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

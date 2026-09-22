package vcs

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type gitlabProvider struct{}

func init() { register(gitlabProvider{}) }

func (gitlabProvider) Kind() Kind { return KindGitLab }

func (gitlabProvider) EventKind(h http.Header) EventKind {
	switch h.Get("X-Gitlab-Event") {
	case "Merge Request Hook":
		return EventPullRequest
	case "Pipeline Hook":
		return EventCIStatus
	default:
		return EventOther
	}
}

func (gitlabProvider) VerifySignature(secret string, h http.Header, _ []byte) bool {
	if secret == "" {
		return false
	}
	got := h.Get("X-Gitlab-Token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
}

type glMergeRequestPayload struct {
	ObjectKind string `json:"object_kind"`
	User       struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	ObjectAttributes struct {
		IID            int32  `json:"iid"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		State          string `json:"state"`
		Action         string `json:"action"`
		SourceBranch   string `json:"source_branch"`
		URL            string `json:"url"`
		Draft          bool   `json:"draft"`
		WorkInProgress bool   `json:"work_in_progress"`
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
		LastCommit     struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

func (gitlabProvider) ParsePullRequest(body []byte) (PullRequestEvent, error) {
	var d glMergeRequestPayload
	if err := json.Unmarshal(body, &d); err != nil {
		return PullRequestEvent{}, err
	}
	owner, name := splitNamespace(d.Project.PathWithNamespace)
	draft := d.ObjectAttributes.Draft || d.ObjectAttributes.WorkInProgress ||
		strings.HasPrefix(strings.ToLower(d.ObjectAttributes.Title), "draft:")
	return PullRequestEvent{
		Action:          d.ObjectAttributes.Action,
		RepoOwner:       owner,
		RepoName:        name,
		Number:          d.ObjectAttributes.IID,
		Title:           d.ObjectAttributes.Title,
		Body:            d.ObjectAttributes.Description,
		State:           normalizeGitLabMRState(d.ObjectAttributes.State, draft),
		HTMLURL:         d.ObjectAttributes.URL,
		Branch:          d.ObjectAttributes.SourceBranch,
		HeadSHA:         d.ObjectAttributes.LastCommit.ID,
		AuthorLogin:     d.User.Username,
		AuthorAvatarURL: d.User.AvatarURL,
		CreatedAt:       normalizeGitLabTime(d.ObjectAttributes.CreatedAt),
		UpdatedAt:       normalizeGitLabTime(d.ObjectAttributes.UpdatedAt),
	}, nil
}

func normalizeGitLabTime(s string) string {
	if s == "" {
		return ""
	}
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05.999999 MST",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}

func normalizeGitLabMRState(state string, draft bool) string {
	switch state {
	case "merged":
		return "merged"
	case "closed":
		return "closed"
	default:
		if draft {
			return "draft"
		}
		return "open"
	}
}

type glPipelinePayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		SHA        string `json:"sha"`
		Status     string `json:"status"`
		URL        string `json:"url"`
		CreatedAt  string `json:"created_at"`
		FinishedAt string `json:"finished_at"`
	} `json:"object_attributes"`
}

func (gitlabProvider) ParseCIStatus(body []byte) (CIStatusEvent, error) {
	var d glPipelinePayload
	if err := json.Unmarshal(body, &d); err != nil {
		return CIStatusEvent{}, err
	}

	updatedAt := d.ObjectAttributes.FinishedAt
	if updatedAt == "" {
		updatedAt = d.ObjectAttributes.CreatedAt
	}
	return CIStatusEvent{
		SHA: d.ObjectAttributes.SHA,

		Context:   "gitlab/pipeline",
		State:     normalizeGitLabPipelineState(d.ObjectAttributes.Status),
		TargetURL: d.ObjectAttributes.URL,
		UpdatedAt: normalizeGitLabTime(updatedAt),
	}, nil
}

func normalizeGitLabPipelineState(s string) string {
	switch s {
	case "success", "skipped":
		return "passed"
	case "failed", "canceled":
		return "failed"
	default:
		return "pending"
	}
}

func (gitlabProvider) ValidateToken(ctx context.Context, instanceURL, token string) (Account, error) {
	endpoint := NormalizeInstanceURL(instanceURL) + "/api/v4/user"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Account{}, fmt.Errorf("gitlab: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Account{}, fmt.Errorf("gitlab: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Account{}, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Account{}, fmt.Errorf("gitlab: GET /user: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var u struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return Account{}, fmt.Errorf("gitlab: decode user: %w", err)
	}
	if u.Username == "" {
		return Account{}, errors.New("gitlab: user response missing username")
	}
	return Account{Login: u.Username}, nil
}

func splitNamespace(path string) (owner, name string) {
	path = strings.Trim(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}

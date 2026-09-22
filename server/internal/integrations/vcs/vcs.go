// Пакет vcs — абстракция провайдеров Git с токенной авторизацией, откуда Goosar
// зеркалит пул-реквесты и статус CI: Forgejo, Gitea и GitLab. GitHub намеренно
// не здесь — у него своя модель приложений и отдельный handler.
package vcs

import (
	"context"
	"errors"
	"net/http"
)

type Kind string

const (
	KindForgejo Kind = "forgejo"
	KindGitea   Kind = "gitea"
	KindGitLab  Kind = "gitlab"
)

func (k Kind) Valid() bool {
	switch k {
	case KindForgejo, KindGitea, KindGitLab:
		return true
	}
	return false
}

var ErrUnauthorized = errors.New("vcs: token unauthorized")

type EventKind int

const (
	EventOther EventKind = iota
	EventPullRequest
	EventCIStatus
)

type PullRequestEvent struct {
	Action          string
	RepoOwner       string
	RepoName        string
	Number          int32
	Title           string
	Body            string
	State           string
	HTMLURL         string
	Branch          string
	HeadSHA         string
	AuthorLogin     string
	AuthorAvatarURL string
	Additions       int32
	Deletions       int32
	ChangedFiles    int32
	MergedAt        string
	ClosedAt        string
	CreatedAt       string
	UpdatedAt       string
}

func (e PullRequestEvent) Terminal() bool {
	switch e.Action {
	case "closed", "merged", "merge", "close":
		return true
	}
	return false
}

type CIStatusEvent struct {
	SHA         string
	Context     string
	State       string
	TargetURL   string
	Description string

	UpdatedAt string
}

type Account struct {
	Login string
}

type Provider interface {
	Kind() Kind

	EventKind(h http.Header) EventKind

	VerifySignature(secret string, h http.Header, body []byte) bool

	ParsePullRequest(body []byte) (PullRequestEvent, error)

	ParseCIStatus(body []byte) (CIStatusEvent, error)

	ValidateToken(ctx context.Context, instanceURL, token string) (Account, error)
}

var registry = map[Kind]Provider{}

func register(p Provider) { registry[p.Kind()] = p }

func For(kind string) (Provider, bool) {
	p, ok := registry[Kind(kind)]
	return p, ok
}

package task

import (
	"context"
	"testing"

	"github.com/adanman/goosar/server2/internal/workspace"
)

// TestPullRequestsReturnsFullCard — docs/50-api-contract-changes.md п.3:
// проверяет, что PullRequests (backing GET /api/issues/{id}/pull-requests)
// заполняет workspace_id через JOIN с tickets и отдаёт как заполненные
// вебхуком поля (repo_owner/repo_name/branch/author...), так и явные null
// для полей снапшота/чеклистов, которые эта сессия не заполняет (см.
// комментарий у PullRequestLink).
func TestPullRequestsReturnsFullCard(t *testing.T) {
	db := newTestStore(t)
	s := NewStore(db)
	f := seedWorkspace(t, db)

	wsStore := workspace.NewStore(db)
	issue, err := s.CreateIssue(context.Background(), wsStore, CreateParams{
		WorkspaceID: f.WorkspaceID, Title: "issue with a PR", CreatorType: "member", CreatorID: f.AccountID,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	title, state := "Fix the thing", "open"
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO ticket_pr_links (ticket_id, tpr_provider, tpr_url, tpr_number, tpr_title, tpr_state,
		                              tpr_repo_owner, tpr_repo_name, tpr_branch, tpr_author_login)
		VALUES ($1, 'github', 'https://github.com/acme/repo/pull/7', 7, $2, $3, 'acme', 'repo', 'feature-x', 'octocat')`,
		issue.ID, title, state); err != nil {
		t.Fatalf("seed ticket_pr_links: %v", err)
	}

	links, err := s.PullRequests(context.Background(), f.WorkspaceID, issue.ID)
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	l := links[0]
	if l.WorkspaceID != f.WorkspaceID {
		t.Errorf("workspace_id = %q, want %q", l.WorkspaceID, f.WorkspaceID)
	}
	if l.RepoOwner == nil || *l.RepoOwner != "acme" || l.RepoName == nil || *l.RepoName != "repo" {
		t.Errorf("repo_owner/repo_name not populated from webhook fields: %+v %+v", l.RepoOwner, l.RepoName)
	}
	if l.Branch == nil || *l.Branch != "feature-x" {
		t.Errorf("branch not populated: %v", l.Branch)
	}
	if l.HTMLURL != "https://github.com/acme/repo/pull/7" {
		t.Errorf("html_url = %q", l.HTMLURL)
	}
	if l.SnapshotAvailable {
		t.Errorf("snapshot_available should default false, this session does not populate it")
	}
	if l.ChecksRollup != nil || l.Additions != nil {
		t.Errorf("checks/diff fields should be null (documented gap), got checks_rollup=%v additions=%v", l.ChecksRollup, l.Additions)
	}
}

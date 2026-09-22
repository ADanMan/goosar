package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func deleteUserRequest(t *testing.T, actorID, targetID string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(http.MethodDelete, "/api/deployment/users/"+targetID, nil)
	req.Header.Set("X-User-ID", actorID)
	req = withURLParam(req, "userId", targetID)
	rec := httptest.NewRecorder()
	testHandler.DeleteDeploymentUser(rec, req)
	return rec
}

type erasureSubject struct {
	userID      string
	email       string
	workspaceID string
	issueID     string
	commentID   string
}

func newErasureSubject(t *testing.T, label string) erasureSubject {
	t.Helper()
	ctx := context.Background()
	var s erasureSubject
	s.userID, s.email = deploymentUserFixture(t, label)
	s.workspaceID = testWorkspaceID

	must := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture %s: %v", what, err)
		}
	}
	must(testPool.QueryRow(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member') RETURNING id`,
		s.workspaceID, s.userID).Scan(new(string)), "member")
	must(testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, number, title, status, creator_type, creator_id)
		 VALUES ($1, (SELECT coalesce(max(number), 0) + 1 FROM issue WHERE workspace_id = $1),
		         'work that outlives the person', 'todo', 'member', $2) RETURNING id`,
		s.workspaceID, s.userID).Scan(&s.issueID), "issue")
	must(testPool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		 VALUES ($1, $2, 'member', $3, 'a comment that outlives the person') RETURNING id`,
		s.workspaceID, s.issueID, s.userID).Scan(&s.commentID), "comment")

	must(exec(ctx, `INSERT INTO verification_code (email, code, expires_at) VALUES ($1, '123456', now() + interval '1 hour')`, s.email), "verification_code")
	must(exec(ctx, `INSERT INTO notification_preference (workspace_id, user_id, preferences) VALUES ($1, $2, '{}'::jsonb)`, s.workspaceID, s.userID), "notification_preference")
	must(exec(ctx, `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, title) VALUES ($1, 'member', $2, 'mention', 'hello')`, s.workspaceID, s.userID), "inbox_item")

	must(exec(ctx,
		`INSERT INTO user_identity (user_id, provider, subject, email_at_link) VALUES ($1, 'oidc', $2, $3)`,
		s.userID, "https://idp.example.test|"+s.email, s.email), "user_identity")

	must(exec(ctx,
		`INSERT INTO workspace_invitation (workspace_id, inviter_id, invitee_email, invitee_user_id, role, status, expires_at)
		 VALUES ($1, $2, $3, $2, 'member', 'accepted', now() + interval '1 day')`,
		s.workspaceID, s.userID, s.email), "workspace_invitation")

	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, s.issueID)
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id = $1`, s.userID)
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, s.email)
		testPool.Exec(ctx, `DELETE FROM workspace_invitation WHERE invitee_email = $1`, s.email)
		testPool.Exec(ctx, `DELETE FROM user_identity WHERE user_id = $1`, s.userID)
	})
	return s
}

func exec(ctx context.Context, sql string, args ...any) error {
	_, err := testPool.Exec(ctx, sql, args...)
	return err
}

func countRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestUserDeletionAnonymisesPersonalDataAndKeepsTheWork(t *testing.T) {
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "eraser")
	grantDeploymentAdminFixture(t, adminID)
	subject := newErasureSubject(t, "subject")

	rec := deleteUserRequest(t, adminID, subject.userID)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE user = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var resp DeleteDeploymentUserResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Email == subject.email {
		t.Error("the account still carries the subject's e-mail address")
	}
	if !strings.HasSuffix(resp.Email, "@deleted.invalid") {
		t.Errorf("email = %q, want a tombstone address", resp.Email)
	}
	if resp.Name != TombstoneUserName {
		t.Errorf("name = %q, want %q", resp.Name, TombstoneUserName)
	}

	var name, email string
	var avatar, profile *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT name, email, avatar_url, nullif(profile_description, '') FROM "user" WHERE id = $1`,
		subject.userID).Scan(&name, &email, &avatar, &profile); err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if strings.Contains(email, subject.email) || name != TombstoneUserName {
		t.Errorf("stored identity = %q / %q, want a tombstone", name, email)
	}
	if avatar != nil || profile != nil {
		t.Errorf("avatar/profile survived erasure: %v / %v", avatar, profile)
	}

	if countRows(t, `SELECT count(*) FROM issue WHERE id = $1`, subject.issueID) != 1 {
		t.Error("the subject's issue was deleted along with their personal data")
	}
	if countRows(t, `SELECT count(*) FROM comment WHERE id = $1`, subject.commentID) != 1 {
		t.Error("the subject's comment was deleted along with their personal data")
	}

	for _, tc := range []struct {
		what string
		sql  string
		args []any
	}{
		{"membership", `SELECT count(*) FROM member WHERE user_id = $1`, []any{subject.userID}},
		{"login code", `SELECT count(*) FROM verification_code WHERE email = $1`, []any{subject.email}},
		{"notification preference", `SELECT count(*) FROM notification_preference WHERE user_id = $1`, []any{subject.userID}},
		{"inbox item", `SELECT count(*) FROM inbox_item WHERE recipient_type = 'member' AND recipient_id = $1`, []any{subject.userID}},

		{"workspace invitation", `SELECT count(*) FROM workspace_invitation WHERE invitee_email = $1 OR invitee_user_id = $2`, []any{subject.email, subject.userID}},

		{"corporate identity link", `SELECT count(*) FROM user_identity WHERE user_id = $1`, []any{subject.userID}},
	} {
		if n := countRows(t, tc.sql, tc.args...); n != 0 {
			t.Errorf("%s survived the erasure (%d rows)", tc.what, n)
		}
	}
	if resp.RemovedMemberships == 0 {
		t.Error("the response does not report the removed memberships")
	}

	var deactivated bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT deactivated_at IS NOT NULL FROM "user" WHERE id = $1`, subject.userID).Scan(&deactivated); err != nil {
		t.Fatalf("read deactivation: %v", err)
	}
	if !deactivated {
		t.Error("an erased account is not blocked, so its live sessions would keep working")
	}

	if countAdminAuditRows(t, adminAuditActionUserDelete) == 0 {
		t.Error("erasing an account wrote no admin_audit row")
	}
}

func TestUserDeletionRefusesToEraseYourself(t *testing.T) {
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "selferase")
	grantDeploymentAdminFixture(t, adminID)

	rec := deleteUserRequest(t, adminID, adminID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("self-erasure = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestUserDeletionRefusesAnAdministrator(t *testing.T) {
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "actor")
	grantDeploymentAdminFixture(t, adminID)
	targetID, _ := deploymentUserFixture(t, "target-admin")
	grantDeploymentAdminFixture(t, targetID)

	rec := deleteUserRequest(t, adminID, targetID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("erasing an administrator = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "administrator") {
		t.Errorf("the refusal does not say why: %s", rec.Body.String())
	}
}

func TestUserDeletionRequiresTheDeploymentAdminRole(t *testing.T) {
	cleanupDeploymentAdminRows(t)
	callerID, _ := deploymentUserFixture(t, "nobody")
	targetID, _ := deploymentUserFixture(t, "victim")

	rec := deleteUserRequest(t, callerID, targetID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("erasure without the role = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}

	if countRows(t, `SELECT count(*) FROM "user" WHERE id = $1 AND email NOT LIKE '%@deleted.invalid'`, targetID) != 1 {
		t.Error("a refused erasure still changed the target account")
	}
}

func TestErasingTwiceDoesNotCollideOnTheTombstoneAddress(t *testing.T) {
	cleanupDeploymentAdminRows(t)
	adminID, _ := deploymentUserFixture(t, "twice")
	grantDeploymentAdminFixture(t, adminID)
	first := newErasureSubject(t, "first")
	second := newErasureSubject(t, "second")

	for _, subject := range []erasureSubject{first, second} {
		if rec := deleteUserRequest(t, adminID, subject.userID); rec.Code != http.StatusOK {
			t.Fatalf("erasing %s = %d (%s)", subject.userID, rec.Code, rec.Body.String())
		}
	}

	if n := countRows(t,
		`SELECT count(*) FROM "user" WHERE id = ANY($1::uuid[]) AND email LIKE '%@deleted.invalid'`,
		[]string{first.userID, second.userID}); n != 2 {
		t.Errorf("%d of 2 accounts carry a tombstone address", n)
	}
}

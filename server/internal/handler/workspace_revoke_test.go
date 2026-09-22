package handler

import (
	"context"
	"testing"

	"github.com/adanman/goosar/server/internal/util"
)

func userOverrideRowCount(t *testing.T, workspaceID, userID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM user_config_override WHERE workspace_id = $1 AND user_id = $2`,
		workspaceID, userID,
	).Scan(&n); err != nil {
		t.Fatalf("count user_config_override: %v", err)
	}
	return n
}

func TestRevokeAndRemoveMember_DeletesUserConfigOverride(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	member := nonAdminMemberFixture(t)
	otherWorkspaceID, _ := secondWorkspaceFixture(t)

	for _, wsID := range []string{testWorkspaceID, otherWorkspaceID} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO user_config_override (workspace_id, user_id, llm_base_url)
			VALUES ($1, $2, 'https://gw.example/v1')
		`, wsID, member); err != nil {
			t.Fatalf("seed user_config_override for %s: %v", wsID, err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM user_config_override WHERE user_id = $1`, member)
	})

	var memberRowID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, member,
	).Scan(&memberRowID); err != nil {
		t.Fatalf("load member row: %v", err)
	}

	if _, err := testHandler.revokeAndRemoveMember(ctx,
		util.MustParseUUID(testWorkspaceID),
		util.MustParseUUID(member),
		util.MustParseUUID(memberRowID),
		util.MustParseUUID(testUserID),
	); err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}

	if n := userOverrideRowCount(t, testWorkspaceID, member); n != 0 {
		t.Errorf("user_config_override survived the exclusion: %d row(s)", n)
	}
	if n := userOverrideRowCount(t, otherWorkspaceID, member); n != 1 {
		t.Errorf("unrelated workspace's override must survive, got %d row(s)", n)
	}
}

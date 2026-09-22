package main

import (
	"context"
	"testing"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestMembershipChecker_DeactivatedUserIsRefused(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	userID, workspaceID, err := setupIntegrationTestFixture(ctx, testPool)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	t.Cleanup(func() { _ = cleanupIntegrationTestFixture(context.Background(), testPool) })

	queries := db.New(testPool)
	mc := &membershipChecker{queries: queries}

	if !mc.IsMember(ctx, userID, workspaceID, nil) {
		t.Fatal("an active member must be allowed onto the socket")
	}

	if _, err := queries.DeactivateUser(ctx, parseUUID(userID)); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if mc.IsMember(ctx, userID, workspaceID, nil) {
		t.Fatal("a deactivated user must not be able to (re)open a workspace socket")
	}

	if _, err := queries.ReactivateUser(ctx, parseUUID(userID)); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if !mc.IsMember(ctx, userID, workspaceID, nil) {
		t.Fatal("reactivation must restore socket access")
	}

	if mc.IsMember(ctx, userID, "not-a-uuid", nil) {
		t.Fatal("a malformed workspace id must be refused")
	}
	if mc.IsMember(ctx, "not-a-uuid", workspaceID, nil) {
		t.Fatal("a malformed user id must be refused")
	}
}

func TestMembershipChecker_StaleSessionEpochIsRefused(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	userID, workspaceID, err := setupIntegrationTestFixture(ctx, testPool)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	t.Cleanup(func() { _ = cleanupIntegrationTestFixture(context.Background(), testPool) })

	queries := db.New(testPool)
	mc := &membershipChecker{queries: queries}

	state, err := queries.GetUserAuthState(ctx, parseUUID(userID))
	if err != nil {
		t.Fatalf("auth state: %v", err)
	}
	current := state.TokenVersion
	if !mc.IsMember(ctx, userID, workspaceID, &current) {
		t.Fatal("a token minted under the current epoch must be allowed")
	}

	stale := current - 1
	if mc.IsMember(ctx, userID, workspaceID, &stale) {
		t.Fatal("a token from a revoked epoch must not reopen the socket")
	}
}

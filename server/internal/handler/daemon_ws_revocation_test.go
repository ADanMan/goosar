package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/adanman/goosar/server/internal/daemonws"
	"github.com/adanman/goosar/server/internal/util"
)

func seedRevocationFixture(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('WS Revoke Fixture', $1) RETURNING id`,
		"ws-revoke-"+uuid.New().String()+"@test.local",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	var memberRowID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member') RETURNING id`,
		testWorkspaceID, userID,
	).Scan(&memberRowID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
	})

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'ws-revoke-runtime', 'local', 'runtime-j', 'online', '', '{}'::jsonb, $2) RETURNING id
	`, testWorkspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	return userID, memberRowID, runtimeID
}

func TestHandleDaemonWSHeartbeat_ExcludedMemberGetsNoAck(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	userID, _, runtimeID := seedRevocationFixture(t)

	identity := daemonws.ClientIdentity{
		UserID:       userID,
		WorkspaceIDs: []string{testWorkspaceID},
		RuntimeIDs:   []string{runtimeID},
	}

	if _, err := testHandler.HandleDaemonWSHeartbeat(context.Background(), identity, runtimeID, false); err != nil {
		t.Fatalf("heartbeat for a live member: %v", err)
	}

	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, userID); err != nil {
		t.Fatalf("delete member: %v", err)
	}

	if _, err := testHandler.HandleDaemonWSHeartbeat(context.Background(), identity, runtimeID, false); err == nil {
		t.Fatalf("excluded member still received a WS heartbeat ack")
	}
}

func TestRevokeAndRemoveMember_ClosesLiveDaemonWSSessions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	userID, memberRowID, runtimeID := seedRevocationFixture(t)

	hub := daemonws.NewHub()
	prevHub := testHandler.DaemonHub
	testHandler.DaemonHub = hub
	t.Cleanup(func() { testHandler.DaemonHub = prevHub })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, daemonws.ClientIdentity{
			UserID:       userID,
			WorkspaceIDs: []string{testWorkspaceID},
			RuntimeIDs:   []string{runtimeID},
		})
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for hub.RuntimeConnectionCount(runtimeID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("ws connection was not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctx := context.Background()
	result, err := testHandler.revokeAndRemoveMember(ctx,
		util.MustParseUUID(testWorkspaceID),
		util.MustParseUUID(userID),
		util.MustParseUUID(memberRowID),
		util.MustParseUUID(testUserID),
	)
	if err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}
	testHandler.publishRevocation(ctx, result, testWorkspaceID, "member", testUserID)

	deadline = time.Now().Add(2 * time.Second)
	for hub.RuntimeConnectionCount(runtimeID) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("revoked runtime's WS session still connected after publishRevocation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

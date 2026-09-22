package realtime

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type capturingPublisher struct {
	called    bool
	scopeType string
	scopeID   string
	exclude   string
	frame     []byte
}

func (p *capturingPublisher) PublishWithID(scopeType, scopeID, exclude string, frame []byte, _ string) error {
	p.called = true
	p.scopeType = scopeType
	p.scopeID = scopeID
	p.exclude = exclude
	p.frame = append([]byte(nil), frame...)
	return nil
}

func TestRelayDisconnectorClosesLocallyAndFansOut(t *testing.T) {
	hub := NewHub()
	client := attachRealtimeTestClient(hub, ScopeUser, "user-1")

	pub := &capturingPublisher{}
	d := NewRelayDisconnector(hub, pub)

	if closed := d.DisconnectUser("user-1", "workspace-1"); closed != 1 {
		t.Fatalf("expected the local connection to be closed, got %d", closed)
	}
	if _, ok := hub.clients[client]; ok {
		t.Fatal("expected the client to be removed from the hub")
	}
	if !pub.called {
		t.Fatal("expected the revocation to be published for the other nodes")
	}
	if pub.scopeType != ScopeUser || pub.scopeID != "user-1" {
		t.Fatalf("expected a user-scoped publish, got %s:%s", pub.scopeType, pub.scopeID)
	}
	if pub.exclude != "workspace-1" {
		t.Fatalf("expected the workspace filter to travel with the frame, got %q", pub.exclude)
	}
	var frame map[string]any
	if err := json.Unmarshal(pub.frame, &frame); err != nil {
		t.Fatalf("published frame is not JSON: %v", err)
	}
	if frame["type"] != EventConnectionRevoked {
		t.Fatalf("expected type %q, got %v", EventConnectionRevoked, frame["type"])
	}
}

func TestRelayDisconnectorIgnoresEmptyUser(t *testing.T) {
	pub := &capturingPublisher{}
	d := NewRelayDisconnector(NewHub(), pub)
	if closed := d.DisconnectUser("", ""); closed != 0 {
		t.Fatalf("expected no work for an empty user id, got %d", closed)
	}
	if pub.called {
		t.Fatal("an empty user id must not publish a revocation to every node")
	}
}

func TestDeliverEnvelopeRevocationDisconnectsInsteadOfFanout(t *testing.T) {
	hub := NewHub()
	client := attachRealtimeTestClient(hub, ScopeUser, "user-1")

	deliverEnvelope(hub, nil, envelope{
		EventID:     "evt-1",
		EventType:   EventConnectionRevoked,
		Scope:       ScopeUser,
		ScopeID:     "user-1",
		WorkspaceID: "workspace-1",
		PayloadJSON: `{"type":"` + EventConnectionRevoked + `"}`,
	})

	if _, ok := hub.clients[client]; ok {
		t.Fatal("expected the remote revocation to close the local connection")
	}

	if frame, ok := <-client.send; ok {
		t.Fatalf("the revocation must not be forwarded to the browser, got %s", frame)
	}
}

func TestDeliverEnvelopeRevocationRespectsWorkspaceFilter(t *testing.T) {
	hub := NewHub()
	client := attachRealtimeTestClient(hub, ScopeUser, "user-1")

	deliverEnvelope(hub, nil, envelope{
		EventID:     "evt-2",
		EventType:   EventConnectionRevoked,
		Scope:       ScopeUser,
		ScopeID:     "user-1",
		WorkspaceID: "workspace-other",
		PayloadJSON: `{"type":"` + EventConnectionRevoked + `"}`,
	})

	if _, ok := hub.clients[client]; !ok {
		t.Fatal("removal from one workspace must not sign the user out of the others")
	}
}

func TestRelayDisconnectorReachesAnotherNode(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set — cross-node disconnect test needs Redis")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Skipf("invalid REDIS_URL: %v", err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis unreachable: %v", err)
	}

	userID := "user-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		rdb.Del(context.Background(), StreamKey(ScopeUser, userID), NodesKey(ScopeUser, userID))
	})

	hubB := NewHub()
	relayB := NewRedisRelay(hubB, rdb)
	relayB.Start(ctx)
	t.Cleanup(func() { relayB.Stop(); relayB.Wait() })
	client := attachRealtimeTestClient(hubB, ScopeUser, userID)

	client.userID = userID
	relayB.startConsumer(ctx, ScopeUser, userID)

	hubA := NewHub()
	relayA := NewRedisRelay(hubA, rdb)
	t.Cleanup(func() { relayA.Stop(); relayA.Wait() })

	disconnector := NewRelayDisconnector(hubA, relayA)

	deadline := time.After(10 * time.Second)
	for {
		disconnector.DisconnectUser(userID, "workspace-1")
		hubB.mu.RLock()
		_, stillConnected := hubB.clients[client]
		hubB.mu.RUnlock()
		if !stillConnected {
			return
		}
		select {
		case <-deadline:
			t.Fatal("node B never closed the revoked user's connection")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

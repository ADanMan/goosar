package daemonws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/adanman/goosar/server/pkg/protocol"
)

func dialRPCTestConn(t *testing.T, hub *Hub, identity ClientIdentity) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, identity)
	}))
	t.Cleanup(server.Close)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func sendRPCRequest(t *testing.T, conn *websocket.Conn, req protocol.RPCRequestPayload) protocol.RPCResponsePayload {
	t.Helper()
	frame, err := json.Marshal(protocol.Message{
		Type:    protocol.EventDaemonRPCRequest,
		Payload: mustMarshalRaw(req),
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg protocol.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal msg: %v", err)
	}
	if msg.Type != protocol.EventDaemonRPCResponse {
		t.Fatalf("type = %q, want %q", msg.Type, protocol.EventDaemonRPCResponse)
	}
	var resp protocol.RPCResponsePayload
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	return resp
}

func TestRPCDispatch_RoundTrip(t *testing.T) {
	hub := NewHub()
	var gotMethod, gotDaemonID string
	hub.SetRPCHandler(func(ctx context.Context, identity ClientIdentity, method string, body json.RawMessage) (int, json.RawMessage, error) {
		gotMethod = method
		gotDaemonID = identity.DaemonID
		return http.StatusOK, json.RawMessage(`{"ok":true}`), nil
	})
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "daemon-1", RuntimeIDs: []string{"rt-1"}})

	resp := sendRPCRequest(t, conn, protocol.RPCRequestPayload{
		RequestID: "req-1",
		Method:    "tasks.claim",
		Body:      json.RawMessage(`{"max_tasks":3}`),
	})
	if resp.RequestID != "req-1" || resp.Status != http.StatusOK || string(resp.Body) != `{"ok":true}` {
		t.Fatalf("resp = %+v, want req-1/200/{ok:true}", resp)
	}
	if gotMethod != "tasks.claim" || gotDaemonID != "daemon-1" {
		t.Fatalf("handler saw method=%q daemon=%q, want tasks.claim/daemon-1", gotMethod, gotDaemonID)
	}
}

func TestRPCDispatch_HandlerError(t *testing.T) {
	hub := NewHub()
	hub.SetRPCHandler(func(ctx context.Context, identity ClientIdentity, method string, body json.RawMessage) (int, json.RawMessage, error) {
		return 0, nil, context.DeadlineExceeded
	})
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "daemon-1", RuntimeIDs: []string{"rt-1"}})
	resp := sendRPCRequest(t, conn, protocol.RPCRequestPayload{RequestID: "req-2", Method: "tasks.claim"})
	if resp.RequestID != "req-2" || resp.Status < 400 || resp.Error == "" {
		t.Fatalf("resp = %+v, want req-2 with 5xx + error", resp)
	}
}

func TestRPCDispatch_NoHandler(t *testing.T) {
	hub := NewHub()
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "daemon-1", RuntimeIDs: []string{"rt-1"}})
	resp := sendRPCRequest(t, conn, protocol.RPCRequestPayload{RequestID: "req-3", Method: "tasks.claim"})
	if resp.RequestID != "req-3" || resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("resp = %+v, want req-3 with 503", resp)
	}
}

func TestRPCDispatch_DisconnectDuringHandlerNoPanic(t *testing.T) {
	hub := NewHub()
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	hub.SetRPCHandler(func(ctx context.Context, identity ClientIdentity, method string, body json.RawMessage) (int, json.RawMessage, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return http.StatusOK, json.RawMessage(`{"ok":true}`), nil
	})
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "daemon-1", RuntimeIDs: []string{"rt-1"}})

	frame, _ := json.Marshal(protocol.Message{
		Type:    protocol.EventDaemonRPCRequest,
		Payload: mustMarshalRaw(protocol.RPCRequestPayload{RequestID: "req-1", Method: "tasks.claim"}),
	})
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	conn.Close()
	time.Sleep(50 * time.Millisecond)
	close(release)
	time.Sleep(100 * time.Millisecond)

}

func TestRPCDispatch_ServerTimeoutCancelsHandler(t *testing.T) {
	hub := NewHub()
	cancelled := make(chan struct{}, 1)
	hub.SetRPCHandler(func(ctx context.Context, identity ClientIdentity, method string, body json.RawMessage) (int, json.RawMessage, error) {
		select {
		case <-ctx.Done():
			select {
			case cancelled <- struct{}{}:
			default:
			}
			return 0, nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return http.StatusOK, json.RawMessage(`{}`), nil
		}
	})
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "daemon-1", RuntimeIDs: []string{"rt-1"}})

	resp := sendRPCRequest(t, conn, protocol.RPCRequestPayload{
		RequestID: "req-timeout",
		Method:    "tasks.claim",
		TimeoutMs: 100,
	})
	if resp.RequestID != "req-timeout" || resp.Status < 400 || resp.Error == "" {
		t.Fatalf("resp = %+v, want an error response from the cancelled handler", resp)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler context was not cancelled at the server-side TimeoutMs deadline")
	}
}

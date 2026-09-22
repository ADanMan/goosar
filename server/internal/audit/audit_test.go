package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func captureStream(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	restore := setStreamWriterForTest(&buf)
	t.Cleanup(restore)
	return &buf
}

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no audit line was emitted")
	}
	if strings.Count(line, "\n") > 0 {
		t.Fatalf("expected exactly one line, got:\n%s", line)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		t.Fatalf("audit line is not JSON: %v\n%s", err, line)
	}
	return out
}

func TestEmitWritesOneJSONLineWithSchemaVersion(t *testing.T) {
	buf := captureStream(t)
	Emit(Event{
		Action:     ActionLoginCodeVerified,
		ActorType:  ActorUser,
		ActorID:    "11111111-1111-1111-1111-111111111111",
		TargetType: "user",
		TargetID:   "11111111-1111-1111-1111-111111111111",
		Outcome:    OutcomeSuccess,
		RequestID:  "req-1",
		ClientIP:   "203.0.113.7",
		UserAgent:  "curl/8",
	})

	got := decodeLine(t, buf)
	for _, field := range []string{"time", "logger", "schema", "action", "actor_type", "outcome"} {
		if _, ok := got[field]; !ok {
			t.Fatalf("field %q missing from the audit line: %v", field, got)
		}
	}
	if got["logger"] != LoggerName {
		t.Fatalf("logger = %v, want %q", got["logger"], LoggerName)
	}
	if got["schema"] != float64(SchemaVersion) {
		t.Fatalf("schema = %v, want %d", got["schema"], SchemaVersion)
	}
	if got["action"] != ActionLoginCodeVerified {
		t.Fatalf("action = %v", got["action"])
	}
}

func TestEmitFailureCarriesReasonAndClientIdentity(t *testing.T) {
	buf := captureStream(t)
	Emit(Event{
		Action:     ActionLoginCodeFailed,
		ActorType:  ActorAnonymous,
		ActorID:    "abc123",
		TargetType: "email",
		Outcome:    OutcomeFailure,
		Reason:     ReasonInvalidCode,
		ClientIP:   "198.51.100.9",
		UserAgent:  "Mozilla/5.0",
	})

	got := decodeLine(t, buf)
	if got["outcome"] != OutcomeFailure {
		t.Fatalf("outcome = %v", got["outcome"])
	}
	if got["reason"] != ReasonInvalidCode {
		t.Fatalf("reason = %v", got["reason"])
	}
	if got["client_ip"] != "198.51.100.9" {
		t.Fatalf("client_ip = %v", got["client_ip"])
	}
}

func TestEmitOmitsEmptyOptionalFields(t *testing.T) {
	buf := captureStream(t)
	Emit(Event{Action: ActionLogout, ActorType: ActorUser, TargetType: "session", Outcome: OutcomeSuccess})

	got := decodeLine(t, buf)
	for _, field := range []string{"reason", "client_ip", "user_agent", "workspace_id", "actor_role"} {
		if _, ok := got[field]; ok {
			t.Fatalf("empty field %q was emitted: %v", field, got)
		}
	}
}

func TestEventFieldsCannotCarrySecrets(t *testing.T) {

	buf := captureStream(t)
	Emit(Event{Action: ActionLoginCodeSent, ActorType: ActorAnonymous, TargetType: "email", Outcome: OutcomeSuccess})
	got := decodeLine(t, buf)
	allowed := map[string]bool{
		"time": true, "level": true, "msg": true, "logger": true, "schema": true,
		"action": true, "actor_type": true, "actor_id": true, "actor_role": true,
		"target_type": true, "target_id": true, "outcome": true, "reason": true,
		"workspace_id": true, "request_id": true, "client_ip": true, "user_agent": true,
	}
	for key := range got {
		if !allowed[key] {
			t.Fatalf("unreviewed field %q in the audit line — every field must be checked for secrets", key)
		}
	}
}

func TestFromRequestReadsRequestIdentity(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/auth/verify", nil)
	r.Header.Set("User-Agent", "Goosar/1.0")
	r.RemoteAddr = "192.0.2.5:44444"

	ev := FromRequest(r, nil)
	if ev.UserAgent != "Goosar/1.0" {
		t.Fatalf("user_agent = %q", ev.UserAgent)
	}
	if ev.ClientIP != "192.0.2.5" {
		t.Fatalf("client_ip = %q, want the socket peer when no proxy is trusted", ev.ClientIP)
	}
}

func TestFromRequestIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/auth/verify", nil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.RemoteAddr = "192.0.2.5:44444"

	ev := FromRequest(r, nil)
	if ev.ClientIP != "192.0.2.5" {
		t.Fatalf("client_ip = %q — a forged X-Forwarded-For was believed", ev.ClientIP)
	}
}

func TestFromRequestTruncatesUserAgent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", strings.Repeat("x", maxUserAgentLen*2))
	ev := FromRequest(r, nil)
	if len(ev.UserAgent) != maxUserAgentLen {
		t.Fatalf("user_agent length = %d, want %d", len(ev.UserAgent), maxUserAgentLen)
	}
}

func TestRecordSurvivesANilRecorder(t *testing.T) {
	captureStream(t)
	var rec *Recorder
	rec.Record(context.Background(), Event{Action: ActionLogout, ActorType: ActorUser, TargetType: "session", Outcome: OutcomeSuccess})
}

func TestRecordFailOpenWhenTheDatabaseWriteFails(t *testing.T) {
	buf := captureStream(t)

	pool, err := pgxpool.New(context.Background(), "postgres://goosar:goosar@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		NewRecorder(db.New(pool)).Record(context.Background(), Event{
			Action:     ActionAuthRefused,
			ActorType:  ActorAnonymous,
			TargetType: "session",
			Outcome:    OutcomeDenied,
			Reason:     ReasonInvalidToken,
		})
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Record blocked the caller when the journal write failed")
	}

	if got := decodeLine(t, buf)["action"]; got != ActionAuthRefused {
		t.Fatalf("SIEM line lost when the database write failed: action = %v", got)
	}
}

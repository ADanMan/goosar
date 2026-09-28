package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/migrate"
	"github.com/adanman/goosar/server2/internal/store"
)

// newAppTestStore — своя одноразовая БД для этого смоук-теста, тот же приём,
// что и chat/project/feed/task.newTestStore (internal/<domain>/testdb_test.go);
// не переиспользует их файлы напрямую, чтобы не трогать чужие пакеты.
func newAppTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminURL := "postgres://postgres@localhost:5432/postgres"
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("postgres недоступен (%v) — WS-смоук-тест пропущен", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Skipf("postgres недоступен (%v) — WS-смоук-тест пропущен", err)
	}
	t.Cleanup(admin.Close)

	dbName := fmt.Sprintf("server2_app_wstest_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("CREATE DATABASE %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(cctx, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
	})

	dbURL := adminURL[:len(adminURL)-len("/postgres")] + "/" + dbName
	db, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(db.Close)
	if _, err := migrate.Apply(ctx, db.Pool, "../../migrations"); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return db
}

// wsSmokeClient — тонкая обёртка над httptest.Server для этого файла: несёт
// bearer-токен между вызовами post(), не через глобальную переменную пакета.
type wsSmokeClient struct {
	t     *testing.T
	srv   *httptest.Server
	token string
}

func (c *wsSmokeClient) post(path string, body any) map[string]any {
	c.t.Helper()
	var buf []byte
	if body != nil {
		buf, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(http.MethodPost, c.srv.URL+path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		c.t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 {
		c.t.Fatalf("POST %s: status %d, body=%s", path, resp.StatusCode, raw)
	}
	return out
}

// TestWebSocketIssueCreatedEvent — небольшой сквозной тест протокола /ws
// (T-027, задача 5 тикета): логин по dev-коду, создание воркспейса,
// подключение к /ws с auth-фреймом (сессионный JWT, без cookie — тот же путь,
// которым подключается desktop/CLI клиент), subscribe на комнату workspace,
// затем создание задачи через POST /api/issues — и получение issue:created
// в этом же сокете.
func TestWebSocketIssueCreatedEvent(t *testing.T) {
	db := newAppTestStore(t)
	cfg := config.Config{
		JWTSecret: "test-secret", AllowSignup: true, DevVerifyCode: "424242",
		FrontendOrigin: "http://localhost:3199", AppEnv: "development",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := New(cfg, db, logger)
	router := NewRouter(deps)
	srv := httptest.NewServer(deps.BuildHandler(router))
	t.Cleanup(srv.Close)

	c := &wsSmokeClient{t: t, srv: srv}

	email := fmt.Sprintf("wstest-%d@example.test", time.Now().UnixNano())
	c.post("/auth/send-code", map[string]any{"email": email})
	login := c.post("/auth/verify-code", map[string]any{"email": email, "code": "424242"})
	c.token, _ = login["token"].(string)
	if c.token == "" {
		t.Fatal("verify-code did not return a token")
	}

	ws := c.post("/api/workspaces", map[string]any{"name": "WS smoke", "slug": fmt.Sprintf("ws-smoke-%d", time.Now().UnixNano())})
	workspaceID, _ := ws["id"].(string)
	if workspaceID == "" {
		t.Fatal("create workspace did not return an id")
	}

	wsURL := "ws" + srv.URL[len("http"):] + "/ws?workspace_id=" + workspaceID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	send := func(v any) {
		b, _ := json.Marshal(v)
		if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatalf("ws write: %v", err)
		}
	}
	readFrame := func() map[string]any {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("ws frame not JSON: %v (%s)", err, data)
		}
		return m
	}

	// Без cookie — сервер ждёт auth-фрейм (contract §2.1).
	send(map[string]any{"type": "auth", "payload": map[string]any{"token": c.token}})
	if ack := readFrame(); ack["type"] != "auth_ack" {
		t.Fatalf("expected auth_ack, got %+v", ack)
	}

	send(map[string]any{"type": "subscribe", "payload": map[string]any{"scope": "workspace", "id": workspaceID}})
	if ack := readFrame(); ack["type"] != "subscribe_ack" {
		t.Fatalf("expected subscribe_ack, got %+v", ack)
	}

	issue := c.post("/api/issues", map[string]any{"title": "WS smoke issue"})
	issueID, _ := issue["id"].(string)
	if issueID == "" {
		t.Fatal("create issue did not return an id")
	}

	// Единственный сокет в комнате workspace:<id> — первое содержательное
	// событие обязано быть issue:created на нашу же задачу.
	event := readFrame()
	if event["type"] != "issue:created" {
		t.Fatalf("expected issue:created, got %+v", event)
	}
	payload, _ := event["payload"].(map[string]any)
	got, _ := payload["issue"].(map[string]any)
	if got == nil || got["id"] != issueID {
		t.Fatalf("issue:created payload does not reference the created issue: %+v", event)
	}
}

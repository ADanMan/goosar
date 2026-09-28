package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server2/internal/config"
)

func TestDebugWsRouteDirect(t *testing.T) {
	db := newAppTestStore(t)
	cfg := config.Config{JWTSecret: "test-secret", AllowSignup: true, DevVerifyCode: "424242", FrontendOrigin: "http://localhost:3199", AppEnv: "development"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := New(cfg, db, logger)
	router := NewRouter(deps)
	srv := httptest.NewServer(deps.BuildHandler(router))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ws?workspace_id=x", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	fmt.Println("STATUS", resp.StatusCode, "BODY", string(body))
	_ = context.Background()
}

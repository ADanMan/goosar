package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestRequireWorkspaceMember_DBOutageIs503(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	deadPool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("open second pool: %v", err)
	}
	deadPool.Close()

	broken := &Handler{Queries: db.New(deadPool)}

	req := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()
	if _, ok := broken.requireWorkspaceMember(w, req, testWorkspaceID, "not found"); ok {
		t.Fatalf("requireWorkspaceMember unexpectedly passed on a dead pool")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("db-outage status = %d, want 503: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatalf("db-outage response missing Retry-After header")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	req.Header.Set("X-User-ID", "00000000-0000-0000-0000-00000000dead")
	w = httptest.NewRecorder()
	if _, ok := testHandler.requireWorkspaceMember(w, req, testWorkspaceID, "not found"); ok {
		t.Fatalf("requireWorkspaceMember passed for a non-member")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-member status = %d, want 404: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	req.Header.Set("X-User-ID", testUserID)
	w = httptest.NewRecorder()
	if _, ok := broken.requireWorkspaceMember(w, req, "not-a-uuid", "not found"); ok {
		t.Fatalf("requireWorkspaceMember passed for a malformed workspace id")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("malformed-id status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

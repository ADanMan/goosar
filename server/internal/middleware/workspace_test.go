package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const testResolverSlug = "middleware-resolver-test"

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("skipping: could not connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping: database not reachable: %v", err)
	}
	return pool
}

func setupResolverFixture(t *testing.T, pool *pgxpool.Pool) (workspaceID string, cleanup func()) {
	t.Helper()
	ctx := context.Background()

	_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, testResolverSlug)

	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, '', 'MRT') RETURNING id`,
		"Middleware Resolver Test", testResolverSlug,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	return workspaceID, func() {
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, testResolverSlug)
	}
}

func TestResolveWorkspaceIDFromRequest(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	workspaceID, cleanup := setupResolverFixture(t, pool)
	defer cleanup()

	const (
		uuidA = "00000000-0000-0000-0000-000000000001"
		uuidB = "00000000-0000-0000-0000-000000000002"
	)

	cases := []struct {
		name      string
		setup     func(r *http.Request)
		want      string
		wantEmpty bool
	}{
		{
			name: "context UUID wins over everything else",
			setup: func(r *http.Request) {
				ctx := context.WithValue(r.Context(), ctxKeyWorkspaceID, uuidA)
				*r = *r.WithContext(ctx)
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: uuidA,
		},
		{
			name: "X-Workspace-Slug header resolves to UUID via DB lookup",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
			},
			want: workspaceID,
		},
		{
			name: "X-Workspace-Slug wins over X-Workspace-ID (post-refactor priority)",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: workspaceID,
		},
		{
			name: "unknown X-Workspace-Slug falls through to UUID header",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", "does-not-exist")
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: uuidB,
		},
		{
			name: "?workspace_slug query resolves to UUID via DB lookup",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("workspace_slug", testResolverSlug)
				r.URL.RawQuery = q.Encode()
			},
			want: workspaceID,
		},
		{
			name: "X-Workspace-ID header is returned when no slug provided",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-ID", uuidA)
			},
			want: uuidA,
		},
		{
			name: "?workspace_id query is the last-resort fallback",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("workspace_id", uuidA)
				r.URL.RawQuery = q.Encode()
			},
			want: uuidA,
		},
		{
			name:      "no identifier at all returns empty",
			setup:     func(r *http.Request) {},
			wantEmpty: true,
		},
		{
			name: "unknown slug with no UUID fallback returns empty",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", "does-not-exist")
			},
			wantEmpty: true,
		},
		{

			name: "task_token actor: client-supplied slug/id cannot override token-bound workspace",
			setup: func(r *http.Request) {
				r.Header.Set("X-Actor-Source", "task_token")
				r.Header.Set("X-Workspace-ID", uuidA)

				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				q := r.URL.Query()
				q.Set("workspace_slug", testResolverSlug)
				q.Set("workspace_id", uuidB)
				r.URL.RawQuery = q.Encode()
			},
			want: uuidA,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/anything", nil)
			tc.setup(req)

			got := ResolveWorkspaceIDFromRequest(req, queries)

			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty, got %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestRequireWorkspaceMember_DBErrorIs503Not404(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()

	ctx := context.Background()
	workspaceID, cleanupWs := setupResolverFixture(t, pool)
	defer cleanupWs()

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('MW H2 Member', 'mw-h2-member@test.local') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	if _, err := pool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	doRequest := func(queries *db.Queries, userID string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/anything", nil)
		req.Header.Set("X-Workspace-ID", workspaceID)
		req.Header.Set("X-User-ID", userID)
		w := httptest.NewRecorder()
		RequireWorkspaceMember(queries)(next).ServeHTTP(w, req)
		return w
	}

	liveQueries := db.New(pool)

	t.Run("member passes", func(t *testing.T) {
		if w := doRequest(liveQueries, userID); w.Code != http.StatusOK {
			t.Fatalf("member request status = %d, want 200: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-member is a definitive 404", func(t *testing.T) {

		w := doRequest(liveQueries, "00000000-0000-0000-0000-00000000dead")
		if w.Code != http.StatusNotFound {
			t.Fatalf("non-member status = %d, want 404: %s", w.Code, w.Body.String())
		}
	})

	t.Run("db outage on member lookup is 503 with Retry-After", func(t *testing.T) {

		deadPool, err := pgxpool.New(ctx, poolURL())
		if err != nil {
			t.Fatalf("open second pool: %v", err)
		}
		deadPool.Close()
		w := doRequest(db.New(deadPool), userID)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("db-outage status = %d, want 503: %s", w.Code, w.Body.String())
		}
		if w.Header().Get("Retry-After") == "" {
			t.Fatalf("db-outage response missing Retry-After header")
		}
	})

	t.Run("db outage on slug resolution is 503 not 404", func(t *testing.T) {
		deadPool, err := pgxpool.New(ctx, poolURL())
		if err != nil {
			t.Fatalf("open second pool: %v", err)
		}
		deadPool.Close()
		req := httptest.NewRequest("GET", "/api/anything", nil)
		req.Header.Set("X-Workspace-Slug", testResolverSlug)
		req.Header.Set("X-User-ID", userID)
		w := httptest.NewRecorder()
		RequireWorkspaceMember(db.New(deadPool))(next).ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("slug-outage status = %d, want 503: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown slug stays a definitive 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/anything", nil)
		req.Header.Set("X-Workspace-Slug", "definitely-no-such-slug")
		req.Header.Set("X-User-ID", userID)
		w := httptest.NewRecorder()
		RequireWorkspaceMember(liveQueries)(next).ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("unknown-slug status = %d, want 404: %s", w.Code, w.Body.String())
		}
	})
}

func poolURL() string {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}
	return dbURL
}

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGCUploadsFlagParsing(t *testing.T) {
	if _, err := parseGCUploadsFlags([]string{"--dry-run"}); err != nil {
		t.Fatalf("--dry-run: %v", err)
	}
	opts, err := parseGCUploadsFlags([]string{"--grace=1h", "--limit=7"})
	if err != nil {
		t.Fatalf("--grace/--limit: %v", err)
	}
	if opts.Grace != time.Hour || opts.BatchSize != 7 || opts.DryRun {
		t.Fatalf("unexpected options: %+v", opts)
	}
	for _, args := range [][]string{
		{"--wat"},
		{"--grace=banana"},
		{"--grace=-1h"},
		{"--limit=0"},
	} {
		if _, err := parseGCUploadsFlags(args); err == nil {
			t.Fatalf("%v must be rejected", args)
		}
	}
}

func TestGCUploadsDryRunEndToEnd(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database not reachable: %v", err)
	}

	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	t.Setenv("S3_BUCKET", "")

	var wsID, userID string
	if err := pool.QueryRow(ctx, `SELECT id FROM workspace LIMIT 1`).Scan(&wsID); err != nil {
		t.Skipf("no workspace in test database: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('GC Uploads CLI', 'gc-uploads-cli@goosar.test')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var orphanID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, created_at)
		VALUES ($1, 'member', $2, 'cli.png', 'https://cdn.test/cli-orphan', 'image/png', 3, now() - interval '30 days')
		RETURNING id`, wsID, userID).Scan(&orphanID); err != nil {
		t.Fatalf("create orphan: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, orphanID)
	})

	var out bytes.Buffer
	if err := run(ctx, []string{"gc-uploads", "--dry-run"}, &out); err != nil {
		t.Fatalf("gc-uploads --dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "https://cdn.test/cli-orphan") {
		t.Fatalf("dry run must list the orphan it would remove, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Fatalf("dry run must say so, got: %s", out.String())
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM attachment WHERE id = $1`, orphanID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatal("dry run deleted the row it only promised to report")
	}
}

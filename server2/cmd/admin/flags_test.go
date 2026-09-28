package main

import (
	"testing"
	"time"
)

func TestParseGCUploadsFlags_defaults(t *testing.T) {
	opts, err := parseGCUploadsFlags(nil)
	if err != nil {
		t.Fatalf("parseGCUploadsFlags(nil): %v", err)
	}
	if opts.dryRun {
		t.Errorf("dryRun = true, want false by default")
	}
	if opts.grace != 168*time.Hour {
		t.Errorf("grace = %v, want 168h", opts.grace)
	}
	if opts.limit != 500 {
		t.Errorf("limit = %d, want 500", opts.limit)
	}
}

func TestParseGCUploadsFlags_overrides(t *testing.T) {
	opts, err := parseGCUploadsFlags([]string{"--dry-run", "--grace=1h", "--limit=10"})
	if err != nil {
		t.Fatalf("parseGCUploadsFlags: %v", err)
	}
	if !opts.dryRun {
		t.Errorf("dryRun = false, want true")
	}
	if opts.grace != time.Hour {
		t.Errorf("grace = %v, want 1h", opts.grace)
	}
	if opts.limit != 10 {
		t.Errorf("limit = %d, want 10", opts.limit)
	}
}

func TestParseGCUploadsFlags_rejectsNonPositiveLimit(t *testing.T) {
	if _, err := parseGCUploadsFlags([]string{"--limit=0"}); err == nil {
		t.Fatal("expected error for --limit=0")
	}
	if _, err := parseGCUploadsFlags([]string{"--limit=-5"}); err == nil {
		t.Fatal("expected error for --limit=-5")
	}
}

func TestParsePurgeFlags_defaultsAndOverrides(t *testing.T) {
	defaults := purgeOptions{
		chat: 720 * time.Hour, tasks: 720 * time.Hour, closedIssues: 8760 * time.Hour,
		activity: 720 * time.Hour, attachmentGrace: 168 * time.Hour,
	}
	opts, err := parsePurgeFlags(nil, defaults)
	if err != nil {
		t.Fatalf("parsePurgeFlags(nil): %v", err)
	}
	if opts != defaults {
		t.Errorf("parsePurgeFlags(nil) = %+v, want defaults %+v", opts, defaults)
	}

	opts, err = parsePurgeFlags([]string{"--dry-run", "--chat=48h", "--closed-issues=100h"}, defaults)
	if err != nil {
		t.Fatalf("parsePurgeFlags: %v", err)
	}
	if !opts.dryRun {
		t.Error("dryRun = false, want true")
	}
	if opts.chat != 48*time.Hour {
		t.Errorf("chat = %v, want 48h", opts.chat)
	}
	if opts.closedIssues != 100*time.Hour {
		t.Errorf("closedIssues = %v, want 100h", opts.closedIssues)
	}
	// поля, не переданные в этом вызове, остаются дефолтными.
	if opts.tasks != defaults.tasks {
		t.Errorf("tasks = %v, want default %v", opts.tasks, defaults.tasks)
	}
}

func TestParseRotateSecretsFlags_requiresMcp(t *testing.T) {
	if _, err := parseRotateSecretsFlags(nil); err == nil {
		t.Fatal("expected error when --mcp is not passed")
	}
	opts, err := parseRotateSecretsFlags([]string{"--mcp"})
	if err != nil {
		t.Fatalf("parseRotateSecretsFlags(--mcp): %v", err)
	}
	if !opts.mcp || opts.mfa || opts.dryRun {
		t.Errorf("opts = %+v, want {mcp:true mfa:false dryRun:false}", opts)
	}
	opts, err = parseRotateSecretsFlags([]string{"--mcp", "--mfa", "--dry-run"})
	if err != nil {
		t.Fatalf("parseRotateSecretsFlags: %v", err)
	}
	if !opts.mcp || !opts.mfa || !opts.dryRun {
		t.Errorf("opts = %+v, want all true", opts)
	}
}

func TestParseMcpLibrarySeedFlags(t *testing.T) {
	opts, err := parseMcpLibrarySeedFlags(nil)
	if err != nil {
		t.Fatalf("parseMcpLibrarySeedFlags(nil): %v", err)
	}
	if opts.dryRun {
		t.Error("dryRun = true, want false by default")
	}
	opts, err = parseMcpLibrarySeedFlags([]string{"--dry-run"})
	if err != nil {
		t.Fatalf("parseMcpLibrarySeedFlags(--dry-run): %v", err)
	}
	if !opts.dryRun {
		t.Error("dryRun = false, want true")
	}
}

func TestRun_helpAndUnknownCommand(t *testing.T) {
	var out, errOut testBuf
	if code := run(nil, &out, &errOut); code == 0 {
		t.Error("run(nil) (bare invocation) exit code = 0, want non-zero (matches the reference binary)")
	}
	if out.String() == "" {
		t.Error("run(nil) printed nothing to stdout")
	}

	out, errOut = testBuf{}, testBuf{}
	if code := run([]string{"--help"}, &out, &errOut); code != 0 {
		t.Errorf("run(--help) exit code = %d, want 0", code)
	}

	out, errOut = testBuf{}, testBuf{}
	if code := run([]string{"bogus-command"}, &out, &errOut); code == 0 {
		t.Error("run(bogus-command) exit code = 0, want non-zero")
	}

	out, errOut = testBuf{}, testBuf{}
	if code := run([]string{"confirm"}, &out, &errOut); code == 0 {
		t.Error("run(confirm) with no id should fail with a non-zero exit code")
	}
}

// testBuf — минимальный io.Writer для тестов, не тянущий bytes.Buffer только
// ради String().
type testBuf struct{ data []byte }

func (b *testBuf) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *testBuf) String() string { return string(b.data) }

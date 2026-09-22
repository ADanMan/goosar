package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server/internal/retention"
)

func TestPurgeFlagsDefaultToTheConfiguredPolicy(t *testing.T) {
	t.Setenv(retention.EnvChat, "720h")
	opts, err := parsePurgeFlags([]string{"--dry-run"})
	if err != nil {
		t.Fatalf("parsePurgeFlags: %v", err)
	}
	if !opts.dryRun {
		t.Error("--dry-run was not picked up")
	}
	if opts.policy.Chat != 720*time.Hour {
		t.Errorf("chat window = %s, want the configured 720h", opts.policy.Chat)
	}
}

func TestPurgeFlagsOverrideOneWindow(t *testing.T) {
	t.Setenv(retention.EnvChat, "720h")
	opts, err := parsePurgeFlags([]string{"--chat=24h", "--closed-issues=8760h"})
	if err != nil {
		t.Fatalf("parsePurgeFlags: %v", err)
	}
	if opts.policy.Chat != 24*time.Hour {
		t.Errorf("chat window = %s, want the overriding 24h", opts.policy.Chat)
	}
	if opts.policy.ClosedIssues != 8760*time.Hour {
		t.Errorf("closed-issues window = %s, want 8760h", opts.policy.ClosedIssues)
	}
}

func TestPurgeRejectsNonsenseFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--nope=1h"},
		{"--chat=yesterday"},
		{"--chat=-1h"},
		{"--chat"},
	} {
		if _, err := parsePurgeFlags(args); err == nil {
			t.Errorf("parsePurgeFlags(%v) accepted a bad flag", args)
		}
	}
}

func TestPurgeRefusesWhenNothingIsConfigured(t *testing.T) {
	t.Setenv(retention.EnvAttachmentGrace, "0s")
	opts, err := parsePurgeFlags(nil)
	if err != nil {
		t.Fatalf("parsePurgeFlags: %v", err)
	}
	var out bytes.Buffer
	err = purge(context.Background(), nil, &out, opts)
	if err == nil {
		t.Fatal("purge with an all-keep-forever policy reported success")
	}
	if !strings.Contains(err.Error(), "keep forever") {
		t.Errorf("error does not explain the policy: %v", err)
	}
}

func TestUsageDocumentsPurge(t *testing.T) {
	if !strings.Contains(usage, "goosar_admin purge") {
		t.Error("usage does not mention the purge command")
	}
	if !strings.Contains(usage, "--dry-run") {
		t.Error("usage does not mention --dry-run")
	}
}

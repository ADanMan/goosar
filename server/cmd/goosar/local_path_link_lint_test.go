package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withAgentContext(t *testing.T) {
	t.Helper()
	t.Setenv("GOOSAR_TASK_ID", "task-1")
}

func withWorkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return resolved
}

func targets(findings []localPathLinkFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Target)
	}
	return out
}

func TestFindLocalPathLinksHighConfidenceSignals(t *testing.T) {
	workdir := withWorkdir(t)

	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	resolvedOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}

	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "file:// URL in a link",
			body: "see [the chart](file:///tmp/chart.png)",
			want: []string{"file:///tmp/chart.png"},
		},
		{
			name: "file:// URL in an autolink renders clickable too",
			body: "see <file:///tmp/chart.png>",
			want: []string{"file:///tmp/chart.png"},
		},
		{
			name: "absolute path inside the task workdir",
			body: "![screenshot](" + shot + ")",
			want: []string{shot},
		},
		{
			name: "absolute path to a file that exists on this machine",
			body: "[log](" + resolvedOutside + ")",
			want: []string{resolvedOutside},
		},
		{
			name: "the reported repro: image link to a workdir file",
			body: "Done. Screenshot: [点我](" + shot + ")",
			want: []string{shot},
		},
		{
			name: "one path linked repeatedly reports once",
			body: "[a](" + shot + ") and [b](" + shot + ")",
			want: []string{shot},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := targets(findLocalPathLinks(tc.body))
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestFindLocalPathLinksAllowsLegitimateContent(t *testing.T) {
	workdir := withWorkdir(t)
	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cases := []struct {
		name string
		body string
	}{
		{

			name: "origin-relative in-app link",
			body: "see [MUL-1](/acme/issues/MUL-1) and [inbox](/issues/123)",
		},
		{
			name: "external http link",
			body: "the PR is at [#42](https://github.com/adanman/goosar/pull/42)",
		},
		{
			name: "mention link",
			body: "[@Nevi](mention://member/abc-123)",
		},
		{

			name: "path inside a code span",
			body: "the repro is `[x](" + shot + ")` — note the local path",
		},
		{
			name: "path inside a fenced code block",
			body: "```md\n[screenshot](" + shot + ")\n![x](file:///tmp/x.png)\n```",
		},
		{
			name: "path inside an indented code block",
			body: "    [screenshot](" + shot + ")\n",
		},
		{
			name: "path as plain prose is not a link",
			body: "I wrote the screenshot to " + shot + " on my machine.",
		},
		{
			name: "relative link is left alone",
			body: "[readme](docs/readme.md)",
		},
		{

			name: "absolute path that does not exist",
			body: "[x](/Users/someone-else/never-existed.png)",
		},
		{
			name: "link text mentioning a path is not a destination",
			body: "[" + shot + "](/acme/issues/1)",
		},
		{
			name: "empty body",
			body: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := targets(findLocalPathLinks(tc.body)); len(got) != 0 {
				t.Errorf("expected no findings, got %v\n--- body ---\n%s", got, tc.body)
			}
		})
	}
}

func TestFindLocalPathLinksIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Chdir(t.TempDir())
	if got := targets(findLocalPathLinks("[dir](" + resolved + ")")); len(got) != 0 {
		t.Errorf("a directory target should not be reported, got %v", got)
	}
}

func TestGuardLocalPathLinksOnlyFiresInAgentContext(t *testing.T) {
	workdir := withWorkdir(t)
	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	body := "[screenshot](" + shot + ")"

	t.Run("human PAT context is never linted", func(t *testing.T) {

		t.Setenv("GOOSAR_AGENT_ID", "")
		t.Setenv("GOOSAR_TASK_ID", "")
		if err := guardLocalPathLinks(body, "comment body", "hint"); err != nil {
			t.Errorf("expected no error outside agent context, got: %v", err)
		}
	})

	t.Run("agent task context hard-fails", func(t *testing.T) {
		withAgentContext(t)
		err := guardLocalPathLinks(body, "comment body", "hint")
		if err == nil {
			t.Fatal("expected a hard failure inside agent context")
		}
		if !strings.Contains(err.Error(), shot) {
			t.Errorf("error should name the offending target, got: %v", err)
		}
	})

	t.Run("agent context with a clean body passes", func(t *testing.T) {
		withAgentContext(t)
		if err := guardLocalPathLinks("all good — see [MUL-1](/acme/issues/MUL-1)", "comment body", "hint"); err != nil {
			t.Errorf("expected no error for a clean body, got: %v", err)
		}
	})
}

func TestGuardLocalPathLinksHintIsPerCommand(t *testing.T) {
	workdir := withWorkdir(t)
	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	withAgentContext(t)

	err := guardLocalPathLinks(
		"[screenshot]("+shot+")",
		"issue description",
		"`goosar issue update` cannot carry files — deliver the file with `goosar issue comment add <issue-id> --attachment <path>` instead, and drop the link.",
	)
	if err == nil {
		t.Fatal("expected a hard failure")
	}
	сообщение := err.Error()
	if !strings.Contains(сообщение, "`goosar issue update` cannot carry files") {
		t.Errorf("update hint missing, got: %v", сообщение)
	}
	if !strings.Contains(сообщение, "goosar issue comment add <issue-id> --attachment <path>") {
		t.Errorf("update hint must redirect to comment add, got: %v", сообщение)
	}
	if strings.Contains(сообщение, "goosar issue update --attachment") {
		t.Errorf("update hint must never name a flag `issue update` does not have, got: %v", сообщение)
	}
}

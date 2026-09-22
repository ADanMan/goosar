package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCommentReplyInstructionsCodexLinux(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "linux"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	got := BuildCommentReplyInstructions("runtime-e", issueID, triggerID)

	for _, want := range []string{
		"goosar issue comment add " + issueID + " --parent " + triggerID + " --content-file ./reply.md",
		"Write the reply body to a UTF-8 file",
		"`--content-file`",
		"#4182",
		"rm ./reply.md",
		"Do NOT write literal `\\n` escapes to simulate line breaks",
		"do NOT reuse --parent values from previous turns",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("codex/linux reply instructions missing %q\n---\n%s", want, got)
		}
	}

	for _, banned := range []string{
		"--content \"...\"",
		"<<'COMMENT'",
		"cat <<",
		"--parent " + triggerID + " --content-stdin",
	} {
		if strings.Contains(got, banned) {
			t.Fatalf("codex/linux reply instructions should not contain %q\n---\n%s", banned, got)
		}
	}
}

func TestBuildCommentReplyInstructionsNonCodexLinux(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, host := range []string{"linux", "darwin"} {
		for _, provider := range []string{"runtime-c", "runtime-m", "runtime-n", "runtime-j", "runtime-k", "runtime-l", "runtime-g"} {
			name := provider + "/" + host
			t.Run(name, func(t *testing.T) {
				runtimeGOOS = host
				got := BuildCommentReplyInstructions(provider, issueID, triggerID)

				for _, want := range []string{
					"goosar issue comment add " + issueID + " --parent " + triggerID + " --content-file ./reply.md",
					"Write the reply body to a UTF-8 file",
					"`--content-file`",
					"#4182",
					"rm ./reply.md",
					"do NOT reuse --parent values from previous turns",
					"If you decide to reply",
				} {
					if !strings.Contains(got, want) {
						t.Errorf("%s reply instructions missing %q\n---\n%s", name, want, got)
					}
				}

				for _, banned := range []string{
					"--content \"...\"",
					"<<'COMMENT'",
					"cat <<",
					"--parent " + triggerID + " --content-stdin",
				} {
					if strings.Contains(got, banned) {
						t.Errorf("%s reply instructions still contains %q\n---\n%s", name, banned, got)
					}
				}
			})
		}
	}
}

func TestBuildCommentReplyInstructionsWindowsUsesContentFile(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, provider := range []string{"runtime-e", "runtime-c", "runtime-m", "runtime-n", "runtime-j", "runtime-k", "runtime-l", "runtime-g"} {
		t.Run(provider+"/windows", func(t *testing.T) {
			got := BuildCommentReplyInstructions(provider, issueID, triggerID)
			for _, want := range []string{
				"goosar issue comment add " + issueID + " --parent " + triggerID + " --content-file",
				"On Windows, write the reply body to a UTF-8 file",
				"Do NOT pipe via `--content-stdin`",
				"silently drops non-ASCII",
				"$OutputEncoding",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("%s reply instructions missing %q\n---\n%s", provider, want, got)
				}
			}
			for _, banned := range []string{
				"<<'COMMENT'",
				"--parent " + triggerID + " --content-stdin",
				"cat <<",
			} {
				if strings.Contains(got, banned) {
					t.Errorf("%s/windows reply instructions should not contain %q\n---\n%s", provider, banned, got)
				}
			}
		})
	}
}

func TestBuildCommentReplyInstructionsEmptyWhenNoTrigger(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"runtime-e", "runtime-c", "runtime-m"} {
		if got := BuildCommentReplyInstructions(provider, "issue-id", ""); got != "" {
			t.Fatalf("expected empty string when triggerCommentID is empty for %s, got %q", provider, got)
		}
	}
}

func TestInjectRuntimeConfigKeepsTriggerCommentOutOfBrief(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "linux"

	dir := t.TempDir()
	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	if _, err := InjectRuntimeConfig(dir, "runtime-c", TaskContextForEnv{
		IssueID:          issueID,
		TriggerCommentID: triggerID,
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s := string(content)

	if strings.Contains(s, triggerID) {
		t.Errorf("CLAUDE.md must not carry the trigger comment id (MUL-5377)\n---\n%s", s)
	}
	for _, want := range []string{
		"Mode router",
		"`Turn mode: Reply.`",
		"`Turn mode: Ownership.`",
		"Use the `--parent` value the per-turn user message gives you for this turn",
		"do NOT reuse a `--parent` from an earlier turn in this session",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing %q\n---\n%s", want, s)
		}
	}
}

func TestWindowsCommentReplyInstructionsHaveNoStdin(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, provider := range []string{"runtime-c", "runtime-e", "runtime-m"} {
		t.Run(provider, func(t *testing.T) {
			s := BuildCommentReplyInstructions(provider, issueID, triggerID)
			for _, want := range []string{
				"goosar issue comment add " + issueID + " --parent " + triggerID + " --content-file",
				"--content-file",
			} {
				if !strings.Contains(s, want) {
					t.Errorf("%s reply instructions missing %q\n---\n%s", provider, want, s)
				}
			}
			for _, banned := range []string{
				"--parent " + triggerID + " --content-stdin",
				"always use `--content-stdin` with a HEREDOC, even for short single-line replies",
			} {
				if strings.Contains(s, banned) {
					t.Errorf("%s reply instructions must not prescribe stdin on Windows: %q\n---\n%s", provider, banned, s)
				}
			}
		})
	}
}

func TestInjectRuntimeConfigWindowsAssignmentBriefStaysFileOnly(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	ctx := TaskContextForEnv{IssueID: "issue-1"}

	for _, provider := range []string{"runtime-c", "runtime-e", "runtime-m"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := InjectRuntimeConfig(dir, provider, ctx); err != nil {
				t.Fatalf("InjectRuntimeConfig failed: %v", err)
			}
			fileName := "CLAUDE.md"
			if provider != "runtime-c" {
				fileName = "AGENTS.md"
			}
			data, err := os.ReadFile(filepath.Join(dir, fileName))
			if err != nil {
				t.Fatalf("read %s: %v", fileName, err)
			}
			s := string(data)

			for _, want := range []string{
				"## Comment Formatting",
				"On Windows, **always write the comment body to a UTF-8 file",
				"do NOT pipe via `--content-stdin`",
			} {
				if !strings.Contains(s, want) {
					t.Errorf("%s missing Windows file-only guidance %q\n---\n%s", fileName, want, s)
				}
			}

			for _, banned := range []string{
				"or `--content-stdin`",
				"using `--content-file` or `--content-stdin`",
				"use `--content-file <path>` or `--content-stdin`",
			} {
				if strings.Contains(s, banned) {
					t.Errorf("%s recommends stdin on Windows: %q\n---\n%s", fileName, banned, s)
				}
			}
		})
	}
}

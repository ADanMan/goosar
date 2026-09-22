package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/runtimeapps"
)

func TestSubIssueCreationSectionPresentForIssueRuns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "assignment-triggered",
			ctx:  TaskContextForEnv{IssueID: "11111111-2222-3333-4444-555555555555"},
		},
		{
			name: "comment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "22222222-3333-4444-5555-666666666666",
				TriggerCommentID: "33333333-4444-5555-6666-777777777777",
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("runtime-c", tc.ctx)

			if !strings.Contains(out, "## Sub-issue Creation") {
				t.Fatalf("expected Sub-issue Creation section in %s brief", tc.name)
			}
			for _, want := range []string{
				"**Choosing `--status` when creating sub-issues.**",
				"`--status todo` = **start now**",
				"`--status backlog` = **wait**",
				"`goosar issue status <child-id> todo`",
				"all `--status todo`",
				"`--status backlog` from the start",

				"**Ordering with stages.**",
				"`--stage <N>`",
				"`goosar issue children <id>`",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("[%s] section missing %q", tc.name, want)
				}
			}
		})
	}
}

func TestBriefHasNoParentNotificationGuidance(t *testing.T) {
	t.Parallel()
	cases := []TaskContextForEnv{
		{IssueID: "11111111-2222-3333-4444-555555555555"},
		{
			IssueID:          "22222222-3333-4444-5555-666666666666",
			TriggerCommentID: "33333333-4444-5555-6666-777777777777",
		},
	}
	for _, ctx := range cases {
		ctx := ctx
		out := buildMetaSkillContent("runtime-c", ctx)

		for _, banned := range []string{

			"## Parent / Sub-issue Protocol",
			"**Tell the parent when you finish a child.**",
			"goosar issue comment add <parent-id>",
			"with NO `--parent`",
			"link the child as `[MUL-",
			"`@mention` the parent's assignee",
			"`mention://agent/<id>`",
			"`mention://member/<id>`",
			"`mention://squad/<id>`",

			"**Do NOT post your own parent-notification comment.**",
			"Do NOT post your own parent-notification comment",
			"parent-notification comment",
			"system comment on the parent fires from the status transition",
			"re-trigger the parent's assignee for nothing",
			"platform posts a top-level system comment on the parent",

			"| Parent assignee | Parent status |",
			"The same agent as yourself",
			"| Member or squad |",
			"### A. Notify the parent",
			"### B. Choose",
			"When this issue has `parent_issue_id`:",
			"**Closing out child work** (only if this issue has `parent_issue_id`)",
			"**Notify the parent** (only if this issue has `parent_issue_id`",
			"**Creating sub-issues** (applies to any issue-bound run)",
			"For parent/child work, use these best-effort rules",

			"`goosar issue status <this-issue-id> in_review`",

			"issue list --parent",
		} {
			if strings.Contains(out, banned) {
				t.Errorf("expected %q to be removed from the brief", banned)
			}
		}
	}
}

func TestCommentTriggeredProtocolDoesNotForceInReview(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID:          "55555555-6666-7777-8888-999999999999",
		TriggerCommentID: "66666666-7777-8888-9999-aaaaaaaaaaaa",
	}
	out := buildMetaSkillContent("runtime-c", ctx)

	if strings.Contains(out, "`goosar issue status <this-issue-id> in_review`") {
		t.Errorf("comment-triggered brief must not contain a placeholder `<this-issue-id> in_review` flip — that conflicts with the comment-triggered \"do not change status unless asked\" rule")
	}

	const guardrail = "Do NOT change the issue status unless the comment explicitly asks for it"
	if !strings.Contains(out, guardrail) {
		t.Errorf("expected the comment-triggered workflow guardrail %q to be present", guardrail)
	}

	if strings.Contains(out, "Own the parent issue status") {
		t.Errorf("ordinary-agent comment brief must not reference the squad status grant:\n%s", out)
	}
}

func TestCommentTriggeredSquadLeaderDefersToStatusOwnershipGrant(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{
		IssueID:          "55555555-6666-7777-8888-999999999999",
		TriggerCommentID: "66666666-7777-8888-9999-aaaaaaaaaaaa",
		IsSquadLeader:    true,
	})

	for _, want := range []string{
		"Do NOT change the issue status unless the comment explicitly asks for it",
		`Squad Operating Protocol's "Own the parent issue status"`,
		"only appears when this issue is assigned to your squad",
		"without waiting to be asked",
		"When it is absent, the rule above is absolute.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("squad-leader comment brief missing %q\n---\n%s", want, out)
		}
	}

	if strings.Contains(out, "explicitly asks for it\n") {
		t.Errorf("squad-leader comment brief still ends the guardrail unqualified\n---\n%s", out)
	}
}

func TestPerRunCommentContextStaysOutOfBrief(t *testing.T) {
	t.Parallel()
	const (
		issueID = "55555555-6666-7777-8888-999999999999"
		since   = "2026-05-28T11:00:00Z"
	)
	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{
		IssueID:          issueID,
		TriggerCommentID: "reply-abc",
		TriggerThreadID:  "thread-abc",
		NewCommentCount:  4,
		NewCommentsSince: since,
		CommentReplyTargets: []ThreadReplyTarget{
			{ThreadID: "thread-abc", ParentID: "reply-abc"},
			{ThreadID: "thread-def", ParentID: "reply-def"},
		},
	})

	for _, banned := range []string{
		"reply-abc", "thread-abc", "reply-def", "thread-def", since,
		"4 new comment(s) on this issue since your last run",
		"DISTINCT threads",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("brief must not carry per-run comment value %q (MUL-5377)\n---\n%s", banned, out)
		}
	}

	hint := BuildNewCommentsHint(issueID, "reply-abc", "thread-abc", since, 4)
	for _, want := range []string{
		"4 new comment(s) on this issue since your last run",
		"blindly",
		"--thread thread-abc --since " + since + " --output json",
		"--tail 30",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("BuildNewCommentsHint missing %q\n---\n%s", want, hint)
		}
	}
}

func TestColdCommentsHintPointsAtTriggeringThread(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	hint := BuildColdCommentsHint(issueID, "trigger-1", "thread-root-1")
	if strings.Contains(hint, "new comment(s) since your last run") {
		t.Errorf("no since-delta hint should render on cold start, got:\n%s", hint)
	}
	if !strings.Contains(hint, "goosar issue comment list "+issueID+" --thread thread-root-1 --tail 30 --output json") {
		t.Errorf("cold start must point at the triggering thread read, got:\n%s", hint)
	}
	if strings.Contains(buildMetaSkillContent("runtime-c", TaskContextForEnv{IssueID: issueID, TriggerCommentID: "trigger-1", TriggerThreadID: "thread-root-1"}), "thread-root-1") {
		t.Error("brief must not carry the per-run thread id (MUL-5377)")
	}
}

func TestResumedCommentsHintSkipsDefaultThreadRead(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	hint := BuildResumedCommentsHint(issueID, "trigger-1", "thread-root-1")

	for _, want := range []string{
		"triggering comment is already included above",
		"No other new comments on this issue since your last run",
		"active thread anchor `thread-root-1` and triggering comment ID `trigger-1`",
		"If your reply depends on thread context",
		"do not rely only on resumed session memory",
		"goosar issue comment list " + issueID + " --thread thread-root-1 --tail 30 --output json",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("resumed/no-delta hint missing %q\n--- output ---\n%s", want, hint)
		}
	}
	if strings.Contains(hint, "scoped to the triggering thread") {
		t.Errorf("resumed/no-delta hint must not claim the delta is thread-scoped, got:\n%s", hint)
	}
	if strings.Contains(hint, "Read the triggering conversation first") {
		t.Errorf("resumed/no-delta hint must not use the cold-start forced-read wording, got:\n%s", hint)
	}
}

func TestSessionContinuityNoticeLivesOutsideBrief(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"## Session Continuity Notice",
		"could NOT be restored",
		"tell the user up front",
	} {
		if !strings.Contains(SessionContinuityNotice, want) {
			t.Errorf("SessionContinuityNotice missing %q", want)
		}
	}

	lost := TaskContextForEnv{
		IssueID:                       "11111111-2222-3333-4444-555555555555",
		TriggerCommentID:              "trigger-1",
		PriorSessionResumeUnavailable: true,
	}
	if strings.Contains(buildMetaSkillContent("runtime-e", lost), "Session Continuity Notice") {
		t.Error("brief must never carry the continuity notice — it is per-run state (MUL-5377)")
	}
}

func TestIssueWorkflowHonorsAgentIdentity(t *testing.T) {
	t.Parallel()
	const issueID = "77777777-8888-9999-aaaa-bbbbbbbbbbbb"
	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{IssueID: issueID})

	for _, want := range []string{
		"## Instruction Precedence",
		"Agent Identity instructions have priority over the issue workflow below.",
		"If a workflow step conflicts with Agent Identity, skip the conflicting action",
		"Never treat this runtime workflow as permission to change issue status, investigate, implement",
		"Before step 4, run `goosar issue status " + issueID + " in_progress` unless your Agent Identity forbids issue status changes; if it does, skip it.",
		"Complete the task within your Agent Identity boundaries.",
		"Do not investigate, implement, create issues, update issues, or delegate if your Agent Identity forbids that action",
		"When done, run `goosar issue status " + issueID + " in_review` unless your Agent Identity forbids issue status changes; if it does, skip it.",
		"If blocked, run `goosar issue status " + issueID + " blocked` unless your Agent Identity forbids issue status changes.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("issue brief missing identity-bound workflow text %q\n---\n%s", want, out)
		}
	}

	for _, banned := range []string{
		"4. Run `goosar issue status " + issueID + " in_progress`\n",
		"5. Follow your Skills and Agent Identity to complete the task (write code, investigate, etc.)",
		"8. When done, run `goosar issue status " + issueID + " in_review`\n",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("issue brief still contains unconditional legacy workflow text %q\n---\n%s", banned, out)
		}
	}
}

func TestSquadLeaderIssueWorkflowKeepsParentInProgress(t *testing.T) {
	t.Parallel()
	const issueID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{
		IssueID:       issueID,
		IsSquadLeader: true,
	})

	for _, want := range []string{
		"Before step 4, run `goosar issue status " + issueID + " in_progress` unless your Agent Identity forbids issue status changes; if it does, skip it.",
		"After this initial dispatch, leave the parent issue `in_progress`",
		"do NOT run `goosar issue status " + issueID + " in_review` or `done` on this turn",
		"only then, if the overall goal is met, move the parent to `in_review`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("squad-leader issue brief missing %q\n---\n%s", want, out)
		}
	}

	if strings.Contains(out, "When done, run `goosar issue status "+issueID+" in_review`") {
		t.Errorf("squad-leader issue brief must not contain the ordinary-agent completion step\n---\n%s", out)
	}
}

func TestInstructionPrecedenceOnlyAppliesToIssueWorkflow(t *testing.T) {
	t.Parallel()
	if out := buildMetaSkillContent("runtime-c", TaskContextForEnv{
		IssueID:          "11111111-2222-3333-4444-555555555555",
		TriggerCommentID: "22222222-3333-4444-5555-666666666666",
	}); !strings.Contains(out, "## Instruction Precedence") {
		t.Errorf("comment-triggered issue brief must carry Instruction Precedence\n---\n%s", out)
	}

	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{"chat", TaskContextForEnv{ChatSessionID: "chat-1"}},
		{"quick-create", TaskContextForEnv{QuickCreatePrompt: "create me an issue"}},
		{"autopilot run-only", TaskContextForEnv{AutopilotRunID: "run-1"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("runtime-c", tc.ctx)
			for _, banned := range []string{
				"## Instruction Precedence",
				"issue workflow below",
				"Never treat this runtime workflow as permission to change issue status",
			} {
				if strings.Contains(out, banned) {
					t.Errorf("%s brief must not inherit issue-only precedence text %q\n---\n%s", tc.name, banned, out)
				}
			}
		})
	}
}

func TestChatOutputDoesNotRequireIssueComment(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{ChatSessionID: "chat-1"})

	for _, want := range []string{
		"This is a chat session",
		"Your reply is delivered directly to the chat window the user is reading",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("chat brief missing chat output guidance %q\n---\n%s", want, out)
		}
	}

	for _, banned := range []string{
		"Final results MUST be delivered via `goosar issue comment add`",
		"The user does NOT see your terminal output",
		"do not call `goosar issue comment add`",
		"unless the user explicitly asks",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("chat brief must not inherit issue-comment output warning %q\n---\n%s", banned, out)
		}
	}
}

func TestOutputForbidsMidRunProgressComments(t *testing.T) {
	wantPhrases := []string{
		"Post exactly ONE comment per run",
		"Do NOT post progress updates",
	}
	issueCtxs := map[string]TaskContextForEnv{
		"assignment": {IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		"comment":    {IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", TriggerCommentID: "tc-1"},
	}

	run := func(t *testing.T, label string) {
		for name, ctx := range issueCtxs {
			out := buildMetaSkillContent("runtime-c", ctx)
			for _, want := range wantPhrases {
				if !strings.Contains(out, want) {
					t.Errorf("%s/%s brief missing output rule %q\n---\n%s", label, name, want, out)
				}
			}
		}

		chat := buildMetaSkillContent("runtime-c", TaskContextForEnv{ChatSessionID: "chat-1"})
		for _, banned := range wantPhrases {
			if strings.Contains(chat, banned) {
				t.Errorf("%s chat brief must not inherit issue output rule %q", label, banned)
			}
		}
	}

	run(t, "brief")
}

func TestSubIssueCreationSectionIsUnconditional(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	}
	out := buildMetaSkillContent("runtime-c", ctx)

	const header = "## Sub-issue Creation"
	start := strings.Index(out, header)
	if start == -1 {
		t.Fatalf("sub-issue creation section missing")
	}
	rest := out[start:]
	end := strings.Index(rest[len(header):], "\n## ")
	var section string
	if end == -1 {
		section = rest
	} else {
		section = rest[:len(header)+end]
	}

	if strings.Contains(section, "parent_issue_id") {
		t.Errorf("Sub-issue Creation section must not reference `parent_issue_id` — it applies to any issue-bound run, including top-level parents:\n%s", section)
	}
}

func TestWorkspaceContextRenderedAcrossTaskKinds(t *testing.T) {
	t.Parallel()
	const wsContext = "All comments must be in English. Prefer concise PR descriptions."
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "assignment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: wsContext,
			},
		},
		{
			name: "comment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "22222222-3333-4444-5555-666666666666",
				TriggerCommentID: "33333333-4444-5555-6666-777777777777",
				WorkspaceContext: wsContext,
			},
		},
		{
			name: "chat",
			ctx: TaskContextForEnv{
				ChatSessionID:    "chat-1",
				WorkspaceContext: wsContext,
			},
		},
		{
			name: "quick-create",
			ctx: TaskContextForEnv{
				QuickCreatePrompt: "create me an issue",
				WorkspaceContext:  wsContext,
			},
		},
		{
			name: "autopilot run-only",
			ctx: TaskContextForEnv{
				AutopilotRunID:   "run-1",
				WorkspaceContext: wsContext,
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("runtime-c", tc.ctx)

			if !strings.Contains(out, "## Workspace Context") {
				t.Fatalf("[%s] expected `## Workspace Context` heading", tc.name)
			}
			if !strings.Contains(out, wsContext) {
				t.Errorf("[%s] brief missing workspace context body %q", tc.name, wsContext)
			}

			ctxIdx := strings.Index(out, "## Workspace Context")
			cmdsIdx := strings.Index(out, "## Available Commands")
			if ctxIdx == -1 || cmdsIdx == -1 || ctxIdx > cmdsIdx {
				t.Errorf("[%s] `## Workspace Context` must appear above `## Available Commands` (ctx=%d, cmds=%d)", tc.name, ctxIdx, cmdsIdx)
			}
		})
	}
}

func TestWorkspaceContextHeadingSkippedWhenEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "empty string",
			ctx: TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: "",
			},
		},
		{
			name: "whitespace only",
			ctx: TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: "   \n\t  \r\n",
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("runtime-c", tc.ctx)
			if strings.Contains(out, "## Workspace Context") {
				t.Errorf("[%s] empty workspace context must NOT emit the heading", tc.name)
			}
		})
	}
}

func TestConnectedAppsBlockLivesOutsideBrief(t *testing.T) {
	t.Parallel()
	apps := []runtimeapps.ConnectedApp{{
		Provider:    "composio",
		ServerName:  "composio",
		ToolkitSlug: "notion",
		ToolkitName: "Notion",
	}}

	block := BuildConnectedAppsBlock(apps)
	for _, want := range []string{
		"## Connected Apps",
		"- Notion (`notion`) via MCP server `composio`",
		"Use the listed MCP server when the task asks to read or act in one of these apps.",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("connected-apps block missing %q\n---\n%s", want, block)
		}
	}
	if BuildConnectedAppsBlock(nil) != "" {
		t.Error("empty app list must render nothing")
	}

	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{
		IssueID:          "11111111-2222-3333-4444-555555555555",
		WorkspaceContext: "Prefer source-of-truth systems.",
		ConnectedApps:    apps,
	})
	if strings.Contains(out, "## Connected Apps") {
		t.Errorf("brief must not carry Connected Apps — it is per-run state (MUL-5377)\n---\n%s", out)
	}
}

func TestConnectedAppsHeadingSkippedWhenEmpty(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{IssueID: "11111111-2222-3333-4444-555555555555"})
	if strings.Contains(out, "## Connected Apps") {
		t.Fatalf("empty connected apps must not emit the heading")
	}
}

func TestSubIssueCreationSectionSkippedForNonIssueModes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "chat",
			ctx:  TaskContextForEnv{ChatSessionID: "chat-1"},
		},
		{
			name: "quick-create",
			ctx:  TaskContextForEnv{QuickCreatePrompt: "create me an issue"},
		},
		{
			name: "autopilot run-only",
			ctx:  TaskContextForEnv{AutopilotRunID: "run-1"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("runtime-c", tc.ctx)
			if strings.Contains(out, "## Sub-issue Creation") {
				t.Errorf("%s mode must NOT emit the Sub-issue Creation section", tc.name)
			}
		})
	}
}

func TestWriteRuntimeConfigFileCreatesMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const brief = "# Goosar Agent Runtime\n\nbrief body line"

	if err := writeRuntimeConfigFile(path, brief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, runtimeMarkerBegin+"\n") {
		t.Errorf("output should start with begin marker, got:\n%s", s)
	}
	if !strings.Contains(s, brief) {
		t.Errorf("output should contain brief body, got:\n%s", s)
	}
	if !strings.Contains(s, "\n"+runtimeMarkerEnd+"\n") {
		t.Errorf("output should contain end marker followed by newline, got:\n%s", s)
	}
}

func TestWriteRuntimeConfigFilePreservesUserContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User repo CLAUDE.md\n\n- rule one\n- rule two\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	const brief = "## Goosar brief\n\ninjected body"
	if err := writeRuntimeConfigFile(path, brief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)

	if !strings.HasPrefix(s, userContent) {
		t.Errorf("user content must be preserved verbatim at the top of the file, got:\n%s", s)
	}
	beginIdx := strings.Index(s, runtimeMarkerBegin)
	endIdx := strings.Index(s, runtimeMarkerEnd)
	if beginIdx < 0 || endIdx <= beginIdx {
		t.Fatalf("expected a well-formed marker block in:\n%s", s)
	}
	if beginIdx < len(userContent) {
		t.Errorf("begin marker must appear after user content, beginIdx=%d userLen=%d", beginIdx, len(userContent))
	}
	if !strings.Contains(s, brief) {
		t.Errorf("brief body missing from output:\n%s", s)
	}
}

func TestWriteRuntimeConfigFileReplacesExistingBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const userBefore = "# User AGENTS.md\n\nuser line above\n"
	const userAfter = "\nuser line below the block\n"
	original := userBefore +
		runtimeMarkerBegin + "\n" +
		"OLD BRIEF CONTENT THAT MUST GO AWAY\n" +
		runtimeMarkerEnd + "\n" +
		userAfter
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "## New Goosar brief\n\nfresh body"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, userBefore) {
		t.Errorf("content above the marker block must be preserved, got:\n%s", s)
	}
	if !strings.HasSuffix(s, userAfter) {
		t.Errorf("content below the marker block must be preserved, got:\n%s", s)
	}
	if strings.Contains(s, "OLD BRIEF CONTENT THAT MUST GO AWAY") {
		t.Errorf("previous block body must be replaced, got:\n%s", s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}
	if strings.Count(s, runtimeMarkerBegin) != 1 || strings.Count(s, runtimeMarkerEnd) != 1 {
		t.Errorf("there must be exactly one begin/end marker pair, got:\n%s", s)
	}
}

func TestWriteRuntimeConfigFileIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User CLAUDE.md\n\nimportant rules\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	const brief = "## Goosar brief\n\nbody"
	for i := 0; i < 5; i++ {
		if err := writeRuntimeConfigFile(path, brief); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if strings.Count(s, runtimeMarkerBegin) != 1 {
		t.Errorf("repeated runs must not duplicate the begin marker, count=%d, file:\n%s", strings.Count(s, runtimeMarkerBegin), s)
	}
	if strings.Count(s, runtimeMarkerEnd) != 1 {
		t.Errorf("repeated runs must not duplicate the end marker, count=%d, file:\n%s", strings.Count(s, runtimeMarkerEnd), s)
	}
	if strings.Count(s, brief) != 1 {
		t.Errorf("repeated runs must not duplicate the brief body, count=%d, file:\n%s", strings.Count(s, brief), s)
	}
	if !strings.HasPrefix(s, userContent) {
		t.Errorf("user content must remain intact at the top of the file, got:\n%s", s)
	}
}

func TestInjectRuntimeConfigPreservesUserContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		filename string
	}{
		{"runtime-c", "CLAUDE.md"},
		{"runtime-d", "CODEBUDDY.md"},
		{"runtime-e", "AGENTS.md"},
		{"runtime-f", "AGENTS.md"},
		{"runtime-m", "AGENTS.md"},
		{"runtime-n", "AGENTS.md"},
		{"runtime-j", "AGENTS.md"},
		{"runtime-o", "AGENTS.md"},
		{"runtime-g", "AGENTS.md"},
		{"runtime-k", "AGENTS.md"},
		{"runtime-l", "AGENTS.md"},
		{"runtime-a", "AGENTS.md"},
		{"runtime-q", "QWEN.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			const userContent = "# User-authored file\n\ndon't touch this\n"
			if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			content, err := InjectRuntimeConfig(dir, tc.provider, TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			})
			if err != nil {
				t.Fatalf("InjectRuntimeConfig: %v", err)
			}
			if content == "" {
				t.Fatalf("returned brief content must be non-empty")
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)
			if !strings.HasPrefix(s, userContent) {
				t.Errorf("[%s] user content must be preserved verbatim at the top of %s, got:\n%s", tc.provider, tc.filename, s)
			}
			if !strings.Contains(s, runtimeMarkerBegin) || !strings.Contains(s, runtimeMarkerEnd) {
				t.Errorf("[%s] %s must contain the runtime marker block, got:\n%s", tc.provider, tc.filename, s)
			}
		})
	}
}

func TestRuntimeConfigPathDistinguishesCodebuddyFromClaude(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	claudePath := runtimeConfigPath(dir, "runtime-c")
	codebuddyPath := runtimeConfigPath(dir, "runtime-d")

	if claudePath != filepath.Join(dir, "CLAUDE.md") {
		t.Errorf("claude runtime config path = %q, want CLAUDE.md", claudePath)
	}
	if codebuddyPath != filepath.Join(dir, "CODEBUDDY.md") {
		t.Errorf("codebuddy runtime config path = %q, want CODEBUDDY.md", codebuddyPath)
	}
	if claudePath == codebuddyPath {
		t.Fatal("claude and codebuddy must not share a runtime config path")
	}
}

func TestInjectRuntimeConfigUnknownProviderSkipsWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("untouched\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if _, err := InjectRuntimeConfig(dir, "totally-unknown-provider", TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555",
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != "untouched\n" {
			t.Errorf("unknown provider must not write %s; got:\n%s", name, string(got))
		}
	}
}

func TestWriteRuntimeConfigFileIgnoresStrayEndMarkerBeforeBegin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	const userDoc = "# Repo CLAUDE.md\n\nExample of what Goosar writes:\n" +
		runtimeMarkerEnd + "\n\n# Real config below\n"
	original := userDoc +
		runtimeMarkerBegin + "\nFIRST BRIEF\n" + runtimeMarkerEnd + "\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "SECOND BRIEF"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)

	if !strings.Contains(s, userDoc) {
		t.Errorf("user doc with stray end marker must be preserved verbatim, got:\n%s", s)
	}
	if got, want := strings.Count(s, runtimeMarkerBegin), 1; got != want {
		t.Errorf("expected exactly %d begin markers, got %d:\n%s", want, got, s)
	}
	if got, want := strings.Count(s, runtimeMarkerEnd), 2; got != want {
		t.Errorf("expected exactly %d end markers (1 user stray + 1 closing our block), got %d:\n%s", want, got, s)
	}
	if strings.Contains(s, "FIRST BRIEF") {
		t.Errorf("previous brief body must be replaced, got:\n%s", s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}

	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("second writeRuntimeConfigFile: %v", err)
	}
	got2, _ := os.ReadFile(path)
	s2 := string(got2)
	if got, want := strings.Count(s2, runtimeMarkerBegin), 1; got != want {
		t.Errorf("repeat write must not grow begin markers, got %d, want %d:\n%s", got, want, s2)
	}
}

func TestWriteRuntimeConfigFileReplacesMalformedHalfBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	const userTop = "# Repo AGENTS.md\n\nrules above\n"
	const halfBlock = "leftover from crashed write\nsecond line\n"
	original := userTop + runtimeMarkerBegin + "\n" + halfBlock
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "recovered brief"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, userTop) {
		t.Errorf("user content above the half-block must be preserved, got:\n%s", s)
	}
	if strings.Contains(s, "leftover from crashed write") {
		t.Errorf("half-block contents must be replaced, got:\n%s", s)
	}
	if got, want := strings.Count(s, runtimeMarkerBegin), 1; got != want {
		t.Errorf("expected exactly %d begin marker, got %d:\n%s", want, got, s)
	}
	if got, want := strings.Count(s, runtimeMarkerEnd), 1; got != want {
		t.Errorf("expected exactly %d end marker after recovery, got %d:\n%s", want, got, s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}
}

func TestCleanupRuntimeConfigPreservesUserContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	const userBefore = "# Repo CLAUDE.md\n\nuser line above\n"
	const userAfter = "\nuser line below the block\n"
	const userExpected = "# Repo CLAUDE.md\n\nuser line above\n\nuser line below the block\n"

	if err := os.WriteFile(path, []byte(userBefore+userAfter), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "runtime-c"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if strings.Contains(s, runtimeMarkerBegin) || strings.Contains(s, runtimeMarkerEnd) {
		t.Errorf("marker block must be removed, got:\n%s", s)
	}
	if strings.Contains(s, "brief body") {
		t.Errorf("brief body must be removed, got:\n%s", s)
	}
	if s != userExpected {
		t.Errorf("user content must be preserved byte-for-byte\n got:\n%q\nwant:\n%q", s, userExpected)
	}
}

func TestCleanupRuntimeConfigRemovesFileWhenOnlyBlockRemained(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "runtime-c"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed, stat err=%v", err)
	}
}

func TestCleanupRuntimeConfigNoOpCases(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := CleanupRuntimeConfig(dir, "runtime-c"); err != nil {
			t.Errorf("missing file must be no-op, got: %v", err)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected dir to remain empty, got: %v", entries)
		}
	})

	t.Run("file without marker block", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "CLAUDE.md")
		const userContent = "# Repo CLAUDE.md\n\nrules\n"
		if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := CleanupRuntimeConfig(dir, "runtime-c"); err != nil {
			t.Errorf("no-marker-block file must be no-op, got: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != userContent {
			t.Errorf("file must be untouched\n got:\n%q\nwant:\n%q", string(got), userContent)
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("untouched\n"), 0o644); err != nil {
				t.Fatalf("seed %s: %v", name, err)
			}
		}
		if err := CleanupRuntimeConfig(dir, "totally-unknown-provider"); err != nil {
			t.Errorf("unknown provider must be no-op, got: %v", err)
		}
		for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if string(got) != "untouched\n" {
				t.Errorf("unknown provider must not touch %s; got:\n%s", name, string(got))
			}
		}
	})
}

func TestCleanupRuntimeConfigRemovesMalformedHalfBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	const userTop = "# Repo AGENTS.md\n\nrules\n"
	original := userTop + runtimeMarkerBegin + "\nhalf-written brief no end\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "runtime-e"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if strings.Contains(s, runtimeMarkerBegin) {
		t.Errorf("half-block begin marker must be excised, got:\n%s", s)
	}
	if strings.Contains(s, "half-written brief no end") {
		t.Errorf("half-block body must be excised, got:\n%s", s)
	}
	if !strings.HasPrefix(s, userTop) {
		t.Errorf("user content above the half-block must remain, got:\n%s", s)
	}
}

func TestCleanupRuntimeConfigByProvider(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		filename string
	}{
		{"runtime-c", "CLAUDE.md"},
		{"runtime-d", "CODEBUDDY.md"},
		{"runtime-e", "AGENTS.md"},
		{"runtime-f", "AGENTS.md"},
		{"runtime-m", "AGENTS.md"},
		{"runtime-n", "AGENTS.md"},
		{"runtime-j", "AGENTS.md"},
		{"runtime-o", "AGENTS.md"},
		{"runtime-g", "AGENTS.md"},
		{"runtime-k", "AGENTS.md"},
		{"runtime-l", "AGENTS.md"},
		{"runtime-a", "AGENTS.md"},
		{"runtime-q", "QWEN.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			const userContent = "# User file\n\ndon't touch this\n"
			if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			if _, err := InjectRuntimeConfig(dir, tc.provider, TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			}); err != nil {
				t.Fatalf("InjectRuntimeConfig: %v", err)
			}
			if err := CleanupRuntimeConfig(dir, tc.provider); err != nil {
				t.Fatalf("CleanupRuntimeConfig: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)
			if strings.Contains(s, runtimeMarkerBegin) || strings.Contains(s, runtimeMarkerEnd) {
				t.Errorf("[%s] marker block must be removed from %s, got:\n%s", tc.provider, tc.filename, s)
			}
			if s != userContent {
				t.Errorf("[%s] user content in %s must be preserved byte-for-byte\n got:\n%q\nwant:\n%q", tc.provider, tc.filename, s, userContent)
			}
		})
	}
}

func TestInjectThenCleanupRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User-authored CLAUDE.md\n\n- rule A\n- rule B\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 0; i < 2; i++ {
		if _, err := InjectRuntimeConfig(dir, "runtime-c", TaskContextForEnv{
			IssueID: "11111111-2222-3333-4444-555555555555",
		}); err != nil {
			t.Fatalf("iter %d inject: %v", i, err)
		}
		if err := CleanupRuntimeConfig(dir, "runtime-c"); err != nil {
			t.Fatalf("iter %d cleanup: %v", i, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("iter %d read back: %v", i, err)
		}
		if string(got) != userContent {
			t.Errorf("iter %d: user file must be byte-identical to pre-injection state\n got:\n%q\nwant:\n%q", i, string(got), userContent)
		}
	}
}

func TestInjectThenCleanupRoundTripByteExactBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string

		seedExists  bool
		seedContent string
	}{
		{
			name:        "file missing — Inject creates, Cleanup removes",
			seedExists:  false,
			seedContent: "",
		},
		{
			name:        "pre-existing empty file (zero bytes)",
			seedExists:  true,
			seedContent: "",
		},
		{
			name:        "pre-existing whitespace-only file",
			seedExists:  true,
			seedContent: "   \n",
		},
		{
			name:        "no trailing newline",
			seedExists:  true,
			seedContent: "rules",
		},
		{
			name:        "one trailing newline (the common markdown shape)",
			seedExists:  true,
			seedContent: "# Rules\n\nbody\n",
		},
		{
			name:        "two trailing newlines",
			seedExists:  true,
			seedContent: "rules\n\n",
		},
		{
			name:        "many trailing newlines",
			seedExists:  true,
			seedContent: "rules\n\n\n\n",
		},
		{
			name:        "CRLF line endings",
			seedExists:  true,
			seedContent: "rule A\r\nrule B\r\n",
		},
		{
			name:        "no final newline AND embedded blank lines",
			seedExists:  true,
			seedContent: "para 1\n\npara 2\n\npara 3",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")

			if tc.seedExists {
				if err := os.WriteFile(path, []byte(tc.seedContent), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			for i := 0; i < 2; i++ {
				if _, err := InjectRuntimeConfig(dir, "runtime-c", TaskContextForEnv{
					IssueID: "11111111-2222-3333-4444-555555555555",
				}); err != nil {
					t.Fatalf("iter %d inject: %v", i, err)
				}
				if err := CleanupRuntimeConfig(dir, "runtime-c"); err != nil {
					t.Fatalf("iter %d cleanup: %v", i, err)
				}

				if !tc.seedExists {

					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Errorf("iter %d: file must remain missing, stat err=%v", i, err)
					}
					continue
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("iter %d read back: %v", i, err)
				}
				if string(got) != tc.seedContent {
					t.Errorf("iter %d: file must be byte-identical to seed\n got:  %q\n want: %q", i, string(got), tc.seedContent)
				}
			}
		})
	}
}

func TestInjectReplaceThenCleanupRestoresByteExact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		seedContent string
	}{
		{name: "no trailing newline", seedContent: "rules"},
		{name: "two trailing newlines", seedContent: "rules\n\n"},
		{name: "empty file", seedContent: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")
			if err := os.WriteFile(path, []byte(tc.seedContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			if _, err := InjectRuntimeConfig(dir, "runtime-c", TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			}); err != nil {
				t.Fatalf("first inject: %v", err)
			}

			if _, err := InjectRuntimeConfig(dir, "runtime-c", TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			}); err != nil {
				t.Fatalf("second inject: %v", err)
			}
			if err := CleanupRuntimeConfig(dir, "runtime-c"); err != nil {
				t.Fatalf("cleanup: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != tc.seedContent {
				t.Errorf("file must be byte-identical to seed after replace+cleanup\n got:  %q\n want: %q", string(got), tc.seedContent)
			}
		})
	}
}

func TestWriteRuntimeConfigFileAlwaysInsertsFixedManagedSeparator(t *testing.T) {
	t.Parallel()
	for _, seed := range []string{"", "rules", "rules\n", "rules\n\n", "rules\n\n\n\n"} {
		seed := seed
		t.Run(fmt.Sprintf("seed=%q", seed), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")
			if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)

			if !strings.HasPrefix(s, seed) {
				t.Errorf("seed bytes must survive verbatim at the start of the file\n got: %q\n seed: %q", s, seed)
			}

			markerStart := len(seed) + len(runtimeManagedSeparator)
			if len(s) < markerStart+len(runtimeMarkerBegin) {
				t.Fatalf("file shorter than expected layout\n got: %q", s)
			}
			if got, want := s[len(seed):markerStart], runtimeManagedSeparator; got != want {
				t.Errorf("expected managed separator %q immediately after seed, got %q", want, got)
			}
			if got, want := s[markerStart:markerStart+len(runtimeMarkerBegin)], runtimeMarkerBegin; got != want {
				t.Errorf("expected begin marker after managed separator, got %q", got)
			}
		})
	}
}

func TestMultiThreadReplyInstructionsFanOut(t *testing.T) {
	t.Parallel()
	out := BuildMultiThreadCommentReplyInstructions("55555555-6666-7777-8888-999999999999", []ThreadReplyTarget{
		{ThreadID: "c1", ParentID: "c1"},
		{ThreadID: "c2", ParentID: "c2"},
		{ThreadID: "c3", ParentID: "c3"},
	})

	for _, want := range []string{"3 DISTINCT threads", "Post ONE reply per thread", "--parent c1", "--parent c2", "--parent c3"} {
		if !strings.Contains(out, want) {
			t.Errorf("cross-thread instructions must contain %q, got:\n%s", want, out)
		}
	}
}

func TestSingleThreadReplyInstructionsKeepSingleParent(t *testing.T) {
	t.Parallel()
	out := BuildCommentReplyInstructions("runtime-c", "55555555-6666-7777-8888-999999999999", "c3")

	if strings.Contains(out, "DISTINCT threads") {
		t.Errorf("single/same-thread instructions must not emit the multi-thread fan-out block, got:\n%s", out)
	}
	if !strings.Contains(out, "--parent c3 --content-file ./reply.md") {
		t.Errorf("single/same-thread instructions must keep the single --parent=trigger cookbook, got:\n%s", out)
	}
}

func TestInjectRuntimeConfigByteIdenticalAcrossTriggers(t *testing.T) {
	t.Parallel()

	const issueID = "11111111-2222-3333-4444-555555555555"
	base := TaskContextForEnv{
		IssueID:   issueID,
		AgentID:   "agent-1",
		AgentName: "Eve",
	}

	variants := []struct {
		name   string
		mutate func(c *TaskContextForEnv)
	}{
		{"assignment-triggered", func(c *TaskContextForEnv) {}},
		{"comment-triggered", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-1"
			c.TriggerThreadID = "thread-1"
		}},
		{"comment-triggered-other-comment", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-2"
			c.TriggerThreadID = "thread-2"
		}},
		{"resumed-with-delta", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-3"
			c.PriorSessionResumed = true
			c.NewCommentCount = 7
			c.NewCommentsSince = "2026-05-28T11:00:00Z"
		}},
		{"resume-unavailable", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-4"
			c.PriorSessionResumeUnavailable = true
		}},
		{"cross-thread-fan-out", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-5"
			c.CommentReplyTargets = []ThreadReplyTarget{
				{ThreadID: "t1", ParentID: "t1"},
				{ThreadID: "t2", ParentID: "t2"},
			}
		}},
		{"member-initiator", func(c *TaskContextForEnv) {
			c.InitiatorType = "member"
			c.InitiatorID = "user-1"
			c.InitiatorName = "Bohan"
			c.InitiatorEmail = "bohan@example.com"
		}},
		{"agent-initiator", func(c *TaskContextForEnv) {
			c.InitiatorType = "agent"
			c.InitiatorID = "agent-9"
			c.InitiatorName = "GPT-Boy"
		}},
		{"connected-apps", func(c *TaskContextForEnv) {
			c.ConnectedApps = []runtimeapps.ConnectedApp{{
				Provider:    "composio",
				ServerName:  "composio",
				ToolkitSlug: "notion",
				ToolkitName: "Notion",
			}}
		}},
	}

	otherIssue := base
	otherIssue.IssueID = "99999999-8888-7777-6666-555555555555"
	if buildMetaSkillContent("runtime-c", base) == buildMetaSkillContent("runtime-c", otherIssue) {
		t.Fatal("brief does not vary with issue id — byte-identity assertions below would be vacuous")
	}

	for _, provider := range []string{"runtime-c", "runtime-e"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			var want string
			for i, v := range variants {
				ctx := base
				v.mutate(&ctx)
				got := buildMetaSkillContent(provider, ctx)
				if i == 0 {
					want = got
					continue
				}
				if got != want {
					t.Errorf("brief differs for variant %q — per-run state leaked into messages[0] (MUL-5377).\n%s",
						v.name, firstBriefDiff(want, got))
				}
			}
		})
	}
}

func firstBriefDiff(want, got string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	i := 0
	for i < n && want[i] == got[i] {
		i++
	}
	lo := i - 120
	if lo < 0 {
		lo = 0
	}
	hiW, hiG := i+120, i+120
	if hiW > len(want) {
		hiW = len(want)
	}
	if hiG > len(got) {
		hiG = len(got)
	}
	return "first difference at byte " + strconv.Itoa(i) +
		"\n--- baseline ---\n" + want[lo:hiW] +
		"\n--- variant ---\n" + got[lo:hiG]
}

func TestBriefByteIdenticalAcrossRunsForEveryKind(t *testing.T) {
	t.Parallel()

	kinds := map[string]TaskContextForEnv{
		"chat":         {ChatSessionID: "chat-1", ChatChannelType: ChannelTypeSlack, AgentID: "a-1", AgentName: "Eve"},
		"quick-create": {QuickCreatePrompt: "make an issue", AgentID: "a-1", AgentName: "Eve"},
		"autopilot":    {AutopilotRunID: "run-1", AutopilotID: "ap-1", AgentID: "a-1", AgentName: "Eve"},
	}

	variants := []struct {
		name   string
		mutate func(c *TaskContextForEnv)
	}{
		{"baseline", func(c *TaskContextForEnv) {}},
		{"resumed", func(c *TaskContextForEnv) { c.PriorSessionResumed = true }},
		{"resume-unavailable", func(c *TaskContextForEnv) { c.PriorSessionResumeUnavailable = true }},
		{"member-initiator", func(c *TaskContextForEnv) {
			c.InitiatorType, c.InitiatorID = "member", "u-1"
			c.InitiatorName, c.InitiatorEmail = "Bohan", "bohan@example.com"
		}},
		{"other-initiator", func(c *TaskContextForEnv) {

			c.InitiatorType, c.InitiatorID = "member", "u-2"
			c.InitiatorName, c.InitiatorEmail = "Someone Else", "else@example.com"
		}},
		{"agent-initiator", func(c *TaskContextForEnv) {
			c.InitiatorType, c.InitiatorID = "agent", "a-9"
			c.InitiatorName = "GPT-Boy"
		}},
		{"connected-apps", func(c *TaskContextForEnv) {
			c.ConnectedApps = []runtimeapps.ConnectedApp{{
				Provider: "composio", ServerName: "composio",
				ToolkitSlug: "notion", ToolkitName: "Notion",
			}}
		}},
	}

	for kindName, baseCtx := range kinds {
		kindName, baseCtx := kindName, baseCtx
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()
			var want string
			for i, v := range variants {
				ctx := baseCtx
				v.mutate(&ctx)
				got := buildMetaSkillContent("runtime-c", ctx)
				if i == 0 {
					want = got
					continue
				}
				if got != want {
					t.Errorf("%s brief differs for variant %q — per-run state leaked into the cached prefix (MUL-5377).\n%s",
						kindName, v.name, firstBriefDiff(want, got))
				}
			}
		})
	}
}

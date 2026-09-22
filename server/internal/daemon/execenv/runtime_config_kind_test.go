package execenv

import (
	"strings"
	"testing"
)

func TestClassifyTask(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
		want taskKind
	}{
		{"chat", TaskContextForEnv{ChatSessionID: "c"}, kindChat},
		{"quick-create", TaskContextForEnv{QuickCreatePrompt: "p"}, kindQuickCreate},
		{"autopilot", TaskContextForEnv{AutopilotRunID: "r"}, kindAutopilotRunOnly},
		{"issue-comment-triggered", TaskContextForEnv{IssueID: "i", TriggerCommentID: "c"}, kindIssue},
		{"issue-assignment-triggered", TaskContextForEnv{IssueID: "i"}, kindIssue},
		{"issue-bare", TaskContextForEnv{}, kindIssue},
		{"tiebreak-chat-vs-quick", TaskContextForEnv{ChatSessionID: "c", QuickCreatePrompt: "p"}, kindChat},
		{"tiebreak-quick-vs-autopilot", TaskContextForEnv{QuickCreatePrompt: "p", AutopilotRunID: "r"}, kindQuickCreate},
		{"tiebreak-autopilot-vs-comment", TaskContextForEnv{AutopilotRunID: "r", IssueID: "i", TriggerCommentID: "c"}, kindAutopilotRunOnly},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyTask(tc.ctx); got != tc.want {
				t.Errorf("classifyTask: got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTaskKindHasIssueContext(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind taskKind
		want bool
	}{
		{kindIssue, true},
		{kindAutopilotRunOnly, false},
		{kindQuickCreate, false},
		{kindChat, false},
	}
	for _, tc := range cases {
		if got := tc.kind.hasIssueContext(); got != tc.want {
			t.Errorf("kind=%d hasIssueContext: got %v, want %v", tc.kind, got, tc.want)
		}
	}
}

func TestBuildMetaSkillContentBriefContent(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
		AgentName:        "Eve",
		AgentID:          "eve-1",
	})

	if !strings.Contains(out, "- `goosar issue get <id> --output json` — full issue.\n") {
		t.Errorf("brief is missing the `issue get` one-liner\n---\n%s", out)
	}
	if strings.Contains(out, "Get full issue details.") {
		t.Errorf("brief still carries the retired legacy `issue get` description\n---\n%s", out)
	}
}

func TestBuildMetaSkillContentSlimKindMatrix(t *testing.T) {
	baseRepo := []RepoContextForEnv{{URL: "https://example.com/x.git", Description: "x"}}
	baseSkill := []SkillContextForEnv{{Name: "skill-x", Description: "x"}}

	type sectionCheck struct {
		heading  string
		mustHave map[taskKind]bool
	}
	allKinds := map[taskKind]bool{
		kindIssue: true, kindAutopilotRunOnly: true,
		kindQuickCreate: true, kindChat: true,
	}
	issueKinds := map[taskKind]bool{kindIssue: true}
	checks := []sectionCheck{
		{"# Goosar Agent Runtime", allKinds},
		{"## Background Task Safety", allKinds},
		{"## Agent Identity", allKinds},
		{"## Available Commands", allKinds},
		{"### Workflow", allKinds},
		{"## Important: Always Use the `goosar` CLI", allKinds},
		{"## Output", allKinds},
		{"## Comment Formatting", issueKinds},
		{"## Repositories", map[taskKind]bool{
			kindIssue: true, kindAutopilotRunOnly: true, kindChat: true,
		}},
		{"## Issue Metadata", issueKinds},
		{"## Instruction Precedence", issueKinds},
		{"## Sub-issue Creation", issueKinds},
		{"## Skills", map[taskKind]bool{
			kindIssue: true, kindAutopilotRunOnly: true, kindChat: true,
		}},
		{"## Mentions", issueKinds},
		{"## Attachments", issueKinds},
	}

	fixtures := map[taskKind]TaskContextForEnv{
		kindChat: {
			ChatSessionID: "c-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill,
		},
		kindQuickCreate: {
			QuickCreatePrompt: "p", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill,
		},
		kindAutopilotRunOnly: {
			AutopilotRunID: "r-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill,
		},
		kindIssue: {
			IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill,
		},
	}

	for kind, ctx := range fixtures {
		out := buildMetaSkillContent("runtime-c", ctx)
		for _, c := range checks {
			needle := "\n" + c.heading + "\n"
			firstLine := c.heading + "\n"
			present := strings.HasPrefix(out, firstLine) || strings.Contains(out, needle)
			want := c.mustHave[kind]
			if want && !present {
				t.Errorf("kind=%d: expected heading %q in slim brief", kind, c.heading)
			}
			if !want && present {
				t.Errorf("kind=%d: heading %q should NOT be in slim brief (matrix gating regression)", kind, c.heading)
			}
		}
	}
}

func TestSlimQuickCreateAvailableCommands(t *testing.T) {
	out := buildMetaSkillContent("runtime-e", TaskContextForEnv{
		QuickCreatePrompt: "create an issue about flaky tests",
		AgentName:         "Eve", AgentID: "eve-1",
	})

	for _, want := range []string{
		"## Available Commands",
		"goosar issue create --title",
		"`goosar --help`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("quick_create slim Available Commands missing %q", want)
		}
	}

	for _, banned := range []string{
		"goosar issue get <id>",
		"goosar issue comment list <issue-id>",
		"goosar issue update <id>",
		"goosar issue status <id> <status>",
		"goosar issue comment add <issue-id>",
		"goosar issue metadata list <issue-id>",
		"goosar issue metadata set <issue-id>",
		"goosar issue metadata delete <issue-id>",
		"goosar issue children <id>",
		"goosar repo checkout <url>",
		"### Squad maintenance",
		"goosar squad member set-role",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("quick_create slim Available Commands should NOT advertise %q (hard guardrails forbid the call)", banned)
		}
	}
}

func TestBackgroundTaskSafetySlimHardPins(t *testing.T) {
	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{
		IssueID: "i-1", TriggerCommentID: "tc-1",
		AgentName: "Eve", AgentID: "eve-1",
	})

	for _, want := range []string{
		"## Background Task Safety",
		"Do NOT end your turn while background tasks",
		"wait for a future notification/reminder",
		"run the work synchronously instead",
		"Never background-and-yield",
		"foreground tool call that blocks",

		"persistent service handoff",
		"running service itself is the requested deliverable",
		"stdio redirected to durable logs",
		"PID/profile",
		"verify readiness before replying",
		"survival as best-effort, not guaranteed",
		"does not cover tests, builds, CI polling",
		"are not agent-owned background tasks",
		"GitHub Actions after a successful push",
		"Do not wait for them by default",

		"do NOT run `gh pr checks --watch`",
		"any sleep / retry loop that polls check status",
		"NOT your delivery acceptance criteria",
		"CI running: <PR link>",
		"unless the explicit exception below applies",
		"The one exception",
		"ONE foreground blocking call (`gh pr checks <pr> --watch`)",
		"running in the background so you can keep working",
		"standing by",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("slim Background Task Safety missing hardened pin %q\n---\n%s", want, out)
		}
	}

	if strings.Contains(out, "e.g. `gh run watch`") {
		t.Errorf("slim Background Task Safety should not suggest waiting for external GitHub CI\n---\n%s", out)
	}

	if strings.Contains(out, "The rules above") {
		t.Errorf("slim Background Task Safety must not reintroduce the ambiguous \"The rules above\" scoping sentence\n---\n%s", out)
	}
}

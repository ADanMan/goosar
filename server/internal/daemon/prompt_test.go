package daemon

import (
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

func TestBuildQuickCreatePromptRules(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})

	mustContain := []string{

		"Faithfully restate what the user wants",
		"Preserve specific names, identifiers, file paths",

		"verbal routing wrappers about creating the issue",
		"pure conversational fillers",

		"CC exception",
		"auto-subscribes members",

		"include ONLY when the input cited external resources",
		"never use it as an apology log",

		"goosar issue create --output json",
		"JSON response",
		"identifier",
		"Do not scrape human output",
		"do not assume any workspace issue prefix",
		"Created <identifier-or-id>: <title>",

		"never invent requirements",
		"never reduce multi-sentence input",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt output missing required rule: %q", s)
		}
	}
}

func TestBuildQuickCreatePromptAssigneeIncludesSquads(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	mustContain := []string{
		"goosar squad list",
		"Squads are first-class assignees",
		"Treat bare @-routing as an assignee directive",
		"让 @独立团 review 这个 PR",
		"pass the squad's `id` as `--assignee-id`",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt assignee block missing %q\n--- output ---\n%s", s, out)
		}
	}
}

func TestBuildQuickCreatePromptSquadDefaultsToSquad(t *testing.T) {
	const (
		squadID   = "aaaa1111-2222-3333-4444-555555555555"
		squadName = "独立团"
		leaderID  = "bbbb1111-2222-3333-4444-666666666666"
	)
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		Agent:             &AgentData{ID: leaderID, Name: "leader-agent"},
		SquadID:           squadID,
		SquadName:         squadName,
	})

	if !strings.Contains(out, "--assignee-id \""+squadID+"\"") {
		t.Errorf("buildQuickCreatePrompt with SquadID must default to the squad's UUID, got:\n%s", out)
	}

	if strings.Contains(out, "--assignee-id \""+leaderID+"\"") {
		t.Errorf("buildQuickCreatePrompt with SquadID must NOT default to the leader agent's UUID, got:\n%s", out)
	}

	if !strings.Contains(out, squadName) {
		t.Errorf("buildQuickCreatePrompt with SquadID should mention the squad name %q, got:\n%s", squadName, out)
	}

	mustContain := []string{
		"picker SQUAD",
		"running on the squad's behalf",
		"do not assign it to your own agent UUID",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with SquadID missing %q\n--- output ---\n%s", s, out)
		}
	}
}

func TestBuildQuickCreatePromptProjectPinning(t *testing.T) {
	const projectID = "11111111-2222-3333-4444-555555555555"
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		ProjectID:         projectID,
		ProjectTitle:      "Web App",
	})
	mustContain := []string{
		"--project \"" + projectID + "\"",
		"Web App",
		"modal selection is authoritative",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with project missing %q\n--- output ---\n%s", s, out)
		}
	}

	plain := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	if !strings.Contains(plain, "**project**: omit") {
		t.Errorf("buildQuickCreatePrompt without project must keep the omit instruction, got:\n%s", plain)
	}
	if strings.Contains(plain, "--project") {
		t.Errorf("buildQuickCreatePrompt without project must NOT mention --project, got:\n%s", plain)
	}
}

func TestBuildQuickCreatePromptExplicitPriorityAndDueDate(t *testing.T) {
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt:   "fix the login button color",
		QuickCreatePriority: "urgent",
		QuickCreateDueDate:  "2026-08-01",
	})
	for _, want := range []string{
		"--priority urgent",
		"--due-date 2026-08-01",
		"quick-create selection is authoritative",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("buildQuickCreatePrompt with explicit fields missing %q\n--- output ---\n%s", want, out)
		}
	}
	if strings.Contains(out, "Map P0/P1") {
		t.Errorf("explicit priority must replace inference rules, got:\n%s", out)
	}
}

func TestBuildQuickCreatePromptParentPinning(t *testing.T) {
	const (
		parentID         = "33333333-2222-1111-4444-555555555555"
		parentIdentifier = "MUL-2534"
	)
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt:     "fix the login button color",
		ParentIssueID:         parentID,
		ParentIssueIdentifier: parentIdentifier,
	})
	mustContain := []string{
		"--parent \"" + parentID + "\"",
		parentIdentifier,
		"modal entry point is authoritative",
		"filed as a sub-issue",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with parent missing %q\n--- output ---\n%s", s, out)
		}
	}

	uuidOnly := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		ParentIssueID:     parentID,
	})
	if !strings.Contains(uuidOnly, "--parent \""+parentID+"\"") {
		t.Errorf("buildQuickCreatePrompt with parent UUID only must still pin --parent, got:\n%s", uuidOnly)
	}

	plain := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	if strings.Contains(plain, "--parent") {
		t.Errorf("buildQuickCreatePrompt without parent must NOT mention --parent, got:\n%s", plain)
	}
}

func TestBuildPromptSquadLeaderNoActionForMemberTrigger(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "LGTM",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		Agent: &AgentData{
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	out := BuildPrompt(task, "runtime-c")
	if !strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must inject squad leader no_action rule for member-triggered comments, got:\n%s", out)
	}
	if !strings.Contains(out, "DO NOT post any comment") {
		t.Errorf("buildCommentPrompt must contain DO NOT post prohibition for member-triggered squad leader, got:\n%s", out)
	}
}

func TestBuildPromptSquadLeaderNoActionForAgentTrigger(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "Deploy complete.",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "deploy-boy",
		Agent: &AgentData{
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	out := BuildPrompt(task, "runtime-c")
	if !strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must inject squad leader no_action rule for agent-triggered comments, got:\n%s", out)
	}
}

func TestBuildChatPromptAttachmentIDsCanBeBoundToCreatedIssues(t *testing.T) {
	task := Task{
		ChatSessionID: "sess-1",
		ChatMessage:   "please create an issue with this screenshot",
		ChatMessageAttachments: []ChatAttachmentMeta{
			{ID: "019ec09d-6222-722b-bdfa-427b105d80be", Filename: "shot.png", ContentType: "image/png"},
		},
	}
	out := BuildPrompt(task, "runtime-c")
	for _, want := range []string{
		"Attachments on this message:",
		"id=019ec09d-6222-722b-bdfa-427b105d80be",
		"goosar attachment download <id>",
		"--attachment-id <id>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("chat prompt missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestBuildChatPromptChannelAwareness(t *testing.T) {
	t.Run("slack-backed prompt teaches both read commands", func(t *testing.T) {
		out := buildChatPrompt(Task{
			ChatSessionID:   "sess-1",
			ChatChannelType: "slack",
			ChatMessage:     "你刚刚和 xxx 聊了什么",
		})
		for _, want := range []string{"Slack", "NOT in Goosar", "goosar chat history", "goosar chat thread", "Do NOT narrate"} {
			if !strings.Contains(out, want) {
				t.Fatalf("slack-backed prompt missing %q\n--- output ---\n%s", want, out)
			}
		}
	})

	t.Run("top-level mention starts with history", func(t *testing.T) {
		out := buildChatPrompt(Task{ChatSessionID: "s", ChatChannelType: "slack", ChatInThread: false, ChatMessage: "hi"})
		if !strings.Contains(out, "top level: start with `goosar chat history`") {
			t.Fatalf("expected top-level guidance, got:\n%s", out)
		}
	})

	t.Run("in-thread mention starts with thread", func(t *testing.T) {
		out := buildChatPrompt(Task{ChatSessionID: "s", ChatChannelType: "slack", ChatInThread: true, ChatMessage: "hi"})
		if !strings.Contains(out, "inside a thread: start with `goosar chat thread`") {
			t.Fatalf("expected in-thread guidance, got:\n%s", out)
		}
	})

	t.Run("web-only session has no channel block", func(t *testing.T) {
		out := buildChatPrompt(Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "hi",
		})
		if strings.Contains(out, "goosar chat history") {
			t.Fatalf("web-only chat prompt should not mention channel history, got:\n%s", out)
		}
	})
}

func TestBuildChatPromptNoNarrationOnEveryChannel(t *testing.T) {
	const (
		prohibition = "Do NOT narrate planned or in-progress steps"
		carveOut    = "completed actions are part of the outcome"
	)

	for _, tc := range []struct {
		name        string
		channelType string
		want        bool
	}{
		{name: "slack", channelType: execenv.ChannelTypeSlack, want: true},
		{name: "feishu", channelType: execenv.ChannelTypeFeishu, want: true},
		{name: "direct chat has no channel to deliver into", channelType: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := buildChatPrompt(Task{
				ChatSessionID:   "sess-1",
				ChatChannelType: tc.channelType,
				ChatMessage:     "hi",
			})
			for _, phrase := range []string{prohibition, carveOut} {
				if got := strings.Contains(out, phrase); got != tc.want {
					t.Errorf("%q present=%v, want %v\n--- output ---\n%s", phrase, got, tc.want, out)
				}
			}
			if !tc.want {
				return
			}

			if strings.Contains(out, "must not say what you are about to do or just did") {
				t.Errorf("prohibition must scope to process, not completed outcomes\n--- output ---\n%s", out)
			}
		})
	}
}

func TestBuildChatPromptTwoLayerChannelPolicy(t *testing.T) {

	const uploadGuidance = "run `goosar attachment upload <local-path>`"
	const historyGuidance = "goosar chat history"

	cases := []struct {
		name        string
		channelType string
		wantUpload  bool
		wantHistory bool
		wantPhrases []string
	}{
		{
			name:        "direct chat: upload, no history",
			channelType: "",
			wantUpload:  true,
			wantHistory: false,
		},
		{
			name:        "slack: no upload, has history",
			channelType: execenv.ChannelTypeSlack,
			wantUpload:  false,
			wantHistory: true,
			wantPhrases: []string{"Slack", "delivered to Slack as text", "You cannot attach a file to it"},
		},
		{
			name:        "feishu: no upload, no history",
			channelType: execenv.ChannelTypeFeishu,
			wantUpload:  false,
			wantHistory: false,
			wantPhrases: []string{
				"Feishu",
				"no history reader for Feishu",
				"delivered to Feishu as text",
				"You cannot attach a file to it",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := buildChatPrompt(Task{
				ChatSessionID:   "sess-1",
				ChatChannelType: tc.channelType,
				ChatMessage:     "hi",
			})
			if got := strings.Contains(out, uploadGuidance); got != tc.wantUpload {
				t.Errorf("upload guidance present=%v, want %v\n--- output ---\n%s", got, tc.wantUpload, out)
			}
			if got := strings.Contains(out, historyGuidance); got != tc.wantHistory {
				t.Errorf("history guidance present=%v, want %v\n--- output ---\n%s", got, tc.wantHistory, out)
			}
			for _, phrase := range tc.wantPhrases {
				if !strings.Contains(out, phrase) {
					t.Errorf("missing %q\n--- output ---\n%s", phrase, out)
				}
			}
		})
	}
}

func TestBuildChatPromptFeishuIgnoresChatInThread(t *testing.T) {
	out := buildChatPrompt(Task{
		ChatSessionID:   "sess-1",
		ChatChannelType: execenv.ChannelTypeFeishu,
		ChatInThread:    true,
		ChatMessage:     "hi",
	})
	for _, unwanted := range []string{"goosar chat thread", "goosar chat history"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("feishu prompt must not teach %q (no Feishu history reader exists)\n--- output ---\n%s", unwanted, out)
		}
	}
}

func TestBuildChatPromptAgentIntro(t *testing.T) {

	out := buildChatPrompt(Task{ChatSessionID: "sess-1", ChatIntro: true})
	for _, want := range []string{
		"You were just created",
		"you are opening the conversation",
		"introduce yourself",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("intro prompt missing %q\n--- output ---\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"Respond to their message", "User message:"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("intro prompt should not contain %q\n--- output ---\n%s", unwanted, out)
		}
	}
}

func TestBuildChatPromptSlashSkills(t *testing.T) {
	t.Run("injects selected skills block", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "please [/deploy](slash://skill/abc-123) this",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "abc-123", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if !strings.Contains(out, "Explicitly selected skills:\n- deploy\n") {
			t.Fatalf("expected selected skills block, got:\n%s", out)
		}
		if !strings.Contains(out, "User message:\nplease [/deploy](slash://skill/abc-123) this") {
			t.Fatalf("expected raw user message preserved, got:\n%s", out)
		}
	})

	t.Run("ignores skills not belonging to agent", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/hacker-skill](slash://skill/evil-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "good-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block for unknown skill ID, got:\n%s", out)
		}
	})

	t.Run("validates by ID not label", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/wrong-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "real-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("matching label with wrong ID must not pass, got:\n%s", out)
		}
	})

	t.Run("uses canonical name not label", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/spoofed-name](slash://skill/real-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "real-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if !strings.Contains(out, "- deploy\n") {
			t.Fatalf("expected canonical name 'deploy', got:\n%s", out)
		}
		if strings.Contains(out, "- spoofed-name\n") {
			t.Fatalf("selected skills block must not use spoofed label, got:\n%s", out)
		}
		if !strings.Contains(out, "User message:\n[/spoofed-name](slash://skill/real-id)") {
			t.Fatalf("expected raw user message with spoofed label preserved, got:\n%s", out)
		}
	})

	t.Run("deduplicates skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/a) and [/deploy](slash://skill/a) again",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "a", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Count(out, "- deploy") != 1 {
			t.Fatalf("expected exactly 1 '- deploy', got:\n%s", out)
		}
	})

	t.Run("omits block when no valid skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "just a normal message",
			Agent:         &AgentData{Skills: []SkillData{{ID: "a", Name: "deploy"}}},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block when no slash links, got:\n%s", out)
		}
	})

	t.Run("omits block when agent has no skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/abc-123)",
			Agent:         &AgentData{},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block for agent with no skills, got:\n%s", out)
		}
	})
}

func TestBuildPromptDefaultMentionsRecent(t *testing.T) {
	out := BuildPrompt(Task{IssueID: "issue-default-1"}, "runtime-c")
	for _, s := range []string{
		"goosar issue comment list issue-default-1 --recent 10 --output json",
		"Next thread cursor:",
		"--since",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("default BuildPrompt missing %q\n--- output ---\n%s", s, out)
		}
	}

	if strings.Contains(out, "--thread") {
		t.Errorf("default BuildPrompt should NOT mention --thread (no trigger comment to anchor on)\n--- output ---\n%s", out)
	}

	if strings.Contains(out, "If you need comment history") {
		t.Errorf("default BuildPrompt still carries the legacy 'If you need' soft phrasing that conflicts with the mandatory workflow\n--- output ---\n%s", out)
	}
	if strings.Contains(out, "goosar issue comment list issue-default-1 --output json") {
		t.Errorf("default BuildPrompt still presents the unbounded flat read as the assignment catch-up command\n--- output ---\n%s", out)
	}
}

func TestBuildPromptNonSquadLeaderNoRule(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "LGTM",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		Agent: &AgentData{
			Instructions: "Some instructions without the squad marker",
		},
	}
	out := BuildPrompt(task, "runtime-c")
	if strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must NOT inject squad leader no_action rule for non-squad-leader agents, got:\n%s", out)
	}
}

func TestBuildPromptNewCommentsHint(t *testing.T) {
	const (
		issueID = "issue-new-1"
		since   = "2026-05-28T11:00:00Z"
	)
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerThreadID:       "thread-root-1",
		TriggerCommentContent: "please look",
		TriggerAuthorType:     "member",
		NewCommentCount:       3,
		NewCommentsSince:      since,
	}
	out := BuildPrompt(task, "runtime-c")

	if !strings.Contains(out, "3 new comment(s) on this issue since your last run") {
		t.Errorf("hint must report the issue-wide new-comment count, got:\n%s", out)
	}

	if !strings.Contains(out, "blindly") {
		t.Errorf("hint must discourage blindly reading every new comment, got:\n%s", out)
	}

	if !strings.Contains(out, "goosar issue comment list "+issueID+" --thread thread-root-1 --since "+since+" --output json") {
		t.Errorf("hint must point at the triggering (parent) thread --since read first, got:\n%s", out)
	}
	if !strings.Contains(out, "--tail 30") {
		t.Errorf("hint must offer the full-thread (--tail 30) option, got:\n%s", out)
	}

	if !strings.Contains(out, "goosar issue comment list "+issueID+" --since "+since+" --output json") {
		t.Errorf("hint must keep the issue-wide --since catch-up as a fallback, got:\n%s", out)
	}

	if strings.Contains(out, "Next reply cursor") || strings.Contains(out, "--before-id") {
		t.Errorf("the old cursor-pagination paragraph must not render, got:\n%s", out)
	}
}

func TestBuildPromptColdStartThreadRead(t *testing.T) {
	const issueID = "issue-cold-1"
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerThreadID:       "thread-root-1",
		TriggerCommentContent: "hi",
		TriggerAuthorType:     "member",
		NewCommentCount:       0,
		NewCommentsSince:      "",
	}
	out := BuildPrompt(task, "runtime-c")
	if strings.Contains(out, "new comment(s) since your last run") {
		t.Errorf("no since-delta hint should render on cold start, got:\n%s", out)
	}
	if !strings.Contains(out, "goosar issue comment list "+issueID+" --thread thread-root-1 --tail 30 --output json") {
		t.Errorf("cold start must point at the triggering thread read, got:\n%s", out)
	}
	if !strings.Contains(out, "goosar issue comment list "+issueID+" --recent 10 --output json") {
		t.Errorf("cold start cross-thread fallback should use recent 10, got:\n%s", out)
	}
	if strings.Contains(out, "--recent 20") {
		t.Errorf("cold start cross-thread fallback still uses recent 20, got:\n%s", out)
	}
}

func TestBuildPromptResumedNoDeltaDoesNotForceThreadRead(t *testing.T) {
	const issueID = "issue-resumed-1"
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerThreadID:       "thread-root-1",
		TriggerCommentContent: "hi again",
		TriggerAuthorType:     "member",
		PriorSessionID:        "session-123",
		NewCommentCount:       0,
		NewCommentsSince:      "",
	}
	out := BuildPrompt(task, "runtime-c")

	for _, want := range []string{
		"triggering comment is already included above",
		"No other new comments on this issue since your last run",
		"active thread anchor `thread-root-1` and triggering comment ID `trigger-1`",
		"If your reply depends on thread context",
		"do not rely only on resumed session memory",
		"goosar issue comment list " + issueID + " --thread thread-root-1 --tail 30 --output json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("resumed/no-delta prompt missing %q\n--- output ---\n%s", want, out)
		}
	}

	if strings.Contains(out, "scoped to the triggering thread") {
		t.Errorf("resumed/no-delta prompt must not claim the delta is thread-scoped, got:\n%s", out)
	}
	if strings.Contains(out, "Read the triggering conversation first") {
		t.Errorf("resumed/no-delta prompt must not use the cold-start forced-read wording, got:\n%s", out)
	}
}

func TestBuildCommentPromptCoalescedCrossThread(t *testing.T) {
	task := Task{
		IssueID:               "issue-xthread-1",
		TriggerCommentID:      "trigger-newest",
		TriggerThreadID:       "thread-root-A",
		TriggerCommentContent: "latest instruction",
		TriggerAuthorType:     "member",
		CoalescedCommentIDs:   []string{"c-old-1", "c-old-2"},
		CoalescedComments: []CoalescedCommentData{
			{ID: "c-old-1", ThreadID: "thread-root-A", AuthorType: "member", AuthorName: "Alice", Content: "first earlier comment", CreatedAt: "2026-07-08T01:00:00Z"},
			{ID: "c-old-2", ThreadID: "thread-root-B", AuthorType: "member", AuthorName: "Bob", Content: "comment in a different thread", CreatedAt: "2026-07-08T02:00:00Z"},
		},
	}
	out := BuildPrompt(task, "runtime-c")

	if strings.Contains(out, "they are in the triggering thread") {
		t.Errorf("prompt must not assume coalesced comments share the triggering thread, got:\n%s", out)
	}

	for _, want := range []string{"first earlier comment", "comment in a different thread"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt must embed coalesced comment content %q, got:\n%s", want, out)
		}
	}

	for _, want := range []string{"thread-root-A", "thread-root-B"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt must surface coalesced comment thread id %q, got:\n%s", want, out)
		}
	}

	for _, id := range []string{"c-old-1", "c-old-2"} {
		if !strings.Contains(out, id) {
			t.Errorf("prompt must reference coalesced comment id %s, got:\n%s", id, out)
		}
	}
}

func TestBuildCommentPromptCoalescedIDsOnlyFallback(t *testing.T) {
	task := Task{
		IssueID:               "issue-fallback-1",
		TriggerCommentID:      "trigger-newest",
		TriggerThreadID:       "thread-root-A",
		TriggerCommentContent: "latest instruction",
		TriggerAuthorType:     "member",
		CoalescedCommentIDs:   []string{"c-old-1", "c-old-2"},
	}
	out := BuildPrompt(task, "runtime-c")

	if strings.Contains(out, "they are in the triggering thread") {
		t.Errorf("id-only fallback must not assume a shared thread, got:\n%s", out)
	}
	if !strings.Contains(out, "--recent 30") {
		t.Errorf("id-only fallback must point at an issue-wide fetch (--recent 30), got:\n%s", out)
	}
	for _, id := range []string{"c-old-1", "c-old-2"} {
		if !strings.Contains(out, id) {
			t.Errorf("id-only fallback must reference coalesced comment id %s, got:\n%s", id, out)
		}
	}
}

func TestCommentReplyThreadsGrouping(t *testing.T) {
	t.Run("three distinct root threads fan out", func(t *testing.T) {
		task := Task{
			TriggerCommentID: "c3",
			TriggerThreadID:  "c3",
			CoalescedComments: []CoalescedCommentData{
				{ID: "c1", ThreadID: "c1", Content: "背一首宋词"},
				{ID: "c2", ThreadID: "c2", Content: "毛泽东诗词背一首"},
			},
		}
		targets := commentReplyThreads(task)
		if len(targets) != 3 {
			t.Fatalf("want 3 targets, got %d: %+v", len(targets), targets)
		}
		wantParent := map[string]string{"c1": "c1", "c2": "c2", "c3": "c3"}
		for _, tgt := range targets {
			if wantParent[tgt.ThreadID] != tgt.ParentID {
				t.Errorf("thread %s: parent = %s, want %s", tgt.ThreadID, tgt.ParentID, wantParent[tgt.ThreadID])
			}
		}
	})

	t.Run("same-thread follow-ups consolidate to a single group", func(t *testing.T) {
		task := Task{
			TriggerCommentID: "c3",
			TriggerThreadID:  "thread-A",
			CoalescedComments: []CoalescedCommentData{
				{ID: "c1", ThreadID: "thread-A", Content: "追问 1"},
				{ID: "c2", ThreadID: "thread-A", Content: "追问 2"},
			},
		}
		if targets := commentReplyThreads(task); targets != nil {
			t.Fatalf("same-thread follow-ups must not fan out; got %d targets: %+v", len(targets), targets)
		}
	})

	t.Run("mixed: trigger thread plus one other thread", func(t *testing.T) {
		task := Task{
			TriggerCommentID: "c3",
			TriggerThreadID:  "thread-A",
			CoalescedComments: []CoalescedCommentData{
				{ID: "c1", ThreadID: "thread-A", Content: "same-thread follow-up"},
				{ID: "c2", ThreadID: "thread-B", Content: "other thread"},
			},
		}
		targets := commentReplyThreads(task)
		if len(targets) != 2 {
			t.Fatalf("want 2 targets (thread-A, thread-B), got %d: %+v", len(targets), targets)
		}
		got := map[string]string{}
		for _, tgt := range targets {
			got[tgt.ThreadID] = tgt.ParentID
		}

		if got["thread-A"] != "c3" {
			t.Errorf("trigger thread parent = %q, want c3 (the trigger comment)", got["thread-A"])
		}

		if got["thread-B"] != "c2" {
			t.Errorf("other thread parent = %q, want c2 (the specific mentioning comment)", got["thread-B"])
		}
	})

	t.Run("no coalesced comments → nil", func(t *testing.T) {
		task := Task{TriggerCommentID: "c1", TriggerThreadID: "thread-A"}
		if targets := commentReplyThreads(task); targets != nil {
			t.Fatalf("ordinary single-comment run must not fan out; got %+v", targets)
		}
	})

	t.Run("non-trigger thread replies under its newest mention, not root", func(t *testing.T) {

		task := Task{
			TriggerCommentID: "c9",
			TriggerThreadID:  "thread-A",
			CoalescedComments: []CoalescedCommentData{
				{ID: "c1", ThreadID: "thread-B", Content: "older mention", CreatedAt: "2026-07-10T01:00:00Z"},
				{ID: "c2", ThreadID: "thread-B", Content: "newer mention", CreatedAt: "2026-07-10T02:00:00Z"},
			},
		}
		targets := commentReplyThreads(task)
		got := map[string]string{}
		for _, tgt := range targets {
			got[tgt.ThreadID] = tgt.ParentID
		}
		if got["thread-B"] != "c2" {
			t.Errorf("thread-B parent = %q, want newest mention c2 (not root)", got["thread-B"])
		}
		if got["thread-A"] != "c9" {
			t.Errorf("trigger thread parent = %q, want trigger c9", got["thread-A"])
		}
	})
}

func TestBuildCommentPromptCrossThreadFansOutReplies(t *testing.T) {
	task := Task{
		IssueID:               "issue-xthread-2",
		TriggerCommentID:      "c3",
		TriggerThreadID:       "c3",
		TriggerCommentContent: "莎士比亚名言来一句",
		TriggerAuthorType:     "member",
		CoalescedCommentIDs:   []string{"c1", "c2"},
		CoalescedComments: []CoalescedCommentData{
			{ID: "c1", ThreadID: "c1", AuthorType: "member", AuthorName: "Yushen", Content: "背一首宋词", CreatedAt: "2026-07-10T01:00:00Z"},
			{ID: "c2", ThreadID: "c2", AuthorType: "member", AuthorName: "Yushen", Content: "毛泽东诗词背一首", CreatedAt: "2026-07-10T02:00:00Z"},
		},
	}
	out := BuildPrompt(task, "runtime-c")

	for _, want := range []string{
		"3 DISTINCT threads",
		"Post ONE reply per thread",
		"OVERRIDES",
		"--parent c1",
		"--parent c2",
		"--parent c3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cross-thread prompt must contain %q, got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "always use the trigger comment ID below") {
		t.Errorf("cross-thread prompt must not emit the single-parent reply cookbook, got:\n%s", out)
	}

	if !strings.Contains(out, "OLDEST thread first") {
		t.Errorf("cross-thread prompt must instruct oldest-first chronological order, got:\n%s", out)
	}
	posC1 := strings.Index(out, "--parent c1")
	posC2 := strings.Index(out, "--parent c2")
	posC3 := strings.Index(out, "--parent c3")
	if !(posC1 >= 0 && posC1 < posC2 && posC2 < posC3) {
		t.Errorf("reply targets must be listed oldest-first (c1 < c2 < c3); got positions c1=%d c2=%d c3=%d\n%s", posC1, posC2, posC3, out)
	}
}

func TestBuildCommentPromptSameThreadKeepsSingleReply(t *testing.T) {
	task := Task{
		IssueID:               "issue-samethread-1",
		TriggerCommentID:      "c3",
		TriggerThreadID:       "thread-A",
		TriggerCommentContent: "追问 3",
		TriggerAuthorType:     "member",
		CoalescedCommentIDs:   []string{"c1", "c2"},
		CoalescedComments: []CoalescedCommentData{
			{ID: "c1", ThreadID: "thread-A", AuthorType: "member", AuthorName: "Yushen", Content: "追问 1", CreatedAt: "2026-07-10T01:00:00Z"},
			{ID: "c2", ThreadID: "thread-A", AuthorType: "member", AuthorName: "Yushen", Content: "追问 2", CreatedAt: "2026-07-10T02:00:00Z"},
		},
	}
	out := BuildPrompt(task, "runtime-c")

	if strings.Contains(out, "DISTINCT threads") {
		t.Errorf("same-thread coalescing must not emit the multi-thread fan-out block, got:\n%s", out)
	}

	if !strings.Contains(out, "--parent c3 --content-file ./reply.md") {
		t.Errorf("same-thread run must keep the single --parent=trigger reply cookbook, got:\n%s", out)
	}
}

func TestPerTurnContextBlocksCarryMovedBriefSections(t *testing.T) {
	t.Parallel()

	task := Task{
		IssueID:                       "issue-1",
		TriggerCommentID:              "comment-1",
		TriggerCommentContent:         "please look at this",
		PriorSessionResumeUnavailable: true,
		InitiatorType:                 "member",
		InitiatorName:                 "Bohan",
		InitiatorEmail:                "bohan@example.com",
		ConnectedApps: []ConnectedAppData{{
			Provider:    "composio",
			ServerName:  "composio",
			ToolkitSlug: "notion",
			ToolkitName: "Notion",
		}},
	}

	prompt := BuildPrompt(task, "runtime-c")
	for _, want := range []string{
		"## Session Continuity Notice",
		"could NOT be restored",
		"## Task Initiator",
		"initiated by **Bohan** (bohan@example.com), a member of this workspace",
		"credentials stay scoped to the runtime owner",
		"## Connected Apps",
		"- Notion (`notion`) via MCP server `composio`",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("per-turn prompt lost moved brief content %q\n---\n%s", want, prompt)
		}
	}
}

func TestPerTurnContextBlocksOmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{IssueID: "issue-1"}, "runtime-c")
	for _, banned := range []string{
		"## Session Continuity Notice",
		"## Task Initiator",
		"## Connected Apps",
	} {
		if strings.Contains(prompt, banned) {
			t.Errorf("per-turn prompt must not emit %q with no data\n---\n%s", banned, prompt)
		}
	}
}

func TestPerTurnContextBlocksOnAssignmentPath(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:       "issue-1",
		InitiatorType: "agent",
		InitiatorName: "GPT-Boy",
	}, "runtime-c")
	if !strings.Contains(prompt, "initiated by **GPT-Boy**, another agent in this workspace") {
		t.Errorf("assignment-triggered prompt lost the initiator block\n---\n%s", prompt)
	}
}

func TestTurnModeMarkerAlwaysPresent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		task Task
		want string
		deny string
	}{
		{
			name: "comment-triggered with content",
			task: Task{IssueID: "issue-1", TriggerCommentID: "c-1", TriggerCommentContent: "please look"},
			want: "**Turn mode: Reply.**",
			deny: "**Turn mode: Ownership.**",
		},
		{
			name: "comment-triggered with EMPTY content",
			task: Task{IssueID: "issue-1", TriggerCommentID: "c-1"},
			want: "**Turn mode: Reply.**",
			deny: "**Turn mode: Ownership.**",
		},
		{
			name: "assignment-triggered",
			task: Task{IssueID: "issue-1"},
			want: "**Turn mode: Ownership.**",
			deny: "**Turn mode: Reply.**",
		},
		{
			name: "assignment-triggered with handoff note",
			task: Task{IssueID: "issue-1", HandoffNote: "start with the API"},
			want: "**Turn mode: Ownership.**",
			deny: "**Turn mode: Reply.**",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prompt := BuildPrompt(tc.task, "runtime-c")
			if !strings.Contains(prompt, tc.want) {
				t.Errorf("prompt missing turn-mode marker %q\n---\n%s", tc.want, prompt)
			}
			if strings.Contains(prompt, tc.deny) {
				t.Errorf("prompt carries the wrong turn-mode marker %q\n---\n%s", tc.deny, prompt)
			}
		})
	}
}

func TestTurnModeMarkerAbsentOnIssuelessKinds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		task Task
	}{
		{"chat", Task{ChatSessionID: "chat-1"}},
		{"quick-create", Task{QuickCreatePrompt: "make an issue"}},
		{"autopilot", Task{AutopilotRunID: "run-1"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prompt := BuildPrompt(tc.task, "runtime-c")
			for _, banned := range []string{"**Turn mode: Reply.**", "**Turn mode: Ownership.**"} {
				if strings.Contains(prompt, banned) {
					t.Errorf("%s prompt must not carry %q\n---\n%s", tc.name, banned, prompt)
				}
			}
		})
	}
}

func TestBriefModeRouterMatchesPromptMarkers(t *testing.T) {
	t.Parallel()

	brief, err := execenv.InjectRuntimeConfig(t.TempDir(), "runtime-c", execenv.TaskContextForEnv{IssueID: "issue-1"})
	if err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	for _, want := range []string{"`Turn mode: Reply.`", "`Turn mode: Ownership.`"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief mode router does not name %s\n---\n%s", want, brief)
		}
	}

	if strings.Contains(brief, "It opens with a `[NEW COMMENT]` block") {
		t.Error("brief still routes on the prompt's opening line; it must route on the explicit marker")
	}
}

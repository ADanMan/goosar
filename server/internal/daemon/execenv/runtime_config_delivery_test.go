package execenv

import (
	"strings"
	"testing"
)

func deliveryInvariantFixtures() map[string]TaskContextForEnv {
	return map[string]TaskContextForEnv{
		"comment":     {IssueID: "i-1", TriggerCommentID: "tc-1", AgentName: "Eve", AgentID: "eve-1"},
		"assignment":  {IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1"},
		"autopilot":   {AutopilotRunID: "r-1", AgentName: "Eve", AgentID: "eve-1"},
		"quickcreate": {QuickCreatePrompt: "p", AgentName: "Eve", AgentID: "eve-1"},
		"chat_direct": {ChatSessionID: "c-1", AgentName: "Eve", AgentID: "eve-1"},
		"chat_slack":  {ChatSessionID: "c-1", ChatChannelType: ChannelTypeSlack, AgentName: "Eve", AgentID: "eve-1"},
		"chat_feishu": {ChatSessionID: "c-1", ChatChannelType: ChannelTypeFeishu, AgentName: "Eve", AgentID: "eve-1"},
	}
}

func TestBriefDeliveryInvariantIsAlwaysOn(t *testing.T) {
	t.Parallel()

	wantAll := []string{
		"Runtime-local paths are never deliverables",
		"NEVER write an absolute path or a `file://` URL as a clickable link",
		"`path/to/file.ts:42`",
	}

	for name, ctx := range deliveryInvariantFixtures() {
		out := buildMetaSkillContent("runtime-c", ctx)
		for _, want := range wantAll {
			if !strings.Contains(out, want) {
				t.Errorf("kind=%s: brief is missing always-on delivery invariant %q", name, want)
			}
		}
	}
}

func TestBriefSurfaceDeliveryPolicy(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mustHave []string
		mustNot  []string
	}{

		"comment": {
			mustHave: []string{"`--attachment <path>` to `goosar issue comment add`"},
			mustNot:  []string{"goosar attachment upload"},
		},
		"assignment": {
			mustHave: []string{"`--attachment <path>` to `goosar issue comment add`"},
			mustNot:  []string{"goosar attachment upload"},
		},

		"chat_direct": {
			mustHave: []string{"`goosar attachment upload <local-path>`"},
			mustNot:  []string{"text-only"},
		},

		"chat_slack": {
			mustHave: []string{"Slack conversation is text-only", "does NOT apply"},
			mustNot:  []string{"run `goosar attachment upload"},
		},
		"chat_feishu": {
			mustHave: []string{"Feishu conversation is text-only", "does NOT apply"},
			mustNot:  []string{"run `goosar attachment upload"},
		},
		"autopilot": {
			mustHave: []string{"this surface is text-only"},
			mustNot:  []string{"goosar attachment upload"},
		},
		"quickcreate": {
			mustHave: []string{"your stdout is text-only", "`goosar issue create` call itself via `--attachment <path>`"},
			mustNot:  []string{"goosar attachment upload"},
		},
	}

	fixtures := deliveryInvariantFixtures()
	for name, want := range cases {
		ctx, ok := fixtures[name]
		if !ok {
			t.Fatalf("no fixture for surface %q", name)
		}
		out := buildMetaSkillContent("runtime-c", ctx)
		for _, phrase := range want.mustHave {
			if !strings.Contains(out, phrase) {
				t.Errorf("surface=%s: brief missing surface policy %q\n--- Output section ---\n%s",
					name, phrase, outputSection(out))
			}
		}
		for _, phrase := range want.mustNot {
			if strings.Contains(out, phrase) {
				t.Errorf("surface=%s: brief must NOT carry %q (wrong surface's delivery mechanism)\n--- Output section ---\n%s",
					name, phrase, outputSection(out))
			}
		}
	}
}

func TestBriefChatRequiresTextReply(t *testing.T) {
	t.Parallel()

	wantAll := []string{
		"Your final text output IS the chat reply",
		"never a substitute",
		"reference it (e.g. `MUL-123`)",
	}

	fixtures := deliveryInvariantFixtures()
	for _, name := range []string{"chat_direct", "chat_slack", "chat_feishu"} {
		out := buildMetaSkillContent("runtime-c", fixtures[name])
		for _, want := range wantAll {
			if !strings.Contains(out, want) {
				t.Errorf("surface=%s: brief missing chat delivery contract %q\n--- Output section ---\n%s",
					name, want, outputSection(out))
			}
		}
	}

	for _, name := range []string{"comment", "assignment"} {
		out := buildMetaSkillContent("runtime-c", fixtures[name])
		if strings.Contains(out, "Your final text output IS the chat reply") {
			t.Errorf("surface=%s: issue-kind brief must not carry the chat delivery contract", name)
		}
	}
}

func TestBriefInboundAttachmentIsNotADeliverable(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("runtime-c", TaskContextForEnv{
		IssueID: "i-1", TriggerCommentID: "tc-1", AgentName: "Eve", AgentID: "eve-1",
	})
	for _, want := range []string{
		"private working copy",
		"Never echo it back into a deliverable as a link",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Attachments section missing %q\n---\n%s", want, out)
		}
	}
}

func TestChannelDisplayName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		ChannelTypeSlack:  "Slack",
		ChannelTypeFeishu: "Feishu",
		"":                "",

		"discord": "discord",
	}
	for in, want := range cases {
		if got := ChannelDisplayName(in); got != want {
			t.Errorf("ChannelDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func outputSection(brief string) string {
	idx := strings.Index(brief, "\n## Output\n")
	if idx < 0 {
		return "<no ## Output section>"
	}
	return brief[idx:]
}
